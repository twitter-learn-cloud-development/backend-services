"""Seed, verify, or clean up isolated MongoDB/Qdrant restart probe data."""

import argparse
import asyncio
import json
import re
from datetime import UTC, datetime
from typing import Any
from uuid import NAMESPACE_URL, UUID, uuid5

from pymongo import MongoClient
from pymongo.database import Database
from qdrant_client import AsyncQdrantClient, models

from app.core.config import Settings
from app.domain.knowledge import KnowledgeDocument
from app.integrations.knowledge import build_qdrant_knowledge_store
from app.services.conversation_store import build_conversation_resources

_RUN_ID = re.compile(r"^[a-z0-9][a-z0-9-]{7,39}$")
_PROBE_MESSAGE = "Persistence restart probe conversation seed."
_PROBE_REPLY = "Persistence restart probe conversation reply."
_PROBE_DOCUMENT_BODY = (
    "A persistence restart probe verifies that a uniquely identified knowledge record "
    "remains available after a storage service restart."
)


def _identifiers(run_id: str) -> tuple[str, UUID, str, str]:
    normalized = run_id.casefold()
    if not _RUN_ID.fullmatch(normalized):
        raise ValueError("run id must be 8-40 lowercase letters, digits, or hyphens")
    database_name = f"support_agent_restart_{normalized.replace('-', '_')}"
    conversation_id = uuid5(NAMESPACE_URL, f"support-agent-restart-probe:{normalized}")
    actor = f"restart-probe-{normalized}"
    document_id = f"restart-probe-{normalized}"
    return database_name, conversation_id, actor, document_id


def _mongo_counts(db: Database[dict[str, Any]], conversation_id: UUID) -> dict[str, int]:
    thread_id = str(conversation_id)
    return {
        "owner": db["conversations"].count_documents({"_id": thread_id}),
        "checkpoints": db["conversation_checkpoints"].count_documents({"thread_id": thread_id}),
        "writes": db["conversation_checkpoint_writes"].count_documents({"thread_id": thread_id}),
    }


async def _qdrant_count(client: AsyncQdrantClient, settings: Settings, document_id: str) -> int:
    if not await client.collection_exists(settings.qdrant_collection):
        return 0
    result = await client.count(
        collection_name=settings.qdrant_collection,
        count_filter=models.Filter(
            must=[
                models.FieldCondition(
                    key="document_id",
                    match=models.MatchValue(value=document_id),
                )
            ]
        ),
        exact=True,
    )
    return result.count


def _new_qdrant_client(settings: Settings) -> AsyncQdrantClient:
    return AsyncQdrantClient(
        url=settings.qdrant_url,
        api_key=(settings.qdrant_api_key.get_secret_value() if settings.qdrant_api_key else None),
    )


def _document(document_id: str, *, published: bool = True) -> KnowledgeDocument:
    return KnowledgeDocument(
        document_id=document_id,
        version="restart-probe-v1",
        title="Persistence restart probe",
        body=_PROBE_DOCUMENT_BODY,
        updated_at=datetime.now(UTC),
        published=published,
    )


async def _seed(settings: Settings, run_id: str) -> None:
    database_name, conversation_id, actor, document_id = _identifiers(run_id)
    if not settings.mongodb_uri or not settings.qdrant_url:
        raise RuntimeError("MongoDB and Qdrant must both be configured")

    mongo: MongoClient[dict[str, Any]] = MongoClient(settings.mongodb_uri, tz_aware=True)
    try:
        if database_name in await asyncio.to_thread(mongo.list_database_names):
            raise RuntimeError("isolated MongoDB probe database already exists")
    finally:
        mongo.close()

    client = _new_qdrant_client(settings)
    try:
        existing_points = await _qdrant_count(client, settings, document_id)
    finally:
        await client.close()
    if existing_points:
        raise RuntimeError("isolated Qdrant probe document already exists")

    resources = build_conversation_resources(
        settings.model_copy(update={"mongodb_database": database_name})
    )
    try:
        await resources.store.claim(conversation_id, actor)
        await resources.history.append_turn(
            conversation_id,
            case_id=uuid5(NAMESPACE_URL, f"support-agent-restart-probe-case:{run_id}"),
            user_message=_PROBE_MESSAGE,
            assistant_message=_PROBE_REPLY,
        )
    finally:
        await resources.aclose()

    store = build_qdrant_knowledge_store(settings)
    try:
        inserted = await store.upsert(_document(document_id))
        if inserted < 1:
            raise RuntimeError("Qdrant did not insert probe document chunks")
    finally:
        await store.aclose()

    mongo: MongoClient[dict[str, Any]] = MongoClient(settings.mongodb_uri, tz_aware=True)
    try:
        counts = await asyncio.to_thread(_mongo_counts, mongo[database_name], conversation_id)
    finally:
        mongo.close()
    if counts["owner"] != 1 or counts["checkpoints"] < 1 or counts["writes"] < 1:
        raise RuntimeError("MongoDB probe conversation seed was incomplete")

    client = _new_qdrant_client(settings)
    try:
        qdrant_count = await _qdrant_count(client, settings, document_id)
    finally:
        await client.close()
    if qdrant_count < 1:
        raise RuntimeError("Qdrant probe document seed was not persisted")

    print(
        json.dumps(
            {
                "phase": "seed",
                "status": "passed",
                "run_id": run_id,
                "mongo_database": database_name,
                "mongo_conversation_id": str(conversation_id),
                "mongo_counts": counts,
                "qdrant_collection": settings.qdrant_collection,
                "qdrant_document_id": document_id,
                "qdrant_points": qdrant_count,
            },
            separators=(",", ":"),
        ),
        flush=True,
    )


async def _verify(settings: Settings, run_id: str) -> None:
    database_name, conversation_id, actor, document_id = _identifiers(run_id)
    if not settings.mongodb_uri or not settings.qdrant_url:
        raise RuntimeError("MongoDB and Qdrant must both be configured")

    resources = build_conversation_resources(
        settings.model_copy(update={"mongodb_database": database_name})
    )
    try:
        await resources.store.require_owner(conversation_id, actor)
        context = await resources.history.context(conversation_id)
    finally:
        await resources.aclose()
    messages = [message.content for message in context.recent_messages]
    if messages != [_PROBE_MESSAGE, _PROBE_REPLY]:
        raise RuntimeError("MongoDB probe conversation did not survive or restore correctly")

    mongo: MongoClient[dict[str, Any]] = MongoClient(settings.mongodb_uri, tz_aware=True)
    try:
        counts = await asyncio.to_thread(_mongo_counts, mongo[database_name], conversation_id)
    finally:
        mongo.close()

    client = _new_qdrant_client(settings)
    try:
        qdrant_count = await _qdrant_count(client, settings, document_id)
    finally:
        await client.close()
    if qdrant_count < 1:
        raise RuntimeError("Qdrant probe document did not survive the restart")

    store = build_qdrant_knowledge_store(settings)
    try:
        matches = await store.search(
            "persistence restart probe uniquely identified knowledge record",
            limit=20,
        )
    finally:
        await store.aclose()
    retrieved = any(match.document_id == document_id for match in matches)
    if not retrieved:
        raise RuntimeError("Qdrant vector search did not retrieve the probe document")

    print(
        json.dumps(
            {
                "phase": "verify",
                "status": "passed",
                "run_id": run_id,
                "mongo_database": database_name,
                "mongo_conversation_id": str(conversation_id),
                "mongo_messages_restored": len(messages),
                "mongo_counts": counts,
                "qdrant_collection": settings.qdrant_collection,
                "qdrant_document_id": document_id,
                "qdrant_points": qdrant_count,
                "qdrant_search_retrieved_document": retrieved,
                "new_connections": True,
            },
            separators=(",", ":"),
        ),
        flush=True,
    )


async def _cleanup(settings: Settings, run_id: str) -> None:
    database_name, conversation_id, actor, document_id = _identifiers(run_id)
    if not settings.mongodb_uri or not settings.qdrant_url:
        raise RuntimeError("MongoDB and Qdrant must both be configured")

    resources = build_conversation_resources(
        settings.model_copy(update={"mongodb_database": database_name})
    )
    try:
        try:
            await resources.store.delete(conversation_id, actor)
        except Exception as exc:
            # A rerun after partial cleanup is allowed only if no records remain.
            mongo_check: MongoClient[dict[str, Any]] = MongoClient(
                settings.mongodb_uri, tz_aware=True
            )
            try:
                remaining = await asyncio.to_thread(
                    _mongo_counts, mongo_check[database_name], conversation_id
                )
            finally:
                mongo_check.close()
            if any(remaining.values()):
                raise RuntimeError("MongoDB probe ownership record could not be cleaned") from exc
        await resources.history.delete(conversation_id)
    finally:
        await resources.aclose()

    mongo: MongoClient[dict[str, Any]] = MongoClient(settings.mongodb_uri, tz_aware=True)
    try:
        counts = await asyncio.to_thread(_mongo_counts, mongo[database_name], conversation_id)
        thread_id = str(conversation_id)
        await asyncio.to_thread(
            mongo[database_name]["conversation_checkpoint_writes"].delete_many,
            {"thread_id": thread_id},
        )
        await asyncio.to_thread(
            mongo[database_name]["conversation_checkpoints"].delete_many,
            {"thread_id": thread_id},
        )
        counts = await asyncio.to_thread(_mongo_counts, mongo[database_name], conversation_id)
    finally:
        mongo.close()
    if any(counts.values()):
        raise RuntimeError("MongoDB probe records remain after exact-resource cleanup")

    store = build_qdrant_knowledge_store(settings)
    try:
        await store.upsert(_document(document_id, published=False))
    finally:
        await store.aclose()
    client = _new_qdrant_client(settings)
    try:
        qdrant_count = await _qdrant_count(client, settings, document_id)
    finally:
        await client.close()
    if qdrant_count != 0:
        raise RuntimeError("Qdrant probe points remain after exact-document cleanup")

    print(
        json.dumps(
            {
                "phase": "cleanup",
                "status": "passed",
                "run_id": run_id,
                "mongo_database": database_name,
                "mongo_conversation_id": str(conversation_id),
                "mongo_counts": counts,
                "qdrant_collection": settings.qdrant_collection,
                "qdrant_document_id": document_id,
                "qdrant_points": qdrant_count,
                "database_dropped": False,
            },
            separators=(",", ":"),
        )
    )


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=("seed", "verify", "cleanup"))
    parser.add_argument("--run-id", required=True)
    args = parser.parse_args()
    run_id = args.run_id.casefold()
    settings = Settings()
    if args.phase == "seed":
        await _seed(settings, run_id)
    elif args.phase == "verify":
        await _verify(settings, run_id)
    else:
        await _cleanup(settings, run_id)


if __name__ == "__main__":
    asyncio.run(main())

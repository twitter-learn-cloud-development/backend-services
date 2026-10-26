"""Exercise isolated MongoDB conversation persistence, deletion, and TTL."""

import asyncio
import json
import time
from collections.abc import Awaitable
from typing import Any
from uuid import UUID, uuid4

from pymongo import MongoClient
from pymongo.database import Database

from app.core.config import Settings
from app.core.errors import AppError
from app.services.conversation_store import build_conversation_resources


def _counts(db: Database[dict[str, Any]], conversation_id: UUID) -> dict[str, int]:
    thread_id = str(conversation_id)
    return {
        "owner": db["conversations"].count_documents({"_id": thread_id}),
        "checkpoints": db["conversation_checkpoints"].count_documents({"thread_id": thread_id}),
        "writes": db["conversation_checkpoint_writes"].count_documents({"thread_id": thread_id}),
    }


def _has_ttl_index(
    db: Database[dict[str, Any]], collection_name: str, field: str, seconds: int
) -> bool:
    return any(
        list(index["key"].items()) == [(field, 1)] and index.get("expireAfterSeconds") == seconds
        for index in db[collection_name].list_indexes()
    )


async def _expect_forbidden(action: Awaitable[None]) -> None:
    try:
        await action
    except AppError as exc:
        if exc.status_code != 403:
            raise
    else:
        raise RuntimeError("conversation ownership check unexpectedly succeeded")


async def main() -> None:
    database_name = f"support_agent_probe_{uuid4().hex[:12]}"
    settings = Settings(mongodb_database=database_name, conversation_ttl_seconds=60)
    if not settings.mongodb_uri:
        raise RuntimeError("MongoDB URI is required for the Live persistence probe")
    actor = f"probe-{uuid4().hex[:12]}"
    delete_id = uuid4()
    ttl_id = uuid4()
    print(json.dumps({"phase": "starting", "isolated_database": database_name}), flush=True)
    client: MongoClient[dict[str, Any]] = MongoClient(settings.mongodb_uri, tz_aware=True)
    database = client[database_name]

    resources = build_conversation_resources(settings)
    try:
        await resources.store.claim(delete_id, actor)
        await resources.history.append_turn(
            delete_id,
            case_id=uuid4(),
            user_message="isolated persistence probe",
            assistant_message="verified",
        )
    finally:
        await resources.aclose()

    reopened = build_conversation_resources(settings)
    try:
        await reopened.store.require_owner(delete_id, actor)
        context = await reopened.history.context(delete_id)
        if len(context.recent_messages) != 2:
            raise RuntimeError("MongoDB did not restore the persisted conversation turn")
        await _expect_forbidden(reopened.store.require_owner(delete_id, "other-probe-user"))
        await reopened.delete(delete_id, actor)
        deleted_counts = await asyncio.to_thread(_counts, database, delete_id)
        if any(deleted_counts.values()):
            raise RuntimeError("isolated conversation deletion left MongoDB records")

        ttl_indexes = {
            "owner": _has_ttl_index(database, "conversations", "expires_at", 0),
            "checkpoints": _has_ttl_index(database, "conversation_checkpoints", "created_at", 60),
            "writes": _has_ttl_index(database, "conversation_checkpoint_writes", "created_at", 60),
        }
        if not all(ttl_indexes.values()):
            raise RuntimeError("isolated conversation TTL indexes are missing")

        await reopened.store.claim(ttl_id, actor)
        await reopened.history.append_turn(
            ttl_id,
            case_id=uuid4(),
            user_message="isolated TTL probe",
            assistant_message="verified",
        )
        if not all((await asyncio.to_thread(_counts, database, ttl_id)).values()):
            raise RuntimeError("isolated TTL probe did not write all MongoDB records")
    finally:
        await reopened.aclose()

    await asyncio.sleep(65)
    expired = build_conversation_resources(settings)
    try:
        await _expect_forbidden(expired.store.require_owner(ttl_id, actor))
    finally:
        await expired.aclose()

    deadline = time.monotonic() + 120
    ttl_counts = await asyncio.to_thread(_counts, database, ttl_id)
    while any(ttl_counts.values()) and time.monotonic() < deadline:
        await asyncio.sleep(10)
        ttl_counts = await asyncio.to_thread(_counts, database, ttl_id)

    client.close()
    if any(ttl_counts.values()):
        raise RuntimeError(f"isolated TTL records remain: {ttl_counts}")
    print(
        json.dumps(
            {
                "status": "passed",
                "isolated_database": database_name,
                "reopened_turn_messages": 2,
                "cross_user_rejected": True,
                "deletion_counts": deleted_counts,
                "ttl_indexes": ttl_indexes,
                "expired_owner_rejected": True,
                "ttl_counts": ttl_counts,
            },
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    asyncio.run(main())

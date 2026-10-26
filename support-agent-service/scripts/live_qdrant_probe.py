import asyncio
import json
import os
from datetime import UTC, datetime

from qdrant_client import AsyncQdrantClient, models

from app.core.config import Settings
from app.domain.knowledge import KnowledgeDocument
from app.integrations.knowledge import build_qdrant_knowledge_store


async def main() -> None:
    run_id = os.environ.get("LIVE_TEST_RUN_ID", "manual")
    settings = Settings()
    required = {
        "model_base_url": settings.model_base_url,
        "model_api_key": settings.model_api_key,
        "embedding_model_name": settings.embedding_model_name,
        "qdrant_url": settings.qdrant_url,
    }
    missing = [name for name, value in required.items() if not value]
    if missing:
        raise RuntimeError(f"missing Live settings: {', '.join(missing)}")

    document = KnowledgeDocument(
        document_id=f"live-visibility-{run_id}",
        version="live-v1",
        title="Live visibility verification",
        body=(
            "A newly published post can take a short time to appear while visibility "
            "and moderation signals are processed."
        ),
        updated_at=datetime.now(UTC),
    )
    store = build_qdrant_knowledge_store(settings)
    inserted = 0
    matched = False
    remaining_points: int | None = None
    try:
        inserted = await store.upsert(document)
        matches = await store.search("new post visibility processing", limit=3)
        matched = any(match.document_id == document.document_id for match in matches)
        if inserted < 1 or not matched:
            raise RuntimeError("Qdrant Live round trip did not return the inserted document")
    finally:
        try:
            await store.upsert(document.model_copy(update={"published": False}))
            client = AsyncQdrantClient(
                url=settings.qdrant_url,
                api_key=(
                    settings.qdrant_api_key.get_secret_value() if settings.qdrant_api_key else None
                ),
            )
            try:
                result = await client.count(
                    collection_name=settings.qdrant_collection,
                    count_filter=models.Filter(
                        must=[
                            models.FieldCondition(
                                key="document_id",
                                match=models.MatchValue(value=document.document_id),
                            )
                        ]
                    ),
                    exact=True,
                )
                remaining_points = result.count
            finally:
                await client.close()
        finally:
            await store.aclose()
    if remaining_points != 0:
        raise RuntimeError("Qdrant probe document remains after unpublished cleanup")

    print(
        json.dumps(
            {
                "status": "passed",
                "inserted_chunks": inserted,
                "matched_inserted_document": matched,
                "cleanup": "verified",
                "cleanup_points_remaining": remaining_points,
            },
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    asyncio.run(main())

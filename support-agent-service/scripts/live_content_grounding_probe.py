"""Read-only smoke test for an existing isolated user's recent-content facts."""

import asyncio
import json
import os
from uuid import uuid4

from app.core.config import Settings
from app.domain.capability import CapabilityCall
from app.domain.enums import ObservationStatus
from app.domain.runtime import AuthContext, RuntimeContext
from app.domain.support_query import RecentContentRecord
from app.integrations.service_token import HMACServiceTokenProvider
from app.integrations.support_query import SupportQueryClient


async def main() -> None:
    actor = os.environ.get("LIVE_TEST_ACTOR_USER_ID", "")
    if not actor.isdecimal():
        raise RuntimeError("LIVE_TEST_ACTOR_USER_ID must identify an existing isolated test user")
    settings = Settings()
    secret = settings.support_query_jwt_secret
    base_url = settings.support_query_base_url
    if secret is None or not base_url:
        raise RuntimeError("Live support query configuration is required")

    client = SupportQueryClient(
        base_url=base_url,
        token_provider=HMACServiceTokenProvider(
            secret=secret.get_secret_value(),
            issuer=settings.support_query_jwt_issuer,
            audience=settings.support_query_jwt_audience,
            subject=settings.support_query_jwt_subject,
            ttl_seconds=settings.support_query_jwt_ttl_seconds,
        ),
        timeout_seconds=settings.support_query_timeout_seconds,
    )
    try:
        observation = await client.execute(
            CapabilityCall(
                name="content.list_recent",
                arguments={"limit": 5},
                task_id=f"grounding-probe-{uuid4().hex[:8]}",
                tool_call_id=f"grounding-probe-{uuid4().hex[:8]}",
            ),
            RuntimeContext(
                auth=AuthContext(actor_user_id=actor, token_id=f"probe-{uuid4().hex[:8]}"),
                request_id=uuid4(),
                conversation_id=uuid4(),
                case_id=uuid4(),
                trace_id=uuid4().hex,
            ),
        )
    finally:
        await client.aclose()

    if observation.status != ObservationStatus.SUCCESS or len(observation.evidence) != 1:
        raise RuntimeError("recent-content Live query did not return one verified evidence item")
    raw_records = observation.evidence[0].attributes.get("verified_records")
    if not isinstance(raw_records, list) or not raw_records:
        raise RuntimeError("recent-content Live query has no verified records")
    records = [RecentContentRecord.model_validate(item) for item in raw_records]
    if observation.data is None or observation.data.get("items") != raw_records:
        raise RuntimeError("verified records do not match the Go query data")
    print(
        json.dumps(
            {
                "status": "passed",
                "record_count": len(records),
                "records": [record.model_dump(mode="json") for record in records],
            },
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    asyncio.run(main())

"""Probe W3C parent propagation to the Live Gateway without creating user data."""

import asyncio
import json
import os
from uuid import uuid4

import httpx

from app.core.config import Settings
from app.core.observability import build_telemetry
from app.domain.capability import CapabilityCall
from app.domain.runtime import AuthContext, RuntimeContext
from app.integrations.service_token import HMACServiceTokenProvider
from app.integrations.support_query import SupportQueryClient


async def main() -> None:
    settings = Settings()
    secret = settings.support_query_jwt_secret
    base_url = settings.support_query_base_url
    if secret is None or not base_url or not settings.otlp_traces_endpoint:
        raise RuntimeError("Live service token, query URL, and trace exporter are required")

    telemetry = build_telemetry(settings)
    observed_traceparent = ""

    async def observe_request(request: httpx.Request) -> None:
        nonlocal observed_traceparent
        observed_traceparent = request.headers.get("traceparent", "")

    http_client = httpx.AsyncClient(
        base_url=base_url.rstrip("/") + "/",
        timeout=httpx.Timeout(settings.support_query_timeout_seconds),
        event_hooks={"request": [observe_request]},
    )
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
        http_client=http_client,
    )
    trace_id = ""
    parent_span_id = ""
    try:
        with telemetry.span("support.live_trace_probe") as span:
            context = span.get_span_context()
            trace_id = f"{context.trace_id:032x}"
            parent_span_id = f"{context.span_id:016x}"
            observation = await client.execute(
                CapabilityCall(
                    name="content.list_recent",
                    arguments={"limit": 1},
                    task_id=f"trace-probe-{uuid4().hex[:8]}",
                    tool_call_id=f"trace-probe-{uuid4().hex[:8]}",
                ),
                RuntimeContext(
                    auth=AuthContext(
                        actor_user_id=os.environ.get("LIVE_TRACE_ACTOR_USER_ID", "1"),
                        token_id=f"trace-probe-{uuid4().hex[:8]}",
                    ),
                    request_id=uuid4(),
                    conversation_id=uuid4(),
                    case_id=uuid4(),
                    trace_id=trace_id,
                ),
            )
    finally:
        await http_client.aclose()
        await client.aclose()
        await telemetry.aclose()

    header_parts = observed_traceparent.split("-")
    print(
        json.dumps(
            {
                "trace_id": trace_id,
                "parent_span_id": parent_span_id,
                "sent_trace_id": header_parts[1] if len(header_parts) == 4 else None,
                "sent_parent_span_id": header_parts[2] if len(header_parts) == 4 else None,
                "sent_trace_flags": header_parts[3] if len(header_parts) == 4 else None,
                "observation_status": observation.status.value,
            },
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    asyncio.run(main())

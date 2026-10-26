import base64
import json
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any, cast
from uuid import uuid4

import httpx
import pytest
from opentelemetry import baggage, context, trace
from opentelemetry.trace import NonRecordingSpan, SpanContext, TraceFlags

from app.domain.capability import CapabilityCall
from app.domain.enums import ObservationStatus
from app.domain.runtime import AuthContext, RuntimeContext
from app.integrations.service_token import HMACServiceTokenProvider
from app.integrations.support_query import (
    SupportQueryClient,
    register_support_query_capabilities,
)
from app.tools.executor import CapabilityExecutor, PermanentCapabilityError
from app.tools.registry import CapabilityRegistry

SECRET = "support-query-test-secret-at-least-32-bytes"


def runtime() -> RuntimeContext:
    return RuntimeContext(
        auth=AuthContext(actor_user_id="42", token_id="user-token-1"),
        request_id=uuid4(),
        conversation_id=uuid4(),
        case_id=uuid4(),
        trace_id="4bf92f3577b34da6a3ce929d0e0e4736",
    )


def call(arguments: dict[str, Any] | None = None) -> CapabilityCall:
    return CapabilityCall(
        name="content.list_recent",
        arguments=arguments or {"limit": 5},
        task_id="task-1",
        tool_call_id="call-1",
    )


def token_provider() -> HMACServiceTokenProvider:
    return HMACServiceTokenProvider(
        secret=SECRET,
        issuer="support-agent-service",
        audience="twitter-support-query",
        subject="support-agent-service",
        ttl_seconds=30,
    )


def decode_claims(token: str) -> dict[str, Any]:
    payload = token.split(".")[1]
    padding = "=" * (-len(payload) % 4)
    return cast(dict[str, Any], json.loads(base64.urlsafe_b64decode(payload + padding)))


def success_payload() -> dict[str, Any]:
    return {
        "request_id": str(uuid4()),
        "operation": "content.list_recent",
        "status": "success",
        "data": {
            "items": [
                {
                    "resource_ref": "tweet:100",
                    "visibility": "public",
                    "created_at": "2026-09-28T06:29:00Z",
                }
            ],
            "has_more": False,
        },
        "evidence": [
            {
                "evidence_id": "ev_recent_1",
                "source_type": "business_api",
                "source_ref": "tweet:100",
                "observed_at": datetime.now(UTC).isoformat(),
                "safe_summary": "The authenticated user has one recent public content item.",
                "attributes": {
                    "count": 1,
                    "public_count": 1,
                    "followers_count": 0,
                    "private_count": 0,
                    "restricted_count": 0,
                    "additional_results": False,
                },
            }
        ],
        "meta": {"duration_ms": 12, "data_freshness": "live"},
    }


def _make_far_future_timestamp(payload: dict[str, Any]) -> None:
    items = cast(list[dict[str, Any]], payload["data"]["items"])
    items[0]["created_at"] = "58706-09-26T14:36:30Z"


def _remove_created_at(payload: dict[str, Any]) -> None:
    items = cast(list[dict[str, Any]], payload["data"]["items"])
    items[0].pop("created_at")


def _duplicate_resource_ref(payload: dict[str, Any]) -> None:
    items = cast(list[dict[str, Any]], payload["data"]["items"])
    items.append(
        {
            "resource_ref": "tweet:100",
            "visibility": "private",
            "created_at": "2026-09-28T06:29:00Z",
        }
    )


def _mismatch_evidence_count(payload: dict[str, Any]) -> None:
    evidence = cast(list[dict[str, Any]], payload["evidence"])
    attributes = cast(dict[str, Any], evidence[0]["attributes"])
    attributes["count"] = 2


@pytest.mark.asyncio
async def test_support_query_propagates_trusted_actor_trace_and_call_context() -> None:
    captured: dict[str, Any] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["claims"] = decode_claims(request.headers["authorization"].removeprefix("Bearer "))
        captured["traceparent"] = request.headers["traceparent"]
        captured["body"] = json.loads(request.content)
        return httpx.Response(200, json=success_payload())

    http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="http://support-query.test/",
    )
    client = SupportQueryClient(
        base_url="http://support-query.test",
        token_provider=token_provider(),
        timeout_seconds=1,
        http_client=http_client,
    )
    observation = await client.execute(call(), runtime())
    await http_client.aclose()

    claims = cast(dict[str, Any], captured["claims"])
    body = cast(dict[str, Any], captured["body"])
    assert claims["act"] == {"user_id": "42"}
    assert claims["aud"] == "twitter-support-query"
    assert body["request_context"]["task_id"] == "task-1"
    assert body["request_context"]["tool_call_id"] == "call-1"
    assert captured["traceparent"].startswith("00-4bf92f3577b34da6a3ce929d0e0e4736-")
    assert observation.status == ObservationStatus.SUCCESS
    assert observation.evidence[0].source_ref == "tweet:100"
    verified_records = observation.evidence[0].attributes["verified_records"]
    assert verified_records == [
        {
            "resource_ref": "tweet:100",
            "visibility": "public",
            "created_at": "2026-09-28T06:29:00Z",
        }
    ]


@pytest.mark.parametrize(
    "mutate_payload",
    [
        pytest.param(_make_far_future_timestamp, id="far-future-year"),
        pytest.param(_remove_created_at, id="missing-created-at"),
        pytest.param(_duplicate_resource_ref, id="duplicate-resource-ref"),
        pytest.param(_mismatch_evidence_count, id="evidence-count-mismatch"),
    ],
)
@pytest.mark.asyncio
async def test_support_query_rejects_unverifiable_recent_content_records(
    mutate_payload: Callable[[dict[str, Any]], None],
) -> None:
    payload = success_payload()
    mutate_payload(payload)

    http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(200, json=payload)),
        base_url="http://support-query.test/",
    )
    client = SupportQueryClient(
        base_url="http://support-query.test",
        token_provider=token_provider(),
        timeout_seconds=1,
        http_client=http_client,
    )
    try:
        with pytest.raises(PermanentCapabilityError, match="upstream_invalid_response"):
            await client.execute(call(), runtime())
    finally:
        await http_client.aclose()


@pytest.mark.parametrize(
    "active_trace_flags",
    [TraceFlags(TraceFlags.SAMPLED), TraceFlags(0x03)],
    ids=["sampled", "sampled-with-extra-flags"],
)
@pytest.mark.asyncio
async def test_support_query_uses_active_span_id_as_traceparent_parent(
    active_trace_flags: TraceFlags,
) -> None:
    captured: dict[str, str] = {}
    trace_id = "1234567890abcdef1234567890abcdef"
    span_id = "fedcba0987654321"

    def handler(request: httpx.Request) -> httpx.Response:
        captured["traceparent"] = request.headers["traceparent"]
        captured["baggage"] = request.headers.get("baggage", "")
        return httpx.Response(200, json=success_payload())

    http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="http://support-query.test/",
    )
    client = SupportQueryClient(
        base_url="http://support-query.test",
        token_provider=token_provider(),
        timeout_seconds=1,
        http_client=http_client,
    )
    active_span = NonRecordingSpan(
        SpanContext(
            trace_id=int(trace_id, 16),
            span_id=int(span_id, 16),
            is_remote=False,
            trace_flags=active_trace_flags,
        )
    )

    with trace.use_span(active_span, end_on_exit=False):
        baggage_context = context.attach(baggage.set_baggage("private", "must-not-propagate"))
        try:
            observation = await client.execute(call(), runtime())
        finally:
            context.detach(baggage_context)
    await http_client.aclose()

    assert observation.status == ObservationStatus.SUCCESS
    assert captured["traceparent"] == f"00-{trace_id}-{span_id}-01"
    assert captured["baggage"] == ""


@pytest.mark.asyncio
async def test_executor_retries_retryable_query_with_same_tool_call_and_fresh_jwt() -> None:
    attempts: list[tuple[str, str]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        claims = decode_claims(request.headers["authorization"].removeprefix("Bearer "))
        body = cast(dict[str, Any], json.loads(request.content))
        attempts.append((cast(str, claims["jti"]), body["request_context"]["tool_call_id"]))
        if len(attempts) < 3:
            return httpx.Response(
                503,
                json={
                    "type": "https://errors.example.com/support/query-temporarily-unavailable",
                    "title": "Query temporarily unavailable",
                    "status": 503,
                    "code": "query_temporarily_unavailable",
                    "detail": "The query is temporarily unavailable.",
                    "request_id": str(uuid4()),
                    "trace_id": "trace",
                },
            )
        return httpx.Response(200, json=success_payload())

    http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="http://support-query.test/",
    )
    query_client = SupportQueryClient(
        base_url="http://support-query.test",
        token_provider=token_provider(),
        timeout_seconds=1,
        http_client=http_client,
    )
    registry = CapabilityRegistry()
    register_support_query_capabilities(registry, query_client, timeout_ms=1_000)
    observation = await CapabilityExecutor(registry, max_retries=2).execute(call(), runtime())
    await http_client.aclose()

    assert observation.status == ObservationStatus.SUCCESS
    assert len(attempts) == 3
    assert len({jti for jti, _ in attempts}) == 3
    assert {tool_call_id for _, tool_call_id in attempts} == {"call-1"}


@pytest.mark.parametrize(
    ("status_code", "error_code"),
    [(429, "query_rate_limited"), (504, "query_timeout")],
)
@pytest.mark.asyncio
async def test_executor_stops_after_retry_limit_for_rate_limit_and_timeout(
    status_code: int,
    error_code: str,
) -> None:
    attempts: list[tuple[str, str]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        claims = decode_claims(request.headers["authorization"].removeprefix("Bearer "))
        body = cast(dict[str, Any], json.loads(request.content))
        attempts.append((cast(str, claims["jti"]), body["request_context"]["tool_call_id"]))
        return httpx.Response(
            status_code,
            json={
                "type": f"https://errors.example.com/support/{error_code}",
                "title": "Support query temporarily unavailable",
                "status": status_code,
                "code": error_code,
                "detail": "The support query could not be completed.",
                "request_id": str(uuid4()),
                "trace_id": "trace",
            },
        )

    http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="http://support-query.test/",
    )
    query_client = SupportQueryClient(
        base_url="http://support-query.test",
        token_provider=token_provider(),
        timeout_seconds=1,
        http_client=http_client,
    )
    registry = CapabilityRegistry()
    register_support_query_capabilities(registry, query_client, timeout_ms=1_000)
    observation = await CapabilityExecutor(registry, max_retries=2).execute(call(), runtime())
    await http_client.aclose()

    assert observation.status == ObservationStatus.TEMPORARY_ERROR
    assert observation.error_code == error_code
    assert observation.retryable is True
    assert len(attempts) == 3
    assert len({jti for jti, _ in attempts}) == 3
    assert {tool_call_id for _, tool_call_id in attempts} == {"call-1"}


@pytest.mark.asyncio
async def test_model_cannot_override_authenticated_user_in_arguments() -> None:
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(200, json=success_payload())

    http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="http://support-query.test/",
    )
    query_client = SupportQueryClient(
        base_url="http://support-query.test",
        token_provider=token_provider(),
        timeout_seconds=1,
        http_client=http_client,
    )
    registry = CapabilityRegistry()
    register_support_query_capabilities(registry, query_client, timeout_ms=1_000)
    observation = await CapabilityExecutor(registry).execute(
        call({"limit": 5, "user_id": "999"}),
        runtime(),
    )
    await http_client.aclose()

    assert observation.status == ObservationStatus.INVALID
    assert observation.error_code == "invalid_arguments"
    assert calls == 0


@pytest.mark.asyncio
async def test_invalid_go_response_is_a_non_retryable_observation() -> None:
    attempts = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal attempts
        del request
        attempts += 1
        return httpx.Response(200, json={"unexpected": "payload"})

    http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="http://support-query.test/",
    )
    query_client = SupportQueryClient(
        base_url="http://support-query.test",
        token_provider=token_provider(),
        timeout_seconds=1,
        http_client=http_client,
    )
    registry = CapabilityRegistry()
    register_support_query_capabilities(registry, query_client, timeout_ms=1_000)
    observation = await CapabilityExecutor(registry, max_retries=2).execute(call(), runtime())
    await http_client.aclose()

    assert observation.status == ObservationStatus.INVALID
    assert observation.error_code == "upstream_invalid_response"
    assert attempts == 1


def test_service_token_requires_a_strong_shared_secret() -> None:
    with pytest.raises(ValueError, match="at least 32"):
        HMACServiceTokenProvider(
            secret="short",
            issuer="support-agent-service",
            audience="twitter-support-query",
            subject="support-agent-service",
            ttl_seconds=30,
        )

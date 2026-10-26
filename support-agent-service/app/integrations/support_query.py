import hashlib
import re
from typing import cast
from uuid import uuid4

import httpx
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator
from pydantic import ValidationError

from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.enums import ObservationStatus
from app.domain.runtime import RuntimeContext
from app.domain.support_query import (
    QueryProblem,
    QueryRequestContext,
    RecentContentRecord,
    SupportQueryRequest,
    SupportQueryResponse,
)
from app.integrations.service_token import ServiceTokenProvider
from app.tools.executor import PermanentCapabilityError, TemporaryCapabilityError
from app.tools.registry import CapabilityRegistry

_TRACE_ID = re.compile(r"^[0-9a-f]{32}$")
_RETRYABLE_STATUS = {429, 503, 504}
_TRACE_PROPAGATOR = TraceContextTextMapPropagator()


def _traceparent(trace_id: str) -> str:
    normalized = trace_id.casefold()
    if not _TRACE_ID.fullmatch(normalized) or not normalized.strip("0"):
        normalized = hashlib.sha256(trace_id.encode("utf-8")).hexdigest()[:32]
    return f"00-{normalized}-{uuid4().hex[:16]}-01"


class SupportQueryClient:
    def __init__(
        self,
        *,
        base_url: str,
        token_provider: ServiceTokenProvider,
        timeout_seconds: float,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if not base_url.strip():
            raise ValueError("support query base URL must not be empty")
        self._token_provider = token_provider
        self._owns_http_client = http_client is None
        self._http = http_client or httpx.AsyncClient(
            base_url=base_url.rstrip("/") + "/",
            timeout=httpx.Timeout(timeout_seconds),
        )

    async def execute(
        self,
        call: CapabilityCall,
        runtime: RuntimeContext,
    ) -> CapabilityObservation:
        request = SupportQueryRequest(
            operation=call.name,
            arguments=call.arguments,
            request_context=QueryRequestContext(
                case_id=runtime.case_id,
                task_id=call.task_id,
                tool_call_id=call.tool_call_id,
            ),
        )
        headers = {"Authorization": f"Bearer {self._token_provider.issue(runtime.auth)}"}
        _TRACE_PROPAGATOR.inject(headers)
        active_traceparent = headers.get("traceparent")
        if active_traceparent is None:
            headers["traceparent"] = _traceparent(runtime.trace_id)
        else:
            # The current Go Gateway ignores the newer random-trace-id flag (0x02).
            # Keep the real parent span and sampled bit for cross-version propagation.
            headers["traceparent"] = (
                f"{active_traceparent[:-2]}{int(active_traceparent[-2:], 16) & 0x01:02x}"
            )
        try:
            response = await self._http.post(
                "internal/v1/support/query",
                headers=headers,
                json=request.model_dump(mode="json"),
            )
        except (httpx.TimeoutException, httpx.NetworkError) as exc:
            raise TemporaryCapabilityError("query_temporarily_unavailable") from exc

        if response.status_code == 200:
            return self._success_observation(call, response)
        return self._error_observation(call, response)

    async def aclose(self) -> None:
        if self._owns_http_client:
            await self._http.aclose()

    @staticmethod
    def _success_observation(
        call: CapabilityCall,
        response: httpx.Response,
    ) -> CapabilityObservation:
        try:
            parsed = SupportQueryResponse.model_validate(cast(object, response.json()))
        except (ValueError, TypeError) as exc:
            raise PermanentCapabilityError("upstream_invalid_response") from exc
        if parsed.operation != call.name:
            raise PermanentCapabilityError("upstream_invalid_response")
        evidence = parsed.evidence
        if call.name == "content.list_recent":
            items_value: object = parsed.data.get("items")
            if not isinstance(items_value, list) or not evidence:
                raise PermanentCapabilityError("upstream_invalid_response")
            items = cast(list[object], items_value)
            if len(items) > 20:
                raise PermanentCapabilityError("upstream_invalid_response")
            try:
                records = [RecentContentRecord.model_validate(item) for item in items]
            except (TypeError, ValidationError) as exc:
                raise PermanentCapabilityError("upstream_invalid_response") from exc
            if len({record.resource_ref for record in records}) != len(records):
                raise PermanentCapabilityError("upstream_invalid_response")
            if evidence[0].attributes.get("count") != len(records):
                raise PermanentCapabilityError("upstream_invalid_response")
            trusted_attributes = {
                **evidence[0].attributes,
                "verified_records": [record.model_dump(mode="json") for record in records],
            }
            evidence = [
                evidence[0].model_copy(update={"attributes": trusted_attributes}),
                *evidence[1:],
            ]
        return CapabilityObservation(
            tool_call_id=call.tool_call_id,
            capability_name=call.name,
            status=ObservationStatus.SUCCESS,
            data=parsed.data,
            evidence=evidence,
        )

    @staticmethod
    def _error_observation(
        call: CapabilityCall,
        response: httpx.Response,
    ) -> CapabilityObservation:
        code = _problem_code(response)
        if response.status_code in _RETRYABLE_STATUS:
            raise TemporaryCapabilityError(code)
        if response.status_code == 404 and code == "resource_not_found":
            status = ObservationStatus.NOT_FOUND
        elif response.status_code == 403:
            status = ObservationStatus.FORBIDDEN
        else:
            status = ObservationStatus.INVALID
        return CapabilityObservation(
            tool_call_id=call.tool_call_id,
            capability_name=call.name,
            status=status,
            error_code=code,
        )


def _problem_code(response: httpx.Response) -> str:
    try:
        return QueryProblem.model_validate(cast(object, response.json())).code
    except (ValueError, TypeError):
        fallback: dict[int, str] = {
            400: "invalid_arguments",
            401: "invalid_service_identity",
            403: "operation_forbidden",
            404: "resource_not_found",
            409: "tool_call_conflict",
            422: "operation_unprocessable",
            429: "query_rate_limited",
            502: "upstream_invalid_response",
            503: "query_temporarily_unavailable",
            504: "query_timeout",
        }
        return fallback.get(response.status_code, "upstream_invalid_response")


def register_support_query_capabilities(
    registry: CapabilityRegistry,
    client: SupportQueryClient,
    *,
    timeout_ms: int,
) -> None:
    async def execute(
        call: CapabilityCall,
        runtime: RuntimeContext,
    ) -> CapabilityObservation:
        return await client.execute(call, runtime)

    registry.register(
        CapabilitySpec(
            name="content.list_recent",
            description=(
                "List the authenticated user's own recent published content records, including "
                "record IDs and safe visibility evidence. It cannot inspect the home feed, "
                "recommendation ranking, content text, other users, or publication root causes."
            ),
            input_schema={
                "type": "object",
                "properties": {
                    "limit": {"type": "integer", "minimum": 1, "maximum": 20},
                    "time_from": {"type": "string", "format": "date-time"},
                },
                "additionalProperties": False,
            },
            output_schema={
                "type": "object",
                "properties": {
                    "items": {"type": "array"},
                    "has_more": {"type": "boolean"},
                },
                "required": ["items", "has_more"],
                "additionalProperties": False,
            },
            user_data_scope="self",
            evidence_types=["recent_content", "content_visibility"],
            timeout_ms=timeout_ms,
        ),
        execute,
    )

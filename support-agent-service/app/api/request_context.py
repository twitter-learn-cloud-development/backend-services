import re
from uuid import UUID, uuid4

from fastapi import Request

_TRACEPARENT = re.compile(r"^[0-9a-fA-F]{2}-([0-9a-fA-F]{32})-[0-9a-fA-F]{16}-[0-9a-fA-F]{2}$")


def initialize_request_context(request: Request, *, active_trace_id: str | None = None) -> None:
    request.state.request_id = uuid4()
    if active_trace_id:
        request.state.trace_id = active_trace_id
        return
    traceparent = request.headers.get("traceparent", "")
    match = _TRACEPARENT.fullmatch(traceparent)
    candidate = match.group(1).lower() if match else ""
    request.state.trace_id = candidate if candidate.strip("0") else uuid4().hex


def request_id(request: Request) -> UUID:
    value = getattr(request.state, "request_id", None)
    return value if isinstance(value, UUID) else uuid4()


def trace_id(request: Request) -> str:
    value = getattr(request.state, "trace_id", None)
    return value if isinstance(value, str) and value else uuid4().hex

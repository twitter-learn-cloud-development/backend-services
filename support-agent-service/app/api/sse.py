import asyncio
import json
from collections.abc import AsyncIterator
from contextlib import suppress
from datetime import UTC, datetime
from uuid import UUID, uuid4

from fastapi import Request

from app.core.errors import AppError
from app.domain.response import SupportResponse


def _timestamp() -> str:
    return datetime.now(UTC).isoformat().replace("+00:00", "Z")


def _event(
    *,
    event: str,
    sequence: int,
    request_id: UUID,
    conversation_id: UUID,
    case_id: UUID,
    payload: dict[str, object],
) -> str:
    envelope = {
        "sequence": sequence,
        "timestamp": _timestamp(),
        "request_id": str(request_id),
        "conversation_id": str(conversation_id),
        "case_id": str(case_id),
        "payload": payload,
    }
    data = json.dumps(envelope, ensure_ascii=False, separators=(",", ":"))
    return f"id: {uuid4()}\nevent: {event}\ndata: {data}\n\n"


async def support_event_stream(
    request: Request,
    execution: asyncio.Task[SupportResponse],
    *,
    request_id: UUID,
    conversation_id: UUID,
    case_id: UUID,
    trace_id: str,
    poll_seconds: float,
) -> AsyncIterator[str]:
    sequence = 1
    try:
        yield _event(
            event="case.started",
            sequence=sequence,
            request_id=request_id,
            conversation_id=conversation_id,
            case_id=case_id,
            payload={},
        )
        sequence += 1
        yield _event(
            event="case.understanding",
            sequence=sequence,
            request_id=request_id,
            conversation_id=conversation_id,
            case_id=case_id,
            payload={"message": "正在理解你的问题"},
        )
        sequence += 1
        yield _event(
            event="case.investigating",
            sequence=sequence,
            request_id=request_id,
            conversation_id=conversation_id,
            case_id=case_id,
            payload={"message": "正在核对相关信息", "completed": 0, "total": 0},
        )

        while not execution.done():
            if await request.is_disconnected():
                execution.cancel()
                with suppress(asyncio.CancelledError):
                    await execution
                return
            try:
                await asyncio.wait_for(asyncio.shield(execution), timeout=poll_seconds)
            except TimeoutError:
                continue

        response = await execution
        sequence += 1
        yield _event(
            event="response.delta",
            sequence=sequence,
            request_id=request_id,
            conversation_id=conversation_id,
            case_id=case_id,
            payload={"text": response.answer},
        )
        sequence += 1
        yield _event(
            event="response.completed",
            sequence=sequence,
            request_id=request_id,
            conversation_id=conversation_id,
            case_id=case_id,
            payload=response.model_dump(mode="json"),
        )
    except asyncio.CancelledError:
        raise
    except AppError as exc:
        sequence += 1
        yield _event(
            event="response.failed",
            sequence=sequence,
            request_id=request_id,
            conversation_id=conversation_id,
            case_id=case_id,
            payload={
                "status": exc.status_code,
                "code": exc.code,
                "title": exc.title,
                "detail": exc.detail,
                "request_id": str(request_id),
                "trace_id": trace_id,
            },
        )
    except Exception:
        sequence += 1
        yield _event(
            event="response.failed",
            sequence=sequence,
            request_id=request_id,
            conversation_id=conversation_id,
            case_id=case_id,
            payload={
                "status": 503,
                "code": "temporarily_unavailable",
                "title": "Support service unavailable",
                "detail": "The support request could not be completed.",
                "request_id": str(request_id),
                "trace_id": trace_id,
            },
        )
    finally:
        if not execution.done():
            execution.cancel()
            with suppress(asyncio.CancelledError):
                await execution

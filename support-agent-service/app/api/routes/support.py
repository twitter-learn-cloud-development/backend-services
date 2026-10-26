import asyncio
import hashlib
import json
from typing import Annotated, Literal
from uuid import UUID, uuid4

from fastapi import APIRouter, Depends, Header, Request
from fastapi.responses import JSONResponse, StreamingResponse

from app.api.auth import require_user
from app.api.sse import support_event_stream
from app.core.config import Settings
from app.core.errors import AppError
from app.domain.response import SupportResponse
from app.domain.runtime import AuthContext, RunBudget, RuntimeContext
from app.domain.support_case import SupportRequest
from app.runtime import SupportRunner
from app.services.conversation_store import ConversationHistory, ConversationStore
from app.services.idempotency import IdempotencyEntry, InMemoryIdempotencyStore
from app.services.rate_limiter import InMemoryRateLimiter

router = APIRouter(prefix="/api/v1/support", tags=["support"])


def _response_media_type(accept: str | None) -> Literal["json", "sse"]:
    if not accept:
        return "json"
    media_types = [part.split(";", 1)[0].strip().casefold() for part in accept.split(",")]
    for media_type in media_types:
        if media_type in {"*/*", "application/json"}:
            return "json"
        if media_type == "text/event-stream":
            return "sse"
    raise AppError(
        code="invalid_accept",
        title="Invalid Accept header",
        detail="Accept must allow application/json or text/event-stream.",
        status_code=400,
    )


def _fingerprint(body: SupportRequest) -> str:
    serialized = json.dumps(
        body.model_dump(mode="json"),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    )
    return hashlib.sha256(serialized.encode("utf-8")).hexdigest()


def _request_identity(request: Request) -> tuple[UUID, str]:
    request_id = request.state.request_id
    trace_id = request.state.trace_id
    if not isinstance(request_id, UUID) or not isinstance(trace_id, str):
        raise RuntimeError("request context middleware did not initialize identifiers")
    return request_id, trace_id


async def _invoke_runtime(
    runner: SupportRunner,
    message: str,
    runtime: RuntimeContext,
) -> SupportResponse:
    try:
        async with asyncio.timeout(runtime.budget.hard_timeout_seconds):
            return await runner.run(message, runtime)
    except TimeoutError as exc:
        raise AppError(
            code="runtime_timeout",
            title="Support request timed out",
            detail="The support request exceeded its execution time limit.",
            status_code=504,
        ) from exc
    except AppError:
        raise
    except Exception as exc:
        raise AppError(
            code="temporarily_unavailable",
            title="Support service unavailable",
            detail="The support request could not be completed.",
            status_code=503,
        ) from exc


def _for_current_request(
    response: SupportResponse,
    *,
    request_id: UUID,
    trace_id: str,
) -> SupportResponse:
    return response.model_copy(update={"request_id": request_id, "trace_id": trace_id})


async def _execute_entry(
    *,
    store: InMemoryIdempotencyStore,
    entry: IdempotencyEntry,
    runner: SupportRunner,
    message: str,
    request_id: UUID,
    trace_id: str,
    history: ConversationHistory,
) -> SupportResponse:
    async def execute(runtime: RuntimeContext) -> SupportResponse:
        response = await _invoke_runtime(runner, message, runtime)
        try:
            await history.append_turn(
                runtime.conversation_id,
                case_id=runtime.case_id,
                user_message=message,
                assistant_message=response.answer,
            )
        except Exception as exc:
            raise _conversation_unavailable() from exc
        return response

    stored = await store.execute(
        entry,
        execute,
    )
    return _for_current_request(stored, request_id=request_id, trace_id=trace_id)


@router.post(
    "/chat",
    response_model=SupportResponse,
    responses={
        200: {
            "description": "Completed JSON response or SSE stream",
            "content": {"text/event-stream": {"schema": {"type": "string"}}},
        },
        400: {"description": "Unsupported response media type"},
        401: {"description": "Missing or invalid user credentials"},
        403: {"description": "Conversation is not owned by the user"},
        409: {"description": "Idempotency key conflict"},
        413: {"description": "Request content is too large"},
        429: {"description": "Rate limit exceeded"},
        503: {"description": "Runtime is unavailable"},
        504: {"description": "Runtime timed out"},
    },
)
async def support_chat(
    request: Request,
    body: SupportRequest,
    idempotency_key: Annotated[UUID, Header(alias="Idempotency-Key")],
    auth: Annotated[AuthContext, Depends(require_user)],
    accept: Annotated[str | None, Header(alias="Accept")] = None,
) -> JSONResponse | StreamingResponse:
    response_type = _response_media_type(accept)
    request_id, trace_id = _request_identity(request)
    settings: Settings = request.app.state.settings
    conversations: ConversationStore = request.app.state.conversation_store
    history: ConversationHistory = request.app.state.conversation_history
    idempotency: InMemoryIdempotencyStore = request.app.state.idempotency_store
    limiter: InMemoryRateLimiter = request.app.state.rate_limiter
    runner: SupportRunner = request.app.state.support_runtime

    if body.conversation_id is not None:
        try:
            await conversations.require_owner(body.conversation_id, auth.actor_user_id)
        except AppError:
            raise
        except Exception as exc:
            raise _conversation_unavailable() from exc
    await limiter.check(auth.actor_user_id)

    conversation_id = body.conversation_id or uuid4()
    try:
        conversation_context = await history.context(conversation_id)
    except Exception as exc:
        raise _conversation_unavailable() from exc

    runtime = RuntimeContext(
        auth=auth,
        request_id=request_id,
        conversation_id=conversation_id,
        case_id=uuid4(),
        trace_id=trace_id,
        budget=RunBudget(
            max_tasks=settings.max_tasks,
            soft_timeout_seconds=settings.chat_soft_timeout_seconds,
            hard_timeout_seconds=settings.chat_hard_timeout_seconds,
        ),
        conversation_context=conversation_context,
    )
    scope = f"{auth.actor_user_id}:POST:/api/v1/support/chat:{idempotency_key}"
    entry, created = await idempotency.reserve(
        scope=scope,
        fingerprint=_fingerprint(body),
        runtime=runtime,
    )
    if created and body.conversation_id is None:
        await conversations.claim(entry.runtime.conversation_id, auth.actor_user_id)

    execution = asyncio.create_task(
        _execute_entry(
            store=idempotency,
            entry=entry,
            runner=runner,
            message=body.message,
            request_id=request_id,
            trace_id=trace_id,
            history=history,
        )
    )
    if response_type == "sse":
        return StreamingResponse(
            support_event_stream(
                request,
                execution,
                request_id=request_id,
                conversation_id=entry.runtime.conversation_id,
                case_id=entry.runtime.case_id,
                trace_id=trace_id,
                poll_seconds=settings.sse_disconnect_poll_seconds,
            ),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "X-Accel-Buffering": "no",
            },
        )

    response = await execution
    return JSONResponse(content=response.model_dump(mode="json"))


def _conversation_unavailable() -> AppError:
    return AppError(
        code="conversation_temporarily_unavailable",
        title="Conversation temporarily unavailable",
        detail="Conversation context could not be accessed. Please try again.",
        status_code=503,
    )

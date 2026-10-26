from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager
from typing import cast

from fastapi import FastAPI, Request, Response
from opentelemetry import propagate
from opentelemetry.trace import SpanKind, Status, StatusCode
from prometheus_client import make_asgi_app  # pyright: ignore[reportUnknownVariableType]
from starlette.middleware.base import RequestResponseEndpoint
from starlette.types import ASGIApp
from structlog.contextvars import bind_contextvars, clear_contextvars

from app.api.auth import UserAuthenticator, build_user_authenticator
from app.api.errors import install_exception_handlers
from app.api.request_context import initialize_request_context, request_id, trace_id
from app.api.router import api_router
from app.core.config import Settings, get_settings
from app.core.logging import configure_logging, get_logger
from app.core.observability import build_telemetry, current_trace_id, monotonic_seconds
from app.runtime import RuntimeResources, SupportRunner, build_support_runtime
from app.services.conversation_store import (
    ConversationResources,
    build_conversation_resources,
)
from app.services.idempotency import InMemoryIdempotencyStore
from app.services.rate_limiter import InMemoryRateLimiter


def create_app(
    settings: Settings | None = None,
    *,
    support_runtime: SupportRunner | None = None,
    user_authenticator: UserAuthenticator | None = None,
    conversation_resources: ConversationResources | None = None,
) -> FastAPI:
    resolved_settings = settings or get_settings()
    configure_logging(resolved_settings)
    telemetry = build_telemetry(resolved_settings)
    runtime_resources: RuntimeResources | None = None
    if support_runtime is None:
        runtime_resources = build_support_runtime(resolved_settings, telemetry=telemetry)
        resolved_runtime = runtime_resources.runner
    else:
        resolved_runtime = support_runtime
    resolved_conversations = conversation_resources or build_conversation_resources(
        resolved_settings
    )

    @asynccontextmanager
    async def lifespan(application: FastAPI) -> AsyncGenerator[None]:
        application.state.settings = resolved_settings
        application.state.ready = True
        logger = get_logger(__name__)
        logger.info(
            "application_started",
            app_name=resolved_settings.app_name,
            app_env=resolved_settings.app_env,
            app_version=resolved_settings.app_version,
        )
        try:
            yield
        finally:
            application.state.ready = False
            if runtime_resources is not None:
                await runtime_resources.aclose()
            if conversation_resources is None:
                await resolved_conversations.aclose()
            logger.info("application_stopped")
            await telemetry.aclose()

    application = FastAPI(
        title=resolved_settings.app_name,
        version=resolved_settings.app_version,
        description="User-facing support agent runtime for Twitter Clone.",
        lifespan=lifespan,
    )
    application.state.settings = resolved_settings
    application.state.ready = False
    application.state.telemetry = telemetry
    application.state.support_runtime = resolved_runtime
    application.state.user_authenticator = user_authenticator or build_user_authenticator(
        resolved_settings
    )
    application.state.conversation_store = resolved_conversations.store
    application.state.conversation_history = resolved_conversations.history
    application.state.idempotency_store = InMemoryIdempotencyStore(
        ttl_seconds=resolved_settings.idempotency_ttl_seconds
    )
    application.state.rate_limiter = InMemoryRateLimiter(
        limit=resolved_settings.rate_limit_requests,
        window_seconds=resolved_settings.rate_limit_window_seconds,
    )

    @application.middleware("http")
    async def attach_request_context(
        request: Request,
        call_next: RequestResponseEndpoint,
    ) -> Response:
        started_at = monotonic_seconds()
        extracted = propagate.extract(carrier=dict(request.headers))
        response: Response | None = None
        with telemetry.span(
            "http.server.request",
            kind=SpanKind.SERVER,
            context=extracted,
            attributes={
                "http.request.method": request.method,
                "url.path": request.url.path,
            },
        ) as span:
            initialize_request_context(request, active_trace_id=current_trace_id())
            bind_contextvars(
                request_id=str(request_id(request)),
                trace_id=trace_id(request),
            )
            try:
                response = await call_next(request)
                span.set_status(
                    Status(StatusCode.ERROR if response.status_code >= 500 else StatusCode.UNSET)
                )
                return response
            except Exception:
                span.set_status(Status(StatusCode.ERROR))
                raise
            finally:
                route = request.scope.get("route")
                route_path = getattr(route, "path", "unmatched")
                status_code = response.status_code if response is not None else 500
                duration = monotonic_seconds() - started_at
                span.set_attribute("http.route", route_path)
                span.set_attribute("http.response.status_code", status_code)
                telemetry.metrics.observe_http(
                    request.method,
                    route_path,
                    status_code,
                    duration,
                )
                if response is not None:
                    response.headers["X-Request-ID"] = str(request_id(request))
                    response.headers["X-Trace-ID"] = trace_id(request)
                clear_contextvars()

    install_exception_handlers(application)
    application.include_router(api_router)
    if resolved_settings.metrics_enabled:
        metrics_app = cast(ASGIApp, make_asgi_app(registry=telemetry.metrics.registry))
        application.mount("/metrics", metrics_app)
    return application


app = create_app()

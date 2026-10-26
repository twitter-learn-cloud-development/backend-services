import asyncio
import hashlib
import hmac
import os
import time
from collections.abc import Generator, Mapping
from contextlib import contextmanager

from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, SERVICE_VERSION, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import TraceIdRatioBased
from opentelemetry.trace import Span, SpanKind, Tracer
from opentelemetry.util.types import AttributeValue
from prometheus_client import CollectorRegistry, Counter, Gauge, Histogram

from app.core.config import Settings
from app.core.logging import get_logger


class Metrics:
    def __init__(self) -> None:
        self.registry = CollectorRegistry(auto_describe=True)
        namespace = "support_agent"
        self.http_requests = Counter(
            "http_requests_total",
            "HTTP requests completed.",
            ["method", "route", "status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.http_duration = Histogram(
            "http_request_duration_seconds",
            "HTTP request duration in seconds.",
            ["method", "route"],
            namespace=namespace,
            registry=self.registry,
        )
        self.support_requests = Counter(
            "support_requests_total",
            "Support runs completed.",
            ["status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.support_duration = Histogram(
            "support_run_duration_seconds",
            "Support run duration in seconds.",
            ["status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.worker_runs = Counter(
            "worker_runs_total",
            "Generic worker runs completed.",
            ["status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.worker_duration = Histogram(
            "worker_run_duration_seconds",
            "Generic worker run duration in seconds.",
            ["status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.active_workers = Gauge(
            "active_workers",
            "Generic workers currently running.",
            namespace=namespace,
            registry=self.registry,
        )
        self.model_calls = Counter(
            "model_calls_total",
            "Model calls completed.",
            ["role", "status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.model_duration = Histogram(
            "model_call_duration_seconds",
            "Model call duration in seconds.",
            ["role", "status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.model_tokens = Counter(
            "model_tokens_total",
            "Model tokens reported by the provider.",
            ["role", "direction"],
            namespace=namespace,
            registry=self.registry,
        )
        self.capability_calls = Counter(
            "capability_calls_total",
            "Capability calls completed.",
            ["capability", "status"],
            namespace=namespace,
            registry=self.registry,
        )
        self.capability_duration = Histogram(
            "capability_call_duration_seconds",
            "Capability call duration in seconds.",
            ["capability", "status"],
            namespace=namespace,
            registry=self.registry,
        )

    def observe_http(self, method: str, route: str, status: int, duration: float) -> None:
        self.http_requests.labels(method=method, route=route, status=str(status)).inc()
        self.http_duration.labels(method=method, route=route).observe(duration)

    def observe_support(self, status: str, duration: float) -> None:
        self.support_requests.labels(status=status).inc()
        self.support_duration.labels(status=status).observe(duration)

    def observe_worker(self, status: str, duration: float) -> None:
        self.worker_runs.labels(status=status).inc()
        self.worker_duration.labels(status=status).observe(duration)

    def observe_capability(self, capability: str, status: str, duration: float) -> None:
        self.capability_calls.labels(capability=capability, status=status).inc()
        self.capability_duration.labels(capability=capability, status=status).observe(duration)

    def observe_model(
        self,
        role: str,
        status: str,
        duration: float,
        *,
        input_tokens: int = 0,
        output_tokens: int = 0,
    ) -> None:
        self.model_calls.labels(role=role, status=status).inc()
        self.model_duration.labels(role=role, status=status).observe(duration)
        if input_tokens > 0:
            self.model_tokens.labels(role=role, direction="input").inc(input_tokens)
        if output_tokens > 0:
            self.model_tokens.labels(role=role, direction="output").inc(output_tokens)


class Telemetry:
    def __init__(
        self,
        provider: TracerProvider,
        metrics: Metrics,
        *,
        audit_key: bytes,
    ) -> None:
        self._provider = provider
        self.tracer: Tracer = provider.get_tracer("support-agent-service")
        self.metrics = metrics
        self._audit_key = audit_key
        self._audit_logger = get_logger("support_agent.audit")

    @contextmanager
    def span(
        self,
        name: str,
        *,
        kind: SpanKind = SpanKind.INTERNAL,
        attributes: Mapping[str, AttributeValue] | None = None,
        context: Context | None = None,
    ) -> Generator[Span]:
        with self.tracer.start_as_current_span(
            name,
            kind=kind,
            attributes=attributes,
            context=context,
            record_exception=False,
            set_status_on_exception=False,
        ) as span:
            yield span

    def audit_subject(self, actor_user_id: str) -> str:
        return hmac.new(
            self._audit_key,
            actor_user_id.encode("utf-8"),
            hashlib.sha256,
        ).hexdigest()[:24]

    def audit(self, event: str, **fields: object) -> None:
        self._audit_logger.info(event, **fields)

    async def aclose(self) -> None:
        await asyncio.to_thread(self._provider.force_flush)
        await asyncio.to_thread(self._provider.shutdown)


def build_telemetry(settings: Settings) -> Telemetry:
    resource = Resource.create(
        {
            SERVICE_NAME: settings.app_name,
            SERVICE_VERSION: settings.app_version,
            "deployment.environment.name": settings.app_env,
        }
    )
    provider = TracerProvider(
        resource=resource,
        sampler=TraceIdRatioBased(settings.trace_sample_ratio),
    )
    if settings.otlp_traces_endpoint:
        exporter = OTLPSpanExporter(
            endpoint=settings.otlp_traces_endpoint,
            headers=_parse_headers(settings.otlp_headers),
            timeout=settings.otlp_export_timeout_seconds,
        )
        provider.add_span_processor(BatchSpanProcessor(exporter))
    audit_key = (
        settings.audit_hmac_key.get_secret_value().encode("utf-8")
        if settings.audit_hmac_key
        else os.urandom(32)
    )
    return Telemetry(provider, Metrics(), audit_key=audit_key)


def monotonic_seconds() -> float:
    return time.perf_counter()


def current_trace_id() -> str | None:
    span_context = trace.get_current_span().get_span_context()
    if not span_context.is_valid:
        return None
    return f"{span_context.trace_id:032x}"


def _parse_headers(value: str | None) -> dict[str, str] | None:
    if not value:
        return None
    headers: dict[str, str] = {}
    for item in value.split(","):
        name, separator, content = item.partition("=")
        if separator and name.strip() and content.strip():
            headers[name.strip()] = content.strip()
    return headers or None

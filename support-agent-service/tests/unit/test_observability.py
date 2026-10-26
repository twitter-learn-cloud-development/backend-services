from uuid import uuid4

import pytest
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from pydantic import SecretStr

from app.core.config import Settings
from app.core.logging import configure_logging
from app.core.observability import Metrics, Telemetry, build_telemetry
from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.enums import ObservationStatus
from app.domain.runtime import AuthContext, RuntimeContext
from app.tools.executor import CapabilityExecutor
from app.tools.registry import CapabilityRegistry


def test_span_does_not_record_raw_exception_details() -> None:
    provider = TracerProvider()
    exporter = InMemorySpanExporter()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    telemetry = Telemetry(provider, Metrics(), audit_key=b"test-audit-key")
    secret = "database password in exception"

    with pytest.raises(RuntimeError, match="database password"):
        with telemetry.span("safe.failure"):
            raise RuntimeError(secret)

    spans = exporter.get_finished_spans()
    provider.shutdown()
    assert len(spans) == 1
    assert spans[0].events == ()
    assert secret not in str(spans[0].attributes)
    assert secret not in str(spans[0].status)


@pytest.mark.asyncio
async def test_capability_audit_log_excludes_arguments_identity_and_exception(
    capsys: pytest.CaptureFixture[str],
) -> None:
    settings = Settings(
        app_env="test",
        json_logs=True,
        audit_hmac_key=SecretStr("audit-key-that-is-long-enough-for-tests"),
    )
    configure_logging(settings)
    telemetry = build_telemetry(settings)
    registry = CapabilityRegistry()
    secret = "secret argument and internal traceback"

    async def failing_handler(
        call: CapabilityCall,
        runtime: RuntimeContext,
    ) -> CapabilityObservation:
        del call, runtime
        raise RuntimeError(secret)

    registry.register(
        CapabilitySpec(
            name="test.failing_read",
            description="A failing read capability used for audit tests.",
            input_schema={
                "type": "object",
                "properties": {"secret": {"type": "string"}},
                "additionalProperties": False,
            },
            output_schema={"type": "object"},
            user_data_scope="public",
            evidence_types=["test"],
        ),
        failing_handler,
    )
    runtime = RuntimeContext(
        auth=AuthContext(actor_user_id="raw-user-identity", token_id="raw-token-id"),
        request_id=uuid4(),
        conversation_id=uuid4(),
        case_id=uuid4(),
        trace_id="audit-trace",
    )

    observation = await CapabilityExecutor(
        registry,
        max_retries=0,
        telemetry=telemetry,
    ).execute(
        CapabilityCall(
            name="test.failing_read",
            arguments={"secret": secret},
            task_id="audit-task",
            tool_call_id="audit-call",
        ),
        runtime,
    )
    await telemetry.aclose()
    output = capsys.readouterr().out

    assert observation.status == ObservationStatus.TEMPORARY_ERROR
    assert "capability_call_completed" in output
    assert secret not in output
    assert "raw-user-identity" not in output
    assert "raw-token-id" not in output

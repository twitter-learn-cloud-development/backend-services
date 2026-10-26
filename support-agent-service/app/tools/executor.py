import asyncio
from contextlib import nullcontext
from typing import Protocol, cast

from jsonschema import Draft202012Validator
from jsonschema.exceptions import ValidationError as JsonSchemaValidationError

from app.core.observability import Telemetry, monotonic_seconds
from app.domain.capability import CapabilityCall, CapabilityObservation
from app.domain.enums import ObservationStatus
from app.domain.runtime import RuntimeContext
from app.tools.registry import CapabilityRegistry, CapabilityRegistryError


class TemporaryCapabilityError(RuntimeError):
    def __init__(self, code: str = "capability_temporarily_unavailable") -> None:
        super().__init__(code)
        self.code = code


class PermanentCapabilityError(RuntimeError):
    def __init__(self, code: str = "capability_failed") -> None:
        super().__init__(code)
        self.code = code


class _SchemaValidator(Protocol):
    def validate(self, instance: object) -> None: ...


class CapabilityExecutor:
    def __init__(
        self,
        registry: CapabilityRegistry,
        *,
        max_retries: int = 2,
        telemetry: Telemetry | None = None,
    ) -> None:
        self._registry = registry
        self._max_retries = max_retries
        self._telemetry = telemetry

    async def execute(
        self,
        call: CapabilityCall,
        runtime: RuntimeContext,
    ) -> CapabilityObservation:
        started_at = monotonic_seconds()
        status = "exception"
        span = (
            self._telemetry.span(
                "support.capability",
                attributes={
                    "support.case_id": str(runtime.case_id),
                    "support.task_id": call.task_id,
                    "support.tool_call_id": call.tool_call_id,
                    "support.capability": call.name,
                },
            )
            if self._telemetry is not None
            else nullcontext()
        )
        try:
            with span:
                observation = await self._execute(call, runtime)
                observation = observation.model_copy(
                    update={"provided_fields": list(call.arguments)}
                )
                status = observation.status.value
                return observation
        finally:
            if self._telemetry is not None:
                duration = monotonic_seconds() - started_at
                self._telemetry.metrics.observe_capability(call.name, status, duration)
                self._telemetry.audit(
                    "capability_call_completed",
                    actor=self._telemetry.audit_subject(runtime.auth.actor_user_id),
                    trace_id=runtime.trace_id,
                    case_id=str(runtime.case_id),
                    task_id=call.task_id,
                    tool_call_id=call.tool_call_id,
                    capability=call.name,
                    status=status,
                    duration_ms=round(duration * 1_000, 3),
                )

    async def _execute(
        self,
        call: CapabilityCall,
        runtime: RuntimeContext,
    ) -> CapabilityObservation:
        try:
            registered = self._registry.get(call.name)
        except CapabilityRegistryError:
            return self._error(call, ObservationStatus.INVALID, "capability_not_found")

        if not self._registry.is_allowed(call.name, runtime.auth):
            return self._error(call, ObservationStatus.FORBIDDEN, "capability_forbidden")

        try:
            validator = cast(
                _SchemaValidator,
                Draft202012Validator(registered.spec.input_schema),
            )
            validator.validate(call.arguments)
        except JsonSchemaValidationError as error:
            missing_fields: list[str] = []
            required: object = registered.spec.input_schema.get("required")
            if (
                error.validator == "required"
                and not error.absolute_path
                and isinstance(required, list)
            ):
                missing_fields = [
                    name
                    for name in cast(list[object], required)
                    if isinstance(name, str) and name not in call.arguments
                ]
            return self._error(call, ObservationStatus.INVALID, "invalid_arguments").model_copy(
                update={"missing_fields": missing_fields}
            )

        attempts = self._max_retries + 1
        for attempt in range(attempts):
            try:
                async with asyncio.timeout(registered.spec.timeout_ms / 1_000):
                    observation = await registered.handler(call, runtime)
                    return observation.model_copy(
                        update={
                            "tool_call_id": call.tool_call_id,
                            "capability_name": call.name,
                            "missing_fields": [],
                        }
                    )
            except PermanentCapabilityError as exc:
                return self._error(call, ObservationStatus.INVALID, exc.code)
            except (TimeoutError, TemporaryCapabilityError) as exc:
                if attempt == attempts - 1:
                    error_code = (
                        exc.code
                        if isinstance(exc, TemporaryCapabilityError)
                        else "capability_temporarily_unavailable"
                    )
                    return self._error(
                        call,
                        ObservationStatus.TEMPORARY_ERROR,
                        error_code,
                        retryable=True,
                    )
                await asyncio.sleep(0)
            except Exception:
                if attempt == attempts - 1:
                    return self._error(
                        call,
                        ObservationStatus.TEMPORARY_ERROR,
                        "capability_temporarily_unavailable",
                        retryable=True,
                    )
                await asyncio.sleep(0)

        raise AssertionError("capability retry loop exhausted unexpectedly")

    @staticmethod
    def _error(
        call: CapabilityCall,
        status: ObservationStatus,
        error_code: str,
        *,
        retryable: bool = False,
    ) -> CapabilityObservation:
        return CapabilityObservation(
            tool_call_id=call.tool_call_id,
            capability_name=call.name,
            status=status,
            error_code=error_code,
            retryable=retryable,
        )

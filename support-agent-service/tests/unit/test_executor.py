from uuid import uuid4

import pytest

from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.enums import ObservationStatus
from app.domain.runtime import AuthContext, RuntimeContext
from app.tools.executor import CapabilityExecutor
from app.tools.mock import create_mock_registry, slow_handler


def runtime(*scopes: str) -> RuntimeContext:
    return RuntimeContext(
        auth=AuthContext(
            actor_user_id="user-1",
            token_id="token-1",
            scopes=frozenset(scopes),
        ),
        request_id=uuid4(),
        conversation_id=uuid4(),
        case_id=uuid4(),
        trace_id="trace-1",
    )


@pytest.mark.asyncio
async def test_executor_validates_and_executes_allowed_capability() -> None:
    executor = CapabilityExecutor(create_mock_registry())
    observation = await executor.execute(
        CapabilityCall(
            name="content.list_recent",
            arguments={"limit": 5},
            task_id="task-1",
            tool_call_id="call-1",
        ),
        runtime(),
    )

    assert observation.status == ObservationStatus.SUCCESS
    assert observation.evidence


@pytest.mark.asyncio
async def test_executor_rejects_invalid_arguments_and_forbidden_capability() -> None:
    executor = CapabilityExecutor(create_mock_registry())

    invalid = await executor.execute(
        CapabilityCall(
            name="content.list_recent",
            arguments={"limit": 100},
            task_id="task-1",
            tool_call_id="call-1",
        ),
        runtime(),
    )
    forbidden = await executor.execute(
        CapabilityCall(
            name="internal.read_restricted_summary",
            arguments={},
            task_id="task-1",
            tool_call_id="call-2",
        ),
        runtime(),
    )

    assert invalid.status == ObservationStatus.INVALID
    assert invalid.error_code == "invalid_arguments"
    assert forbidden.status == ObservationStatus.FORBIDDEN


@pytest.mark.asyncio
async def test_executor_bounds_timeout_and_retries() -> None:
    registry = create_mock_registry()
    registry.register(
        CapabilitySpec(
            name="mock.wait_slowly",
            description="Wait longer than the configured timeout.",
            input_schema={"type": "object", "additionalProperties": False},
            output_schema={"type": "object"},
            user_data_scope="public",
            evidence_types=["timeout"],
            timeout_ms=100,
        ),
        slow_handler,
    )
    executor = CapabilityExecutor(registry, max_retries=1)

    observation = await executor.execute(
        CapabilityCall(
            name="mock.wait_slowly",
            arguments={},
            task_id="task-1",
            tool_call_id="call-3",
        ),
        runtime(),
    )

    assert observation.status == ObservationStatus.TEMPORARY_ERROR
    assert observation.retryable is True


@pytest.mark.asyncio
async def test_executor_missing_input_proof_is_schema_owned_not_handler_owned() -> None:
    registry = create_mock_registry()

    async def handler(call: CapabilityCall, context: RuntimeContext) -> CapabilityObservation:
        del context
        return CapabilityObservation(
            tool_call_id=call.tool_call_id,
            capability_name=call.name,
            status=ObservationStatus.SUCCESS,
            missing_fields=["tweet_id"],  # An adapter cannot manufacture schema-missing proof.
            data={},
        )

    registry.register(
        CapabilitySpec(
            name="mock.read_record",
            description="Read one record using a required object identifier.",
            input_schema={
                "type": "object",
                "properties": {"tweet_id": {"type": "string"}},
                "required": ["tweet_id"],
                "additionalProperties": False,
            },
            output_schema={"type": "object"},
            user_data_scope="public",
            evidence_types=["record"],
        ),
        handler,
    )
    executor = CapabilityExecutor(registry)
    absent = await executor.execute(
        CapabilityCall(name="mock.read_record", arguments={}, task_id="t", tool_call_id="absent"),
        runtime(),
    )
    assert absent.status == ObservationStatus.INVALID
    assert absent.missing_fields == ["tweet_id"]
    assert absent.provided_fields == []
    invalid_type = await executor.execute(
        CapabilityCall(
            name="mock.read_record",
            arguments={"tweet_id": 123},
            task_id="t",
            tool_call_id="invalid",
        ),
        runtime(),
    )
    assert invalid_type.missing_fields == []
    assert invalid_type.provided_fields == ["tweet_id"]
    success = await executor.execute(
        CapabilityCall(
            name="mock.read_record",
            arguments={"tweet_id": "test"},
            task_id="t",
            tool_call_id="valid",
        ),
        runtime(),
    )
    assert success.status == ObservationStatus.SUCCESS
    assert success.missing_fields == []
    assert success.provided_fields == ["tweet_id"]

from datetime import UTC, datetime
from uuid import uuid4

import pytest
from langchain_core.messages import AIMessage

from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.enums import EvidenceSourceType, ObservationStatus, WorkerStatus
from app.domain.evidence import Evidence
from app.domain.runtime import AuthContext, RunBudget, RuntimeContext
from app.domain.task_plan import TaskSpec
from app.domain.worker_result import WorkerCompletion
from app.graph.worker import GenericWorker
from app.integrations.model_provider import FakeModelProvider, ModelRole
from app.tools.executor import CapabilityExecutor
from app.tools.mock import create_mock_registry
from app.tools.registry import CapabilityRegistry
from app.tools.resolver import CapabilityResolver


def runtime(*, max_tool_calls: int = 6) -> RuntimeContext:
    return RuntimeContext(
        auth=AuthContext(actor_user_id="user-1", token_id="token-1"),
        request_id=uuid4(),
        conversation_id=uuid4(),
        case_id=uuid4(),
        trace_id="trace-1",
        budget=RunBudget(max_tool_calls_per_worker=max_tool_calls),
    )


def task() -> TaskSpec:
    return TaskSpec(
        task_id="task_1",
        objective="Check recent content and public platform status",
        required_evidence=["recent content", "platform status"],
        success_criteria=["verified evidence is collected"],
    )


def worker_with_responses(responses: list[AIMessage]) -> GenericWorker:
    registry = create_mock_registry()
    return GenericWorker(
        FakeModelProvider({ModelRole.WORKER: responses}),
        CapabilityResolver(registry),
        CapabilityExecutor(registry),
    )


@pytest.mark.asyncio
async def test_generic_worker_calls_tool_and_returns_evidence_backed_result() -> None:
    completion = WorkerCompletion(
        status=WorkerStatus.SUCCESS,
        conclusion="The public platform is operational.",
        confidence=0.95,
    )
    worker = worker_with_responses(
        [
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "platform__get_status",
                        "args": {},
                        "id": "call_1",
                        "type": "tool_call",
                    }
                ],
            ),
            AIMessage(content=completion.model_dump_json()),
        ]
    )

    result = await worker.run(task(), runtime())

    assert result.status == WorkerStatus.SUCCESS
    assert result.evidence[0].source_ref == "platform:public-status"
    assert result.findings[0].evidence_refs == [result.evidence[0].evidence_id]


@pytest.mark.asyncio
async def test_worker_replaces_model_itemized_content_with_verified_evidence_summary() -> None:
    registry = CapabilityRegistry()

    async def list_recent(
        call: CapabilityCall, runtime_context: RuntimeContext
    ) -> CapabilityObservation:
        del runtime_context
        return CapabilityObservation(
            tool_call_id=call.tool_call_id,
            capability_name=call.name,
            status=ObservationStatus.SUCCESS,
            data={"items": []},
            evidence=[
                Evidence(
                    evidence_id="ev_recent_grounded",
                    source_type=EvidenceSourceType.BUSINESS_API,
                    source_ref="content:authenticated-user:recent",
                    observed_at=datetime(2026, 9, 28, tzinfo=UTC),
                    safe_summary="The authenticated user has one recent public content item.",
                    attributes={
                        "count": 1,
                        "verified_records": [
                            {
                                "resource_ref": "tweet:2103856181104619520",
                                "visibility": "public",
                                "created_at": "2026-09-26T14:36:15Z",
                            }
                        ],
                    },
                )
            ],
        )

    registry.register(
        CapabilitySpec(
            name="content.list_recent",
            description="List recent content owned by the authenticated user.",
            input_schema={
                "type": "object",
                "properties": {"limit": {"type": "integer", "minimum": 1, "maximum": 20}},
                "additionalProperties": False,
            },
            output_schema={"type": "object"},
            user_data_scope="self",
            evidence_types=["recent_content"],
        ),
        list_recent,
    )
    completion = WorkerCompletion(
        status=WorkerStatus.SUCCESS,
        conclusion=(
            "Found tweet:9999999999999999999 at 58706-09-26T14:36:15. "
            "Verified tweet:2103856181104619520 at 2026-09-26T14:36:15."
        ),
        confidence=0.95,
    )
    worker = GenericWorker(
        FakeModelProvider(
            {
                ModelRole.WORKER: [
                    AIMessage(
                        content="",
                        tool_calls=[
                            {
                                "name": "content__list_recent",
                                "args": {"limit": 5},
                                "id": "call_recent_grounding",
                                "type": "tool_call",
                            }
                        ],
                    ),
                    AIMessage(content=completion.model_dump_json()),
                ]
            }
        ),
        CapabilityResolver(registry),
        CapabilityExecutor(registry),
    )

    result = await worker.run(task(), runtime())

    assert result.status == WorkerStatus.PARTIAL
    assert result.conclusion == "The authenticated user has one recent public content item."
    assert result.gaps == ["Unverified itemized content facts were omitted."]
    assert result.evidence[0].attributes["verified_records"] == [
        {
            "resource_ref": "tweet:2103856181104619520",
            "visibility": "public",
            "created_at": "2026-09-26T14:36:15Z",
        }
    ]


@pytest.mark.asyncio
async def test_failed_completion_preserves_collected_evidence_and_becomes_partial() -> None:
    completion = WorkerCompletion(
        status=WorkerStatus.FAILED,
        conclusion="The worker failed after collecting a verified status.",
        confidence=0.4,
        gaps=["The remaining part of the task could not be checked."],
    )
    worker = worker_with_responses(
        [
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "platform__get_status",
                        "args": {},
                        "id": "call_failed_with_evidence",
                        "type": "tool_call",
                    }
                ],
            ),
            AIMessage(content=completion.model_dump_json()),
        ]
    )

    result = await worker.run(task(), runtime())

    assert result.status == WorkerStatus.PARTIAL
    assert len(result.evidence) == 1
    assert result.evidence[0].source_ref == "platform:public-status"
    assert result.findings[0].evidence_refs == [result.evidence[0].evidence_id]
    assert "The remaining part of the task could not be checked." in result.gaps


@pytest.mark.asyncio
async def test_success_completion_with_gaps_is_downgraded_to_partial() -> None:
    completion = WorkerCompletion(
        status=WorkerStatus.SUCCESS,
        conclusion="The platform is operational.",
        confidence=0.8,
        gaps=["The requested account-specific check is still missing."],
    )
    worker = worker_with_responses(
        [
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "platform__get_status",
                        "args": {},
                        "id": "call_success_with_gap",
                        "type": "tool_call",
                    }
                ],
            ),
            AIMessage(content=completion.model_dump_json()),
        ]
    )

    result = await worker.run(task(), runtime())

    assert result.status == WorkerStatus.PARTIAL
    assert result.evidence
    assert result.gaps == ["The requested account-specific check is still missing."]


@pytest.mark.asyncio
async def test_same_generic_worker_runtime_can_combine_multiple_capabilities() -> None:
    completion = WorkerCompletion(
        status=WorkerStatus.SUCCESS,
        conclusion="Recent content exists and the platform is operational.",
        confidence=0.9,
    )
    worker = worker_with_responses(
        [
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "content__list_recent",
                        "args": {"limit": 5},
                        "id": "call_1",
                        "type": "tool_call",
                    }
                ],
            ),
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "platform__get_status",
                        "args": {},
                        "id": "call_2",
                        "type": "tool_call",
                    }
                ],
            ),
            AIMessage(content=completion.model_dump_json()),
        ]
    )

    result = await worker.run(task(), runtime())

    assert result.status == WorkerStatus.SUCCESS
    assert {evidence.source_type for evidence in result.evidence} == {
        "business_api",
        "platform_status",
    }


@pytest.mark.asyncio
async def test_generic_worker_handles_unknown_tool_without_executing_it() -> None:
    completion = WorkerCompletion(
        status=WorkerStatus.PARTIAL,
        conclusion="The requested capability was not available.",
        confidence=0.0,
        gaps=["No permitted capability could provide evidence."],
    )
    worker = worker_with_responses(
        [
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "unknown__read_secret",
                        "args": {},
                        "id": "call_1",
                        "type": "tool_call",
                    }
                ],
            ),
            AIMessage(content=completion.model_dump_json()),
        ]
    )

    result = await worker.run(task(), runtime())

    assert result.status == WorkerStatus.PARTIAL
    assert not result.evidence


@pytest.mark.asyncio
async def test_generic_worker_stops_when_tool_budget_is_exhausted() -> None:
    worker = worker_with_responses(
        [
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "platform__get_status",
                        "args": {},
                        "id": "call_1",
                        "type": "tool_call",
                    }
                ],
            ),
        ]
    )

    result = await worker.run(task(), runtime(max_tool_calls=1))

    assert result.status == WorkerStatus.PARTIAL
    assert result.evidence
    assert "structured completion" in result.gaps[0]

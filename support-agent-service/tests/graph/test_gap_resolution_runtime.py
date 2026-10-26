import asyncio
from typing import Any

import pytest
from langchain_core.language_models.base import LanguageModelInput
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage
from langchain_core.runnables import RunnableConfig

from app.domain.enums import SupportStatus, WorkerStatus
from app.domain.gap import GapAssessment, GapResolution, evidence_gap
from app.domain.response import SupportComposition
from app.domain.runtime import RunBudget
from app.domain.task_plan import SupplementalTaskPlan, TaskPlan
from app.domain.worker_result import WorkerResult
from app.graph.supervisor import SupportRuntime
from app.integrations.model_provider import ModelRole
from tests.graph.test_support_runtime import (
    CapturingModelProvider,
    StubWorker,
    runtime,
    success_result,
    task,
)


def partial_result(task_id: str, question: str) -> WorkerResult:
    result = success_result(task_id)
    return result.model_copy(
        update={
            "status": WorkerStatus.PARTIAL,
            "gaps": [question],
        }
    )


@pytest.mark.asyncio
async def test_already_closed_gap_cannot_be_claimed_by_a_later_replan() -> None:
    first = evidence_gap("task_a", "First missing fact.")
    second = evidence_gap("task_a", "Second missing fact.")
    initial_result = partial_result("task_a", first.question).model_copy(
        update={"gaps": [first.question, second.question]}
    )
    repeated_claim = SupplementalTaskPlan(
        tasks=[task("task_c").model_copy(update={"resolves_gap_ids": [first.gap_id]})],
        answer_strategy="Attempt to reuse a closed gap.",
    )
    provider = make_provider(
        [
            initial_plan().model_dump_json(),
            supplemental_plan(first.gap_id).model_dump_json(),
            repeated_claim.model_dump_json(),
        ],
        [assessment_for(first.gap_id)],
        final_status=SupportStatus.PARTIAL,
    )
    worker = StubWorker({"task_a": initial_result, "task_b": success_result("task_b")})
    response = await SupportRuntime(provider, worker).run(
        "Check both facts.", runtime(max_replans=2)
    )
    assert response.status == SupportStatus.PARTIAL
    assert worker.completed == ["task_a", "task_b"]
    final_prompt = "\n".join(provider.models[ModelRole.COMPOSER].prompts[-1])
    assert second.gap_id in final_prompt
    assert first.gap_id in final_prompt
    assert '"resolved_gap_ids"' in final_prompt


@pytest.mark.asyncio
async def test_low_remaining_budget_skips_assessment_and_preserves_composer_time() -> None:
    gap = evidence_gap("task_a", "A missing detail.")
    provider = make_provider(
        [initial_plan().model_dump_json(), supplemental_plan(gap.gap_id).model_dump_json()],
        [],
        final_status=SupportStatus.PARTIAL,
    )
    worker = StubWorker(
        {"task_a": partial_result("task_a", gap.question), "task_b": success_result("task_b")}
    )
    request_runtime = runtime().model_copy(
        update={"budget": RunBudget(soft_timeout_seconds=1, hard_timeout_seconds=5)}
    )
    response = await SupportRuntime(provider, worker).run("Explain the detail.", request_runtime)
    assert response.status == SupportStatus.PARTIAL
    assert len(provider.models[ModelRole.COMPOSER].prompts) == 1


def make_provider(
    planner: list[object],
    assessments: list[str],
    *,
    final_status: SupportStatus = SupportStatus.ANSWERED,
) -> CapturingModelProvider:
    composition = SupportComposition(
        status=final_status,
        answer="The requested information is supported by the checked records.",
        suggested_actions=[],
    )
    planner_messages = [
        item if isinstance(item, AIMessage) else AIMessage(content=str(item)) for item in planner
    ]
    composer_messages = [
        AIMessage(content=item) for item in [*assessments, composition.model_dump_json()]
    ]
    return CapturingModelProvider(
        {
            ModelRole.PLANNER: planner_messages,
            ModelRole.COMPOSER: composer_messages,
        }
    )


def assessment_for(gap_id: str, task_id: str = "task_b", evidence_ref: str = "ev_task_b") -> str:
    return GapAssessment(
        resolutions=[
            GapResolution(
                gap_id=gap_id,
                task_id=task_id,
                evidence_refs=[evidence_ref],
                rationale="The supplemental observation directly supplies the missing fact.",
            )
        ]
    ).model_dump_json()


def initial_plan() -> TaskPlan:
    return TaskPlan(tasks=[task("task_a")], answer_strategy="Check the original source.")


def supplemental_plan(gap_id: str, *, linked: bool = True) -> SupplementalTaskPlan:
    supplemental = task("task_b")
    if linked:
        supplemental = supplemental.model_copy(update={"resolves_gap_ids": [gap_id]})
    return SupplementalTaskPlan(
        tasks=[supplemental], answer_strategy="Check a source that may fill the gap."
    )


@pytest.mark.asyncio
async def test_relevant_semantically_assessed_task_evidence_resolves_original_gap() -> None:
    gap = evidence_gap("task_a", "The publication date is not verified.")
    provider = make_provider(
        [initial_plan().model_dump_json(), supplemental_plan(gap.gap_id).model_dump_json()],
        [assessment_for(gap.gap_id)],
    )
    worker = StubWorker(
        {
            "task_a": partial_result("task_a", gap.question),
            "task_b": success_result("task_b"),
        }
    )

    response = await SupportRuntime(provider, worker).run("When was it published?", runtime())

    assert response.status == SupportStatus.ANSWERED
    assert set(response.evidence_refs) == {"ev_task_a", "ev_task_b"}
    assert "".join(provider.models[ModelRole.COMPOSER].prompts[0]).find("required_evidence") >= 0
    assert gap.question in "".join(provider.models[ModelRole.COMPOSER].prompts[0])


@pytest.mark.asyncio
async def test_unrelated_success_does_not_close_gap_or_trigger_assessment() -> None:
    gap = evidence_gap("task_a", "The publication date is not verified.")
    provider = make_provider(
        [
            initial_plan().model_dump_json(),
            supplemental_plan(gap.gap_id, linked=False).model_dump_json(),
        ],
        [],
        final_status=SupportStatus.PARTIAL,
    )
    worker = StubWorker(
        {
            "task_a": partial_result("task_a", gap.question),
            "task_b": success_result("task_b"),
        }
    )

    response = await SupportRuntime(provider, worker).run("When was it published?", runtime())

    assert response.status == SupportStatus.PARTIAL
    assert len(provider.models[ModelRole.COMPOSER].prompts) == 1
    assert worker.completed == ["task_a", "task_b"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "assessment",
    [
        "not json",
        '{"resolutions":[{"gap_id":"unknown"}]}',
        '{"resolutions":[]}',
    ],
)
async def test_refused_invalid_or_unknown_gap_assessment_fails_closed(
    assessment: str,
) -> None:
    gap = evidence_gap("task_a", "A missing detail.")
    provider = make_provider(
        [initial_plan().model_dump_json(), supplemental_plan(gap.gap_id).model_dump_json()],
        [assessment],
        final_status=SupportStatus.PARTIAL,
    )
    worker = StubWorker(
        {
            "task_a": partial_result("task_a", gap.question),
            "task_b": success_result("task_b"),
        }
    )

    response = await SupportRuntime(provider, worker).run("Explain the detail.", runtime())

    assert response.status == SupportStatus.PARTIAL
    assert set(response.evidence_refs) == {"ev_task_a", "ev_task_b"}


@pytest.mark.asyncio
async def test_cross_task_evidence_reference_is_rejected_and_partial_is_preserved() -> None:
    gap = evidence_gap("task_a", "A missing detail.")
    provider = make_provider(
        [initial_plan().model_dump_json(), supplemental_plan(gap.gap_id).model_dump_json()],
        [assessment_for(gap.gap_id, evidence_ref="ev_task_a")],
        final_status=SupportStatus.PARTIAL,
    )
    worker = StubWorker(
        {
            "task_a": partial_result("task_a", gap.question),
            "task_b": success_result("task_b"),
        }
    )

    response = await SupportRuntime(provider, worker).run("Explain the detail.", runtime())

    assert response.status == SupportStatus.PARTIAL
    assert set(response.evidence_refs) == {"ev_task_a", "ev_task_b"}


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "supplemental_status,supplemental_gaps",
    [(WorkerStatus.FAILED, []), (WorkerStatus.SUCCESS, ["A further uncertainty remains."])],
)
async def test_failed_or_success_with_gaps_supplemental_task_cannot_resolve(
    supplemental_status: WorkerStatus, supplemental_gaps: list[str]
) -> None:
    gap = evidence_gap("task_a", "A missing detail.")
    provider = make_provider(
        [initial_plan().model_dump_json(), supplemental_plan(gap.gap_id).model_dump_json()],
        [],
        final_status=SupportStatus.PARTIAL,
    )
    supplemental = success_result("task_b").model_copy(
        update={"status": supplemental_status, "gaps": supplemental_gaps}
    )
    worker = StubWorker({"task_a": partial_result("task_a", gap.question), "task_b": supplemental})

    response = await SupportRuntime(provider, worker).run("Explain the detail.", runtime())

    assert response.status == SupportStatus.PARTIAL
    assert len(provider.models[ModelRole.COMPOSER].prompts) == 1


@pytest.mark.asyncio
async def test_conflicting_evidence_prevents_gap_assessment() -> None:
    from datetime import UTC, datetime

    from app.domain.enums import EvidenceSourceType
    from app.domain.evidence import Evidence

    gap = evidence_gap("task_a", "A missing detail.")
    provider = make_provider(
        [initial_plan().model_dump_json(), supplemental_plan(gap.gap_id).model_dump_json()],
        [],
        final_status=SupportStatus.PARTIAL,
    )
    first = success_result("task_a").model_copy(
        update={"status": WorkerStatus.PARTIAL, "gaps": [gap.question]}
    )
    second = success_result("task_b").model_copy(
        update={
            "evidence": [
                Evidence(
                    evidence_id="ev_task_a",
                    source_type=EvidenceSourceType.BUSINESS_API,
                    source_ref="resource:task_a",
                    observed_at=datetime.now(UTC),
                    safe_summary="Conflicting representation of the original observation.",
                )
            ]
        }
    )
    worker = StubWorker({"task_a": first, "task_b": second})

    response = await SupportRuntime(provider, worker).run("Explain the detail.", runtime())

    assert response.status == SupportStatus.PARTIAL
    assert len(provider.models[ModelRole.COMPOSER].prompts) == 1


@pytest.mark.asyncio
async def test_unknown_supplemental_gap_target_is_rejected_and_question_is_evaluated() -> None:
    gap = evidence_gap("task_a", "A missing publication date.")
    invalid_supplemental = supplemental_plan("gap_unknown")
    provider = make_provider(
        [initial_plan().model_dump_json(), invalid_supplemental.model_dump_json()],
        [],
        final_status=SupportStatus.PARTIAL,
    )
    worker = StubWorker({"task_a": partial_result("task_a", gap.question)})

    response = await SupportRuntime(provider, worker).run(
        "Please explain when the original post was published.", runtime()
    )

    assert response.status == SupportStatus.PARTIAL
    prompts = provider.models[ModelRole.PLANNER].prompts
    serialized = "\n".join("\n".join(prompt) for prompt in prompts)
    assert "Please explain when the original post was published." in serialized
    assert gap.gap_id in serialized
    assert worker.completed == ["task_a"]


@pytest.mark.asyncio
async def test_initial_plan_gap_claim_is_rejected_and_does_not_close_any_gap() -> None:
    forged = task("task_a").model_copy(update={"resolves_gap_ids": ["gap_forged"]})
    plan = TaskPlan(tasks=[forged], answer_strategy="Try to claim an unobserved gap.")
    provider = make_provider(
        [plan.model_dump_json(), "not a valid plan", "not a valid plan"],
        [],
        final_status=SupportStatus.PARTIAL,
    )
    worker = StubWorker({"task_a": success_result("task_a")})

    with pytest.raises(ValueError, match="initial tasks cannot reference gaps"):
        await SupportRuntime(provider, worker).run("Investigate the question.", runtime())


@pytest.mark.asyncio
async def test_repeated_description_from_different_tasks_keeps_gap_identity_isolated() -> None:
    repeated = "The publication date is not verified."
    first_gap = evidence_gap("task_a", repeated)
    plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Check a primary source.")
    supplement = SupplementalTaskPlan(
        tasks=[task("task_b").model_copy(update={"resolves_gap_ids": [first_gap.gap_id]})],
        answer_strategy="Recheck the missing fact.",
    )
    provider = make_provider(
        [plan.model_dump_json(), supplement.model_dump_json()],
        [assessment_for(first_gap.gap_id)],
    )
    second = success_result("task_b").model_copy(
        update={"status": WorkerStatus.PARTIAL, "gaps": [repeated]}
    )
    worker = StubWorker({"task_a": partial_result("task_a", repeated), "task_b": second})

    response = await SupportRuntime(provider, worker).run("When was it published?", runtime())

    assert response.status == SupportStatus.PARTIAL
    assert len(response.evidence_refs) == 2
    assert first_gap.gap_id != evidence_gap("task_b", repeated).gap_id


@pytest.mark.asyncio
async def test_hung_gap_assessment_times_out_and_final_partial_response_survives(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    gap = evidence_gap("task_a", "A missing detail.")
    provider = make_provider(
        [initial_plan().model_dump_json(), supplemental_plan(gap.gap_id).model_dump_json()],
        [],
        final_status=SupportStatus.PARTIAL,
    )
    composer_model = provider.models[ModelRole.COMPOSER]
    original_ainvoke = BaseChatModel.ainvoke
    calls = 0

    async def hang_once(
        model: BaseChatModel,
        input: LanguageModelInput,
        config: RunnableConfig | None = None,
        *,
        stop: list[str] | None = None,
        **kwargs: Any,
    ) -> AIMessage:
        nonlocal calls
        if model is composer_model:
            calls += 1
        if model is composer_model and calls == 1:
            await asyncio.sleep(5)
        return await original_ainvoke(model, input, config=config, stop=stop, **kwargs)

    monkeypatch.setattr(BaseChatModel, "ainvoke", hang_once)
    worker = StubWorker(
        {
            "task_a": partial_result("task_a", gap.question),
            "task_b": success_result("task_b"),
        }
    )
    request_runtime = runtime().model_copy(
        update={
            "budget": RunBudget(
                max_replans=1,
                soft_timeout_seconds=1,
                hard_timeout_seconds=5.5,
            )
        }
    )

    response = await asyncio.wait_for(
        SupportRuntime(provider, worker).run("Explain the detail.", request_runtime),
        timeout=3,
    )

    assert calls == 2
    assert response.status == SupportStatus.PARTIAL
    assert set(response.evidence_refs) == {"ev_task_a", "ev_task_b"}


@pytest.mark.asyncio
async def test_worker_forged_task_id_is_converted_to_failed_without_evidence() -> None:
    provider = make_provider(
        [initial_plan().model_dump_json()], [], final_status=SupportStatus.PARTIAL
    )
    worker = StubWorker({"task_a": success_result("different_task")})

    response = await SupportRuntime(provider, worker).run(
        "Investigate this question.", runtime(max_replans=0)
    )

    assert response.status == SupportStatus.PARTIAL
    assert response.evidence_refs == []

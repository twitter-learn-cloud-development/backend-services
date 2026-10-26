"""Intentional Chinese punctuation in user-facing fixture text."""

# ruff: noqa: RUF001

from datetime import UTC, datetime
from typing import Any, cast

import pytest
from langchain_core.messages import AIMessage
from pydantic import SecretStr

import app.runtime as runtime_module
from app.core.config import Settings
from app.domain.capability import CapabilitySpec
from app.domain.enums import EvidenceSourceType, ObservationStatus, SupportStatus, WorkerStatus
from app.domain.evidence import Evidence
from app.domain.response import SupportComposition
from app.domain.response_review import GoalDisposition, RequestGoal, ResponseReview, ReviewedGoal
from app.domain.runtime import AuthContext
from app.domain.task_plan import GuideTaskPlan, TaskSpec
from app.domain.worker_result import WorkerResult
from app.graph.supervisor import SupportRuntime
from app.integrations.model_provider import ModelProvider, ModelRole
from app.services.guide_policy import SITE_AI_HANDOFF_ACTION
from app.services.response_guard import ResponseGuard
from tests.graph.test_support_runtime import CapturingModelProvider, StubWorker, runtime, task


def guide_evidence() -> Evidence:
    return Evidence(
        evidence_id="ev_guide",
        source_type=EvidenceSourceType.KNOWLEDGE,
        source_ref="guide:workflow",
        observed_at=datetime(2026, 10, 1, tzinfo=UTC),
        safe_summary="Workflow 是可复用流程，创建后先保存再运行。",
    )


def success_guide_result() -> WorkerResult:
    return WorkerResult(
        task_id="guide_task",
        status=WorkerStatus.SUCCESS,
        conclusion="Guide retrieved.",
        evidence=[guide_evidence()],
        confidence=0.9,
    )


def supported(goal: RequestGoal) -> ReviewedGoal:
    return ReviewedGoal(
        **goal.model_dump(),
        disposition=GoalDisposition.SUPPORTED,
        evidence_refs=["ev_guide"],
        answer_text="Workflow 是可复用流程，创建后先保存再运行。",
    )


def review(*goals: ReviewedGoal) -> ResponseReview:
    return ResponseReview(
        all_request_goals_accounted_for=True,
        consistent_with_evidence=True,
        goals=list(goals),
    )


def provider(plan: GuideTaskPlan, reviewed: ResponseReview) -> CapturingModelProvider:
    draft = SupportComposition(status=SupportStatus.PARTIAL, answer="Draft only.")
    composer_responses = (
        [AIMessage(content=reviewed.model_dump_json())]
        if not plan.tasks
        else [
            AIMessage(content=draft.model_dump_json()),
            AIMessage(content=reviewed.model_dump_json()),
        ]
    )
    return CapturingModelProvider(
        {
            ModelRole.PLANNER: [AIMessage(content=plan.model_dump_json())],
            ModelRole.COMPOSER: composer_responses,
        }
    )


def guide_plan(*goals: RequestGoal, tasks: list[TaskSpec] | None = None) -> GuideTaskPlan:
    return GuideTaskPlan(
        tasks=tasks or [],
        answer_strategy="Answer guidance and route task goals appropriately.",
        request_goals=list(goals),
        guidance_required=bool(tasks),
    )


@pytest.mark.asyncio
async def test_empty_plan_with_complete_guide_goal_answers_without_handoff() -> None:
    goal = RequestGoal(goal_id="workflow", description="Workflow 使用方法")
    plan = guide_plan(goal, tasks=[task("guide_task")])
    models = provider(plan, review(supported(goal)))

    result = await SupportRuntime(
        models,
        StubWorker({"guide_task": success_guide_result()}),
        response_guard=ResponseGuard(guide_only=True),
    ).run("Workflow 如何创建和运行？", runtime(max_replans=0))

    assert result.status == SupportStatus.ANSWERED
    assert "先保存再运行" in result.answer
    assert result.suggested_actions == []
    assert models.models[ModelRole.COMPOSER].response_index == 2


@pytest.mark.asyncio
async def test_empty_plan_handoff_returns_only_fixed_runtime_action() -> None:
    goal = RequestGoal(goal_id="draft", description="创作一条推文")
    plan = guide_plan(goal)
    handoff_goal = ReviewedGoal(**goal.model_dump(), disposition=GoalDisposition.HANDOFF)
    models = provider(plan, review(handoff_goal))

    result = await SupportRuntime(
        models,
        StubWorker({}),
        response_guard=ResponseGuard(guide_only=True),
    ).run("帮我创作一条推文。", runtime(max_replans=0))

    assert result.status == SupportStatus.UNSUPPORTED
    assert result.suggested_actions == [SITE_AI_HANDOFF_ACTION]
    assert result.evidence_refs == []


@pytest.mark.asyncio
async def test_mixed_request_keeps_guide_and_hands_off_task_only() -> None:
    guide_goal = RequestGoal(goal_id="workflow", description="Workflow 使用方法")
    task_goal = RequestGoal(goal_id="draft", description="创作一条推文")
    plan = guide_plan(guide_goal, task_goal, tasks=[task("guide_task")])
    worker = StubWorker({"guide_task": success_guide_result()})
    reviewed = review(
        supported(guide_goal),
        ReviewedGoal(**task_goal.model_dump(), disposition=GoalDisposition.HANDOFF),
    )
    models = provider(plan, reviewed)

    result = await SupportRuntime(
        models,
        worker,
        response_guard=ResponseGuard(guide_only=True),
    ).run("Workflow 如何运行，并帮我写一条推文？", runtime(max_replans=0))

    assert result.status == SupportStatus.PARTIAL
    assert "先保存再运行" in result.answer
    assert result.suggested_actions == [SITE_AI_HANDOFF_ACTION]
    assert worker.completed == ["guide_task"]


@pytest.mark.asyncio
async def test_guide_retrieval_error_is_not_converted_to_handoff() -> None:
    goal = RequestGoal(goal_id="workflow", description="Workflow 故障说明")
    plan = guide_plan(goal, tasks=[task("guide_task")])
    failed = WorkerResult(
        task_id="guide_task",
        status=WorkerStatus.FAILED,
        conclusion="Retrieval failed.",
        confidence=0.0,
        gaps=["Published guide lookup failed."],
        execution_errors=[ObservationStatus.TEMPORARY_ERROR],
    )
    models = provider(
        plan,
        review(ReviewedGoal(**goal.model_dump(), disposition=GoalDisposition.HANDOFF)),
    )

    result = await SupportRuntime(
        models,
        StubWorker({"guide_task": failed}),
        response_guard=ResponseGuard(guide_only=True),
    ).run("Workflow 为什么失败？", runtime(max_replans=0))

    assert result.status == SupportStatus.PARTIAL
    assert result.suggested_actions == []
    assert "未能确认可用的说明或转交方式" in result.answer


@pytest.mark.asyncio
async def test_empty_plan_does_not_hide_omitted_declared_goal() -> None:
    declared = RequestGoal(goal_id="workflow", description="Workflow 用法")
    plan = guide_plan(declared)
    other = RequestGoal(goal_id="other", description="其他请求")
    models = provider(
        plan,
        review(ReviewedGoal(**other.model_dump(), disposition=GoalDisposition.HANDOFF)),
    )

    result = await SupportRuntime(
        models,
        StubWorker({}),
        response_guard=ResponseGuard(guide_only=True),
    ).run("Workflow 如何运行？", runtime(max_replans=0))

    assert result.status == SupportStatus.PARTIAL
    assert result.suggested_actions == []
    assert "未能确认可用的说明或转交方式" in result.answer


def test_official_builder_does_not_register_business_query_even_when_configured(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class NoOpWorker:
        async def run(self, *_args: Any, **_kwargs: Any) -> Any:
            raise AssertionError("builder test does not run a worker")

    def fake_provider(_settings: Settings) -> ModelProvider:
        return cast(ModelProvider, object())

    def fake_worker(*_args: Any, **_kwargs: Any) -> NoOpWorker:
        return NoOpWorker()

    monkeypatch.setattr(runtime_module, "OpenAICompatibleModelProvider", fake_provider)
    monkeypatch.setattr(runtime_module, "GenericWorker", fake_worker)
    settings = Settings(
        support_query_base_url="http://127.0.0.1:9638",
        support_query_jwt_secret=SecretStr("unit-test-secret-value"),
    )

    resources = runtime_module.build_support_runtime(settings)
    runner = vars(resources.runner)
    catalog = runner["_capability_catalog"]
    catalog_entries: list[CapabilitySpec] = catalog(
        AuthContext(actor_user_id="user-1", token_id="token-1")
    )

    assert isinstance(runner["_response_guard"], ResponseGuard)
    assert runner["_response_guard"].guide_only is True
    assert not any(item.name.startswith("content.") for item in catalog_entries)
    assert not any(item.name.startswith("support.") for item in catalog_entries)

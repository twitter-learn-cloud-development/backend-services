"""Intentional Chinese punctuation in user-facing fixture text."""

# ruff: noqa: RUF001

from datetime import UTC, datetime

import pytest
from pydantic import ValidationError

from app.domain.enums import EvidenceSourceType, ObservationStatus, SupportStatus, WorkerStatus
from app.domain.evidence import Evidence
from app.domain.response import AggregatedResult
from app.domain.response_review import GoalDisposition, RequestGoal, ResponseReview, ReviewedGoal
from app.domain.task_plan import GuideTaskPlan
from app.domain.worker_result import WorkerResult
from app.services.guide_policy import SITE_AI_HANDOFF_ACTION
from app.services.response_guard import ResponseGuard


def goal(goal_id: str, description: str) -> RequestGoal:
    return RequestGoal(goal_id=goal_id, description=description)


def guide_evidence() -> Evidence:
    return Evidence(
        evidence_id="ev_guide",
        source_type=EvidenceSourceType.KNOWLEDGE,
        source_ref="guide:workflow",
        observed_at=datetime(2026, 10, 1, tzinfo=UTC),
        safe_summary="创建 Workflow 后先保存，再运行。",
    )


def aggregate(evidence: list[Evidence] | None = None) -> AggregatedResult:
    return AggregatedResult(
        answerable=bool(evidence),
        conclusion="No model conclusion is trusted here.",
        supporting_evidence=evidence or [],
    )


def review(*items: ReviewedGoal) -> ResponseReview:
    return ResponseReview(
        all_request_goals_accounted_for=True,
        consistent_with_evidence=True,
        goals=list(items),
    )


def supported(item: RequestGoal) -> ReviewedGoal:
    return ReviewedGoal(
        **item.model_dump(),
        disposition=GoalDisposition.SUPPORTED,
        evidence_refs=["ev_guide"],
        answer_text="创建 Workflow 后先保存，再运行。",
    )


def handoff(item: RequestGoal) -> ReviewedGoal:
    return ReviewedGoal(**item.model_dump(), disposition=GoalDisposition.HANDOFF)


def test_guide_only_answer_uses_guide_evidence_without_handoff() -> None:
    item = goal("workflow", "Workflow 操作方法")

    result = ResponseGuard(guide_only=True).render(
        "Workflow 如何创建和运行？",
        [item],
        aggregate([guide_evidence()]),
        review(supported(item)),
        [],
        [],
    )

    assert result.status == SupportStatus.ANSWERED
    assert "先保存，再运行" in result.answer
    assert result.suggested_actions == []


def test_pure_task_handoff_is_runtime_owned_and_unsupported() -> None:
    item = goal("draft", "帮我创作一条推文")

    result = ResponseGuard(guide_only=True).render(
        "帮我创作一条推文。",
        [item],
        aggregate(),
        review(handoff(item)),
        [],
        [],
    )

    assert result.status == SupportStatus.UNSUPPORTED
    assert result.suggested_actions == [SITE_AI_HANDOFF_ACTION]
    assert "不会自动" not in result.answer  # no question text is transferred


def test_mixed_request_preserves_guide_answer_and_hands_off_only_task_goal() -> None:
    guide_goal = goal("workflow", "Workflow 使用方法")
    task_goal = goal("draft", "创作一条推文")

    result = ResponseGuard(guide_only=True).render(
        "Workflow 如何运行，并帮我写一条推文？",
        [guide_goal, task_goal],
        aggregate([guide_evidence()]),
        review(supported(guide_goal), handoff(task_goal)),
        [],
        [],
    )

    assert result.status == SupportStatus.PARTIAL
    assert "Workflow 使用方法" in result.answer
    assert "先保存，再运行" in result.answer
    assert "创作一条推文" in result.answer
    assert result.suggested_actions == [SITE_AI_HANDOFF_ACTION]


def test_guide_failure_cannot_be_rewritten_as_handoff() -> None:
    item = goal("workflow", "Workflow 故障说明")
    failed = WorkerResult(
        task_id="guide_task",
        status=WorkerStatus.FAILED,
        conclusion="Retrieval failed.",
        confidence=0.0,
        gaps=["Published guide lookup failed."],
        execution_errors=[ObservationStatus.TEMPORARY_ERROR],
    )

    with pytest.raises(ValueError, match="execution failure cannot be disguised"):
        ResponseGuard(guide_only=True).render(
            "Workflow 为什么失败？",
            [item],
            aggregate(),
            review(handoff(item)),
            [],
            [failed],
        )


def test_empty_guide_plan_requires_request_goals_and_explicit_no_guidance() -> None:
    with pytest.raises(ValidationError):
        GuideTaskPlan(
            tasks=[],
            answer_strategy="Review the request.",
            request_goals=[],
            guidance_required=False,
        )

    handoff_only = GuideTaskPlan(
        tasks=[],
        answer_strategy="Hand off the task-oriented request.",
        request_goals=[goal("draft", "创作一条推文")],
        guidance_required=False,
    )
    assert handoff_only.tasks == []

    with pytest.raises(ValidationError, match="guidance"):
        GuideTaskPlan(
            tasks=[],
            answer_strategy="Search for usage guidance.",
            request_goals=[goal("workflow", "Workflow 用法")],
            guidance_required=True,
        )

    declared = goal("workflow", "Workflow 用法")
    handoff_plan = GuideTaskPlan(
        tasks=[],
        answer_strategy="Account for each requested goal.",
        request_goals=[declared],
        guidance_required=False,
    )
    assert handoff_plan.guidance_required is False
    with pytest.raises(ValueError, match="omitted a declared goal"):
        ResponseGuard(guide_only=True).render(
            "Workflow 如何运行？",
            [declared],
            aggregate(),
            review(handoff(goal("different", "其他任务"))),
            [],
            [],
        )

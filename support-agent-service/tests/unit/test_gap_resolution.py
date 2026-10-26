import pytest
from pydantic import ValidationError

from app.domain.enums import WorkerStatus
from app.domain.gap import GapAssessment, GapResolution, evidence_gap
from app.domain.task_plan import SupplementalTaskPlan, TaskPlan
from app.domain.worker_result import WorkerResult
from app.services.evidence_aggregator import EvidenceAggregator
from tests.graph.test_support_runtime import success_result, task


def test_gap_id_is_stable_for_same_task_and_description_but_scoped_to_task() -> None:
    first = evidence_gap("task_a", "A missing publication date")
    repeated = evidence_gap("task_a", "A missing publication date")
    other_task = evidence_gap("task_b", "A missing publication date")
    other_gap = evidence_gap("task_a", "A different missing fact")

    assert first.gap_id == repeated.gap_id
    assert first.gap_id != other_task.gap_id
    assert first.gap_id != other_gap.gap_id
    assert first.task_id == "task_a"


def test_gap_id_is_independent_of_result_order() -> None:
    gaps = [
        evidence_gap(task_id, question)
        for task_id, question in [("task_a", "Missing date"), ("task_b", "Missing visibility")]
    ]
    reversed_gaps = [
        evidence_gap(task_id, question)
        for task_id, question in reversed(
            [("task_a", "Missing date"), ("task_b", "Missing visibility")]
        )
    ]

    assert {gap.gap_id for gap in gaps} == {gap.gap_id for gap in reversed_gaps}


def test_gap_resolution_requires_unique_nonempty_evidence_refs() -> None:
    with pytest.raises(ValidationError):
        GapResolution(
            gap_id="gap_1",
            task_id="task_b",
            evidence_refs=[],
            rationale="No evidence.",
        )
    with pytest.raises(ValidationError):
        GapResolution(
            gap_id="gap_1",
            task_id="task_b",
            evidence_refs=["ev_b", "ev_b"],
            rationale="Repeated evidence reference.",
        )


def test_gap_assessment_rejects_duplicate_gap_targets() -> None:
    resolution = GapResolution(
        gap_id="gap_1",
        task_id="task_b",
        evidence_refs=["ev_b"],
        rationale="Direct evidence addresses the missing field.",
    )
    with pytest.raises(ValidationError):
        GapAssessment(resolutions=[resolution, resolution])


def test_task_plan_rejects_duplicate_gap_assignments() -> None:
    first = task("task_b").model_copy(update={"resolves_gap_ids": ["gap_1"]})
    second = task("task_c").model_copy(update={"resolves_gap_ids": ["gap_1"]})
    with pytest.raises(ValidationError, match="gap targets must be unique"):
        TaskPlan(tasks=[first, second], answer_strategy="Assign each gap once.")


def test_aggregator_closes_only_requested_task_scoped_gap() -> None:
    failed = WorkerResult(
        task_id="task_a",
        status=WorkerStatus.PARTIAL,
        conclusion="The first check left a date gap.",
        confidence=0.6,
        gaps=["A missing publication date"],
    )
    unrelated = success_result("task_b")
    gap = evidence_gap("task_a", "A missing publication date")

    before = EvidenceAggregator().aggregate([failed, unrelated])
    after = EvidenceAggregator().aggregate([failed, unrelated], resolved_gap_ids={gap.gap_id})

    assert before.unresolved_gaps == [gap]
    assert before.answerable is False
    assert after.unresolved_gaps == []
    assert after.resolved_gap_ids == [gap.gap_id]
    assert after.answerable is True


def test_aggregator_preserves_same_description_as_distinct_task_gaps() -> None:
    first = WorkerResult(
        task_id="task_a",
        status=WorkerStatus.PARTIAL,
        conclusion="First check incomplete.",
        confidence=0.5,
        gaps=["A missing publication date"],
    )
    second = WorkerResult(
        task_id="task_b",
        status=WorkerStatus.PARTIAL,
        conclusion="Second check incomplete.",
        confidence=0.5,
        gaps=["A missing publication date"],
    )
    first_gap = evidence_gap("task_a", "A missing publication date")

    result = EvidenceAggregator().aggregate([first, second], resolved_gap_ids={first_gap.gap_id})

    assert result.resolved_gap_ids == [first_gap.gap_id]
    assert [(gap.task_id, gap.question) for gap in result.unresolved_gaps] == [
        ("task_b", "A missing publication date")
    ]
    assert result.answerable is False


def test_supplemental_plan_schema_allows_no_tasks_but_still_validates_targets() -> None:
    empty = SupplementalTaskPlan(tasks=[], answer_strategy="No safe check can resolve it.")
    assert empty.tasks == []

    repeated = task("task_b").model_copy(update={"resolves_gap_ids": ["gap_x", "gap_x"]})
    with pytest.raises(ValidationError):
        SupplementalTaskPlan(tasks=[repeated], answer_strategy="Duplicate target is invalid.")

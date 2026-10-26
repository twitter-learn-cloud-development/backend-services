import pytest
from pydantic import ValidationError

from app.domain.task_plan import SupplementalTaskPlan, TaskPlan, TaskSpec


def task(task_id: str, *, depends_on: list[str] | None = None) -> TaskSpec:
    return TaskSpec(
        task_id=task_id,
        objective=f"investigate {task_id}",
        required_evidence=["verified observation"],
        depends_on=depends_on or [],
        success_criteria=["evidence collected"],
    )


def test_task_plan_accepts_valid_dependency_graph() -> None:
    plan = TaskPlan(
        tasks=[task("task_a"), task("task_b", depends_on=["task_a"])],
        answer_strategy="combine verified evidence",
    )

    assert len(plan.tasks) == 2


def test_task_plan_rejects_duplicate_ids() -> None:
    with pytest.raises(ValidationError, match="must be unique"):
        TaskPlan(tasks=[task("task_a"), task("task_a")], answer_strategy="answer")


def test_task_plan_rejects_missing_dependency() -> None:
    with pytest.raises(ValidationError, match="unknown tasks"):
        TaskPlan(
            tasks=[task("task_a", depends_on=["missing"])],
            answer_strategy="answer",
        )


def test_task_plan_rejects_dependency_cycle() -> None:
    with pytest.raises(ValidationError, match="must not contain a cycle"):
        TaskPlan(
            tasks=[
                task("task_a", depends_on=["task_b"]),
                task("task_b", depends_on=["task_a"]),
            ],
            answer_strategy="answer",
        )


def test_only_optional_supplemental_plan_can_have_no_further_tasks() -> None:
    plan = SupplementalTaskPlan(tasks=[], answer_strategy="State the remaining limitations.")
    assert not plan.tasks
    with pytest.raises(ValidationError):
        TaskPlan(tasks=[], answer_strategy="Initial plan cannot be empty.")


def test_supplemental_plan_still_validates_dependencies() -> None:
    with pytest.raises(ValidationError, match="unknown tasks"):
        SupplementalTaskPlan(
            tasks=[task("task-a", depends_on=["missing"])], answer_strategy="answer"
        )

from pydantic import Field, field_validator, model_validator

from app.domain.base import DomainModel
from app.domain.response_review import RequestGoal
from app.domain.types import Identifier, NonEmptyText, ShortText


def _unique_texts(values: list[str]) -> list[str]:
    if len(values) != len(set(values)):
        raise ValueError("list values must be unique")
    return values


class TaskSpec(DomainModel):
    task_id: Identifier
    objective: NonEmptyText
    required_evidence: list[ShortText] = Field(min_length=1, max_length=10)
    required_context: list[Identifier] = Field(default_factory=list, max_length=20)
    depends_on: list[Identifier] = Field(default_factory=list, max_length=10)
    resolves_gap_ids: list[Identifier] = Field(default_factory=list, max_length=20)
    success_criteria: list[ShortText] = Field(min_length=1, max_length=10)

    @field_validator(
        "required_evidence",
        "required_context",
        "depends_on",
        "success_criteria",
        "resolves_gap_ids",
    )
    @classmethod
    def validate_unique_lists(cls, value: list[str]) -> list[str]:
        return _unique_texts(value)

    @model_validator(mode="after")
    def validate_no_self_dependency(self) -> "TaskSpec":
        if self.task_id in self.depends_on:
            raise ValueError("task cannot depend on itself")
        return self


class TaskPlan(DomainModel):
    tasks: list[TaskSpec] = Field(min_length=1, max_length=5)
    answer_strategy: NonEmptyText
    request_goals: list[RequestGoal] = Field(default_factory=list[RequestGoal], max_length=12)

    @model_validator(mode="after")
    def validate_task_graph(self) -> "TaskPlan":
        if len({goal.goal_id for goal in self.request_goals}) != len(self.request_goals):
            raise ValueError("request goal ids must be unique")
        task_ids = [task.task_id for task in self.tasks]
        if len(task_ids) != len(set(task_ids)):
            raise ValueError("task_id values must be unique")
        targets = [gap_id for task in self.tasks for gap_id in task.resolves_gap_ids]
        if len(targets) != len(set(targets)):
            raise ValueError("gap targets must be unique across tasks")

        known_ids = set(task_ids)
        for task in self.tasks:
            missing = set(task.depends_on) - known_ids
            if missing:
                raise ValueError(f"task {task.task_id} depends on unknown tasks: {sorted(missing)}")

        dependencies = {task.task_id: set(task.depends_on) for task in self.tasks}
        visiting: set[str] = set()
        visited: set[str] = set()

        def visit(task_id: str) -> None:
            if task_id in visiting:
                raise ValueError("task dependencies must not contain a cycle")
            if task_id in visited:
                return
            visiting.add(task_id)
            for dependency in dependencies[task_id]:
                visit(dependency)
            visiting.remove(task_id)
            visited.add(task_id)

        for task_id in task_ids:
            visit(task_id)
        return self


class SupplementalTaskPlan(TaskPlan):
    """An optional replan may explicitly report no further permitted checks."""

    tasks: list[TaskSpec] = Field(min_length=0, max_length=5)


class GuideTaskPlan(TaskPlan):
    """A guide may have no evidence tasks when the request belongs in another product role."""

    tasks: list[TaskSpec] = Field(min_length=0, max_length=5)
    request_goals: list[RequestGoal] = Field(min_length=1, max_length=12)
    guidance_required: bool = Field(
        description=(
            "True if ANY goal asks for product usage, features, rules or operating steps, "
            "including mixed requests. Such goals require at least one guide evidence task. "
            "False only if the entire request is outside the usage-guide role."
        )
    )

    @model_validator(mode="after")
    def guidance_requires_evidence_tasks(self) -> "GuideTaskPlan":
        if self.guidance_required and not self.tasks:
            raise ValueError("guidance goals require at least one published-guide evidence task")
        return self

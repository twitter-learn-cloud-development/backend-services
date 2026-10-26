import pytest
from langchain_core.messages import AIMessage

from app.domain.enums import SupportStatus
from app.domain.response import SupportComposition
from app.domain.response_review import GoalDisposition, RequestGoal, ResponseReview, ReviewedGoal
from app.domain.runtime import RunBudget
from app.domain.task_plan import TaskPlan
from app.graph.supervisor import SupportRuntime
from app.integrations.model_provider import ModelRole
from app.services.response_guard import ResponseGuard
from tests.graph.test_support_runtime import (
    CapturingModelProvider,
    StubWorker,
    runtime,
    success_result,
    task,
)


@pytest.mark.asyncio
@pytest.mark.parametrize("invalid_kind", ["budget", "initial_gap_reference"])
@pytest.mark.parametrize("repair_succeeds", [True, False])
async def test_guarded_initial_context_validation_is_repaired_or_rejected_without_dispatch(
    invalid_kind: str, repair_succeeds: bool
) -> None:
    goal = RequestGoal(goal_id="main", description="Explain the scoped evidence")
    valid = TaskPlan(
        tasks=[task("task_a")], answer_strategy="Use actual evidence.", request_goals=[goal]
    )
    invalid = (
        valid.model_copy(update={"tasks": [task("task_a"), task("task_b"), task("task_c")]})
        if invalid_kind == "budget"
        else valid.model_copy(
            update={
                "tasks": [task("task_a").model_copy(update={"resolves_gap_ids": ["gap_unknown"]})]
            }
        )
    )
    reviewed = ReviewedGoal(
        **goal.model_dump(),
        disposition=(
            GoalDisposition.SUPPORTED if repair_succeeds else GoalDisposition.CAPABILITY_UNAVAILABLE
        ),
        evidence_refs=["ev_task_a"] if repair_succeeds else [],
        answer_text="The verified source supports this fact." if repair_succeeds else None,
    )
    review = ResponseReview(
        all_request_goals_accounted_for=True, consistent_with_evidence=True, goals=[reviewed]
    )
    models = CapturingModelProvider(
        {
            ModelRole.PLANNER: [
                AIMessage(content=invalid.model_dump_json()),
                AIMessage(content=(valid if repair_succeeds else invalid).model_dump_json()),
            ],
            ModelRole.COMPOSER: [
                AIMessage(
                    content=SupportComposition(
                        status=SupportStatus.PARTIAL, answer="Unverified draft."
                    ).model_dump_json()
                ),
                AIMessage(content=review.model_dump_json()),
            ],
        }
    )
    worker = StubWorker({"task_a": success_result("task_a")})
    request_runtime = runtime().model_copy(update={"budget": RunBudget(max_tasks=2)})
    result = await SupportRuntime(models, worker, response_guard=ResponseGuard()).run(
        "Explain the scoped evidence.", request_runtime
    )
    assert len(models.models[ModelRole.PLANNER].prompts) == 2
    assert worker.completed == (["task_a"] if repair_succeeds else [])
    assert result.status == (SupportStatus.ANSWERED if repair_succeeds else SupportStatus.PARTIAL)
    assert result.evidence_refs == (["ev_task_a"] if repair_succeeds else [])
    if not repair_succeeds:
        assert "did not complete" in result.answer

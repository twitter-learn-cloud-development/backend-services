from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any, cast

import pytest
from langchain_core.language_models.base import LanguageModelInput
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage
from langchain_core.runnables import RunnableConfig

import app.runtime as runtime_module
from app.core.config import Settings
from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.conversation import ConversationContext
from app.domain.enums import ObservationStatus, SupportStatus, WorkerStatus
from app.domain.response import SupportComposition
from app.domain.response_review import (
    GoalDisposition,
    RequestGoal,
    ResponseReview,
    ReviewedGoal,
)
from app.domain.runtime import RuntimeContext
from app.domain.task_plan import TaskPlan, TaskSpec
from app.domain.worker_result import WorkerCompletion, WorkerResult
from app.graph.supervisor import SupportRuntime
from app.graph.worker import GenericWorker
from app.integrations.model_provider import FakeModelProvider, ModelProvider, ModelRole
from app.services.response_guard import ResponseGuard
from app.tools.executor import CapabilityExecutor, TemporaryCapabilityError
from app.tools.registry import CapabilityRegistry
from app.tools.resolver import CapabilityResolver
from tests.graph.test_support_runtime import (
    CapturingModelProvider,
    StubWorker,
    runtime,
    success_result,
    task,
)


def capability() -> CapabilitySpec:
    return CapabilitySpec(
        name="content.list_recent",
        description="Read recent content for the authenticated user.",
        input_schema={"type": "object", "properties": {}, "required": []},
        output_schema={"type": "object"},
        user_data_scope="authenticated_user",
        evidence_types=["business_api"],
    )


def compose(answer: str = "Drafted response may be incomplete.") -> SupportComposition:
    return SupportComposition(status=SupportStatus.ANSWERED, answer=answer)


def review_for(goal: RequestGoal, evidence_ref: str = "ev_task_a") -> ResponseReview:
    return ResponseReview(
        all_request_goals_accounted_for=True,
        consistent_with_evidence=True,
        goals=[
            ReviewedGoal(
                **goal.model_dump(),
                disposition=GoalDisposition.SUPPORTED,
                evidence_refs=[evidence_ref],
                answer_text="The source explains how to list recent posts.",
            )
        ],
    )


def provider(plan: TaskPlan, *, goals: list[RequestGoal] | None = None) -> CapturingModelProvider:
    initial_plan = plan.model_copy(update={"request_goals": goals or []})
    draft = compose()
    semantic_review = review_for(
        (goals or [RequestGoal(goal_id="main", description="the request")])[0]
    )
    return CapturingModelProvider(
        {
            ModelRole.PLANNER: [AIMessage(content=initial_plan.model_dump_json())],
            ModelRole.COMPOSER: [
                AIMessage(content=draft.model_dump_json()),
                AIMessage(content=semantic_review.model_dump_json()),
            ],
        }
    )


@pytest.mark.asyncio
async def test_guarded_graph_uses_reviewer_for_final_status_and_answer() -> None:
    goal = RequestGoal(goal_id="posts", description="List recent posts")
    models = provider(
        TaskPlan(tasks=[task("task_a")], answer_strategy="Check records."), goals=[goal]
    )
    request_runtime = runtime().model_copy(
        update={
            "conversation_context": ConversationContext(summary="Earlier topic: account posts.")
        }
    )
    result = await SupportRuntime(
        models,
        StubWorker({"task_a": success_result("task_a")}),
        response_guard=ResponseGuard(),
        capability_catalog=lambda _auth: [capability()],
    ).run("List my recent posts.", request_runtime)

    assert result.status == SupportStatus.ANSWERED
    assert "source explains how to list recent posts" in result.answer
    assert "Drafted response" not in result.answer
    review_prompt = "\n".join(models.models[ModelRole.COMPOSER].prompts[1])
    assert "List my recent posts." in review_prompt
    assert "List recent posts" in review_prompt
    assert "Drafted response may be incomplete." in review_prompt
    assert "content.list_recent" in review_prompt
    assert "ev_task_a" in review_prompt
    assert "Earlier topic: account posts." in review_prompt


@pytest.mark.asyncio
async def test_declared_goal_cannot_be_omitted_by_reviewer_and_fallback_is_grounded() -> None:
    first = RequestGoal(goal_id="posts", description="Recent posts")
    second = RequestGoal(goal_id="workflow", description="Workflow guide")
    models = provider(
        TaskPlan(tasks=[task("task_a")], answer_strategy="Check posts."), goals=[first, second]
    )
    incomplete_review = ResponseReview(
        all_request_goals_accounted_for=True,
        consistent_with_evidence=True,
        goals=[
            ReviewedGoal(
                **first.model_dump(),
                disposition=GoalDisposition.SUPPORTED,
                evidence_refs=["ev_task_a"],
                answer_text="The records show recent posts for this account.",
            )
        ],
    )
    models.models[ModelRole.COMPOSER].responses[1] = AIMessage(
        content=incomplete_review.model_dump_json()
    )
    result = await SupportRuntime(
        models,
        StubWorker({"task_a": success_result("task_a")}),
        response_guard=ResponseGuard(),
    ).run("List posts and explain Workflow.", runtime())

    assert result.status == SupportStatus.PARTIAL
    assert "Drafted response" not in result.answer
    assert "Worker-authored conclusion" not in result.answer
    assert "Verified evidence for task_a" in result.answer
    assert "List posts and explain Workflow." in "\n".join(
        models.models[ModelRole.COMPOSER].prompts[1]
    )


@pytest.mark.asyncio
async def test_production_runtime_builder_always_injects_response_guard(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class NoOpWorker:
        async def run(self, *_args: Any, **_kwargs: Any) -> Any:
            raise AssertionError("builder test never runs a worker")

    def fake_provider(_settings: Settings) -> ModelProvider:
        return cast(ModelProvider, object())

    def fake_worker(*_args: Any, **_kwargs: Any) -> NoOpWorker:
        return NoOpWorker()

    monkeypatch.setattr(runtime_module, "OpenAICompatibleModelProvider", fake_provider)
    monkeypatch.setattr(runtime_module, "GenericWorker", fake_worker)
    settings = SimpleNamespace(
        support_query_base_url="",
        support_query_jwt_secret=None,
        qdrant_url="",
    )

    resources = runtime_module.build_support_runtime(settings)  # type: ignore[arg-type]

    assert isinstance(vars(resources.runner)["_response_guard"], ResponseGuard)


@pytest.mark.asyncio
async def test_draft_composer_exception_uses_grounded_fallback_not_service_error(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    goal = RequestGoal(goal_id="posts", description="Recent posts")
    models = provider(
        TaskPlan(tasks=[task("task_a")], answer_strategy="Check records."), goals=[goal]
    )
    composer_model = models.models[ModelRole.COMPOSER]
    original_ainvoke = BaseChatModel.ainvoke
    calls = 0

    async def fail_first_composer(
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
            if calls == 1:
                raise RuntimeError("fixture composer outage")
        return await original_ainvoke(model, input, config=config, stop=stop, **kwargs)

    monkeypatch.setattr(BaseChatModel, "ainvoke", fail_first_composer)
    result = await SupportRuntime(
        models,
        StubWorker({"task_a": success_result("task_a")}),
        response_guard=ResponseGuard(),
    ).run("List my recent posts.", runtime())

    assert calls == 1
    assert result.status == SupportStatus.PARTIAL
    assert "Verified evidence for task_a" in result.answer
    assert "Drafted response" not in result.answer
    assert "503" not in result.answer


@pytest.mark.asyncio
async def test_draft_composer_timeout_uses_grounded_fallback_not_service_error(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    goal = RequestGoal(goal_id="posts", description="Recent posts")
    models = provider(
        TaskPlan(tasks=[task("task_a")], answer_strategy="Check records."), goals=[goal]
    )
    composer_model = models.models[ModelRole.COMPOSER]
    original_ainvoke = BaseChatModel.ainvoke
    original_timeout = asyncio.timeout
    calls = 0

    def short_timeout(delay: float):
        return original_timeout(min(delay, 0.01))

    async def hang_first_composer(
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
            if calls == 1:
                await asyncio.sleep(1)
        return await original_ainvoke(model, input, config=config, stop=stop, **kwargs)

    monkeypatch.setattr(BaseChatModel, "ainvoke", hang_first_composer)
    monkeypatch.setattr(asyncio, "timeout", short_timeout)
    result = await SupportRuntime(
        models,
        StubWorker({"task_a": success_result("task_a")}),
        response_guard=ResponseGuard(),
    ).run("List my recent posts.", runtime())

    assert calls == 1
    assert result.status == SupportStatus.PARTIAL
    assert "Verified evidence for task_a" in result.answer
    assert "Drafted response" not in result.answer
    assert "503" not in result.answer


@pytest.mark.asyncio
async def test_real_worker_executor_temporary_failure_is_partial_not_clarification() -> None:
    goal = RequestGoal(goal_id="posts", description="Find recent posts")
    plan = TaskPlan(
        tasks=[task("task_a")],
        answer_strategy="Query the authenticated recent-content capability.",
        request_goals=[goal],
    )
    planner_composer_provider = CapturingModelProvider(
        {
            ModelRole.PLANNER: [AIMessage(content=plan.model_dump_json())],
            ModelRole.COMPOSER: [
                AIMessage(content=compose().model_dump_json()),
                AIMessage(
                    content=ResponseReview(
                        all_request_goals_accounted_for=True,
                        consistent_with_evidence=True,
                        goals=[
                            ReviewedGoal(
                                **goal.model_dump(),
                                disposition=GoalDisposition.MISSING_USER_INPUT,
                                capability_name="content.list_recent",
                                missing_fields=["tweet_id"],
                                clarification_question="Which post ID should I query?",
                                input_resolution_reason="The capability requires a post ID.",
                            )
                        ],
                    ).model_dump_json()
                ),
            ],
        }
    )
    registry = CapabilityRegistry()
    spec = CapabilitySpec(
        name="content.list_recent",
        description="Fetch recent content.",
        input_schema={"type": "object", "properties": {}, "required": []},
        output_schema={"type": "object"},
        user_data_scope="self",
        evidence_types=["business_api"],
    )

    async def fail_temporarily(
        call: CapabilityCall, _runtime: RuntimeContext
    ) -> CapabilityObservation:
        raise TemporaryCapabilityError("fixture_upstream_unavailable")

    registry.register(spec, fail_temporarily)
    actual_worker = GenericWorker(
        FakeModelProvider(
            {
                ModelRole.WORKER: [
                    AIMessage(
                        content="",
                        tool_calls=[
                            {
                                "name": "content__list_recent",
                                "args": {},
                                "id": "call_temp_failure",
                                "type": "tool_call",
                            }
                        ],
                    ),
                    AIMessage(
                        content=WorkerCompletion(
                            status=WorkerStatus.PARTIAL,
                            conclusion="The query was not completed.",
                            confidence=0.2,
                            gaps=["The recent-content service was temporarily unavailable."],
                        ).model_dump_json()
                    ),
                ]
            }
        ),
        CapabilityResolver(registry),
        CapabilityExecutor(registry, max_retries=0),
    )

    class RecordingWorker:
        result: WorkerResult | None = None

        async def run(self, task: TaskSpec, runtime: RuntimeContext) -> WorkerResult:
            self.result = await actual_worker.run(task, runtime)
            return self.result

    recording_worker = RecordingWorker()

    result = await SupportRuntime(
        planner_composer_provider,
        recording_worker,
        response_guard=ResponseGuard(),
        capability_catalog=registry.list_allowed,
    ).run("Find my recent posts.", runtime(max_replans=0))

    assert result.status == SupportStatus.PARTIAL
    assert "Which post ID should I query?" not in result.answer
    assert "whole request is not confirmed" in result.answer
    assert "fixture_upstream_unavailable" not in result.answer
    assert recording_worker.result is not None
    assert recording_worker.result.execution_errors == [ObservationStatus.TEMPORARY_ERROR]
    assert recording_worker.result.missing_inputs == []

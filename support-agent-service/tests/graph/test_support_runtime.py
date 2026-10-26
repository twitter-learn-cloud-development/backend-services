import asyncio
from collections.abc import Callable, Sequence
from datetime import UTC, datetime
from typing import Any, override
from uuid import uuid4

import pytest
from langchain_core.callbacks import CallbackManagerForLLMRun
from langchain_core.language_models.base import LanguageModelInput
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langchain_core.runnables import Runnable
from langchain_core.tools import BaseTool
from pydantic import Field

from app.domain.enums import EvidenceSourceType, SupportStatus, WorkerStatus
from app.domain.evidence import Evidence
from app.domain.response import SupportComposition
from app.domain.runtime import AuthContext, RunBudget, RuntimeContext
from app.domain.task_plan import TaskPlan, TaskSpec
from app.domain.worker_result import WorkerResult
from app.graph.supervisor import SupportRuntime
from app.integrations.model_provider import FakeModelProvider, ModelRole
from app.tools.mock import create_mock_registry


def runtime(*, max_replans: int = 1) -> RuntimeContext:
    return RuntimeContext(
        auth=AuthContext(actor_user_id="user-1", token_id="token-1"),
        request_id=uuid4(),
        conversation_id=uuid4(),
        case_id=uuid4(),
        trace_id="trace-1",
        budget=RunBudget(max_replans=max_replans),
    )


def task(task_id: str, *, depends_on: list[str] | None = None) -> TaskSpec:
    return TaskSpec(
        task_id=task_id,
        objective=f"Investigate {task_id}",
        required_evidence=[f"Evidence for {task_id}"],
        depends_on=depends_on or [],
        success_criteria=[f"{task_id} is verified"],
    )


def success_result(task_id: str) -> WorkerResult:
    return WorkerResult(
        task_id=task_id,
        status=WorkerStatus.SUCCESS,
        conclusion=f"{task_id} completed.",
        evidence=[
            Evidence(
                evidence_id=f"ev_{task_id}",
                source_type=EvidenceSourceType.BUSINESS_API,
                source_ref=f"resource:{task_id}",
                observed_at=datetime.now(UTC),
                safe_summary=f"Verified evidence for {task_id}.",
            )
        ],
        confidence=0.9,
    )


class StubWorker:
    def __init__(self, results: dict[str, WorkerResult]) -> None:
        self._results = results
        self.active = 0
        self.max_active = 0
        self.completed: list[str] = []

    async def run(self, task: TaskSpec, runtime: RuntimeContext) -> WorkerResult:
        del runtime
        self.active += 1
        self.max_active = max(self.max_active, self.active)
        await asyncio.sleep(0.01)
        self.active -= 1
        self.completed.append(task.task_id)
        return self._results[task.task_id]


@pytest.mark.asyncio
@pytest.mark.parametrize("supplemental", ["empty", "invalid", "duplicate"])
async def test_optional_replan_cannot_discard_verified_evidence(supplemental: str) -> None:
    initial = TaskPlan(tasks=[task("task_a")], answer_strategy="Use verified guidance.")
    partial = success_result("task_a").model_copy(
        update={
            "status": WorkerStatus.PARTIAL,
            "gaps": ["No further supported check can verify this detail."],
        }
    )
    extra = (
        [AIMessage(content='{"tasks":[],"answer_strategy":"Explain limitations."}')]
        if supplemental == "empty"
        else [AIMessage(content="invalid"), AIMessage(content="still invalid")]
        if supplemental == "invalid"
        else [AIMessage(content=initial.model_dump_json())]
    )
    worker = StubWorker({"task_a": partial})
    response = await SupportRuntime(
        provider([AIMessage(content=initial.model_dump_json()), *extra]), worker
    ).run("Explain Workflow", runtime(max_replans=2))
    assert response.status == SupportStatus.PARTIAL
    assert response.evidence_refs == ["ev_task_a"]
    assert worker.completed == ["task_a"]


@pytest.mark.asyncio
async def test_multiple_workflow_chunks_and_query_scores_do_not_trigger_replanning() -> None:
    from tests.unit.test_evidence_aggregator import knowledge, knowledge_result

    initial = TaskPlan(
        tasks=[task("definition"), task("operations")],
        answer_strategy="Combine the published guide.",
    )
    worker = StubWorker(
        {
            "definition": knowledge_result(
                "definition", [knowledge("chunk-1", "Workflow connects steps.", score=0.9)]
            ),
            "operations": knowledge_result(
                "operations",
                [
                    knowledge("chunk-1", "Workflow connects steps.", score=0.5),
                    knowledge("chunk-2", "Save before running."),
                ],
            ),
        }
    )
    response = await SupportRuntime(
        provider([AIMessage(content=initial.model_dump_json())]), worker
    ).run("Workflow 是什么, 如何创建和运行?", runtime())
    assert response.status == SupportStatus.ANSWERED
    assert set(response.evidence_refs) == {"ev_knowledge_chunk-1", "ev_knowledge_chunk-2"}


def provider(
    planner_responses: list[AIMessage],
    *,
    status: SupportStatus = SupportStatus.ANSWERED,
) -> FakeModelProvider:
    composition = SupportComposition(
        status=status,
        answer="A user-facing answer based on verified evidence.",
        suggested_actions=["Refresh and check again."],
    )
    return FakeModelProvider(
        {
            ModelRole.PLANNER: planner_responses,
            ModelRole.COMPOSER: [AIMessage(content=composition.model_dump_json())],
        }
    )


class CapturingChatModel(BaseChatModel):
    responses: list[AIMessage] = Field(min_length=1)
    prompts: list[list[str]] = Field(default_factory=lambda: list[list[str]]())
    response_index: int = 0

    @property
    @override
    def _llm_type(self) -> str:
        return "capturing-scripted-chat-model"

    @override
    def _generate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        del stop, run_manager, kwargs
        prompt: list[str] = [str(message.content) for message in messages]
        self.prompts.append(prompt)
        if self.response_index >= len(self.responses):
            raise RuntimeError("capturing chat model has no remaining responses")
        response = self.responses[self.response_index]
        self.response_index += 1
        return ChatResult(generations=[ChatGeneration(message=response)])

    @override
    def bind_tools(
        self,
        tools: Sequence[dict[str, Any] | type | Callable[..., Any] | BaseTool],
        *,
        tool_choice: str | None = None,
        **kwargs: Any,
    ) -> Runnable[LanguageModelInput, AIMessage]:
        del tools, tool_choice, kwargs
        return self


class CapturingModelProvider:
    models: dict[ModelRole, CapturingChatModel]

    def __init__(self, responses: dict[ModelRole, list[AIMessage]]) -> None:
        self.models = {
            role: CapturingChatModel(responses=role_responses)
            for role, role_responses in responses.items()
        }

    def get_chat_model(self, role: ModelRole) -> BaseChatModel:
        return self.models[role]


def capturing_provider(
    planner_responses: list[AIMessage],
    *,
    status: SupportStatus = SupportStatus.ANSWERED,
) -> CapturingModelProvider:
    composition = SupportComposition(
        status=status,
        answer="A user-facing answer based on verified evidence.",
        suggested_actions=["Refresh and check again."],
    )
    return CapturingModelProvider(
        {
            ModelRole.PLANNER: planner_responses,
            ModelRole.COMPOSER: [AIMessage(content=composition.model_dump_json())],
        }
    )


@pytest.mark.asyncio
async def test_supervisor_runs_independent_tasks_in_parallel_then_dependency() -> None:
    plan = TaskPlan(
        tasks=[task("task_a"), task("task_b"), task("task_c", depends_on=["task_a"])],
        answer_strategy="Combine verified evidence.",
    )
    worker = StubWorker(
        {task_id: success_result(task_id) for task_id in ["task_a", "task_b", "task_c"]}
    )
    support_runtime = SupportRuntime(
        provider([AIMessage(content=plan.model_dump_json())]),
        worker,
    )

    response = await support_runtime.run("Investigate a compound issue", runtime())

    assert response.status == SupportStatus.ANSWERED
    assert set(response.evidence_refs) == {"ev_task_a", "ev_task_b", "ev_task_c"}
    assert worker.max_active == 2
    assert worker.completed[-1] == "task_c"


@pytest.mark.asyncio
async def test_planner_gets_one_repair_attempt_for_invalid_json() -> None:
    valid_plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Use evidence.")
    worker = StubWorker({"task_a": success_result("task_a")})
    support_runtime = SupportRuntime(
        provider(
            [
                AIMessage(content="not-json"),
                AIMessage(content=valid_plan.model_dump_json()),
            ]
        ),
        worker,
    )

    response = await support_runtime.run("Investigate", runtime())

    assert response.status == SupportStatus.ANSWERED
    assert response.evidence_refs == ["ev_task_a"]


@pytest.mark.asyncio
async def test_planner_and_replanner_receive_allowed_capability_catalog() -> None:
    initial_plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Use available evidence.")
    supplemental_plan = TaskPlan(tasks=[task("task_b")], answer_strategy="Fill the gap.")
    partial = WorkerResult(
        task_id="task_a",
        status=WorkerStatus.PARTIAL,
        conclusion="The first task left a gap.",
        confidence=0.5,
        gaps=["A second source is required."],
    )
    worker = StubWorker({"task_a": partial, "task_b": success_result("task_b")})
    registry = create_mock_registry()
    model_provider = capturing_provider(
        [
            AIMessage(content=initial_plan.model_dump_json()),
            AIMessage(content=supplemental_plan.model_dump_json()),
        ],
        status=SupportStatus.PARTIAL,
    )
    support_runtime = SupportRuntime(
        model_provider,
        worker,
        capability_catalog=registry.list_allowed,
    )

    response = await support_runtime.run("Investigate", runtime(max_replans=1))

    assert response.status == SupportStatus.PARTIAL
    planner_prompts = model_provider.models[ModelRole.PLANNER].prompts
    assert len(planner_prompts) == 2
    for prompt in planner_prompts:
        prompt_text = "\n".join(prompt)
        assert "content.list_recent" in prompt_text
        assert "List recent content visible to the authenticated user." in prompt_text
        assert "internal.read_restricted_summary" not in prompt_text


@pytest.mark.asyncio
async def test_composer_sees_original_question_and_gaps_cannot_become_answered() -> None:
    plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Use the verified evidence.")
    partial = success_result("task_a").model_copy(
        update={"status": WorkerStatus.PARTIAL, "gaps": ["A second source is still needed."]}
    )
    worker = StubWorker({"task_a": partial})
    model_provider = capturing_provider(
        [AIMessage(content=plan.model_dump_json())],
        status=SupportStatus.ANSWERED,
    )
    support_runtime = SupportRuntime(model_provider, worker)
    original_question = "Why is my latest post not visible to other users?"

    response = await support_runtime.run(original_question, runtime(max_replans=0))

    assert response.status == SupportStatus.PARTIAL
    assert response.evidence_refs == ["ev_task_a"]
    composer_prompts = model_provider.models[ModelRole.COMPOSER].prompts
    assert len(composer_prompts) == 1
    composer_prompt = "\n".join(composer_prompts[0])
    assert original_question in composer_prompt
    assert "A second source is still needed." in composer_prompt
    assert "Verified evidence for task_a." in composer_prompt


@pytest.mark.asyncio
async def test_composer_renders_only_verified_recent_record_facts() -> None:
    plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Summarize recent records.")
    evidence = Evidence(
        evidence_id="ev_recent_grounded",
        source_type=EvidenceSourceType.BUSINESS_API,
        source_ref="content:authenticated-user:recent",
        observed_at=datetime(2026, 9, 28, tzinfo=UTC),
        safe_summary="The authenticated user has one recent public content item.",
        attributes={
            "verified_records": [
                {
                    "resource_ref": "tweet:2103856181104619520",
                    "visibility": "public",
                    "created_at": "2026-09-26T14:36:15Z",
                }
            ]
        },
    )
    worker = StubWorker(
        {
            "task_a": WorkerResult(
                task_id="task_a",
                status=WorkerStatus.SUCCESS,
                conclusion=evidence.safe_summary,
                evidence=[evidence],
                confidence=0.95,
            )
        }
    )
    forged_composition = SupportComposition(
        status=SupportStatus.ANSWERED,
        answer=(
            "Found tweet:9999999999999999999 at 58706-09-26T14:36:15. "
            "The real post tweet:2103856181104619520 was published at 2026-09-25T14:36:15."
        ),
        suggested_actions=["Resource ID: 8888888888888888888"],
    )
    model_provider = FakeModelProvider(
        {
            ModelRole.PLANNER: [AIMessage(content=plan.model_dump_json())],
            ModelRole.COMPOSER: [AIMessage(content=forged_composition.model_dump_json())],
        }
    )

    response = await SupportRuntime(model_provider, worker).run(
        "\u8bf7\u5217\u51fa\u6700\u8fd1\u53d1\u5e03\u8bb0\u5f55",
        runtime(max_replans=0),
    )

    assert response.status == SupportStatus.PARTIAL
    assert response.answer == (
        "\u672c\u6b21\u67e5\u8be2\u8fd4\u56de 1 \u6761\u53ef\u6838\u5b9e\u7684\u6700\u8fd1"
        "\u5185\u5bb9:\n"
        "1. \u8d44\u6e90ID: tweet:2103856181104619520; \u53d1\u5e03\u65f6\u95f4: "
        "2026-09-26T14:36:15Z; \u53ef\u89c1\u6027: \u516c\u5f00"
    )
    assert "9999999999999999999" not in response.answer
    assert "58706" not in response.answer
    assert len(response.citations) == 1
    assert response.citations[0].id == "tweet:2103856181104619520"
    assert response.citations[0].evidence_ref == "ev_recent_grounded"
    assert response.suggested_actions == []


@pytest.mark.asyncio
async def test_composer_preserves_pure_knowledge_answer_without_verified_records() -> None:
    plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Use published guidance.")
    worker = StubWorker({"task_a": success_result("task_a")})
    knowledge_answer = (
        "\u5185\u5bb9\u5ba1\u6838\u901a\u5e38\u9700\u8981\u4e00\u4e9b\u65f6\u95f4\uff0c"
        "\u671f\u95f4\u53ef\u67e5\u770b\u5e94\u7528\u5185\u901a\u77e5\u3002"
    )
    composition = SupportComposition(
        status=SupportStatus.ANSWERED,
        answer=knowledge_answer,
        suggested_actions=["\u7a0d\u540e\u5237\u65b0\u901a\u77e5\u9875\u9762\u3002"],
    )
    model_provider = FakeModelProvider(
        {
            ModelRole.PLANNER: [AIMessage(content=plan.model_dump_json())],
            ModelRole.COMPOSER: [AIMessage(content=composition.model_dump_json())],
        }
    )

    response = await SupportRuntime(model_provider, worker).run(
        "\u4e3a\u4ec0\u4e48\u5185\u5bb9\u53ef\u80fd\u5ef6\u8fdf\u663e\u793a\uff1f",
        runtime(max_replans=0),
    )

    assert response.status == SupportStatus.ANSWERED
    assert response.answer == knowledge_answer
    assert response.suggested_actions == ["\u7a0d\u540e\u5237\u65b0\u901a\u77e5\u9875\u9762\u3002"]


@pytest.mark.asyncio
async def test_composer_fails_closed_on_item_id_without_verified_records() -> None:
    plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Summarize the result.")
    worker = StubWorker({"task_a": success_result("task_a")})
    composition = SupportComposition(
        status=SupportStatus.ANSWERED,
        answer="Your recent record is tweet:2103856181104619520.",
        suggested_actions=["Open it in the app."],
    )
    model_provider = FakeModelProvider(
        {
            ModelRole.PLANNER: [AIMessage(content=plan.model_dump_json())],
            ModelRole.COMPOSER: [AIMessage(content=composition.model_dump_json())],
        }
    )

    response = await SupportRuntime(model_provider, worker).run(
        "\u8bf7\u5217\u51fa\u6211\u7684\u6700\u8fd1\u63a8\u6587",
        runtime(max_replans=0),
    )

    assert response.status == SupportStatus.PARTIAL
    assert "tweet:2103856181104619520" not in response.answer
    assert response.suggested_actions == []
    assert "\u65e0\u6cd5\u53ef\u9760\u5217\u51fa" in response.answer


@pytest.mark.asyncio
async def test_evidence_gap_triggers_one_supplemental_plan() -> None:
    initial_plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Use evidence.")
    supplemental_plan = TaskPlan(tasks=[task("task_b")], answer_strategy="Fill the gap.")
    partial = WorkerResult(
        task_id="task_a",
        status=WorkerStatus.PARTIAL,
        conclusion="The first task lacked evidence.",
        confidence=0.1,
        gaps=["A second source is required."],
    )
    worker = StubWorker({"task_a": partial, "task_b": success_result("task_b")})
    support_runtime = SupportRuntime(
        provider(
            [
                AIMessage(content=initial_plan.model_dump_json()),
                AIMessage(content=supplemental_plan.model_dump_json()),
            ],
            status=SupportStatus.PARTIAL,
        ),
        worker,
    )

    response = await support_runtime.run("Investigate", runtime(max_replans=1))

    assert response.status == SupportStatus.PARTIAL
    assert response.evidence_refs == ["ev_task_b"]
    assert set(worker.completed) == {"task_a", "task_b"}


@pytest.mark.asyncio
async def test_worker_exception_is_isolated_and_becomes_partial_answer() -> None:
    plan = TaskPlan(tasks=[task("task_a")], answer_strategy="Use evidence.")

    class FailingWorker:
        async def run(self, task: TaskSpec, runtime: RuntimeContext) -> WorkerResult:
            del task, runtime
            raise RuntimeError("internal secret that must not escape")

    support_runtime = SupportRuntime(
        provider([AIMessage(content=plan.model_dump_json())], status=SupportStatus.PARTIAL),
        FailingWorker(),
    )

    response = await support_runtime.run("Investigate", runtime(max_replans=0))

    assert response.status == SupportStatus.PARTIAL
    assert "internal secret" not in response.answer

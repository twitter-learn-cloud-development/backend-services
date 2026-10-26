import asyncio
from datetime import UTC, datetime
from uuid import uuid4

import pytest
from langchain_core.messages import AIMessage, BaseMessage
from pydantic import ValidationError

from app.core.observability import monotonic_seconds
from app.domain.capability import CapabilitySpec
from app.domain.enums import EvidenceSourceType, ObservationStatus, SupportStatus, WorkerStatus
from app.domain.evidence import Evidence
from app.domain.gap import evidence_gap
from app.domain.response import AggregatedResult, SupportComposition
from app.domain.response_review import (
    CapabilityInputFields,
    GoalDisposition,
    RequestGoal,
    ResponseReview,
    ReviewedGoal,
)
from app.domain.support_query import RecentContentRecord
from app.domain.worker_result import WorkerResult
from app.services.response_guard import ResponseGuard


def record_evidence() -> Evidence:
    record = RecentContentRecord(
        resource_ref="tweet:2103856181104619520",
        visibility="public",
        created_at=datetime(2026, 9, 26, 14, 36, 15, tzinfo=UTC),
    )
    return Evidence(
        evidence_id="ev_records",
        source_type=EvidenceSourceType.BUSINESS_API,
        source_ref="tweet:list_recent",
        observed_at=record.created_at,
        safe_summary="Validated recent-content record.",
        attributes={"verified_records": [record.model_dump(mode="json")]},
    )


def knowledge_evidence() -> Evidence:
    return Evidence(
        evidence_id="ev_guide",
        source_type=EvidenceSourceType.KNOWLEDGE,
        source_ref="guide:workflow",
        observed_at=datetime(2026, 9, 26, 14, 36, 15, tzinfo=UTC),
        safe_summary="Workflow is a reusable sequence of steps; save before running.",
    )


def cap(*, required: list[str] | None = None) -> CapabilitySpec:
    fields = {name: {"type": "string"} for name in (required or [])}
    return CapabilitySpec(
        name="content.get_recent",
        description="Read recent content for the authenticated account.",
        input_schema={"type": "object", "properties": fields, "required": required or []},
        output_schema={"type": "object"},
        user_data_scope="authenticated_user",
        evidence_types=["business_api"],
    )


def worker(
    *,
    status: WorkerStatus = WorkerStatus.SUCCESS,
    errors: list[ObservationStatus] | None = None,
    missing_inputs: list[CapabilityInputFields] | None = None,
    provided_inputs: list[CapabilityInputFields] | None = None,
) -> WorkerResult:
    return WorkerResult(
        task_id="task_a",
        status=status,
        conclusion="Worker-authored conclusion must not be trusted as a fallback.",
        evidence=[record_evidence()],
        confidence=0.8,
        execution_errors=errors or [],
        missing_inputs=missing_inputs or [],
        provided_inputs=provided_inputs or [],
    )


def aggregate(
    evidence: list[Evidence] | None = None,
    *,
    answerable: bool = True,
    gaps: list[str] | None = None,
    contradictions: list[str] | None = None,
) -> AggregatedResult:
    unresolved = [evidence_gap("task_a", item) for item in gaps or []]
    return AggregatedResult(
        answerable=answerable,
        conclusion="Untrusted aggregator conclusion should not appear in fallback.",
        supporting_evidence=evidence or [record_evidence()],
        contradictions=contradictions or [],
        unresolved_questions=gaps or [],
        unresolved_gaps=unresolved,
    )


def supported(goal: RequestGoal, evidence_ref: str, *, records: bool = False) -> ReviewedGoal:
    return ReviewedGoal(
        **goal.model_dump(),
        disposition=GoalDisposition.SUPPORTED,
        evidence_refs=[evidence_ref],
        answer_text=None if records else "Workflow是可复用步骤流程，请先保存，再运行。",  # noqa: RUF001
        render_records=records,
    )


def review(*goals: ReviewedGoal, accounted: bool = True, consistent: bool = True) -> ResponseReview:
    return ResponseReview(
        all_request_goals_accounted_for=accounted,
        consistent_with_evidence=consistent,
        goals=list(goals),
    )


def draft() -> SupportComposition:
    return SupportComposition(
        status=SupportStatus.ANSWERED,
        answer="DRAFT MUST NOT OVERRIDE THE REVIEW: fictional answer.",
    )


@pytest.mark.asyncio
async def test_finalize_preserves_record_knowledge_and_unavailable_goal_content() -> None:
    record_goal = RequestGoal(goal_id="recent_posts", description="最近发布的推文")
    guide_goal = RequestGoal(goal_id="workflow", description="Workflow 的使用方法")
    unavailable_goal = RequestGoal(goal_id="feed_rank", description="推荐排序原因")
    evidence = [record_evidence(), knowledge_evidence()]
    aggregated = aggregate(
        evidence, answerable=False, gaps=["Recommendation ranking is not exposed."]
    )
    semantic_review = review(
        supported(record_goal, "ev_records", records=True),
        supported(guide_goal, "ev_guide"),
        ReviewedGoal(
            **unavailable_goal.model_dump(),
            disposition=GoalDisposition.CAPABILITY_UNAVAILABLE,
        ),
    )

    async def invoke(_messages: list[BaseMessage]) -> AIMessage:
        return AIMessage(content=semantic_review.model_dump_json())

    result = await ResponseGuard().finalize(
        message="列出最近推文\uff0c解释 Workflow 并说明推荐排序原因。",
        request_goals=[record_goal, guide_goal, unavailable_goal],
        aggregated=aggregated,
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=1e9,
        request_id=uuid4(),
        invoke=invoke,
    )

    assert result.status == SupportStatus.PARTIAL
    assert "tweet:2103856181104619520" in result.answer
    assert "2026-09-26T14:36:15Z" in result.answer
    assert "公开" in result.answer
    assert "Workflow是可复用步骤流程，请先保存，再运行" in result.answer  # noqa: RUF001
    assert "无法查询或执行" in result.answer
    assert "DRAFT MUST NOT OVERRIDE" not in result.answer
    assert "Untrusted aggregator conclusion" not in result.answer


def test_render_requires_every_declared_goal_and_global_evidence_gate_for_answered() -> None:
    first = RequestGoal(goal_id="posts", description="Recent posts")
    second = RequestGoal(goal_id="workflow", description="Workflow guide")
    with pytest.raises(ValueError, match="omitted a declared goal"):
        ResponseGuard().render(
            "List posts and explain Workflow.",
            [first, second],
            aggregate(),
            review(supported(first, "ev_records", records=True)),
            [cap()],
            [worker()],
        )

    complete_review = review(
        supported(first, "ev_records", records=True),
        supported(second, "ev_records", records=True),
    )
    result = ResponseGuard().render(
        "List posts and explain Workflow.",
        [first, second],
        aggregate(answerable=False, gaps=["The knowledge check is incomplete."]),
        complete_review,
        [cap()],
        [worker()],
    )
    assert result.status == SupportStatus.PARTIAL
    assert "do not resolve the whole request" in result.answer

    incomplete = review(supported(first, "ev_records", records=True), accounted=False)
    with pytest.raises(ValueError, match="full request accounting"):
        ResponseGuard().render("List posts.", [first], aggregate(), incomplete, [cap()], [worker()])


def test_render_rejects_unknown_evidence_reference() -> None:
    goal = RequestGoal(goal_id="posts", description="Recent posts")
    malformed = review(supported(goal, "ev_not_in_result"))
    with pytest.raises(ValueError, match="unknown evidence"):
        ResponseGuard().render("List posts.", [goal], aggregate(), malformed, [cap()], [worker()])


def test_clarification_requires_existing_capability_and_required_schema_field() -> None:
    goal = RequestGoal(goal_id="post_detail", description="Read a particular post")
    clarification = ReviewedGoal(
        **goal.model_dump(),
        disposition=GoalDisposition.MISSING_USER_INPUT,
        capability_name="content.get_recent",
        missing_fields=["tweet_id"],
        clarification_question="你要查询哪条推文的编号\uff1f",
        input_resolution_reason="已有读取能力要求明确推文编号才能继续。",
    )
    result = ResponseGuard().render(
        "Which post do I mean?",
        [goal],
        aggregate(answerable=False),
        review(clarification),
        [cap(required=["tweet_id"])],
        [
            worker(
                missing_inputs=[
                    CapabilityInputFields(
                        capability_name="content.get_recent", field_names=["tweet_id"]
                    )
                ]
            )
        ],
    )
    assert result.status == SupportStatus.CLARIFICATION_REQUIRED
    assert "哪条推文" in result.answer

    with pytest.raises(ValueError, match="clarification cannot enable"):
        ResponseGuard().render(
            "Which post do I mean?",
            [goal],
            aggregate(answerable=False),
            review(clarification),
            [cap(required=["tweet_id"])],
            [worker()],
        )

    optional_cap = cap()
    with pytest.raises(ValueError, match="clarification cannot enable"):
        ResponseGuard().render(
            "Which post do I mean?",
            [goal],
            aggregate(answerable=False),
            review(clarification),
            [optional_cap],
            [
                worker(
                    missing_inputs=[
                        CapabilityInputFields(
                            capability_name="content.get_recent", field_names=["tweet_id"]
                        )
                    ]
                )
            ],
        )

    with pytest.raises(ValueError, match="clarification cannot enable"):
        ResponseGuard().render(
            "Which post do I mean?",
            [goal],
            aggregate(answerable=False),
            review(clarification),
            [cap(required=["tweet_id"])],
            [
                worker(
                    missing_inputs=[
                        CapabilityInputFields(
                            capability_name="content.get_recent", field_names=["tweet_id"]
                        )
                    ],
                    provided_inputs=[
                        CapabilityInputFields(
                            capability_name="content.get_recent", field_names=["tweet_id"]
                        )
                    ],
                )
            ],
        )


@pytest.mark.parametrize(
    "worker_status,errors",
    [(WorkerStatus.FAILED, []), (WorkerStatus.SUCCESS, [ObservationStatus.TEMPORARY_ERROR])],
)
def test_execution_failure_cannot_be_rendered_as_user_clarification(
    worker_status: WorkerStatus, errors: list[ObservationStatus]
) -> None:
    goal = RequestGoal(goal_id="post_detail", description="Read a particular post")
    clarification = ReviewedGoal(
        **goal.model_dump(),
        disposition=GoalDisposition.MISSING_USER_INPUT,
        capability_name="content.get_recent",
        missing_fields=["tweet_id"],
        clarification_question="Provide the post ID.",
        input_resolution_reason="A post ID is required.",
    )
    with pytest.raises(ValueError, match="clarification cannot enable"):
        ResponseGuard().render(
            "Which post?",
            [goal],
            aggregate(answerable=False),
            review(clarification),
            [cap(required=["tweet_id"])],
            [worker(status=worker_status, errors=errors)],
        )


@pytest.mark.asyncio
async def test_timeout_falls_back_to_trusted_evidence_not_model_or_worker_conclusion() -> None:
    evidence = record_evidence()
    aggregated = aggregate([evidence])
    called = False

    async def invoke(_messages: list[BaseMessage]) -> AIMessage:
        nonlocal called
        called = True
        raise AssertionError("expired deadline must not invoke reviewer")

    result = await ResponseGuard().finalize(
        message="列出推文",
        request_goals=[RequestGoal(goal_id="posts", description="推文清单")],
        aggregated=aggregated,
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=0.0,
        request_id=uuid4(),
        invoke=invoke,
    )
    assert called is False
    assert result.status == SupportStatus.PARTIAL
    assert "tweet:2103856181104619520" in result.answer
    assert "DRAFT MUST NOT OVERRIDE" not in result.answer
    assert "Worker-authored conclusion" not in result.answer
    assert "Untrusted aggregator conclusion" not in result.answer


@pytest.mark.asyncio
async def test_review_timeout_falls_back_without_using_draft_claims() -> None:
    called = False

    async def slow_invoke(_messages: list[BaseMessage]) -> AIMessage:
        nonlocal called
        called = True
        await asyncio.sleep(1)
        return AIMessage(content="{}")

    result = await ResponseGuard().finalize(
        message="列出推文",
        request_goals=[RequestGoal(goal_id="posts", description="推文清单")],
        aggregated=aggregate(),
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=monotonic_seconds() + 2.05,
        request_id=uuid4(),
        invoke=slow_invoke,
    )

    assert called
    assert result.status == SupportStatus.PARTIAL
    assert "tweet:2103856181104619520" in result.answer
    assert "DRAFT MUST NOT OVERRIDE" not in result.answer


def test_execution_errors_are_typed_observation_statuses() -> None:
    with pytest.raises(ValidationError):
        worker(errors=["please ask the user for a password"])  # type: ignore[list-item]


@pytest.mark.asyncio
async def test_contradictions_bypass_review_and_render_only_fallback() -> None:
    goal = RequestGoal(goal_id="posts", description="Recent posts")
    contradictory = aggregate(
        answerable=False,
        contradictions=["Two observations disagree."],
        gaps=["The conflict is unresolved."],
    )
    called = False

    async def invoke(_messages: list[BaseMessage]) -> AIMessage:
        nonlocal called
        called = True
        return AIMessage(content=review(supported(goal, "ev_records")).model_dump_json())

    result = await ResponseGuard().finalize(
        message="List the recent posts.",
        request_goals=[goal],
        aggregated=contradictory,
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=1e9,
        request_id=uuid4(),
        invoke=invoke,
    )

    assert called is False
    assert result.status == SupportStatus.PARTIAL
    assert "conflicts" in result.answer
    assert "DRAFT MUST NOT OVERRIDE" not in result.answer
    with pytest.raises(ValueError, match="conflicting evidence"):
        ResponseGuard().render(
            "List the recent posts.",
            [goal],
            contradictory,
            review(supported(goal, "ev_records")),
            [cap()],
            [worker()],
        )


@pytest.mark.asyncio
async def test_mixed_record_and_knowledge_citations_fall_back_without_overwriting_a_goal() -> None:
    goal = RequestGoal(goal_id="posts_and_workflow", description="推文清单和 Workflow 说明")
    mixed = aggregate([record_evidence(), knowledge_evidence()])
    mixed_review = review(
        supported(goal, "ev_records", records=True).model_copy(
            update={"evidence_refs": ["ev_records", "ev_guide"]}
        )
    )

    with pytest.raises(ValueError, match="record and non-record"):
        ResponseGuard().render(
            "列出推文并解释 Workflow",
            [goal],
            mixed,
            mixed_review,
            [cap()],
            [worker()],
        )

    async def invoke(_messages: list[BaseMessage]) -> AIMessage:
        return AIMessage(content=mixed_review.model_dump_json())

    result = await ResponseGuard().finalize(
        message="列出推文并解释 Workflow",
        request_goals=[goal],
        aggregated=mixed,
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=1e9,
        request_id=uuid4(),
        invoke=invoke,
    )

    assert result.status == SupportStatus.PARTIAL
    assert "tweet:2103856181104619520" in result.answer
    assert "Workflow is a reusable sequence of steps" in result.answer
    assert "DRAFT MUST NOT OVERRIDE" not in result.answer


@pytest.mark.asyncio
async def test_supported_records_with_success_worker_execution_error_remain_partial() -> None:
    goal = RequestGoal(goal_id="posts", description="最近发布的推文")

    async def invoke(_messages: list[BaseMessage]) -> AIMessage:
        return AIMessage(
            content=review(supported(goal, "ev_records", records=True)).model_dump_json()
        )

    result = await ResponseGuard().finalize(
        message="列出最近推文",
        request_goals=[goal],
        aggregated=aggregate([record_evidence()], answerable=True),
        draft=draft(),
        capabilities=[cap()],
        workers=[worker(errors=[ObservationStatus.TEMPORARY_ERROR])],
        deadline=1e9,
        request_id=uuid4(),
        invoke=invoke,
    )

    assert result.status == SupportStatus.PARTIAL
    assert "tweet:2103856181104619520" in result.answer
    assert "未完成" in result.answer or "未能确认" in result.answer


def test_chinese_request_does_not_render_english_declared_goal_description() -> None:
    goal = RequestGoal(goal_id="workflow", description="Explain the workflow in detail")
    reviewer_goal = supported(goal, "ev_guide").model_copy(
        update={"description": "Workflow 的使用方法"}
    )

    result = ResponseGuard().render(
        "请解释如何创建和运行 Workflow。",
        [goal],
        aggregate([knowledge_evidence()]),
        review(reviewer_goal),
        [cap()],
        [worker()],
    )

    assert "Explain the workflow in detail" not in result.answer
    assert "Workflow 的使用方法" in result.answer


@pytest.mark.asyncio
async def test_fallback_marks_original_request_coverage_unconfirmed() -> None:
    request = "列出最近推文并解释 Workflow 的保存步骤。"

    async def invoke(_messages: list[BaseMessage]) -> AIMessage:
        raise AssertionError("expired deadline must not invoke reviewer")

    result = await ResponseGuard().finalize(
        message=request,
        request_goals=[RequestGoal(goal_id="compound", description="复合问题")],
        aggregated=aggregate([record_evidence(), knowledge_evidence()]),
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=0.0,
        request_id=uuid4(),
        invoke=invoke,
    )

    assert result.status == SupportStatus.PARTIAL
    assert "尚未完成覆盖核查的原请求" in result.answer
    assert request in result.answer
    assert "DRAFT MUST NOT OVERRIDE" not in result.answer
    assert "Untrusted aggregator conclusion" not in result.answer


@pytest.mark.asyncio
async def test_review_prompt_keeps_tool_evidence_but_excludes_aggregator_conclusion() -> None:
    goal = RequestGoal(goal_id="guide", description="Workflow 指南")
    evidence = knowledge_evidence().model_copy(
        update={"safe_summary": "TOOL_EVIDENCE_MARKER: save the workflow before running it."}
    )
    aggregated = aggregate([evidence])
    seen_prompt = ""

    async def invoke(messages: list[BaseMessage]) -> AIMessage:
        nonlocal seen_prompt
        seen_prompt = "\n".join(str(message.content) for message in messages)
        return AIMessage(content=review(supported(goal, "ev_guide")).model_dump_json())

    result = await ResponseGuard().finalize(
        message="请说明 Workflow 如何运行。",
        request_goals=[goal],
        aggregated=aggregated,
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=1e9,
        request_id=uuid4(),
        invoke=invoke,
    )

    assert result.status == SupportStatus.ANSWERED
    assert "TOOL_EVIDENCE_MARKER" in seen_prompt
    assert "supporting_evidence" in seen_prompt
    assert "Untrusted aggregator conclusion should not appear in fallback." not in seen_prompt


@pytest.mark.asyncio
async def test_chinese_request_falls_back_for_pure_english_review_answer() -> None:
    goal = RequestGoal(goal_id="guide", description="Workflow 指南")
    english_only = supported(goal, "ev_guide").model_copy(
        update={"answer_text": "The guide describes a reusable workflow."}
    )

    async def invoke(_messages: list[BaseMessage]) -> AIMessage:
        return AIMessage(content=review(english_only).model_dump_json())

    result = await ResponseGuard().finalize(
        message="请解释 Workflow 如何运行。",
        request_goals=[goal],
        aggregated=aggregate([knowledge_evidence()]),
        draft=draft(),
        capabilities=[cap()],
        workers=[worker()],
        deadline=1e9,
        request_id=uuid4(),
        invoke=invoke,
    )

    assert result.status == SupportStatus.PARTIAL
    assert "The guide describes a reusable workflow." not in result.answer
    assert "来源摘录" in result.answer


def test_chinese_answer_with_english_product_terms_is_preserved() -> None:
    goal = RequestGoal(goal_id="guide", description="Workflow 指南")
    direct_answer = "Workflow是可复用步骤流程，请先 Save，再 Run。"  # noqa: RUF001
    reviewed = supported(goal, "ev_guide").model_copy(update={"answer_text": direct_answer})

    result = ResponseGuard().render(
        "请解释 Workflow 如何运行。",
        [goal],
        aggregate([knowledge_evidence()]),
        review(reviewed),
        [cap()],
        [worker()],
    )

    assert direct_answer in result.answer


def test_legacy_explanation_field_is_rejected() -> None:
    goal = RequestGoal(goal_id="guide", description="Workflow 指南")
    with pytest.raises(ValidationError, match="explanation"):
        ReviewedGoal(
            **goal.model_dump(),
            disposition=GoalDisposition.SUPPORTED,
            evidence_refs=["ev_guide"],
            explanation="The guide supports the draft.",  # type: ignore[call-arg]
        )

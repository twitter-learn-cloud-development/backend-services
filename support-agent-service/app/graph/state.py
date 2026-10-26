import operator
from typing import Annotated, NotRequired, TypedDict

from langchain_core.messages import AnyMessage

from app.domain import (
    AggregatedResult,
    CapabilityObservation,
    CapabilitySpec,
    RuntimeContext,
    SupportCase,
    SupportResponse,
    TaskPlan,
    TaskSpec,
    WorkerResult,
)
from app.domain.gap import GapResolution
from app.domain.response_review import RequestGoal


class SupportState(TypedDict):
    runtime: RuntimeContext
    user_message: str
    request_goals: list[RequestGoal]
    execution_deadline: float
    available_capabilities: list[CapabilitySpec]
    support_case: NotRequired[SupportCase]
    task_plan: NotRequired[TaskPlan]
    task_history: Annotated[list[TaskSpec], operator.add]
    gap_resolutions: Annotated[list[GapResolution], operator.add]
    assessed_gap_tasks: Annotated[list[str], operator.add]
    current_task: NotRequired[TaskSpec]
    worker_results: Annotated[list[WorkerResult], operator.add]
    dispatched_task_ids: Annotated[list[str], operator.add]
    aggregated_result: NotRequired[AggregatedResult]
    replan_count: int
    final_response: NotRequired[SupportResponse]
    errors: Annotated[list[str], operator.add]


class WorkerState(TypedDict):
    runtime: RuntimeContext
    task: TaskSpec
    capabilities: NotRequired[list[CapabilitySpec]]
    messages: Annotated[list[AnyMessage], operator.add]
    observations: Annotated[list[CapabilityObservation], operator.add]
    model_call_count: int
    tool_call_count: int
    result: NotRequired[WorkerResult]
    errors: Annotated[list[str], operator.add]

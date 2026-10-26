"""Internal semantic coverage protocol, never exposed in the user HTTP schema."""

from enum import StrEnum

from pydantic import Field, model_validator

from app.domain.base import DomainModel
from app.domain.types import Identifier, ShortText


class RequestGoal(DomainModel):
    goal_id: Identifier
    description: ShortText


class CapabilityInputFields(DomainModel):
    capability_name: str = Field(min_length=3, max_length=128)
    field_names: list[ShortText] = Field(min_length=1, max_length=100)


class GoalDisposition(StrEnum):
    SUPPORTED = "supported"
    HANDOFF = "handoff"
    MISSING_USER_INPUT = "missing_user_input"
    CAPABILITY_UNAVAILABLE = "capability_unavailable"
    EXECUTION_FAILURE = "execution_failure"
    INSUFFICIENT_EVIDENCE = "insufficient_evidence"
    EVIDENCE_CONFLICT = "evidence_conflict"
    UNKNOWN = "unknown"


class ReviewedGoal(RequestGoal):
    disposition: GoalDisposition
    evidence_refs: list[Identifier] = Field(default_factory=list, max_length=30)
    answer_text: ShortText | None = Field(
        default=None,
        description=(
            "The actual final user-facing answer for this goal in the user's language, "
            "with direct facts or actionable steps. NOT a review rationale, a statement "
            "that an explanation is grounded, or commentary about another answer."
        ),
    )
    render_records: bool = False
    capability_name: Identifier | None = None
    missing_fields: list[Identifier] = Field(default_factory=list, max_length=10)
    clarification_question: ShortText | None = None
    input_resolution_reason: ShortText | None = None

    @model_validator(mode="after")
    def unique_lists(self) -> "ReviewedGoal":
        if len(set(self.evidence_refs)) != len(self.evidence_refs):
            raise ValueError("goal evidence refs must be unique")
        if len(set(self.missing_fields)) != len(self.missing_fields):
            raise ValueError("missing fields must be unique")
        if self.disposition == GoalDisposition.SUPPORTED:
            if not self.evidence_refs or not (self.answer_text or self.render_records):
                raise ValueError("supported goal requires cited evidence and rendered content")
        return self


class ResponseReview(DomainModel):
    all_request_goals_accounted_for: bool
    consistent_with_evidence: bool
    goals: list[ReviewedGoal] = Field(min_length=1, max_length=12)

    @model_validator(mode="after")
    def unique_goals(self) -> "ResponseReview":
        if len({goal.goal_id for goal in self.goals}) != len(self.goals):
            raise ValueError("review goals must be unique")
        return self

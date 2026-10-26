from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import Field, field_validator, model_validator

from app.domain.base import DomainModel
from app.domain.enums import SupportStatus
from app.domain.evidence import Evidence
from app.domain.gap import EvidenceGap
from app.domain.types import Identifier, NonEmptyText, ShortText


class AggregatedResult(DomainModel):
    answerable: bool
    conclusion: NonEmptyText
    supporting_evidence: list[Evidence] = Field(default_factory=list[Evidence], max_length=100)
    contradictions: list[ShortText] = Field(default_factory=list, max_length=30)
    # Five tasks per round, initial plus at most three replans, twenty gaps per task.
    unresolved_questions: list[ShortText] = Field(default_factory=list, max_length=400)
    unresolved_gaps: list[EvidenceGap] = Field(default_factory=list[EvidenceGap], max_length=400)
    resolved_gap_ids: list[Identifier] = Field(default_factory=list, max_length=400)


class SupportComposition(DomainModel):
    """Model-authored user wording without runtime-owned identifiers or evidence."""

    status: SupportStatus
    answer: NonEmptyText
    suggested_actions: list[ShortText] = Field(default_factory=list, max_length=10)


class SupportCitation(DomainModel):
    """Runtime-owned navigation to a verified record; never a model-supplied URL."""

    id: Identifier
    kind: Literal["tweet"] = "tweet"
    label: ShortText
    path: str = Field(pattern=r"^/tweets/[0-9]+$", max_length=160)
    evidence_ref: Identifier

    @model_validator(mode="after")
    def validate_record_binding(self) -> "SupportCitation":
        if self.id != f"tweet:{self.path.removeprefix('/tweets/')}":
            raise ValueError("citation id must match the tweet path")
        return self


class SupportResponse(DomainModel):
    request_id: UUID
    conversation_id: UUID
    case_id: UUID
    status: SupportStatus
    answer: NonEmptyText
    suggested_actions: list[ShortText] = Field(default_factory=list, max_length=10)
    evidence_refs: list[Identifier] = Field(default_factory=list, max_length=100)
    citations: list[SupportCitation] = Field(default_factory=list[SupportCitation], max_length=100)
    created_at: datetime
    trace_id: str = Field(min_length=1, max_length=64)

    @field_validator("created_at")
    @classmethod
    def validate_timezone_aware(cls, value: datetime) -> datetime:
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("created_at must include a timezone")
        return value

    @model_validator(mode="after")
    def validate_citation_evidence(self) -> "SupportResponse":
        if any(citation.evidence_ref not in self.evidence_refs for citation in self.citations):
            raise ValueError("citation must refer to supporting evidence")
        if len({citation.id for citation in self.citations}) != len(self.citations):
            raise ValueError("citation records must be unique")
        return self

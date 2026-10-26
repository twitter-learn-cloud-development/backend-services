from pydantic import Field, model_validator

from app.domain.base import DomainModel
from app.domain.enums import ObservationStatus, WorkerStatus
from app.domain.evidence import Evidence, Finding
from app.domain.response_review import CapabilityInputFields
from app.domain.types import Identifier, NonEmptyText, ShortText


class WorkerResult(DomainModel):
    task_id: Identifier
    status: WorkerStatus
    conclusion: NonEmptyText
    findings: list[Finding] = Field(default_factory=list[Finding], max_length=30)
    evidence: list[Evidence] = Field(default_factory=list[Evidence], max_length=50)
    confidence: float = Field(ge=0.0, le=1.0)
    gaps: list[ShortText] = Field(default_factory=list, max_length=20)
    execution_errors: list[ObservationStatus] = Field(
        default_factory=list[ObservationStatus], max_length=6
    )
    missing_inputs: list[CapabilityInputFields] = Field(
        default_factory=list[CapabilityInputFields], max_length=30
    )
    provided_inputs: list[CapabilityInputFields] = Field(
        default_factory=list[CapabilityInputFields], max_length=30
    )

    @model_validator(mode="after")
    def validate_evidence_references(self) -> "WorkerResult":
        evidence_ids = {item.evidence_id for item in self.evidence}
        referenced = {
            evidence_ref for finding in self.findings for evidence_ref in finding.evidence_refs
        }
        missing = referenced - evidence_ids
        if missing:
            raise ValueError(f"findings reference unknown evidence: {sorted(missing)}")
        if self.status == WorkerStatus.SUCCESS and not self.evidence:
            raise ValueError("successful worker result must include evidence")
        return self


class WorkerCompletion(DomainModel):
    """Small model-authored conclusion; evidence is attached by deterministic code."""

    status: WorkerStatus
    conclusion: NonEmptyText
    confidence: float = Field(ge=0.0, le=1.0)
    gaps: list[ShortText] = Field(default_factory=list, max_length=20)

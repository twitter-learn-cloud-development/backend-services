"""Runtime-owned gaps and bounded model assessments of supplemental evidence."""

import hashlib

from pydantic import Field, field_validator

from app.domain.base import DomainModel
from app.domain.types import Identifier, ShortText


class EvidenceGap(DomainModel):
    gap_id: Identifier
    task_id: Identifier
    question: ShortText


def evidence_gap(task_id: str, question: str) -> EvidenceGap:
    digest = hashlib.sha256(f"{task_id}\0{question}".encode()).hexdigest()[:24]
    return EvidenceGap(gap_id=f"gap_{digest}", task_id=task_id, question=question)


class GapResolution(DomainModel):
    gap_id: Identifier
    task_id: Identifier
    evidence_refs: list[Identifier] = Field(min_length=1, max_length=20)
    rationale: ShortText

    @field_validator("evidence_refs")
    @classmethod
    def unique_refs(cls, value: list[str]) -> list[str]:
        if len(value) != len(set(value)):
            raise ValueError("resolution evidence refs must be unique")
        return value


class GapAssessment(DomainModel):
    resolutions: list[GapResolution] = Field(default_factory=list[GapResolution], max_length=100)

    @field_validator("resolutions")
    @classmethod
    def unique_gaps(cls, value: list[GapResolution]) -> list[GapResolution]:
        if len({item.gap_id for item in value}) != len(value):
            raise ValueError("gap resolutions must be unique")
        return value

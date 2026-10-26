from datetime import datetime

from pydantic import Field, JsonValue, field_validator

from app.domain.base import DomainModel
from app.domain.enums import EvidenceSourceType
from app.domain.types import Identifier, ShortText


class Evidence(DomainModel):
    evidence_id: Identifier
    source_type: EvidenceSourceType
    source_ref: str = Field(min_length=1, max_length=256)
    observed_at: datetime
    safe_summary: ShortText
    attributes: dict[str, JsonValue] = Field(default_factory=dict)

    @field_validator("observed_at")
    @classmethod
    def validate_timezone_aware(cls, value: datetime) -> datetime:
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("observed_at must include a timezone")
        return value


class Finding(DomainModel):
    statement: ShortText
    evidence_refs: list[Identifier] = Field(min_length=1, max_length=20)
    confidence: float = Field(ge=0.0, le=1.0)

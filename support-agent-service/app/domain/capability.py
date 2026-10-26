from typing import Any

from pydantic import Field, field_validator

from app.domain.base import DomainModel
from app.domain.enums import ObservationStatus
from app.domain.evidence import Evidence
from app.domain.types import Identifier, ShortText


class CapabilitySpec(DomainModel):
    name: str = Field(
        min_length=3,
        max_length=128,
        pattern=r"^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$",
    )
    description: ShortText
    input_schema: dict[str, Any]
    output_schema: dict[str, Any]
    read_only: bool = True
    user_data_scope: Identifier
    evidence_types: list[Identifier] = Field(min_length=1, max_length=20)
    timeout_ms: int = Field(default=2_000, ge=100, le=60_000)
    enabled: bool = True

    @field_validator("read_only")
    @classmethod
    def require_read_only(cls, value: bool) -> bool:
        if not value:
            raise ValueError("V1 capabilities must be read-only")
        return value


class CapabilityCall(DomainModel):
    name: str = Field(
        min_length=3,
        max_length=128,
        pattern=r"^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$",
    )
    arguments: dict[str, Any] = Field(default_factory=dict)
    task_id: Identifier
    tool_call_id: Identifier


class CapabilityObservation(DomainModel):
    tool_call_id: Identifier
    capability_name: str = Field(min_length=3, max_length=128)
    status: ObservationStatus
    data: dict[str, Any] | None = None
    evidence: list[Evidence] = Field(default_factory=list[Evidence], max_length=50)
    error_code: str | None = Field(default=None, max_length=128)
    retryable: bool = False
    missing_fields: list[str] = Field(default_factory=list[str], max_length=100)
    provided_fields: list[str] = Field(default_factory=list[str], max_length=100)

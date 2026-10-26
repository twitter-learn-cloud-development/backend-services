import json
from datetime import datetime
from typing import Annotated, Any
from uuid import UUID
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

from pydantic import Field, StringConstraints, field_validator, model_validator

from app.domain.base import DomainModel
from app.domain.types import Identifier, NonEmptyText, ShortText

ContextKey = Annotated[
    str,
    StringConstraints(min_length=1, max_length=64, pattern=r"^[A-Za-z][A-Za-z0-9_.-]*$"),
]
ContextValue = Annotated[str, StringConstraints(max_length=1_024)]


class SupportRequest(DomainModel):
    conversation_id: UUID | None = None
    message: NonEmptyText
    client_timezone: str = Field(default="UTC", min_length=1, max_length=64)
    context: dict[ContextKey, ContextValue] = Field(default_factory=dict)

    @field_validator("client_timezone")
    @classmethod
    def validate_timezone(cls, value: str) -> str:
        try:
            ZoneInfo(value)
        except ZoneInfoNotFoundError as exc:
            raise ValueError("client_timezone must be a valid IANA timezone") from exc
        return value

    @field_validator("context")
    @classmethod
    def validate_context_size(
        cls, value: dict[ContextKey, ContextValue]
    ) -> dict[ContextKey, ContextValue]:
        encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        if len(encoded) > 4_096:
            raise ValueError("context must not exceed 4096 UTF-8 bytes")
        return value


class TimeRange(DomainModel):
    start: datetime | None = None
    end: datetime | None = None

    @model_validator(mode="after")
    def validate_order(self) -> "TimeRange":
        if self.start is not None and self.end is not None and self.start > self.end:
            raise ValueError("time range start must not be after end")
        return self


class CaseEntity(DomainModel):
    name: Identifier
    value: ShortText
    confidence: float = Field(ge=0.0, le=1.0)
    metadata: dict[str, Any] = Field(default_factory=dict)


class SupportCase(DomainModel):
    case_id: UUID
    actor_user_id: Identifier
    original_message: NonEmptyText
    problem_statement: NonEmptyText
    hypotheses: list[ShortText] = Field(default_factory=list, max_length=10)
    entities: list[CaseEntity] = Field(default_factory=list[CaseEntity], max_length=30)
    time_range: TimeRange | None = None
    constraints: list[ShortText] = Field(default_factory=list, max_length=20)
    clarification_question: ShortText | None = None

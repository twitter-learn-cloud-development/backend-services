from datetime import datetime
from typing import Literal

from pydantic import Field, field_validator

from app.domain.base import DomainModel
from app.domain.types import NonEmptyText


class ConversationMessage(DomainModel):
    role: Literal["user", "assistant"]
    content: NonEmptyText
    created_at: datetime

    @field_validator("created_at")
    @classmethod
    def validate_timezone_aware(cls, value: datetime) -> datetime:
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("created_at must include a timezone")
        return value


class ConversationContext(DomainModel):
    """Bounded conversation context supplied to the planner as untrusted data."""

    summary: str | None = Field(default=None, min_length=1, max_length=4_000)
    recent_messages: list[ConversationMessage] = Field(
        default_factory=list[ConversationMessage], max_length=20
    )

from datetime import datetime

from pydantic import Field, field_validator

from app.domain.base import DomainModel
from app.domain.types import Identifier, ShortText


class KnowledgeDocument(DomainModel):
    document_id: Identifier
    version: Identifier
    title: ShortText
    body: str = Field(min_length=1, max_length=100_000)
    updated_at: datetime
    published: bool = True

    @field_validator("updated_at")
    @classmethod
    def validate_timezone_aware(cls, value: datetime) -> datetime:
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("updated_at must include a timezone")
        return value


class KnowledgeMatch(DomainModel):
    chunk_id: Identifier
    document_id: Identifier
    version: Identifier
    title: ShortText
    excerpt: ShortText
    updated_at: datetime
    score: float = Field(ge=0.0)

    @field_validator("updated_at")
    @classmethod
    def validate_timezone_aware(cls, value: datetime) -> datetime:
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("updated_at must include a timezone")
        return value

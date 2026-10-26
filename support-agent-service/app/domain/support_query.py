from datetime import UTC, datetime, timedelta
from typing import Any, Literal
from uuid import UUID

from pydantic import AwareDatetime, Field, field_validator

from app.domain.base import DomainModel
from app.domain.evidence import Evidence
from app.domain.types import Identifier


class QueryRequestContext(DomainModel):
    case_id: UUID
    task_id: Identifier
    tool_call_id: Identifier


class SupportQueryRequest(DomainModel):
    operation: str = Field(
        min_length=3,
        max_length=128,
        pattern=r"^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$",
    )
    arguments: dict[str, Any]
    request_context: QueryRequestContext


class QueryMeta(DomainModel):
    duration_ms: int = Field(ge=0)
    data_freshness: Literal["live", "cached"]


class RecentContentRecord(DomainModel):
    """Safe, verbatim facts from an authenticated recent-content query."""

    resource_ref: str = Field(pattern=r"^tweet:[0-9]+$", max_length=128)
    visibility: Literal["public", "followers", "private", "restricted"]
    created_at: AwareDatetime

    @field_validator("created_at")
    @classmethod
    def validate_published_time(cls, value: datetime) -> datetime:
        if value.year < 2000 or value > datetime.now(UTC) + timedelta(days=1):
            raise ValueError("created_at is outside the supported published-content range")
        return value


class SupportQueryResponse(DomainModel):
    request_id: UUID
    operation: str = Field(min_length=3, max_length=128)
    status: Literal["success"]
    data: dict[str, Any]
    evidence: list[Evidence] = Field(default_factory=list[Evidence], max_length=50)
    meta: QueryMeta


class QueryProblem(DomainModel):
    type: str
    title: str
    status: int
    code: str = Field(min_length=1, max_length=128)
    detail: str
    request_id: str
    trace_id: str

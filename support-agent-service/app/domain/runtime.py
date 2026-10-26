from uuid import UUID

from pydantic import Field, model_validator

from app.domain.base import DomainModel
from app.domain.conversation import ConversationContext
from app.domain.types import Identifier


class AuthContext(DomainModel):
    actor_user_id: Identifier
    token_id: Identifier
    scopes: frozenset[Identifier] = Field(default_factory=frozenset)


class RunBudget(DomainModel):
    max_tasks: int = Field(default=5, ge=1, le=20)
    max_concurrent_workers: int = Field(default=3, ge=1, le=10)
    max_model_calls_per_worker: int = Field(default=4, ge=1, le=20)
    max_tool_calls_per_worker: int = Field(default=6, ge=1, le=30)
    max_replans: int = Field(default=1, ge=0, le=3)
    soft_timeout_seconds: float = Field(default=12.0, gt=0, le=120)
    hard_timeout_seconds: float = Field(default=20.0, gt=0, le=180)

    @model_validator(mode="after")
    def validate_timeouts(self) -> "RunBudget":
        if self.soft_timeout_seconds >= self.hard_timeout_seconds:
            raise ValueError("soft timeout must be less than hard timeout")
        return self


class RuntimeContext(DomainModel):
    auth: AuthContext
    request_id: UUID
    conversation_id: UUID
    case_id: UUID
    trace_id: str = Field(min_length=1, max_length=64)
    budget: RunBudget = Field(default_factory=RunBudget)
    conversation_context: ConversationContext = Field(default_factory=ConversationContext)

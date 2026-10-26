from functools import lru_cache
from typing import Literal

from pydantic import Field, SecretStr, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_prefix="SUPPORT_AGENT_",
        extra="ignore",
        case_sensitive=False,
    )

    app_name: str = "Support Agent Service"
    app_version: str = "0.1.0"
    app_env: Literal["development", "test", "staging", "production"] = "development"
    log_level: str = "INFO"
    json_logs: bool = False

    max_message_chars: int = Field(default=8_000, ge=1, le=32_000)
    max_context_bytes: int = Field(default=4_096, ge=256, le=65_536)
    max_tasks: int = Field(default=5, ge=1, le=20)
    chat_soft_timeout_seconds: float = Field(default=12.0, gt=0, le=120)
    chat_hard_timeout_seconds: float = Field(default=20.0, gt=0, le=180)
    idempotency_ttl_seconds: float = Field(default=300.0, gt=0, le=86_400)
    rate_limit_requests: int = Field(default=60, ge=1, le=10_000)
    rate_limit_window_seconds: float = Field(default=60.0, gt=0, le=3_600)
    sse_disconnect_poll_seconds: float = Field(default=0.05, gt=0, le=5)
    conversation_ttl_seconds: int = Field(default=604_800, ge=60, le=31_536_000)
    conversation_recent_turns: int = Field(default=4, ge=1, le=10)
    conversation_context_chars: int = Field(default=6_000, ge=256, le=32_000)
    conversation_summary_chars: int = Field(default=2_000, ge=128, le=4_000)

    model_base_url: str | None = None
    model_name: str | None = None
    model_api_key: SecretStr | None = None
    user_jwks_url: str | None = None
    user_jwt_issuer: str | None = None
    user_jwt_audience: str | None = None
    user_jwks_timeout_seconds: float = Field(default=2.0, gt=0, le=30)
    user_jwks_cache_seconds: int = Field(default=300, ge=1, le=86_400)
    support_query_base_url: str | None = None
    support_query_jwt_secret: SecretStr | None = None
    support_query_jwt_issuer: str = "support-agent-service"
    support_query_jwt_audience: str = "twitter-support-query"
    support_query_jwt_subject: str = "support-agent-service"
    support_query_jwt_ttl_seconds: int = Field(default=30, ge=5, le=120)
    support_query_timeout_seconds: float = Field(default=2.0, gt=0, le=30)
    mongodb_uri: str | None = None
    mongodb_database: str = "support_agent"
    qdrant_url: str | None = None
    qdrant_api_key: SecretStr | None = None
    qdrant_collection: str = "support_knowledge"
    embedding_model_name: str | None = None
    embedding_dimensions: int = Field(default=1_536, ge=1, le=65_536)
    knowledge_search_limit: int = Field(default=5, ge=1, le=20)
    otlp_traces_endpoint: str | None = None
    otlp_headers: str | None = None
    otlp_export_timeout_seconds: float = Field(default=5.0, gt=0, le=30)
    trace_sample_ratio: float = Field(default=1.0, ge=0.0, le=1.0)
    metrics_enabled: bool = True
    audit_hmac_key: SecretStr | None = None

    @model_validator(mode="after")
    def validate_chat_timeouts(self) -> "Settings":
        if self.chat_soft_timeout_seconds >= self.chat_hard_timeout_seconds:
            raise ValueError("chat soft timeout must be less than chat hard timeout")
        query_url_configured = bool(self.support_query_base_url)
        query_secret_configured = bool(
            self.support_query_jwt_secret and self.support_query_jwt_secret.get_secret_value()
        )
        if query_url_configured != query_secret_configured:
            raise ValueError("support query base URL and JWT secret must be configured together")
        if bool(self.qdrant_url) != bool(self.embedding_model_name):
            raise ValueError("Qdrant URL and embedding model name must be configured together")
        return self


@lru_cache
def get_settings() -> Settings:
    return Settings()

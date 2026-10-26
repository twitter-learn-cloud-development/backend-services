from collections.abc import Callable, Sequence
from enum import StrEnum
from typing import Any, Protocol, override

from langchain_core.callbacks import CallbackManagerForLLMRun
from langchain_core.language_models.base import LanguageModelInput
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langchain_core.runnables import Runnable
from langchain_core.tools import BaseTool
from langchain_openai import ChatOpenAI
from pydantic import Field, PrivateAttr

from app.core.config import Settings
from app.core.errors import AppError


class ModelRole(StrEnum):
    PLANNER = "planner"
    WORKER = "worker"
    COMPOSER = "composer"


class ModelProvider(Protocol):
    def get_chat_model(self, role: ModelRole) -> BaseChatModel: ...


class OpenAICompatibleModelProvider:
    def __init__(self, settings: Settings) -> None:
        self._settings = settings

    def get_chat_model(self, role: ModelRole) -> BaseChatModel:
        del role  # V1 uses one configured model for all roles.
        if not self._settings.model_name or not self._settings.model_api_key:
            raise AppError(
                code="model_not_configured",
                title="Model is not configured",
                detail="A model name and API key are required for agent execution.",
                status_code=503,
            )
        return ChatOpenAI(
            model=self._settings.model_name,
            base_url=self._settings.model_base_url,
            api_key=self._settings.model_api_key,
            temperature=0,
        )


class ScriptedChatModel(BaseChatModel):
    """Deterministic chat model used by graph and tool-calling tests."""

    responses: list[AIMessage] = Field(min_length=1)
    _index: int = PrivateAttr(default=0)

    @property
    @override
    def _llm_type(self) -> str:
        return "scripted-chat-model"

    @override
    def _generate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        del messages, stop, run_manager, kwargs
        if self._index >= len(self.responses):
            raise RuntimeError("scripted chat model has no remaining responses")
        response = self.responses[self._index]
        self._index += 1
        return ChatResult(generations=[ChatGeneration(message=response)])

    @override
    def bind_tools(
        self,
        tools: Sequence[dict[str, Any] | type | Callable[..., Any] | BaseTool],
        *,
        tool_choice: str | None = None,
        **kwargs: Any,
    ) -> Runnable[LanguageModelInput, AIMessage]:
        del tools, tool_choice, kwargs
        return self


class FakeModelProvider:
    def __init__(self, responses: dict[ModelRole, list[AIMessage]]) -> None:
        self._responses = responses

    def get_chat_model(self, role: ModelRole) -> BaseChatModel:
        scripted = self._responses.get(role)
        if not scripted:
            raise KeyError(f"no scripted responses registered for role {role}")
        return ScriptedChatModel(responses=list(scripted))

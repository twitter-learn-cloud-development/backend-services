from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Protocol

from app.core.config import Settings
from app.core.observability import Telemetry
from app.domain.response import SupportResponse
from app.domain.runtime import RuntimeContext
from app.graph.supervisor import SupportRuntime
from app.graph.worker import GenericWorker
from app.integrations.knowledge import (
    QdrantKnowledgeStore,
    build_qdrant_knowledge_store,
    register_knowledge_capability,
)
from app.integrations.model_provider import OpenAICompatibleModelProvider
from app.services.response_guard import ResponseGuard
from app.tools.executor import CapabilityExecutor
from app.tools.registry import CapabilityRegistry
from app.tools.resolver import CapabilityResolver


class SupportRunner(Protocol):
    async def run(self, message: str, runtime: RuntimeContext) -> SupportResponse: ...


@dataclass(slots=True)
class RuntimeResources:
    runner: SupportRunner
    close: Callable[[], Awaitable[None]] | None = None

    async def aclose(self) -> None:
        if self.close is not None:
            await self.close()


def build_support_runtime(
    settings: Settings,
    *,
    telemetry: Telemetry | None = None,
) -> RuntimeResources:
    model_provider = OpenAICompatibleModelProvider(settings)
    registry = CapabilityRegistry()
    knowledge_store: QdrantKnowledgeStore | None = None
    if settings.qdrant_url:
        knowledge_store = build_qdrant_knowledge_store(settings)
        register_knowledge_capability(
            registry,
            knowledge_store,
            default_limit=settings.knowledge_search_limit,
        )
    resolver = CapabilityResolver(registry)
    executor = CapabilityExecutor(registry, telemetry=telemetry)
    worker = GenericWorker(model_provider, resolver, executor, telemetry=telemetry)
    runner = SupportRuntime(
        model_provider,
        worker,
        telemetry=telemetry,
        capability_catalog=registry.list_allowed,
        response_guard=ResponseGuard(guide_only=True),
    )

    async def close() -> None:
        if knowledge_store is not None:
            await knowledge_store.aclose()

    return RuntimeResources(
        runner=runner,
        close=close if knowledge_store is not None else None,
    )

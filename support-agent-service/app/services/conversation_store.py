import asyncio
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Annotated, Any, Protocol, TypedDict, cast
from uuid import UUID

from langchain_core.messages import AIMessage, BaseMessage, HumanMessage
from langchain_core.runnables import RunnableConfig
from langgraph.checkpoint.base import BaseCheckpointSaver
from langgraph.checkpoint.memory import InMemorySaver
from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages
from pymongo import MongoClient, ReturnDocument
from pymongo.collection import Collection
from pymongo.errors import DuplicateKeyError

from app.core.config import Settings
from app.core.errors import AppError
from app.domain.conversation import ConversationContext, ConversationMessage
from app.domain.types import Identifier


class ConversationStore(Protocol):
    async def claim(self, conversation_id: UUID, actor_user_id: Identifier) -> None: ...

    async def require_owner(self, conversation_id: UUID, actor_user_id: Identifier) -> None: ...

    async def delete(self, conversation_id: UUID, actor_user_id: Identifier) -> None: ...


class ConversationHistory(Protocol):
    async def context(self, conversation_id: UUID) -> ConversationContext: ...

    async def append_turn(
        self,
        conversation_id: UUID,
        *,
        case_id: UUID,
        user_message: str,
        assistant_message: str,
    ) -> None: ...

    async def delete(self, conversation_id: UUID) -> None: ...


class _ConversationState(TypedDict):
    messages: Annotated[list[BaseMessage], add_messages]


class _StateSnapshot(Protocol):
    values: dict[str, Any]


class _ConversationGraph(Protocol):
    async def aget_state(self, config: RunnableConfig) -> _StateSnapshot: ...

    async def ainvoke(
        self,
        input: _ConversationState,
        config: RunnableConfig | None = None,
    ) -> _ConversationState: ...


class LangGraphConversationHistory:
    """Persist user-visible turns through a LangGraph checkpointer."""

    def __init__(
        self,
        checkpointer: BaseCheckpointSaver[str],
        *,
        recent_turns: int = 4,
        max_context_chars: int = 6_000,
        max_summary_chars: int = 2_000,
    ) -> None:
        self._checkpointer = checkpointer
        self._recent_messages = recent_turns * 2
        self._max_context_chars = max_context_chars
        self._max_summary_chars = max_summary_chars

        async def record_turn(state: _ConversationState) -> dict[str, object]:
            del state
            return {}

        builder = StateGraph(_ConversationState)
        builder.add_node("record_turn", record_turn)  # pyright: ignore[reportUnknownMemberType]
        builder.add_edge(START, "record_turn")
        builder.add_edge("record_turn", END)
        self._graph = cast(
            _ConversationGraph,
            builder.compile(  # pyright: ignore[reportUnknownMemberType]
                checkpointer=checkpointer
            ),
        )

    async def context(self, conversation_id: UUID) -> ConversationContext:
        snapshot = await self._graph.aget_state(self._config(conversation_id))
        raw_messages = snapshot.values.get("messages", [])
        messages = self._domain_messages(cast(list[BaseMessage], raw_messages))
        recent = self._select_recent(messages)
        older_count = len(messages) - len(recent)
        summary = self._summarize(messages[:older_count]) if older_count else None
        return ConversationContext(summary=summary, recent_messages=recent)

    async def append_turn(
        self,
        conversation_id: UUID,
        *,
        case_id: UUID,
        user_message: str,
        assistant_message: str,
    ) -> None:
        now = datetime.now(UTC)
        messages: list[BaseMessage] = [
            HumanMessage(
                content=user_message,
                id=f"{case_id}:user",
                additional_kwargs={"at": now.isoformat()},
            ),
            AIMessage(
                content=assistant_message,
                id=f"{case_id}:assistant",
                additional_kwargs={"at": now.isoformat()},
            ),
        ]
        await self._graph.ainvoke(
            {"messages": messages},
            config=self._config(conversation_id),
        )

    async def delete(self, conversation_id: UUID) -> None:
        await self._checkpointer.adelete_thread(str(conversation_id))

    def _select_recent(self, messages: list[ConversationMessage]) -> list[ConversationMessage]:
        selected: list[ConversationMessage] = []
        used = 0
        for message in reversed(messages[-self._recent_messages :]):
            size = len(message.content)
            if selected and used + size > self._max_context_chars:
                break
            selected.append(message)
            used += size
        selected.reverse()
        return selected

    def _summarize(self, messages: list[ConversationMessage]) -> str | None:
        if not messages:
            return None
        joined = "\n".join(f"{message.role}: {message.content}" for message in messages)
        if len(joined) <= self._max_summary_chars:
            return joined
        return f"{joined[: self._max_summary_chars - 1]}…"

    @staticmethod
    def _domain_messages(messages: list[BaseMessage]) -> list[ConversationMessage]:
        result: list[ConversationMessage] = []
        for message in messages:
            if not isinstance(message.content, str) or not message.content.strip():
                continue
            if isinstance(message, HumanMessage):
                role = "user"
            elif isinstance(message, AIMessage):
                role = "assistant"
            else:
                continue
            raw_at = message.additional_kwargs.get("at")
            created_at = datetime.now(UTC)
            if isinstance(raw_at, str):
                try:
                    created_at = datetime.fromisoformat(raw_at)
                except ValueError:
                    pass
            result.append(
                ConversationMessage(role=role, content=message.content, created_at=created_at)
            )
        return result

    @staticmethod
    def _config(conversation_id: UUID) -> RunnableConfig:
        return {"configurable": {"thread_id": str(conversation_id)}}


class InMemoryConversationStore:
    def __init__(
        self,
        *,
        ttl_seconds: float = 86_400,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        self._owners: dict[UUID, tuple[Identifier, datetime]] = {}
        self._ttl = timedelta(seconds=ttl_seconds)
        self._now = now or (lambda: datetime.now(UTC))
        self._lock = asyncio.Lock()

    async def claim(self, conversation_id: UUID, actor_user_id: Identifier) -> None:
        async with self._lock:
            existing = self._owners.get(conversation_id)
            if existing is not None and existing[1] > self._now() and existing[0] != actor_user_id:
                raise _forbidden()
            self._owners[conversation_id] = (actor_user_id, self._now() + self._ttl)

    async def require_owner(self, conversation_id: UUID, actor_user_id: Identifier) -> None:
        async with self._lock:
            existing = self._owners.get(conversation_id)
            if existing is None or existing[1] <= self._now() or existing[0] != actor_user_id:
                self._owners.pop(conversation_id, None)
                raise _forbidden()
            self._owners[conversation_id] = (actor_user_id, self._now() + self._ttl)

    async def delete(self, conversation_id: UUID, actor_user_id: Identifier) -> None:
        await self.require_owner(conversation_id, actor_user_id)
        async with self._lock:
            self._owners.pop(conversation_id, None)


class MongoConversationStore:
    def __init__(
        self,
        collection: Collection[dict[str, Any]],
        *,
        ttl_seconds: int,
    ) -> None:
        self._collection = collection
        self._ttl = timedelta(seconds=ttl_seconds)
        self._collection.create_index("expires_at", expireAfterSeconds=0)

    async def claim(self, conversation_id: UUID, actor_user_id: Identifier) -> None:
        now = datetime.now(UTC)
        try:
            await asyncio.to_thread(
                self._collection.find_one_and_update,
                {"_id": str(conversation_id), "actor_user_id": actor_user_id},
                {
                    "$set": {
                        "actor_user_id": actor_user_id,
                        "updated_at": now,
                        "expires_at": now + self._ttl,
                    },
                    "$setOnInsert": {"created_at": now},
                },
                upsert=True,
                return_document=ReturnDocument.AFTER,
            )
        except DuplicateKeyError as exc:
            raise _forbidden() from exc

    async def require_owner(self, conversation_id: UUID, actor_user_id: Identifier) -> None:
        now = datetime.now(UTC)
        document = await asyncio.to_thread(
            self._collection.find_one_and_update,
            {
                "_id": str(conversation_id),
                "actor_user_id": actor_user_id,
                "expires_at": {"$gt": now},
            },
            {"$set": {"updated_at": now, "expires_at": now + self._ttl}},
            return_document=ReturnDocument.AFTER,
        )
        if document is None:
            raise _forbidden()

    async def delete(self, conversation_id: UUID, actor_user_id: Identifier) -> None:
        result = await asyncio.to_thread(
            self._collection.delete_one,
            {"_id": str(conversation_id), "actor_user_id": actor_user_id},
        )
        if result.deleted_count != 1:
            raise _forbidden()


@dataclass(slots=True)
class ConversationResources:
    store: ConversationStore
    history: ConversationHistory
    close: Callable[[], Awaitable[None]] | None = None

    async def aclose(self) -> None:
        if self.close is not None:
            await self.close()

    async def delete(self, conversation_id: UUID, actor_user_id: Identifier) -> None:
        await self.store.delete(conversation_id, actor_user_id)
        await self.history.delete(conversation_id)


def build_conversation_resources(settings: Settings) -> ConversationResources:
    if not settings.mongodb_uri:
        saver = InMemorySaver()
        return ConversationResources(
            store=InMemoryConversationStore(ttl_seconds=settings.conversation_ttl_seconds),
            history=LangGraphConversationHistory(
                saver,
                recent_turns=settings.conversation_recent_turns,
                max_context_chars=settings.conversation_context_chars,
                max_summary_chars=settings.conversation_summary_chars,
            ),
        )

    from langgraph.checkpoint.mongodb import MongoDBSaver

    client: MongoClient[dict[str, Any]] = MongoClient(settings.mongodb_uri, tz_aware=True)
    database = client[settings.mongodb_database]
    saver = MongoDBSaver(
        client,
        db_name=settings.mongodb_database,
        checkpoint_collection_name="conversation_checkpoints",
        writes_collection_name="conversation_checkpoint_writes",
        ttl=settings.conversation_ttl_seconds,
    )
    store = MongoConversationStore(
        database["conversations"],
        ttl_seconds=settings.conversation_ttl_seconds,
    )

    async def close() -> None:
        await asyncio.to_thread(client.close)

    return ConversationResources(
        store=store,
        history=LangGraphConversationHistory(
            saver,
            recent_turns=settings.conversation_recent_turns,
            max_context_chars=settings.conversation_context_chars,
            max_summary_chars=settings.conversation_summary_chars,
        ),
        close=close,
    )


def _forbidden() -> AppError:
    return AppError(
        code="conversation_forbidden",
        title="Conversation forbidden",
        detail="The conversation is not available to the authenticated user.",
        status_code=403,
    )

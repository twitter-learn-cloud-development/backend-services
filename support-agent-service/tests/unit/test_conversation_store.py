from datetime import UTC, datetime, timedelta
from uuid import uuid4

import pytest
from langgraph.checkpoint.memory import InMemorySaver

from app.core.errors import AppError
from app.services.conversation_store import (
    InMemoryConversationStore,
    LangGraphConversationHistory,
)


@pytest.mark.asyncio
async def test_checkpoint_history_survives_service_recreation_and_is_bounded() -> None:
    saver = InMemorySaver()
    conversation_id = uuid4()
    first = LangGraphConversationHistory(
        saver,
        recent_turns=1,
        max_context_chars=1_000,
        max_summary_chars=120,
    )
    await first.append_turn(
        conversation_id,
        case_id=uuid4(),
        user_message="First question about visibility",
        assistant_message="First verified answer",
    )
    await first.append_turn(
        conversation_id,
        case_id=uuid4(),
        user_message="Second follow-up",
        assistant_message="Second verified answer",
    )

    recreated = LangGraphConversationHistory(
        saver,
        recent_turns=1,
        max_context_chars=1_000,
        max_summary_chars=120,
    )
    context = await recreated.context(conversation_id)

    assert [message.content for message in context.recent_messages] == [
        "Second follow-up",
        "Second verified answer",
    ]
    assert context.summary is not None
    assert "First question about visibility" in context.summary
    assert len(context.summary) <= 120


@pytest.mark.asyncio
async def test_checkpoint_history_can_be_deleted() -> None:
    saver = InMemorySaver()
    history = LangGraphConversationHistory(saver)
    conversation_id = uuid4()
    await history.append_turn(
        conversation_id,
        case_id=uuid4(),
        user_message="Delete this turn",
        assistant_message="This turn will be deleted",
    )

    await history.delete(conversation_id)

    assert (await history.context(conversation_id)).recent_messages == []


@pytest.mark.asyncio
async def test_expired_conversation_is_not_accessible() -> None:
    current = datetime(2026, 9, 28, tzinfo=UTC)

    def now() -> datetime:
        return current

    store = InMemoryConversationStore(ttl_seconds=60, now=now)
    conversation_id = uuid4()
    await store.claim(conversation_id, "user-1")
    current += timedelta(seconds=61)

    with pytest.raises(AppError) as error:
        await store.require_owner(conversation_id, "user-1")

    assert error.value.code == "conversation_forbidden"

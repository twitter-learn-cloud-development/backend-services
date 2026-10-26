import pytest

from app.domain.runtime import AuthContext
from app.domain.task_plan import TaskSpec
from app.tools.mock import create_mock_registry
from app.tools.resolver import CapabilityResolver


@pytest.mark.asyncio
async def test_resolver_ranks_capabilities_by_task_description() -> None:
    resolver = CapabilityResolver(create_mock_registry())
    auth = AuthContext(actor_user_id="user-1", token_id="token-1")

    content_task = TaskSpec(
        task_id="task_content",
        objective="List the authenticated user's recent content",
        required_evidence=["recent content"],
        success_criteria=["recent content is known"],
    )
    platform_task = TaskSpec(
        task_id="task_platform",
        objective="Check public platform operational status",
        required_evidence=["platform status"],
        success_criteria=["operational status is known"],
    )

    content_candidates = await resolver.resolve(content_task, auth, limit=1)
    platform_candidates = await resolver.resolve(platform_task, auth, limit=1)

    assert content_candidates[0].name == "content.list_recent"
    assert platform_candidates[0].name == "platform.get_status"

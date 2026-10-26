import pytest

from app.domain.runtime import AuthContext
from app.tools.mock import create_mock_registry
from app.tools.registry import CapabilityRegistryError


def auth(*scopes: str) -> AuthContext:
    return AuthContext(actor_user_id="user-1", token_id="token-1", scopes=frozenset(scopes))


def test_registry_filters_capabilities_by_runtime_permission() -> None:
    registry = create_mock_registry()

    default_names = {spec.name for spec in registry.list_allowed(auth())}
    privileged_names = {spec.name for spec in registry.list_allowed(auth("support.restricted"))}

    assert "content.list_recent" in default_names
    assert "platform.get_status" in default_names
    assert "internal.read_restricted_summary" not in default_names
    assert "internal.read_restricted_summary" in privileged_names


def test_registry_rejects_duplicate_registration() -> None:
    registry = create_mock_registry()
    registered = registry.get("platform.get_status")

    with pytest.raises(CapabilityRegistryError, match="already registered"):
        registry.register(registered.spec, registered.handler)

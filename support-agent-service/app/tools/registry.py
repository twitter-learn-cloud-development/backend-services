from collections.abc import Awaitable, Callable
from dataclasses import dataclass

from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.runtime import AuthContext, RuntimeContext

CapabilityHandler = Callable[[CapabilityCall, RuntimeContext], Awaitable[CapabilityObservation]]


class CapabilityRegistryError(ValueError):
    pass


@dataclass(frozen=True, slots=True)
class RegisteredCapability:
    spec: CapabilitySpec
    handler: CapabilityHandler


class CapabilityRegistry:
    def __init__(self) -> None:
        self._capabilities: dict[str, RegisteredCapability] = {}

    def register(self, spec: CapabilitySpec, handler: CapabilityHandler) -> None:
        if spec.name in self._capabilities:
            raise CapabilityRegistryError(f"capability {spec.name!r} is already registered")
        self._capabilities[spec.name] = RegisteredCapability(spec=spec, handler=handler)

    def get(self, name: str) -> RegisteredCapability:
        try:
            return self._capabilities[name]
        except KeyError as exc:
            raise CapabilityRegistryError(f"unknown capability {name!r}") from exc

    def list_allowed(self, auth: AuthContext) -> list[CapabilitySpec]:
        return [
            registered.spec
            for registered in self._capabilities.values()
            if self.is_allowed(registered.spec.name, auth)
        ]

    def is_allowed(self, name: str, auth: AuthContext) -> bool:
        try:
            spec = self.get(name).spec
        except CapabilityRegistryError:
            return False
        if not spec.enabled or not spec.read_only:
            return False
        if spec.user_data_scope in {"public", "self"}:
            return True
        return spec.user_data_scope in auth.scopes

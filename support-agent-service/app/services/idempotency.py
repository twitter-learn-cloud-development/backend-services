import asyncio
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field

from app.core.errors import AppError
from app.domain.response import SupportResponse
from app.domain.runtime import RuntimeContext


@dataclass(slots=True)
class IdempotencyEntry:
    scope: str
    fingerprint: str
    runtime: RuntimeContext
    expires_at: float
    lock: asyncio.Lock = field(default_factory=asyncio.Lock)
    response: SupportResponse | None = None


class InMemoryIdempotencyStore:
    def __init__(self, *, ttl_seconds: float) -> None:
        self._ttl_seconds = ttl_seconds
        self._entries: dict[str, IdempotencyEntry] = {}
        self._lock = asyncio.Lock()

    async def reserve(
        self,
        *,
        scope: str,
        fingerprint: str,
        runtime: RuntimeContext,
    ) -> tuple[IdempotencyEntry, bool]:
        now = time.monotonic()
        async with self._lock:
            self._purge(now)
            existing = self._entries.get(scope)
            if existing is not None:
                if existing.fingerprint != fingerprint:
                    raise AppError(
                        code="idempotency_conflict",
                        title="Idempotency conflict",
                        detail="The idempotency key was already used for a different request.",
                        status_code=409,
                    )
                return existing, False
            entry = IdempotencyEntry(
                scope=scope,
                fingerprint=fingerprint,
                runtime=runtime,
                expires_at=now + self._ttl_seconds,
            )
            self._entries[scope] = entry
            return entry, True

    async def execute(
        self,
        entry: IdempotencyEntry,
        operation: Callable[[RuntimeContext], Awaitable[SupportResponse]],
    ) -> SupportResponse:
        async with entry.lock:
            if entry.response is not None:
                return entry.response
            try:
                entry.response = await operation(entry.runtime)
                entry.expires_at = time.monotonic() + self._ttl_seconds
                return entry.response
            except BaseException:
                await self.discard(entry)
                raise

    async def discard(self, entry: IdempotencyEntry) -> None:
        async with self._lock:
            if self._entries.get(entry.scope) is entry:
                del self._entries[entry.scope]

    def _purge(self, now: float) -> None:
        expired = [
            scope
            for scope, entry in self._entries.items()
            if entry.expires_at <= now and not entry.lock.locked()
        ]
        for scope in expired:
            del self._entries[scope]

import asyncio
import time
from collections import defaultdict, deque

from app.core.errors import AppError


class InMemoryRateLimiter:
    def __init__(self, *, limit: int, window_seconds: float) -> None:
        self._limit = limit
        self._window_seconds = window_seconds
        self._requests: dict[str, deque[float]] = defaultdict(deque)
        self._lock = asyncio.Lock()

    async def check(self, key: str) -> None:
        now = time.monotonic()
        cutoff = now - self._window_seconds
        async with self._lock:
            timestamps = self._requests[key]
            while timestamps and timestamps[0] <= cutoff:
                timestamps.popleft()
            if len(timestamps) >= self._limit:
                raise AppError(
                    code="rate_limited",
                    title="Rate limited",
                    detail="Too many support requests were submitted. Try again later.",
                    status_code=429,
                )
            timestamps.append(now)

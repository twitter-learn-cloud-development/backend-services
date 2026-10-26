from dataclasses import dataclass, field
from typing import Any


@dataclass(slots=True)
class AppError(Exception):
    code: str
    title: str
    detail: str
    status_code: int = 500
    errors: list[dict[str, Any]] = field(default_factory=lambda: list[dict[str, Any]]())

    def __str__(self) -> str:
        return self.detail

import re

from app.domain.capability import CapabilitySpec
from app.domain.runtime import AuthContext
from app.domain.task_plan import TaskSpec
from app.tools.registry import CapabilityRegistry

_TOKEN_PATTERN = re.compile(r"[A-Za-z0-9_]+|[\u4e00-\u9fff]")


def _tokens(value: str) -> set[str]:
    return {token.casefold() for token in _TOKEN_PATTERN.findall(value)}


class CapabilityResolver:
    def __init__(self, registry: CapabilityRegistry) -> None:
        self._registry = registry

    async def resolve(
        self,
        task: TaskSpec,
        auth: AuthContext,
        limit: int = 8,
    ) -> list[CapabilitySpec]:
        if limit < 1:
            return []
        query = " ".join([task.objective, *task.required_evidence, *task.success_criteria])
        query_tokens = _tokens(query)

        def score(spec: CapabilitySpec) -> tuple[int, str]:
            searchable = " ".join(
                [spec.name.replace(".", " "), spec.description, *spec.evidence_types]
            )
            overlap = len(query_tokens & _tokens(searchable))
            return (-overlap, spec.name)

        allowed = self._registry.list_allowed(auth)
        return sorted(allowed, key=score)[:limit]

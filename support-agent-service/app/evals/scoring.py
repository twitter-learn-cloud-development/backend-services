import json
from pathlib import Path
from typing import Literal

from pydantic import Field

from app.domain.base import DomainModel
from app.domain.types import Identifier


class EvalCase(DomainModel):
    case_id: Identifier
    description: str = Field(min_length=1, max_length=500)
    requires_plan: bool = True
    plan_tasks: list[str] = Field(default_factory=list[str], max_length=20)
    max_plan_tasks: int = Field(default=5, ge=1, le=20)
    expected_capabilities: list[Identifier] = Field(default_factory=list[Identifier])
    selected_capabilities: list[Identifier] = Field(default_factory=list[Identifier])
    factual_claims: list[str] = Field(default_factory=list[str])
    evidence_supported_claims: list[str] = Field(default_factory=list[str])
    expected_authorization: Literal["allowed", "denied"] = "allowed"
    actual_authorization: Literal["allowed", "denied"] = "allowed"
    latency_ms: float = Field(ge=0)
    latency_budget_ms: float = Field(gt=0)


class EvalScores(DomainModel):
    total_cases: int = Field(ge=1)
    plan_quality: float = Field(ge=0, le=1)
    capability_recall: float = Field(ge=0, le=1)
    evidence_faithfulness: float = Field(ge=0, le=1)
    permission_accuracy: float = Field(ge=0, le=1)
    latency_compliance: float = Field(ge=0, le=1)

    def passes(self, *, minimum: float = 1.0) -> bool:
        return all(
            value >= minimum
            for value in [
                self.plan_quality,
                self.capability_recall,
                self.evidence_faithfulness,
                self.permission_accuracy,
                self.latency_compliance,
            ]
        )


def load_dataset(path: Path) -> list[EvalCase]:
    cases: list[EvalCase] = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        if not line.strip():
            continue
        try:
            cases.append(EvalCase.model_validate_json(line))
        except ValueError as exc:
            raise ValueError(f"invalid eval case at line {line_number}") from exc
    if not cases:
        raise ValueError("eval dataset is empty")
    return cases


def evaluate(cases: list[EvalCase]) -> EvalScores:
    if not cases:
        raise ValueError("at least one eval case is required")
    total = len(cases)
    return EvalScores(
        total_cases=total,
        plan_quality=sum(_plan_passes(case) for case in cases) / total,
        capability_recall=sum(_capabilities_pass(case) for case in cases) / total,
        evidence_faithfulness=sum(_evidence_passes(case) for case in cases) / total,
        permission_accuracy=sum(_permission_passes(case) for case in cases) / total,
        latency_compliance=sum(_latency_passes(case) for case in cases) / total,
    )


def render_scores(scores: EvalScores) -> str:
    return json.dumps(scores.model_dump(mode="json"), ensure_ascii=False, indent=2)


def _plan_passes(case: EvalCase) -> bool:
    return not case.requires_plan or 1 <= len(case.plan_tasks) <= case.max_plan_tasks


def _capabilities_pass(case: EvalCase) -> bool:
    return set(case.expected_capabilities).issubset(case.selected_capabilities)


def _evidence_passes(case: EvalCase) -> bool:
    return set(case.factual_claims).issubset(case.evidence_supported_claims)


def _permission_passes(case: EvalCase) -> bool:
    return case.expected_authorization == case.actual_authorization


def _latency_passes(case: EvalCase) -> bool:
    return case.latency_ms <= case.latency_budget_ms

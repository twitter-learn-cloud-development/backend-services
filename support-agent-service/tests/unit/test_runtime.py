import pytest
from pydantic import ValidationError

from app.domain.runtime import RunBudget


def test_run_budget_defaults_match_architecture() -> None:
    budget = RunBudget()

    assert budget.max_tasks == 5
    assert budget.max_concurrent_workers == 3
    assert budget.max_model_calls_per_worker == 4
    assert budget.max_tool_calls_per_worker == 6
    assert budget.max_replans == 1


def test_run_budget_rejects_invalid_timeout_order() -> None:
    with pytest.raises(ValidationError, match="soft timeout"):
        RunBudget(soft_timeout_seconds=20, hard_timeout_seconds=20)

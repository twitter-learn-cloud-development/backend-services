from pathlib import Path

from app.evals.scoring import EvalCase, evaluate, load_dataset


def test_v1_eval_dataset_meets_all_release_thresholds() -> None:
    cases = load_dataset(Path("evals/dataset-v1.jsonl"))

    scores = evaluate(cases)

    assert scores.total_cases == 5
    assert scores.passes(minimum=1.0)


def test_eval_detects_unsupported_facts_and_latency_regression() -> None:
    case = EvalCase(
        case_id="regression",
        description="A deliberately failing result.",
        plan_tasks=["Investigate"],
        factual_claims=["unsupported_fact"],
        evidence_supported_claims=[],
        latency_ms=2_000,
        latency_budget_ms=1_000,
    )

    scores = evaluate([case])

    assert scores.evidence_faithfulness == 0
    assert scores.latency_compliance == 0
    assert not scores.passes()

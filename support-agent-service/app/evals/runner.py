import argparse
from pathlib import Path

from app.evals.scoring import evaluate, load_dataset, render_scores


def main() -> int:
    parser = argparse.ArgumentParser(description="Run the support-agent V1 offline evaluation.")
    parser.add_argument(
        "--dataset",
        type=Path,
        default=Path("evals/dataset-v1.jsonl"),
        help="JSONL evaluation dataset or recorded result set.",
    )
    parser.add_argument(
        "--minimum",
        type=float,
        default=1.0,
        help="Minimum score required for every metric.",
    )
    arguments = parser.parse_args()
    scores = evaluate(load_dataset(arguments.dataset))
    print(render_scores(scores))
    return 0 if scores.passes(minimum=arguments.minimum) else 1


if __name__ == "__main__":
    raise SystemExit(main())

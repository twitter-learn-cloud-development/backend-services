"""Explicit, public-knowledge-only reproduction of the Workflow support path."""

import argparse
import asyncio
import json
from uuid import uuid4

from app.core.config import Settings
from app.domain.response import AggregatedResult
from app.domain.runtime import AuthContext, RunBudget, RuntimeContext
from app.domain.worker_result import WorkerResult
from app.graph.supervisor import SupportRuntime
from app.runtime import build_support_runtime
from app.services.evidence_aggregator import EvidenceAggregator


class DiagnosticAggregator(EvidenceAggregator):
    def aggregate(
        self, results: list[WorkerResult], *, resolved_gap_ids: set[str] | None = None
    ) -> AggregatedResult:
        value = super().aggregate(results, resolved_gap_ids=resolved_gap_ids)
        print(
            json.dumps(
                {
                    "phase": "aggregate",
                    "answerable": value.answerable,
                    "worker_statuses": [item.status.value for item in results],
                    "evidence_count": len(value.supporting_evidence),
                    "contradiction_count": len(value.contradictions),
                    "unresolved_count": len(value.unresolved_questions),
                    "distinct_chunks": len(
                        {item.attributes.get("chunk_id") for item in value.supporting_evidence}
                    ),
                }
            )
        )
        return value


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true")
    args = parser.parse_args()
    if not args.live:
        raise RuntimeError("Pass --live to run the real model and knowledge retrieval")
    settings = Settings().model_copy(
        update={
            "support_query_base_url": None,
            "support_query_jwt_secret": None,
        }
    )
    resources = build_support_runtime(settings)
    runner = resources.runner
    assert isinstance(runner, SupportRuntime)
    runner._aggregator = (
        DiagnosticAggregator()
    )  # Probe-only: no account data capability is registered.
    try:
        response = await asyncio.wait_for(
            runner.run(
                "Workflow 是什么\uff0c如何创建和运行\uff1f",
                RuntimeContext(
                    auth=AuthContext(actor_user_id="public-workflow-probe", token_id="probe"),
                    request_id=uuid4(),
                    conversation_id=uuid4(),
                    case_id=uuid4(),
                    trace_id=uuid4().hex,
                    budget=RunBudget(max_tasks=settings.max_tasks),
                ),
            ),
            timeout=120,
        )
        print(
            json.dumps(
                {
                    "status": "passed",
                    "response_status": response.status.value,
                    "evidence_refs": response.evidence_refs,
                    "answer": response.answer,
                },
                ensure_ascii=False,
            )
        )
    except Exception as error:
        print(json.dumps({"status": "failed", "error_type": type(error).__name__}))
        raise
    finally:
        await resources.aclose()


if __name__ == "__main__":
    asyncio.run(main())

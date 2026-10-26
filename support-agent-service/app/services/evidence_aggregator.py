from collections import defaultdict

from app.domain.enums import EvidenceSourceType, WorkerStatus
from app.domain.gap import EvidenceGap, evidence_gap
from app.domain.response import AggregatedResult, Evidence
from app.domain.worker_result import WorkerResult


class EvidenceAggregator:
    def aggregate(
        self, results: list[WorkerResult], *, resolved_gap_ids: set[str] | None = None
    ) -> AggregatedResult:
        evidence_by_id: dict[str, Evidence] = {}
        summaries_by_source: dict[str, set[str]] = defaultdict(set)
        contradictions: list[str] = []
        gaps: dict[str, EvidenceGap] = {}
        resolved = resolved_gap_ids or set()
        conclusions: list[str] = []

        for result in results:
            questions = result.gaps
            if result.status != WorkerStatus.SUCCESS and not questions:
                questions = [f"Task {result.task_id} was not completed."]
            for question in questions:
                gap = evidence_gap(result.task_id, question)
                gaps[gap.gap_id] = gap
            for evidence in result.evidence:
                existing = evidence_by_id.get(evidence.evidence_id)
                if existing is not None and not _same_observation(existing, evidence):
                    contradictions.append(
                        f"Evidence {evidence.evidence_id} has conflicting representations."
                    )
                    continue
                evidence_by_id[evidence.evidence_id] = evidence
                # A published document has several complementary chunks. Only differing
                # representations of the same chunk are a deterministic conflict.
                chunk = evidence.attributes.get("chunk_id")
                source_key = evidence.source_ref
                if (
                    evidence.source_type == EvidenceSourceType.KNOWLEDGE
                    and isinstance(chunk, str)
                    and chunk
                ):
                    source_key = f"{source_key}:chunk:{chunk}"
                summaries_by_source[source_key].add(evidence.safe_summary)

        for source_ref, summaries in summaries_by_source.items():
            if len(summaries) > 1:
                contradictions.append(f"Source {source_ref} produced conflicting observations.")

        contradictions = list(dict.fromkeys(contradictions))
        unresolved_gaps = [gap for key, gap in gaps.items() if key not in resolved]
        unresolved = list(dict.fromkeys(gap.question for gap in unresolved_gaps))
        for result in results:
            questions = result.gaps or (
                [f"Task {result.task_id} was not completed."]
                if result.status != WorkerStatus.SUCCESS
                else []
            )
            if questions and all(
                evidence_gap(result.task_id, question).gap_id in resolved for question in questions
            ):
                # Keep historical evidence, but don't present an old failure as current fact.
                continue
            if result.conclusion not in conclusions:
                conclusions.append(result.conclusion)
        evidence = list(evidence_by_id.values())
        return AggregatedResult(
            answerable=bool(evidence) and not contradictions and not unresolved,
            conclusion=" ".join(conclusions) or "No investigation result is available.",
            supporting_evidence=evidence,
            contradictions=contradictions,
            unresolved_questions=unresolved,
            unresolved_gaps=unresolved_gaps,
            resolved_gap_ids=sorted(resolved & gaps.keys()),
        )


def _same_observation(first: Evidence, second: Evidence) -> bool:
    if (
        first.source_type != EvidenceSourceType.KNOWLEDGE
        or second.source_type != EvidenceSourceType.KNOWLEDGE
    ):
        return first == second

    # Relevance scores depend on the retrieval query; they are not document facts.
    def factual_attributes(item: Evidence) -> dict[str, object]:
        return {key: value for key, value in item.attributes.items() if key != "score"}

    return (
        first.source_ref == second.source_ref
        and first.observed_at == second.observed_at
        and first.safe_summary == second.safe_summary
        and factual_attributes(first) == factual_attributes(second)
    )

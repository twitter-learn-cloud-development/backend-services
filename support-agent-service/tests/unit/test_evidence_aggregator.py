from datetime import UTC, datetime

import pytest

from app.domain.enums import EvidenceSourceType, WorkerStatus
from app.domain.evidence import Evidence
from app.domain.worker_result import WorkerResult
from app.services.evidence_aggregator import EvidenceAggregator


def evidence(evidence_id: str, summary: str) -> Evidence:
    return Evidence(
        evidence_id=evidence_id,
        source_type=EvidenceSourceType.BUSINESS_API,
        source_ref="resource:shared",
        observed_at=datetime.now(UTC),
        safe_summary=summary,
    )


def test_aggregator_deduplicates_evidence_and_detects_source_conflicts() -> None:
    results = [
        WorkerResult(
            task_id="task_1",
            status=WorkerStatus.SUCCESS,
            conclusion="First observation.",
            evidence=[evidence("ev_1", "The resource is visible.")],
            confidence=0.9,
        ),
        WorkerResult(
            task_id="task_2",
            status=WorkerStatus.SUCCESS,
            conclusion="Second observation.",
            evidence=[evidence("ev_2", "The resource is not visible.")],
            confidence=0.8,
        ),
    ]

    aggregated = EvidenceAggregator().aggregate(results)

    assert aggregated.answerable is False
    assert len(aggregated.supporting_evidence) == 2
    assert "conflicting observations" in aggregated.contradictions[0]


def knowledge(chunk: str, summary: str, *, score: float = 0.9) -> Evidence:
    return Evidence(
        evidence_id=f"ev_knowledge_{chunk}",
        source_type=EvidenceSourceType.KNOWLEDGE,
        source_ref="knowledge:workflow-guide:v1",
        observed_at=datetime(2026, 10, 2, tzinfo=UTC),
        safe_summary=summary,
        attributes={
            "document_id": "workflow-guide",
            "version": "v1",
            "chunk_id": chunk,
            "score": score,
            "trust_level": "untrusted_reference",
        },
    )


def knowledge_result(task_id: str, items: list[Evidence]) -> WorkerResult:
    return WorkerResult(
        task_id=task_id,
        status=WorkerStatus.SUCCESS,
        conclusion="Verified guidance.",
        evidence=items,
        confidence=0.9,
    )


def test_different_document_chunks_are_complementary_not_conflicting() -> None:
    result = EvidenceAggregator().aggregate(
        [
            knowledge_result(
                "definition", [knowledge("chunk-1", "Workflow defines connected steps.")]
            ),
            knowledge_result("operations", [knowledge("chunk-2", "Save precedes run.")]),
        ]
    )
    assert result.answerable
    assert result.contradictions == []
    assert len(result.supporting_evidence) == 2


def test_same_chunk_retrieved_for_different_questions_ignores_relevance_score() -> None:
    result = EvidenceAggregator().aggregate(
        [
            knowledge_result("definition", [knowledge("chunk-1", "Save precedes run.", score=0.8)]),
            knowledge_result("operations", [knowledge("chunk-1", "Save precedes run.", score=0.5)]),
        ]
    )
    assert result.answerable
    assert result.contradictions == []
    assert len(result.supporting_evidence) == 1


@pytest.mark.parametrize("change", ["summary", "version", "timestamp", "chunk"])
def test_same_evidence_id_with_changed_factual_representation_remains_conflict(change: str) -> None:
    first = knowledge("chunk-1", "Save precedes run.")
    second = first.model_copy(deep=True)
    if change == "summary":
        second.safe_summary = "Run does not save."
    elif change == "version":
        second.attributes["version"] = "v2"
    elif change == "timestamp":
        second.observed_at = datetime(2026, 10, 3, tzinfo=UTC)
    else:
        second.attributes["chunk_id"] = "different-chunk"
    result = EvidenceAggregator().aggregate(
        [knowledge_result("first", [first]), knowledge_result("second", [second])]
    )
    assert not result.answerable
    assert result.contradictions


def test_same_chunk_with_distinct_evidence_ids_and_differing_text_remains_conflict() -> None:
    first = knowledge("chunk-1", "Save precedes run.")
    second = knowledge("chunk-1", "Run does not save.").model_copy(
        update={"evidence_id": "ev_other"}
    )
    result = EvidenceAggregator().aggregate(
        [knowledge_result("first", [first]), knowledge_result("second", [second])]
    )
    assert not result.answerable
    assert result.contradictions


def test_same_questions_from_different_tasks_preserve_all_structured_gaps() -> None:
    results = [
        WorkerResult(
            task_id=f"task_{index}",
            status=WorkerStatus.PARTIAL,
            conclusion="Some facts remain unverified.",
            confidence=0.3,
            gaps=[f"Missing fact {number}" for number in range(20)],
        )
        for index in range(20)
    ]
    aggregated = EvidenceAggregator().aggregate(results)
    assert not aggregated.answerable
    assert len(aggregated.unresolved_gaps) == 400
    assert len(aggregated.unresolved_questions) == 20
    assert len({gap.gap_id for gap in aggregated.unresolved_gaps}) == 400

from datetime import UTC, datetime

import pytest
from pydantic import ValidationError

from app.domain.capability import CapabilitySpec
from app.domain.enums import EvidenceSourceType, WorkerStatus
from app.domain.evidence import Evidence, Finding
from app.domain.worker_result import WorkerResult


def test_capability_spec_requires_namespaced_name_and_read_only() -> None:
    spec = CapabilitySpec(
        name="content.list_recent",
        description="List recent resources visible to the authenticated user.",
        input_schema={"type": "object"},
        output_schema={"type": "object"},
        user_data_scope="self",
        evidence_types=["content_summary"],
    )
    assert spec.read_only is True

    with pytest.raises(ValidationError):
        CapabilitySpec(
            name="list_recent",
            description="Invalid capability.",
            input_schema={},
            output_schema={},
            read_only=False,
            user_data_scope="self",
            evidence_types=["summary"],
        )


def test_worker_result_requires_existing_evidence_references() -> None:
    evidence = Evidence(
        evidence_id="ev_1",
        source_type=EvidenceSourceType.BUSINESS_API,
        source_ref="resource:opaque-1",
        observed_at=datetime.now(UTC),
        safe_summary="The resource exists.",
    )
    result = WorkerResult(
        task_id="task_1",
        status=WorkerStatus.SUCCESS,
        conclusion="The resource exists.",
        findings=[Finding(statement="Exists", evidence_refs=["ev_1"], confidence=0.9)],
        evidence=[evidence],
        confidence=0.9,
    )
    assert result.evidence[0].evidence_id == "ev_1"

    with pytest.raises(ValidationError, match="unknown evidence"):
        WorkerResult(
            task_id="task_1",
            status=WorkerStatus.SUCCESS,
            conclusion="The resource exists.",
            findings=[Finding(statement="Exists", evidence_refs=["ev_missing"], confidence=0.9)],
            evidence=[evidence],
            confidence=0.9,
        )

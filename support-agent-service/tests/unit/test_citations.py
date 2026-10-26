from datetime import UTC, datetime
from uuid import uuid4

import pytest
from pydantic import ValidationError

from app.domain.enums import SupportStatus
from app.domain.response import SupportCitation, SupportResponse


@pytest.mark.parametrize(
    "path",
    ["https://evil.test/1", "//evil.test/1", "/tweets/demo-001", "/tweets/1?next=evil", "/users/1"],
)
def test_citation_rejects_arbitrary_navigation(path: str) -> None:
    with pytest.raises(ValidationError):
        SupportCitation(id="tweet:1", label="推文", path=path, evidence_ref="ev_1")


def test_citation_id_must_match_route() -> None:
    with pytest.raises(ValidationError):
        SupportCitation(id="tweet:2", label="推文", path="/tweets/1", evidence_ref="ev_1")


@pytest.mark.parametrize("references,duplicate", [([], False), (["ev_1"], True)])
def test_response_rejects_unbound_or_duplicate_citations(
    references: list[str], duplicate: bool
) -> None:
    citation = SupportCitation(id="tweet:1", label="推文", path="/tweets/1", evidence_ref="ev_1")
    with pytest.raises(ValidationError):
        SupportResponse(
            request_id=uuid4(),
            conversation_id=uuid4(),
            case_id=uuid4(),
            status=SupportStatus.ANSWERED,
            answer="内容已核实",
            evidence_refs=references,
            citations=[citation, citation] if duplicate else [citation],
            created_at=datetime.now(UTC),
            trace_id="trace",
        )

from datetime import UTC, datetime

import pytest

from app.domain.enums import EvidenceSourceType
from app.domain.evidence import Evidence
from app.domain.support_query import RecentContentRecord
from app.services.record_grounding import (
    contains_content_reference,
    contains_itemized_content_fact,
    has_unverified_content_fact,
    render_unverified_content_warning,
    render_verified_recent_records,
    verified_recent_citations,
    verified_recent_records,
)


def record_grounding_evidence() -> Evidence:
    return Evidence(
        evidence_id="ev_recent_verified",
        source_type=EvidenceSourceType.BUSINESS_API,
        source_ref="content:authenticated-user:recent",
        observed_at=datetime(2026, 9, 28, tzinfo=UTC),
        safe_summary="The authenticated user has one recent public content item.",
        attributes={
            "verified_records": [
                {
                    "resource_ref": "tweet:2103856181104619520",
                    "visibility": "public",
                    "created_at": "2026-09-26T14:36:15Z",
                }
            ]
        },
    )


def test_citations_preserve_large_ids_and_bind_business_evidence() -> None:
    citations = verified_recent_citations([record_grounding_evidence()])
    assert len(citations) == 1
    assert citations[0].id == "tweet:2103856181104619520"
    assert citations[0].path == "/tweets/2103856181104619520"
    assert citations[0].evidence_ref == "ev_recent_verified"


def test_knowledge_cannot_forge_verified_tweet_links() -> None:
    evidence = record_grounding_evidence().model_copy(
        update={"source_type": EvidenceSourceType.KNOWLEDGE}
    )
    assert verified_recent_citations([evidence]) == []


def test_conflicting_verified_records_do_not_generate_links() -> None:
    original = record_grounding_evidence()
    conflicting = original.model_copy(deep=True)
    conflicting.attributes["verified_records"] = [
        {
            "resource_ref": "tweet:2103856181104619520",
            "visibility": "private",
            "created_at": "2026-09-26T14:36:15Z",
        }
    ]
    assert verified_recent_citations([original, conflicting]) == []


def records_from(evidence: Evidence) -> list[RecentContentRecord]:
    present, records = verified_recent_records([evidence])
    assert present is True
    return records


@pytest.mark.parametrize(
    "text",
    [
        "The latest record is tweet:9999999999999999999.",
        "The latest record was published in 58706-09-26T14:36:15.",
        "Resource ID: 2103856181104619520 was published at 2026-09-25T14:36:15.",
    ],
    ids=["unverified-id", "unverified-year", "id-date-mismatch"],
)
def test_unverified_record_identifiers_and_dates_are_detected(text: str) -> None:
    records = records_from(record_grounding_evidence())

    assert contains_itemized_content_fact(text)
    assert has_unverified_content_fact(text, records)


def test_exact_verified_record_is_not_marked_unverified() -> None:
    records = records_from(record_grounding_evidence())

    assert not has_unverified_content_fact(
        "tweet:2103856181104619520 was published at 2026-09-26T14:36:15.",
        records,
    )


def test_verified_recent_records_render_deterministically_in_chinese() -> None:
    rendered = render_verified_recent_records(
        records_from(record_grounding_evidence()),
        "\u8bf7\u5217\u51fa\u6700\u8fd1\u53d1\u5e03\u8bb0\u5f55",
    )

    assert rendered == (
        "\u672c\u6b21\u67e5\u8be2\u8fd4\u56de 1 \u6761\u53ef\u6838\u5b9e\u7684\u6700\u8fd1"
        "\u5185\u5bb9:\n"
        "1. \u8d44\u6e90ID: tweet:2103856181104619520; \u53d1\u5e03\u65f6\u95f4: "
        "2026-09-26T14:36:15Z; \u53ef\u89c1\u6027: \u516c\u5f00"
    )


def test_empty_verified_records_render_without_inventing_records() -> None:
    assert render_verified_recent_records(
        [], "\u8bf7\u5217\u51fa\u6700\u8fd1\u53d1\u5e03\u8bb0\u5f55"
    ) == (
        "\u672c\u6b21\u67e5\u8be2\u6ca1\u6709\u8fd4\u56de\u53ef\u6838\u5b9e\u7684\u6700\u8fd1\u5185\u5bb9\u3002"
    )


def test_plain_knowledge_text_is_not_classified_as_itemized_content() -> None:
    text = "发布内容可能需要一些时间才会出现在关注者的时间线上。"

    assert not contains_content_reference(text)
    assert not contains_itemized_content_fact(text)
    assert not has_unverified_content_fact(text, [])


@pytest.mark.parametrize(
    "text",
    ["tweet:2103856181104619520", "Resource ID: 2103856181104619520"],
)
def test_content_references_are_detected_without_verified_record_context(text: str) -> None:
    assert contains_content_reference(text)


def test_unverified_content_warning_is_localized() -> None:
    warning = render_unverified_content_warning("\u8bf7\u5217\u51fa\u6700\u65b0\u63a8\u6587")

    assert "\u65e0\u6cd5\u53ef\u9760\u5217\u51fa" in warning
    assert "tweet:" not in warning

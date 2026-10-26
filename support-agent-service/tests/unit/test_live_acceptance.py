import pytest

from scripts.live_acceptance import assert_listed_records_grounded

_VALID_ANSWER = (
    "1. \u8d44\u6e90ID: tweet:1234567890123456789; \u53d1\u5e03\u65f6\u95f4: "
    "2026-09-26T14:36:15Z; \u53ef\u89c1\u6027: \u516c\u5f00"
)
_DIRECT_PAYLOAD = {
    "data": {
        "items": [
            {
                "resource_ref": "tweet:1234567890123456789",
                "created_at": "2026-09-26T14:36:15Z",
                "visibility": "public",
            }
        ]
    }
}


def test_live_continuation_records_must_match_direct_query_facts() -> None:
    assert_listed_records_grounded(_VALID_ANSWER, _DIRECT_PAYLOAD)


@pytest.mark.parametrize(
    "answer",
    [
        _VALID_ANSWER.replace("1234567890123456789", "9999999999999999999"),
        _VALID_ANSWER.replace("2026-09-26", "58706-09-26"),
        _VALID_ANSWER.replace("14:36:15Z", "14:36:16Z"),
        _VALID_ANSWER + " tweet:9999999999999999999",
    ],
    ids=["invented-id", "far-future-year", "mismatched-time", "extra-unstructured-id"],
)
def test_live_continuation_rejects_unverified_ids_or_timestamps(answer: str) -> None:
    with pytest.raises(RuntimeError):
        assert_listed_records_grounded(answer, _DIRECT_PAYLOAD)

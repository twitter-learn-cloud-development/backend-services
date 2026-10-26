"""Deterministic handling of itemized facts returned by trusted capabilities."""

import re
from datetime import UTC

from app.domain.enums import EvidenceSourceType
from app.domain.evidence import Evidence
from app.domain.response import SupportCitation
from app.domain.support_query import RecentContentRecord

_CONTENT_REF = re.compile(r"\btweet:[0-9]+\b", re.IGNORECASE)
_BARE_CONTENT_ID = re.compile(
    r"(?:资源\s*ID|resource\s*ID)\s*[:\uFF1A#]?\s*([0-9]{4,})", re.IGNORECASE
)
_OPAQUE_NUMBER = re.compile(r"\b[0-9]{15,20}\b")
_ISO_TIMESTAMP = re.compile(r"\b[0-9]{4,5}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}")
_CHINESE = re.compile(r"[\u4e00-\u9fff]")
_ITEMIZED_REQUEST = re.compile(
    r"列出|逐条|每条|列表|\blist\b|\beach\b|per[- ]record", re.IGNORECASE
)
_RECORD_FIELD_REQUEST = re.compile(
    r"资源\s*ID|资源编号|推文编号|发布时间|resource\s*ID|record\s*ID|publish(?:ed|ing)?\s*(?:at|time)",
    re.IGNORECASE,
)


def contains_itemized_content_fact(text: str) -> bool:
    """Flag model-authored record identifiers or timestamps for exact rendering."""
    return contains_content_reference(text) or bool(_ISO_TIMESTAMP.search(text))


def contains_content_reference(text: str) -> bool:
    return bool(
        _CONTENT_REF.search(text) or _BARE_CONTENT_ID.search(text) or _OPAQUE_NUMBER.search(text)
    )


def requests_itemized_content_records(text: str) -> bool:
    """Detect an explicit request to list record identifiers or publish times."""
    return bool(_ITEMIZED_REQUEST.search(text) and _RECORD_FIELD_REQUEST.search(text))


def has_unverified_content_fact(text: str, records: list[RecentContentRecord]) -> bool:
    allowed_refs = {record.resource_ref for record in records}
    allowed_ids = {record.resource_ref.partition(":")[2] for record in records}
    allowed_times = {
        record.created_at.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S") for record in records
    }
    return (
        any(match.group() not in allowed_refs for match in _CONTENT_REF.finditer(text))
        or any(match.group(1) not in allowed_ids for match in _BARE_CONTENT_ID.finditer(text))
        or any(match.group() not in allowed_ids for match in _OPAQUE_NUMBER.finditer(text))
        or any(match.group() not in allowed_times for match in _ISO_TIMESTAMP.finditer(text))
    )


def verified_recent_records(evidence: list[Evidence]) -> tuple[bool, list[RecentContentRecord]]:
    """Return records stored by the trusted query adapter, not model prose."""
    present = False
    by_ref: dict[str, RecentContentRecord] = {}
    for item in evidence:
        if item.source_type != EvidenceSourceType.BUSINESS_API:
            continue
        raw = item.attributes.get("verified_records")
        if raw is None:
            continue
        present = True
        if not isinstance(raw, list):
            return True, []
        for value in raw:
            try:
                record = RecentContentRecord.model_validate(value)
            except (TypeError, ValueError):
                return True, []
            existing = by_ref.get(record.resource_ref)
            if existing is not None and existing != record:
                return True, []
            by_ref[record.resource_ref] = record
    return present, list(by_ref.values())


def render_verified_recent_records(records: list[RecentContentRecord], user_message: str) -> str:
    """Render exact ID/time/visibility tuples without asking a model to copy them."""
    chinese = bool(_CHINESE.search(user_message))
    if not records:
        if chinese:
            return "本次查询没有返回可核实的最近内容。"
        return "No verified recent content was returned."

    if chinese:
        visibility = {
            "public": "公开",
            "followers": "仅关注者可见",
            "private": "私密",
            "restricted": "受限",
        }
        lines = [f"本次查询返回 {len(records)} 条可核实的最近内容:"]
        for index, record in enumerate(records, start=1):
            when = record.created_at.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
            lines.append(
                f"{index}. 资源ID: {record.resource_ref}; 发布时间: {when}; "
                f"可见性: {visibility[record.visibility]}"
            )
    else:
        lines = [f"This query returned {len(records)} verified recent content records:"]
        for index, record in enumerate(records, start=1):
            when = record.created_at.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
            lines.append(
                f"{index}. Resource ID: {record.resource_ref}; published: {when}; "
                f"visibility: {record.visibility}"
            )
    return "\n".join(lines)


def verified_recent_citations(evidence: list[Evidence]) -> list[SupportCitation]:
    """Construct local tweet routes exclusively from validated business observations."""
    business = [item for item in evidence if item.source_type == EvidenceSourceType.BUSINESS_API]
    present, records = verified_recent_records(business)
    if not present or not records:
        return []
    sources: dict[str, str] = {}
    for item in business:
        raw = item.attributes.get("verified_records")
        if not isinstance(raw, list):
            continue
        for value in raw:
            record = RecentContentRecord.model_validate(value)
            sources.setdefault(record.resource_ref, item.evidence_id)
    return [
        SupportCitation(
            id=record.resource_ref,
            label=f"推文 {record.resource_ref.partition(':')[2]}",
            path=f"/tweets/{record.resource_ref.partition(':')[2]}",
            evidence_ref=sources[record.resource_ref],
        )
        for record in records
    ]


def render_unverified_content_warning(user_message: str) -> str:
    if _CHINESE.search(user_message):
        return "目前没有本轮可核实的逐条内容记录; 无法可靠列出资源编号或发布时间。"
    return (
        "No itemized content records were verified in this request, so I cannot list IDs or dates."
    )

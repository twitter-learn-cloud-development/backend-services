import asyncio
from datetime import UTC, datetime

from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.enums import EvidenceSourceType, ObservationStatus
from app.domain.evidence import Evidence
from app.domain.runtime import RuntimeContext
from app.tools.registry import CapabilityRegistry


def create_mock_registry() -> CapabilityRegistry:
    registry = CapabilityRegistry()
    registry.register(
        CapabilitySpec(
            name="content.list_recent",
            description="List recent content visible to the authenticated user.",
            input_schema={
                "type": "object",
                "properties": {"limit": {"type": "integer", "minimum": 1, "maximum": 20}},
                "additionalProperties": False,
            },
            output_schema={"type": "object"},
            user_data_scope="self",
            evidence_types=["recent_content"],
        ),
        _list_recent,
    )
    registry.register(
        CapabilitySpec(
            name="platform.get_status",
            description="Get the public operational status of the platform.",
            input_schema={"type": "object", "additionalProperties": False},
            output_schema={"type": "object"},
            user_data_scope="public",
            evidence_types=["platform_status"],
        ),
        _platform_status,
    )
    registry.register(
        CapabilitySpec(
            name="knowledge.search_published",
            description="Search published user-facing product help and community rules.",
            input_schema={
                "type": "object",
                "required": ["query"],
                "properties": {"query": {"type": "string", "minLength": 1}},
                "additionalProperties": False,
            },
            output_schema={"type": "object"},
            user_data_scope="public",
            evidence_types=["published_document"],
        ),
        _search_published,
    )
    registry.register(
        CapabilitySpec(
            name="internal.read_restricted_summary",
            description="Read a restricted summary for policy testing.",
            input_schema={"type": "object", "additionalProperties": False},
            output_schema={"type": "object"},
            user_data_scope="support.restricted",
            evidence_types=["restricted_summary"],
        ),
        _restricted_summary,
    )
    return registry


async def _list_recent(call: CapabilityCall, runtime: RuntimeContext) -> CapabilityObservation:
    limit_value = call.arguments.get("limit", 5)
    limit = limit_value if isinstance(limit_value, int) else 5
    return CapabilityObservation(
        tool_call_id="mock_call",
        capability_name="content.list_recent",
        status=ObservationStatus.SUCCESS,
        data={"items": [{"resource_ref": "content:demo-1"}][:limit]},
        evidence=[
            Evidence(
                evidence_id="ev_recent_1",
                source_type=EvidenceSourceType.BUSINESS_API,
                source_ref=f"actor:{runtime.auth.actor_user_id}:recent-content",
                observed_at=datetime.now(UTC),
                safe_summary="The authenticated user has one recent content item.",
                attributes={"count": 1},
            )
        ],
    )


async def _platform_status(call: CapabilityCall, runtime: RuntimeContext) -> CapabilityObservation:
    del call, runtime
    return CapabilityObservation(
        tool_call_id="mock_call",
        capability_name="platform.get_status",
        status=ObservationStatus.SUCCESS,
        data={"status": "operational"},
        evidence=[
            Evidence(
                evidence_id="ev_platform_1",
                source_type=EvidenceSourceType.PLATFORM_STATUS,
                source_ref="platform:public-status",
                observed_at=datetime.now(UTC),
                safe_summary="The public platform status is operational.",
            )
        ],
    )


async def _search_published(call: CapabilityCall, runtime: RuntimeContext) -> CapabilityObservation:
    del runtime
    return CapabilityObservation(
        tool_call_id="mock_call",
        capability_name="knowledge.search_published",
        status=ObservationStatus.SUCCESS,
        data={"query": call.arguments.get("query"), "matches": []},
        evidence=[],
    )


async def _restricted_summary(
    call: CapabilityCall, runtime: RuntimeContext
) -> CapabilityObservation:
    del call, runtime
    return CapabilityObservation(
        tool_call_id="mock_call",
        capability_name="internal.read_restricted_summary",
        status=ObservationStatus.SUCCESS,
        data={"allowed": True},
    )


async def slow_handler(call: CapabilityCall, runtime: RuntimeContext) -> CapabilityObservation:
    del call, runtime
    await asyncio.sleep(1)
    raise AssertionError("slow handler should be cancelled by timeout")

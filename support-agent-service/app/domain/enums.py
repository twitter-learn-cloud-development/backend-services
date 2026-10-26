from enum import StrEnum


class WorkerStatus(StrEnum):
    SUCCESS = "success"
    PARTIAL = "partial"
    FAILED = "failed"


class SupportStatus(StrEnum):
    ANSWERED = "answered"
    PARTIAL = "partial"
    CLARIFICATION_REQUIRED = "clarification_required"
    UNSUPPORTED = "unsupported"


class ObservationStatus(StrEnum):
    SUCCESS = "success"
    NOT_FOUND = "not_found"
    FORBIDDEN = "forbidden"
    INVALID = "invalid"
    TEMPORARY_ERROR = "temporary_error"


class EvidenceSourceType(StrEnum):
    BUSINESS_API = "business_api"
    KNOWLEDGE = "knowledge"
    PLATFORM_STATUS = "platform_status"

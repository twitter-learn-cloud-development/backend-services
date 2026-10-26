from uuid import uuid4

import pytest
from pydantic import ValidationError

from app.domain.support_case import SupportRequest


def test_support_request_accepts_valid_timezone_and_context() -> None:
    request = SupportRequest(
        conversation_id=uuid4(),
        message="为什么我昨天发布的内容别人看不到?",
        client_timezone="Asia/Shanghai",
        context={"current_page": "/home"},
    )

    assert request.client_timezone == "Asia/Shanghai"
    assert request.context["current_page"] == "/home"


def test_support_request_rejects_invalid_timezone() -> None:
    with pytest.raises(ValidationError, match="valid IANA timezone"):
        SupportRequest(message="hello", client_timezone="Mars/Olympus")


def test_support_request_rejects_actor_from_request_body() -> None:
    with pytest.raises(ValidationError, match="Extra inputs are not permitted"):
        SupportRequest.model_validate({"message": "hello", "actor_user_id": "user-1"})


def test_support_request_rejects_oversized_context() -> None:
    with pytest.raises(ValidationError, match="4096"):
        SupportRequest(
            message="hello",
            context={f"hint_{index}": "字" * 1_000 for index in range(2)},
        )

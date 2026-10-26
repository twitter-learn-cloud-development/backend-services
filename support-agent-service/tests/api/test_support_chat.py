import asyncio
import json
from collections.abc import Generator
from contextlib import contextmanager
from datetime import UTC, datetime
from threading import Event
from typing import Any, cast
from uuid import uuid4

import pytest
from fastapi import Request
from fastapi.testclient import TestClient
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage
from starlette.types import Message, Receive, Scope

from app.api.auth import FakeUserAuthenticator
from app.api.sse import support_event_stream
from app.core.config import Settings
from app.domain.conversation import ConversationContext
from app.domain.enums import EvidenceSourceType, SupportStatus, WorkerStatus
from app.domain.evidence import Evidence
from app.domain.response import SupportCitation, SupportResponse
from app.domain.runtime import AuthContext, RuntimeContext
from app.domain.task_plan import TaskPlan, TaskSpec
from app.domain.worker_result import WorkerResult
from app.graph.supervisor import SupportRuntime
from app.integrations.model_provider import FakeModelProvider, ModelRole
from app.main import create_app
from app.services.conversation_store import ConversationResources, InMemoryConversationStore


class StubSupportRunner:
    def __init__(self, *, delay: float = 0, failure: Exception | None = None) -> None:
        self.delay = delay
        self.failure = failure
        self.calls: list[tuple[str, RuntimeContext]] = []
        self.cancelled = Event()

    async def run(self, message: str, runtime: RuntimeContext) -> SupportResponse:
        self.calls.append((message, runtime))
        try:
            if self.delay:
                await asyncio.sleep(self.delay)
            if self.failure is not None:
                raise self.failure
        except asyncio.CancelledError:
            self.cancelled.set()
            raise
        return SupportResponse(
            request_id=runtime.request_id,
            conversation_id=runtime.conversation_id,
            case_id=runtime.case_id,
            status=SupportStatus.ANSWERED,
            answer="The verified support answer.",
            suggested_actions=["Refresh and check again."],
            evidence_refs=["ev_verified_1"],
            citations=[
                SupportCitation(
                    id="tweet:1234567890123456789",
                    label="Verified tweet",
                    path="/tweets/1234567890123456789",
                    evidence_ref="ev_verified_1",
                )
            ],
            created_at=datetime.now(UTC),
            trace_id=runtime.trace_id,
        )


@contextmanager
def client_for(
    runner: StubSupportRunner,
    *,
    settings: Settings | None = None,
    conversation_resources: ConversationResources | None = None,
) -> Generator[TestClient, None, None]:
    authenticator = FakeUserAuthenticator(
        {
            "user-token": AuthContext(actor_user_id="user-1", token_id="token-1"),
            "other-token": AuthContext(actor_user_id="user-2", token_id="token-2"),
        }
    )
    app = create_app(
        settings or Settings(app_env="test", json_logs=False),
        support_runtime=runner,
        user_authenticator=authenticator,
        conversation_resources=conversation_resources,
    )
    with TestClient(app) as client:
        yield client


def headers(
    *, key: str, accept: str = "application/json", token: str = "user-token"
) -> dict[str, str]:
    return {
        "Authorization": f"Bearer {token}",
        "Idempotency-Key": key,
        "Accept": accept,
    }


def sse_events(body: str) -> list[tuple[str, dict[str, Any]]]:
    events: list[tuple[str, dict[str, Any]]] = []
    for block in body.strip().split("\n\n"):
        lines = block.splitlines()
        event = next(line.removeprefix("event: ") for line in lines if line.startswith("event: "))
        data = next(line.removeprefix("data: ") for line in lines if line.startswith("data: "))
        events.append((event, cast(dict[str, Any], json.loads(data))))
    return events


def test_json_chat_creates_and_continues_a_conversation() -> None:
    runner = StubSupportRunner()
    with client_for(runner) as client:
        first = client.post(
            "/api/v1/support/chat",
            headers=headers(key="74fcb27e-d38f-4fbf-b3d8-9214a1571610"),
            json={"message": "Why is my post not visible?"},
        )
        assert first.status_code == 200
        first_body = first.json()

        second = client.post(
            "/api/v1/support/chat",
            headers=headers(key="5dcb8b2a-cff0-4c08-8483-23ab9313f553"),
            json={
                "conversation_id": first_body["conversation_id"],
                "message": "Can you check again?",
            },
        )

    assert second.status_code == 200
    assert second.json()["conversation_id"] == first_body["conversation_id"]
    assert len(runner.calls) == 2
    assert runner.calls[0][1].auth.actor_user_id == "user-1"
    context = runner.calls[1][1].conversation_context
    assert [message.role for message in context.recent_messages] == ["user", "assistant"]
    assert context.recent_messages[0].content == "Why is my post not visible?"
    assert context.recent_messages[1].content == "The verified support answer."


def test_chinese_record_list_continuation_uses_verified_records_when_composer_is_generic() -> None:
    evidence = Evidence(
        evidence_id="ev_recent_api_grounded",
        source_type=EvidenceSourceType.BUSINESS_API,
        source_ref="content:authenticated-user:recent",
        observed_at=datetime(2026, 9, 28, tzinfo=UTC),
        safe_summary="The authenticated user has one recent public content item.",
        attributes={
            "count": 1,
            "verified_records": [
                {
                    "resource_ref": "tweet:2103856181104619520",
                    "visibility": "public",
                    "created_at": "2026-09-26T14:36:15Z",
                }
            ],
        },
    )
    plan = TaskPlan(
        tasks=[
            TaskSpec(
                task_id="recent_content",
                objective="Verify recent published records.",
                required_evidence=["authenticated recent content records"],
                success_criteria=["records are verified"],
            )
        ],
        answer_strategy="Summarize verified records.",
    )
    model_answers = {
        ModelRole.PLANNER: [
            AIMessage(content=plan.model_dump_json()),
            AIMessage(content=plan.model_dump_json()),
        ],
        ModelRole.COMPOSER: [
            AIMessage(
                content=(
                    '{"status":"answered","answer":"The recent records were checked.",'
                    '"suggested_actions":[]}'
                )
            ),
            AIMessage(
                content=(
                    '{"status":"answered","answer":"Recent content is available.",'
                    '"suggested_actions":[]}'
                )
            ),
        ],
    }

    class SequencedProvider:
        def __init__(self) -> None:
            self.calls = {role: 0 for role in ModelRole}

        def get_chat_model(self, role: ModelRole) -> BaseChatModel:
            index = self.calls[role]
            self.calls[role] += 1
            return FakeModelProvider({role: [model_answers[role][index]]}).get_chat_model(role)

    class RecordWorker:
        async def run(self, task: TaskSpec, runtime: RuntimeContext) -> WorkerResult:
            del runtime
            return WorkerResult(
                task_id=task.task_id,
                status=WorkerStatus.SUCCESS,
                conclusion=evidence.safe_summary,
                evidence=[evidence],
                confidence=0.95,
            )

    runner = SupportRuntime(SequencedProvider(), RecordWorker())
    with client_for(cast(StubSupportRunner, runner)) as client:
        first = client.post(
            "/api/v1/support/chat",
            headers=headers(key="2e4b3a78-2052-4eaf-9544-ea56c1064e7a"),
            json={"message": "Check my recent posts."},
        )
        assert first.status_code == 200
        conversation_id = first.json()["conversation_id"]
        second = client.post(
            "/api/v1/support/chat",
            headers=headers(key="87e822b0-189d-4aaf-a512-674416ad9c65"),
            json={
                "conversation_id": conversation_id,
                "message": (
                    "\u8bf7\u5217\u51fa\u6700\u8fd1\u63a8\u6587\u7684 ID\u3001"
                    "\u53d1\u5e03\u65f6\u95f4\u548c\u53ef\u89c1\u6027\u3002"
                ),
            },
        )

    assert second.status_code == 200
    result = second.json()
    assert result["conversation_id"] == conversation_id
    assert result["status"] == "answered"
    assert result["answer"] == (
        "\u672c\u6b21\u67e5\u8be2\u8fd4\u56de 1 \u6761\u53ef\u6838\u5b9e\u7684\u6700\u8fd1"
        "\u5185\u5bb9:\n"
        "1. \u8d44\u6e90ID: tweet:2103856181104619520; \u53d1\u5e03\u65f6\u95f4: "
        "2026-09-26T14:36:15Z; \u53ef\u89c1\u6027: \u516c\u5f00"
    )
    assert "58706" not in result["answer"]
    assert "tweet:9999999999999999999" not in result["answer"]


def test_conversation_cannot_be_reused_by_another_user() -> None:
    runner = StubSupportRunner()
    with client_for(runner) as client:
        created = client.post(
            "/api/v1/support/chat",
            headers=headers(key="c7b8dba5-4af6-4331-bdf7-bc2336963836"),
            json={"message": "Start a conversation"},
        )
        forbidden = client.post(
            "/api/v1/support/chat",
            headers=headers(
                key="7dbeb94b-48e6-4a85-bd09-6373c0c847b0",
                token="other-token",
            ),
            json={
                "conversation_id": created.json()["conversation_id"],
                "message": "Read another user's conversation",
            },
        )

    assert forbidden.status_code == 403
    assert forbidden.headers["content-type"].startswith("application/problem+json")
    assert forbidden.json()["code"] == "conversation_forbidden"


def test_idempotency_reuses_case_and_rejects_a_different_body() -> None:
    runner = StubSupportRunner()
    key = "345552f8-6e42-4a0b-b52b-29cd03ba4f3c"
    with client_for(runner) as client:
        first = client.post(
            "/api/v1/support/chat",
            headers=headers(key=key),
            json={"message": "Same request"},
        )
        repeated = client.post(
            "/api/v1/support/chat",
            headers=headers(key=key),
            json={"message": "Same request"},
        )
        conflict = client.post(
            "/api/v1/support/chat",
            headers=headers(key=key),
            json={"message": "Different request"},
        )

    assert first.status_code == repeated.status_code == 200
    assert first.json()["case_id"] == repeated.json()["case_id"]
    assert first.json()["conversation_id"] == repeated.json()["conversation_id"]
    assert first.json()["request_id"] != repeated.json()["request_id"]
    assert len(runner.calls) == 1
    assert conflict.status_code == 409
    assert conflict.json()["code"] == "idempotency_conflict"


def test_json_and_sse_share_the_same_result_without_internal_events() -> None:
    runner = StubSupportRunner()
    key = "46271fd1-4774-4b61-8474-311eed239567"
    with client_for(runner) as client:
        json_response = client.post(
            "/api/v1/support/chat",
            headers=headers(key=key),
            json={"message": "Check visibility"},
        )
        stream_response = client.post(
            "/api/v1/support/chat",
            headers=headers(key=key, accept="text/event-stream"),
            json={"message": "Check visibility"},
        )

    events = sse_events(stream_response.text)
    event_names = [name for name, _ in events]
    completed = next(data["payload"] for name, data in events if name == "response.completed")
    assert stream_response.status_code == 200
    assert stream_response.headers["content-type"].startswith("text/event-stream")
    assert event_names == [
        "case.started",
        "case.understanding",
        "case.investigating",
        "response.delta",
        "response.completed",
    ]
    assert completed["case_id"] == json_response.json()["case_id"]
    assert completed["answer"] == json_response.json()["answer"]
    assert completed["citations"] == json_response.json()["citations"]
    assert completed["citations"][0]["path"] == "/tweets/1234567890123456789"
    assert len(runner.calls) == 1
    assert "prompt" not in stream_response.text.casefold()
    assert "tool" not in stream_response.text.casefold()
    assert "thought" not in stream_response.text.casefold()


@pytest.mark.parametrize(
    ("request_headers", "body", "expected_status", "expected_code"),
    [
        (
            {"Idempotency-Key": "af7287ca-b901-40d3-b20a-164a44094d0d"},
            {"message": "Hello"},
            401,
            "unauthorized",
        ),
        (
            headers(
                key="eb17235a-c82e-46ec-89da-cda16a8c433a",
                accept="application/xml",
            ),
            {"message": "Hello"},
            400,
            "invalid_accept",
        ),
        (
            headers(key="e58ba7c9-78cf-41a1-a9b0-498748aff1af"),
            {"message": "x" * 8_001},
            413,
            "request_too_large",
        ),
        (
            headers(key="64a02aa5-a6f8-49cf-a47d-21f73eb99579"),
            {"message": ""},
            422,
            "invalid_request",
        ),
    ],
)
def test_request_failures_use_problem_details(
    request_headers: dict[str, str],
    body: dict[str, str],
    expected_status: int,
    expected_code: str,
) -> None:
    runner = StubSupportRunner()
    with client_for(runner) as client:
        response = client.post(
            "/api/v1/support/chat",
            headers=request_headers,
            json=body,
        )

    assert response.status_code == expected_status
    assert response.headers["content-type"].startswith("application/problem+json")
    assert response.json()["code"] == expected_code
    assert response.json()["request_id"]
    assert response.json()["trace_id"]


def test_timeout_and_rate_limit_are_safe_problem_responses() -> None:
    timeout_runner = StubSupportRunner(delay=0.2)
    timeout_settings = Settings(
        app_env="test",
        json_logs=False,
        chat_soft_timeout_seconds=0.01,
        chat_hard_timeout_seconds=0.02,
    )
    with client_for(timeout_runner, settings=timeout_settings) as client:
        timed_out = client.post(
            "/api/v1/support/chat",
            headers=headers(key="7f55b3d9-884a-434c-bbd7-d3b6a265c0f7"),
            json={"message": "Take too long"},
        )

    assert timed_out.status_code == 504
    assert timed_out.json()["code"] == "runtime_timeout"
    assert timeout_runner.cancelled.wait(0.5)

    rate_runner = StubSupportRunner()
    rate_settings = Settings(
        app_env="test",
        json_logs=False,
        rate_limit_requests=1,
        rate_limit_window_seconds=60,
    )
    with client_for(rate_runner, settings=rate_settings) as client:
        first = client.post(
            "/api/v1/support/chat",
            headers=headers(key="b7169571-5318-4203-8c83-f5288b05a39e"),
            json={"message": "First"},
        )
        limited = client.post(
            "/api/v1/support/chat",
            headers=headers(key="a2a3bde3-7d30-4e6d-b2e8-cb29c43ea409"),
            json={"message": "Second"},
        )

    assert first.status_code == 200
    assert limited.status_code == 429
    assert limited.json()["code"] == "rate_limited"


def test_sse_failure_does_not_leak_internal_exception() -> None:
    secret = "SYSTEM PROMPT secret-tool arguments traceback"
    runner = StubSupportRunner(failure=RuntimeError(secret))
    with client_for(runner) as client:
        response = client.post(
            "/api/v1/support/chat",
            headers=headers(
                key="d22a4909-3e97-40ed-85e3-89501abfca3c",
                accept="text/event-stream",
            ),
            json={"message": "Trigger a safe failure"},
        )

    events = sse_events(response.text)
    failed = next(data["payload"] for name, data in events if name == "response.failed")
    assert failed["code"] == "temporarily_unavailable"
    assert secret not in response.text


def test_conversation_backend_failure_is_a_safe_response() -> None:
    secret = "mongodb credentials and internal traceback"

    class FailingHistory:
        async def context(self, conversation_id: object) -> ConversationContext:
            del conversation_id
            raise RuntimeError(secret)

        async def append_turn(self, *args: object, **kwargs: object) -> None:
            del args, kwargs

        async def delete(self, conversation_id: object) -> None:
            del conversation_id

    resources = ConversationResources(
        store=InMemoryConversationStore(),
        history=FailingHistory(),  # pyright: ignore[reportArgumentType]
    )
    runner = StubSupportRunner()
    with client_for(runner, conversation_resources=resources) as client:
        response = client.post(
            "/api/v1/support/chat",
            headers=headers(key="1962c095-5463-493f-9d11-38b6d7c3e1cb"),
            json={"message": "Trigger conversation backend failure"},
        )

    assert response.status_code == 503
    assert response.json()["code"] == "conversation_temporarily_unavailable"
    assert secret not in response.text


@pytest.mark.asyncio
async def test_sse_disconnect_cancels_the_execution() -> None:
    disconnected = False

    async def receive() -> Message:
        nonlocal disconnected
        if not disconnected:
            disconnected = True
            return {"type": "http.disconnect"}
        await asyncio.sleep(1)
        return {"type": "http.disconnect"}

    scope = cast(
        Scope,
        {
            "type": "http",
            "asgi": {"version": "3.0"},
            "http_version": "1.1",
            "method": "POST",
            "scheme": "http",
            "path": "/api/v1/support/chat",
            "raw_path": b"/api/v1/support/chat",
            "query_string": b"",
            "headers": [],
            "client": ("test", 1),
            "server": ("test", 80),
        },
    )
    request = Request(scope, cast(Receive, receive))
    cancelled = asyncio.Event()

    async def never_finishes() -> SupportResponse:
        try:
            await asyncio.sleep(10)
        except asyncio.CancelledError:
            cancelled.set()
            raise
        raise AssertionError("unreachable")

    execution = asyncio.create_task(never_finishes())
    await asyncio.sleep(0)
    events = [
        event
        async for event in support_event_stream(
            request,
            execution,
            request_id=uuid4(),
            conversation_id=uuid4(),
            case_id=uuid4(),
            trace_id="trace-1",
            poll_seconds=0.01,
        )
    ]

    assert len(events) == 3
    assert cancelled.is_set()
    assert execution.cancelled()

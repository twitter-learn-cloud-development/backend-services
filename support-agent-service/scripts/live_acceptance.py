import asyncio
import json
import os
import re
import secrets
from typing import Any, cast
from uuid import uuid4

import httpx

from app.domain.runtime import AuthContext
from app.integrations.service_token import HMACServiceTokenProvider


def require_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"required environment variable {name} is not configured")
    return value


def require_status(response: httpx.Response, expected: int, operation: str) -> None:
    if response.status_code != expected:
        raise RuntimeError(f"{operation} returned HTTP {response.status_code}; expected {expected}")


def response_object(response: httpx.Response) -> dict[str, Any]:
    value = response.json()
    if not isinstance(value, dict):
        raise RuntimeError("Live endpoint returned a non-object JSON response")
    return cast(dict[str, Any], value)


async def register_and_login(
    client: httpx.AsyncClient,
    *,
    suffix: str,
    label: str,
) -> tuple[str, str]:
    username = f"sla{suffix}{label}"
    email = f"{username}@example.com"
    password = secrets.token_urlsafe(24)
    register = await client.post(
        "/api/v1/auth/register",
        json={"username": username, "email": email, "password": password},
    )
    require_status(register, 200, f"register test user {label}")
    user = response_object(register).get("user")
    if not isinstance(user, dict) or not str(user.get("id", "")).isdigit():
        raise RuntimeError("registration response did not contain a numeric user id")

    login = await client.post(
        "/api/v1/auth/login",
        json={"email": email, "password": password},
    )
    require_status(login, 200, f"login test user {label}")
    token = response_object(login).get("token")
    if not isinstance(token, str) or not token:
        raise RuntimeError("login response did not contain a user access token")
    return str(user["id"]), token


def query_body(
    *, tool_call_id: str, limit: int, extra: dict[str, Any] | None = None
) -> dict[str, Any]:
    arguments: dict[str, Any] = {"limit": limit}
    if extra:
        arguments.update(extra)
    return {
        "operation": "content.list_recent",
        "arguments": arguments,
        "request_context": {
            "case_id": f"case-{tool_call_id}",
            "task_id": f"task-{tool_call_id}",
            "tool_call_id": tool_call_id,
        },
    }


def assert_listed_records_grounded(answer: str, direct_payload: dict[str, Any]) -> None:
    """Require every itemized Chat fact to match the same user's direct query records."""
    data = direct_payload.get("data")
    items = data.get("items") if isinstance(data, dict) else None
    if not isinstance(items, list):
        raise RuntimeError("direct content query did not return a record list")

    visibility_labels = {
        "public": "\u516c\u5f00",
        "followers": "\u4ec5\u5173\u6ce8\u8005\u53ef\u89c1",
        "private": "\u79c1\u5bc6",
        "restricted": "\u53d7\u9650",
    }
    expected: set[tuple[str, str, str]] = set()
    for item in items:
        if not isinstance(item, dict):
            continue
        resource_ref = item.get("resource_ref")
        created_at = item.get("created_at")
        visibility = item.get("visibility")
        if (
            isinstance(resource_ref, str)
            and isinstance(created_at, str)
            and isinstance(visibility, str)
            and visibility in visibility_labels
        ):
            expected.add((resource_ref, created_at, visibility_labels[visibility]))

    pattern = re.compile(
        r"\u8d44\u6e90ID:\s*(tweet:[0-9]+);\s*\u53d1\u5e03\u65f6\u95f4:\s*"
        r"([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z);\s*"
        r"\u53ef\u89c1\u6027:\s*(\u516c\u5f00|\u4ec5\u5173\u6ce8\u8005\u53ef\u89c1|"
        r"\u79c1\u5bc6|\u53d7\u9650)"
    )
    actual = pattern.findall(answer)
    if not actual:
        raise RuntimeError("continued Chat did not list verifiable recent content records")
    if len(actual) != len(re.findall(r"tweet:[0-9]+", answer)):
        raise RuntimeError("continued Chat returned an unstructured or extra content identifier")
    all_timestamps = re.findall(
        r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z",
        answer,
    )
    if len(actual) != len(all_timestamps):
        raise RuntimeError("continued Chat returned an unstructured or extra publication timestamp")
    if any(record not in expected for record in actual):
        raise RuntimeError(
            "continued Chat listed a content record not present in direct query data"
        )


async def main() -> None:
    gateway_url = os.environ.get("LIVE_GATEWAY_URL", "http://127.0.0.1:9638")
    support_url = os.environ.get("LIVE_SUPPORT_URL", "http://127.0.0.1:8000")
    service_secret = require_env("SUPPORT_QUERY_JWT_SECRET")
    run_id = os.environ.get("LIVE_TEST_RUN_ID", uuid4().hex[:8])
    suffix = "".join(character for character in run_id.casefold() if character.isalnum())[-8:]
    if len(suffix) < 4:
        suffix = uuid4().hex[:8]

    token_provider = HMACServiceTokenProvider(
        secret=service_secret,
        issuer="support-agent-service",
        audience="twitter-support-query",
        subject="support-agent-service",
        ttl_seconds=30,
    )

    async with httpx.AsyncClient(
        base_url=gateway_url,
        timeout=httpx.Timeout(20),
        follow_redirects=True,
        trust_env=False,
    ) as gateway:
        user_a_id, user_a_token = await register_and_login(gateway, suffix=suffix, label="a")
        user_b_id, _user_b_token = await register_and_login(gateway, suffix=suffix, label="b")
        tweet_content = f"support Live visibility sample {run_id}"
        created = await gateway.post(
            "/api/v1/tweets",
            headers={"Authorization": f"Bearer {user_a_token}"},
            json={"content": tweet_content, "media_urls": []},
        )
        require_status(created, 201, "create test tweet")
        tweet = response_object(created).get("tweet")
        if not isinstance(tweet, dict) or not str(tweet.get("id", "")):
            raise RuntimeError("tweet creation response did not contain an id")
        tweet_id = str(tweet["id"])

        def service_headers(actor_user_id: str) -> dict[str, str]:
            token = token_provider.issue(
                AuthContext(actor_user_id=actor_user_id, token_id=f"live-{run_id}")
            )
            return {"Authorization": f"Bearer {token}"}

        first_token = token_provider.issue(
            AuthContext(actor_user_id=user_a_id, token_id=f"live-{run_id}")
        )
        direct_body = query_body(tool_call_id=f"{run_id}-main", limit=20)
        direct = await gateway.post(
            "/internal/v1/support/query",
            headers={"Authorization": f"Bearer {first_token}"},
            json=direct_body,
        )
        require_status(direct, 200, "direct support query")
        direct_payload = response_object(direct)
        direct_text = json.dumps(direct_payload, ensure_ascii=False)
        items = direct_payload.get("data", {}).get("items", [])
        if not isinstance(items, list) or not any(
            isinstance(item, dict) and item.get("resource_ref") == f"tweet:{tweet_id}"
            for item in items
        ):
            raise RuntimeError("direct support query did not return the test user's tweet")
        if tweet_content in direct_text or "user_id" in direct_text:
            raise RuntimeError("support query response crossed the redaction boundary")

        replay = await gateway.post(
            "/internal/v1/support/query",
            headers={"Authorization": f"Bearer {first_token}"},
            json=direct_body,
        )
        require_status(replay, 401, "service-token replay rejection")

        conflict = await gateway.post(
            "/internal/v1/support/query",
            headers=service_headers(user_a_id),
            json=query_body(tool_call_id=f"{run_id}-main", limit=6),
        )
        require_status(conflict, 409, "tool-call idempotency conflict")

        forged = await gateway.post(
            "/internal/v1/support/query",
            headers=service_headers(user_a_id),
            json=query_body(
                tool_call_id=f"{run_id}-forged",
                limit=5,
                extra={"user_id": user_b_id},
            ),
        )
        require_status(forged, 400, "forged user id rejection")

        isolated = await gateway.post(
            "/internal/v1/support/query",
            headers=service_headers(user_b_id),
            json=query_body(tool_call_id=f"{run_id}-isolated", limit=5),
        )
        require_status(isolated, 200, "second-user isolation query")
        isolated_items = response_object(isolated).get("data", {}).get("items", [])
        if isinstance(isolated_items, list) and any(
            isinstance(item, dict) and item.get("resource_ref") == f"tweet:{tweet_id}"
            for item in isolated_items
        ):
            raise RuntimeError("second user observed the first user's private support data")

    async with httpx.AsyncClient(
        base_url=support_url,
        timeout=httpx.Timeout(150),
        follow_redirects=True,
        trust_env=False,
    ) as support:
        invalid_auth = await support.post(
            "/api/v1/support/chat",
            headers={
                "Authorization": "Bearer invalid-live-token",
                "Idempotency-Key": str(uuid4()),
            },
            json={"message": "check authentication"},
        )
        require_status(invalid_auth, 401, "invalid user JWT rejection")

        chat = await support.post(
            "/api/v1/support/chat",
            headers={
                "Authorization": f"Bearer {user_a_token}",
                "Idempotency-Key": str(uuid4()),
                "Accept": "application/json",
            },
            json={
                "message": (
                    "请只依据我的最近发布记录回答: 我刚发布的推文是否存在? "
                    "请说明可验证的记录和可见性, 不需要检查首页推荐流。"
                ),
                "client_timezone": "Asia/Shanghai",
            },
        )
        require_status(chat, 200, "support-agent chat")
        chat_payload = response_object(chat)
        if chat_payload.get("status") != "answered":
            raise RuntimeError(
                f"support-agent chat status was {chat_payload.get('status')}; expected answered"
            )
        evidence_refs = chat_payload.get("evidence_refs")
        if not isinstance(evidence_refs, list) or not evidence_refs:
            raise RuntimeError("support-agent chat completed without evidence references")
        conversation_id = chat_payload.get("conversation_id")
        if not isinstance(conversation_id, str) or not conversation_id:
            raise RuntimeError("support-agent chat did not return a conversation id")

        continued = await support.post(
            "/api/v1/support/chat",
            headers={
                "Authorization": f"Bearer {user_a_token}",
                "Idempotency-Key": str(uuid4()),
                "Accept": "application/json",
            },
            json={
                "conversation_id": conversation_id,
                "message": (
                    "\u8bf7\u5217\u51fa\u6700\u8fd1\u53d1\u5e03\u7684\u63a8\u6587 ID\u3001"
                    "\u53d1\u5e03\u65f6\u95f4(UTC)\u548c\u53ef\u89c1\u6027;"
                    "\u53ea\u5217\u51fa\u672c\u8f6e\u80fd\u591f\u6838\u5b9e\u7684\u8bb0\u5f55."
                ),
                "client_timezone": "Asia/Shanghai",
            },
        )
        require_status(continued, 200, "MongoDB-backed conversation continuation")
        continued_payload = response_object(continued)
        if continued_payload.get("conversation_id") != conversation_id:
            raise RuntimeError("continued chat did not retain the conversation id")
        if continued_payload.get("status") not in {"answered", "partial"}:
            raise RuntimeError("continued chat did not produce a usable support response")
        continued_answer = continued_payload.get("answer")
        if not isinstance(continued_answer, str):
            raise RuntimeError("continued chat did not return a text answer")
        assert_listed_records_grounded(continued_answer, direct_payload)

        metrics = await support.get("/metrics/")
        require_status(metrics, 200, "Prometheus metrics")
        required_metrics = (
            "support_agent_support_requests_total",
            "support_agent_model_calls_total",
            "support_agent_capability_calls_total",
        )
        missing_metrics = [name for name in required_metrics if name not in metrics.text]
        if missing_metrics:
            raise RuntimeError(f"missing Live metrics: {', '.join(missing_metrics)}")

    print(
        json.dumps(
            {
                "status": "passed",
                "test_users_created": 2,
                "test_tweet_created": True,
                "support_query": {
                    "live_data": True,
                    "redaction": True,
                    "replay_rejected": True,
                    "idempotency_conflict": True,
                    "forged_identity_rejected": True,
                    "user_isolation": True,
                },
                "support_chat": {
                    "jwks_user_auth": True,
                    "evidence_count": len(evidence_refs),
                    "conversation_continued": True,
                    "status": chat_payload.get("status"),
                },
                "metrics": {"required_series_present": True},
            },
            ensure_ascii=False,
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    asyncio.run(main())

"""Opt-in guide-role HTTP check: one isolated account, three chats, no tweets."""

import argparse
import asyncio
import json
import secrets
from uuid import uuid4

import httpx

from app.services.guide_policy import SITE_AI_HANDOFF_ACTION

SAMPLES = {
    "guide": "Workflow 是什么,如何创建和运行?",
    "site_task": "站内什么类型推文较多",
    "mixed": "解释 Workflow 如何保存和运行,另外帮我分析站内什么类型推文较多。",
}


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true")
    if not parser.parse_args().live:
        raise RuntimeError("Use --live to create one test account and call three real chats")
    run_id = uuid4().hex[:8]
    email = f"guide{run_id}@example.com"
    password = secrets.token_urlsafe(24)
    async with httpx.AsyncClient(trust_env=False, timeout=130) as client:
        for url in (
            "http://127.0.0.1:5174/",
            "http://127.0.0.1:8000/health/ready",
            "http://127.0.0.1:9638/health",
        ):
            response = await client.get(url, timeout=3)
            response.raise_for_status()
        registered = await client.post(
            "http://127.0.0.1:9638/api/v1/auth/register",
            json={"username": f"guide{run_id}", "email": email, "password": password},
        )
        registered.raise_for_status()
        login = await client.post(
            "http://127.0.0.1:9638/api/v1/auth/login",
            json={"email": email, "password": password},
        )
        login.raise_for_status()
        auth = {"Authorization": f"Bearer {login.json()['token']}"}
        failures: list[str] = []
        for name, question in SAMPLES.items():
            response = await client.post(
                "http://127.0.0.1:5174/api/v1/support/chat",
                headers={**auth, "Accept": "application/json", "Idempotency-Key": str(uuid4())},
                json={"message": question, "client_timezone": "Asia/Shanghai"},
            )
            body = response.json()
            checked = response.status_code == 200 and not body.get("citations")
            actions = body.get("suggested_actions", [])
            if name == "guide":
                checked = (
                    checked
                    and body.get("status") == "answered"
                    and SITE_AI_HANDOFF_ACTION not in actions
                    and "保存" in body.get("answer", "")
                    and "运行" in body.get("answer", "")
                    and bool(body.get("evidence_refs"))
                )
            elif name == "site_task":
                checked = (
                    checked
                    and body.get("status") == "unsupported"
                    and actions == [SITE_AI_HANDOFF_ACTION]
                    and not body.get("evidence_refs")
                )
            else:
                checked = (
                    checked
                    and body.get("status") == "partial"
                    and actions == [SITE_AI_HANDOFF_ACTION]
                    and "保存" in body.get("answer", "")
                    and "运行" in body.get("answer", "")
                    and bool(body.get("evidence_refs"))
                )
            if not checked:
                failures.append(name)
            print(
                json.dumps(
                    {
                        "run_id": run_id,
                        "sample": name,
                        "http_status": response.status_code,
                        "request_id": body.get("request_id"),
                        "trace_id": body.get("trace_id"),
                        "status": body.get("status"),
                        "answer": body.get("answer"),
                        "actions": actions,
                        "evidence_refs": body.get("evidence_refs", []),
                        "checked": checked,
                    },
                    ensure_ascii=False,
                ),
                flush=True,
            )
        print(
            json.dumps(
                {
                    "run_id": run_id,
                    "failed_checks": failures,
                    "retained_test_users": 1,
                    "retained_test_tweets": 0,
                    "semantic_review": "required; no promise of site AI capabilities",
                }
            ),
            flush=True,
        )
        if failures:
            raise RuntimeError("Guide role HTTP checks failed; review the safe sample output")


if __name__ == "__main__":
    asyncio.run(main())

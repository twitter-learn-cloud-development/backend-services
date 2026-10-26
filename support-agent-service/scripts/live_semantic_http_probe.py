"""Opt-in real HTTP semantic samples; creates one isolated user and one tweet."""

import argparse
import asyncio
import json
import secrets
from datetime import UTC, datetime
from uuid import uuid4

import httpx

SAMPLES = {
    "recent": "请用中文列出我最近发布内容的编号、发布时间和可见性。",
    "composite": (
        "请列出我最近发布内容的编号、发布时间和可见性,并根据本项目指南解释 Workflow "
        "如何创建和运行。还请查明这些内容没有进入其他用户推荐流的实际原因;"
        "无法查询的部分请明确说明。"
    ),
    "unavailable": "请查询我账号的后台处罚记录、处罚原因和审核人员姓名;我没有其他问题。",
    "missing_object": (
        "请查询某次 Workflow 运行失败的节点及其异常原因。还需要我提供哪些信息才能查询?"
    ),
}


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true")
    args = parser.parse_args()
    if not args.live:
        raise RuntimeError("Use --live to create test data and call real models four times")
    run_id = uuid4().hex[:8]
    email = f"semantic{run_id}@example.com"
    password = secrets.token_urlsafe(24)
    async with httpx.AsyncClient(trust_env=False, timeout=130) as client:
        for url in (
            "http://127.0.0.1:5174/",
            "http://127.0.0.1:8000/health/ready",
            "http://127.0.0.1:9638/health",
        ):
            ready = await client.get(url, timeout=3)
            ready.raise_for_status()
        registered = await client.post(
            "http://127.0.0.1:9638/api/v1/auth/register",
            json={"username": f"sem{run_id}", "email": email, "password": password},
        )
        registered.raise_for_status()
        login = await client.post(
            "http://127.0.0.1:9638/api/v1/auth/login",
            json={"email": email, "password": password},
        )
        login.raise_for_status()
        token = login.json()["token"]
        auth = {"Authorization": f"Bearer {token}"}
        created = await client.post(
            "http://127.0.0.1:9638/api/v1/tweets",
            headers=auth,
            json={"content": f"Semantic support regression {run_id}", "media_urls": []},
        )
        created.raise_for_status()
        tweet = created.json()["tweet"]
        tweet_id = str(tweet["id"])
        expected_time = datetime.fromtimestamp(tweet["created_at"] / 1000, UTC).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        if tweet["visible_type"] != 0:
            raise RuntimeError("controlled test tweet was not created with public visibility")
        print(json.dumps({"phase": "setup", "run_id": run_id, "tweet_id": tweet_id}), flush=True)
        failures: list[str] = []
        for name, question in SAMPLES.items():
            response = await client.post(
                "http://127.0.0.1:5174/api/v1/support/chat",
                headers={**auth, "Accept": "application/json", "Idempotency-Key": str(uuid4())},
                json={"message": question, "client_timezone": "Asia/Shanghai"},
            )
            payload = response.json()
            checks = response.status_code == 200
            if checks and name == "recent":
                checks = payload["status"] == "answered" and any(
                    item["id"] == f"tweet:{tweet_id}" for item in payload["citations"]
                )
                checks = checks and (
                    f"tweet:{tweet_id}" in payload["answer"]
                    and expected_time in payload["answer"]
                    and "可见性: 公开" in payload["answer"]
                )
            if checks and name == "composite":
                checks = payload["status"] == "partial" and any(
                    item["id"] == f"tweet:{tweet_id}" for item in payload["citations"]
                )
                answer = payload["answer"]
                checks = checks and (
                    "Workflow" in answer
                    and "来源摘录" not in answer
                    and ("保存" in answer or "Save" in answer)
                    and ("运行" in answer or "Run" in answer)
                    and ("无法" in answer or "不足" in answer or "尚未确认" in answer)
                    and any(ref.startswith("ev_knowledge_") for ref in payload["evidence_refs"])
                )
            if checks and name in {"unavailable", "missing_object"}:
                checks = payload["status"] in {"partial", "unsupported"}
            if not checks:
                failures.append(name)
            # Only controlled test questions/results; never print token, password or user profile.
            print(
                json.dumps(
                    {
                        "phase": "sample",
                        "run_id": run_id,
                        "sample": name,
                        "http_status": response.status_code,
                        "request_id": payload.get("request_id"),
                        "trace_id": payload.get("trace_id"),
                        "status": payload.get("status"),
                        "code": payload.get("code"),
                        "answer": payload.get("answer"),
                        "evidence_refs": payload.get("evidence_refs", []),
                        "citations": payload.get("citations", []),
                        "structural_checks": checks,
                    },
                    ensure_ascii=False,
                ),
                flush=True,
            )
        print(
            json.dumps(
                {
                    "phase": "summary",
                    "run_id": run_id,
                    "failed_structural_checks": failures,
                    "semantic_review": "required; status and citations alone are insufficient",
                    "retained_test_users": 1,
                    "retained_test_tweets": 1,
                }
            ),
            flush=True,
        )
        if failures:
            raise RuntimeError(
                "Semantic sample structural checks failed; inspect safe sample output"
            )


if __name__ == "__main__":
    asyncio.run(main())

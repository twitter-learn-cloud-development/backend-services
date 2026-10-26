"""Explicit HTTP regression: one isolated account and one real Workflow question."""

import argparse
import asyncio
import json
import secrets
from uuid import uuid4

import httpx


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true")
    if not parser.parse_args().live:
        raise RuntimeError("Pass --live to create one test account and call the real model")
    run_id = uuid4().hex[:8]
    email = f"wf{run_id}@example.com"
    password = secrets.token_urlsafe(24)
    async with httpx.AsyncClient(trust_env=False, timeout=130) as client:
        registered = await client.post(
            "http://127.0.0.1:9638/api/v1/auth/register",
            json={
                "username": f"wf{run_id}",
                "email": email,
                "password": password,
            },
        )
        if registered.status_code != 200:
            raise RuntimeError(f"test account registration returned {registered.status_code}")
        login = await client.post(
            "http://127.0.0.1:9638/api/v1/auth/login",
            json={
                "email": email,
                "password": password,
            },
        )
        login.raise_for_status()
        token = login.json()["token"]
        key = str(uuid4())
        body = {
            "message": "Workflow 是什么\uff0c如何创建和运行\uff1f",
            "client_timezone": "Asia/Shanghai",
        }
        headers = {
            "Authorization": f"Bearer {token}",
            "Idempotency-Key": key,
            "Accept": "application/json",
        }
        response = await client.post(
            "http://127.0.0.1:5174/api/v1/support/chat", headers=headers, json=body
        )
        payload = response.json()
        if response.status_code != 200:
            print(
                json.dumps(
                    {
                        "status": "failed",
                        "run_id": run_id,
                        "http_status": response.status_code,
                        "code": payload.get("code"),
                        "request_id": payload.get("request_id"),
                    },
                    ensure_ascii=False,
                )
            )
            raise RuntimeError("Workflow HTTP regression failed")
        if payload["status"] != "answered" or not any(
            ref.startswith("ev_knowledge_") for ref in payload["evidence_refs"]
        ):
            raise RuntimeError(
                "Workflow did not return an answered result with published knowledge evidence"
            )
        if payload["citations"]:
            raise RuntimeError("Workflow help invented tweet navigation")
        replay = await client.post(
            "http://127.0.0.1:5174/api/v1/support/chat", headers=headers, json=body
        )
        replay.raise_for_status()
        if (
            replay.json()["case_id"] != payload["case_id"]
            or replay.json()["answer"] != payload["answer"]
        ):
            raise RuntimeError("same request retry did not return the committed result")
        print(
            json.dumps(
                {
                    "status": "passed",
                    "run_id": run_id,
                    "http_status": response.status_code,
                    "response_status": payload["status"],
                    "evidence_count": len(payload["evidence_refs"]),
                    "same_key_replay": True,
                    "retained_test_users": 1,
                    "answer": payload["answer"],
                },
                ensure_ascii=False,
            )
        )


if __name__ == "__main__":
    asyncio.run(main())

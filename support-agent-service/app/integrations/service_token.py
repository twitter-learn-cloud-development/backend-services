import base64
import hashlib
import hmac
import json
import time
from typing import Protocol
from uuid import uuid4

from app.domain.runtime import AuthContext


def _base64url(value: bytes) -> str:
    return base64.urlsafe_b64encode(value).rstrip(b"=").decode("ascii")


class ServiceTokenProvider(Protocol):
    def issue(self, auth: AuthContext) -> str: ...


class HMACServiceTokenProvider:
    def __init__(
        self,
        *,
        secret: str,
        issuer: str,
        audience: str,
        subject: str,
        ttl_seconds: int,
    ) -> None:
        if len(secret.encode("utf-8")) < 32:
            raise ValueError("support query JWT secret must contain at least 32 UTF-8 bytes")
        self._secret = secret.encode("utf-8")
        self._issuer = issuer
        self._audience = audience
        self._subject = subject
        self._ttl_seconds = ttl_seconds

    def issue(self, auth: AuthContext) -> str:
        issued_at = int(time.time())
        header = {"alg": "HS256", "typ": "JWT"}
        claims = {
            "iss": self._issuer,
            "aud": self._audience,
            "sub": self._subject,
            "act": {"user_id": auth.actor_user_id},
            "iat": issued_at,
            "nbf": issued_at,
            "exp": issued_at + self._ttl_seconds,
            "jti": str(uuid4()),
        }
        encoded_header = _base64url(
            json.dumps(header, separators=(",", ":"), sort_keys=True).encode("utf-8")
        )
        encoded_claims = _base64url(
            json.dumps(claims, separators=(",", ":"), sort_keys=True).encode("utf-8")
        )
        signing_input = f"{encoded_header}.{encoded_claims}".encode("ascii")
        signature = _base64url(hmac.new(self._secret, signing_input, hashlib.sha256).digest())
        return f"{encoded_header}.{encoded_claims}.{signature}"

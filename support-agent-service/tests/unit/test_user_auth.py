from datetime import UTC, datetime, timedelta
from typing import Any

import jwt
import pytest
from cryptography.hazmat.primitives.asymmetric import rsa

from app.api.auth import JWKSUserAuthenticator, SigningKey
from app.core.errors import AppError


class StaticKeyProvider:
    def __init__(self, key: Any, *, failure: Exception | None = None) -> None:
        self._key = key
        self._failure = failure

    def get_signing_key_from_jwt(self, token: str | bytes) -> SigningKey:
        del token
        if self._failure is not None:
            raise self._failure
        return _StaticSigningKey(self._key)


class _StaticSigningKey:
    def __init__(self, key: Any) -> None:
        self._key = key

    @property
    def key(self) -> Any:
        return self._key


def issue_token(
    private_key: rsa.RSAPrivateKey,
    *,
    user_id: object = 42,
    issuer: str = "twitter-clone-auth",
    audience: str = "twitter-clone-users",
) -> str:
    now = datetime.now(UTC)
    return jwt.encode(
        {
            "user_id": user_id,
            "username": "live-user",
            "email": "live-user@example.invalid",
            "iss": issuer,
            "aud": audience,
            "iat": now,
            "nbf": now,
            "exp": now + timedelta(minutes=5),
        },
        private_key,
        algorithm="RS256",
        headers={"kid": "live-key"},
    )


@pytest.mark.asyncio
async def test_jwks_authenticator_accepts_go_auth_claims() -> None:
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    authenticator = JWKSUserAuthenticator(
        jwks_url="https://unused.invalid/.well-known/jwks.json",
        issuer="twitter-clone-auth",
        audience="twitter-clone-users",
        key_provider=StaticKeyProvider(private_key.public_key()),
    )

    auth = await authenticator.authenticate(issue_token(private_key))

    assert auth.actor_user_id == "42"
    assert auth.token_id.startswith("sha256:")
    assert auth.scopes == frozenset()


@pytest.mark.asyncio
async def test_jwks_authenticator_rejects_wrong_audience_and_invalid_user() -> None:
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    authenticator = JWKSUserAuthenticator(
        jwks_url="https://unused.invalid/.well-known/jwks.json",
        issuer="twitter-clone-auth",
        audience="twitter-clone-users",
        key_provider=StaticKeyProvider(private_key.public_key()),
    )

    for token in (
        issue_token(private_key, audience="other-audience"),
        issue_token(private_key, user_id=0),
    ):
        with pytest.raises(AppError) as error:
            await authenticator.authenticate(token)
        assert error.value.code == "unauthorized"
        assert error.value.status_code == 401


@pytest.mark.asyncio
async def test_jwks_authenticator_rejects_token_signed_by_another_key() -> None:
    trusted_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    untrusted_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    authenticator = JWKSUserAuthenticator(
        jwks_url="https://unused.invalid/.well-known/jwks.json",
        issuer="twitter-clone-auth",
        audience="twitter-clone-users",
        key_provider=StaticKeyProvider(trusted_key.public_key()),
    )

    with pytest.raises(AppError) as error:
        await authenticator.authenticate(issue_token(untrusted_key))

    assert error.value.code == "unauthorized"
    assert error.value.status_code == 401

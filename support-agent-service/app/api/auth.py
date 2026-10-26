import asyncio
import hashlib
from typing import Annotated, Any, Protocol, cast

from fastapi import Depends, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from jwt import InvalidTokenError, PyJWKClient, decode
from jwt.exceptions import PyJWKClientConnectionError, PyJWKClientError
from jwt.types import Options

from app.core.config import Settings
from app.core.errors import AppError
from app.domain.runtime import AuthContext

bearer_scheme = HTTPBearer(auto_error=False)


class UserAuthenticator(Protocol):
    async def authenticate(self, token: str) -> AuthContext: ...


class SigningKey(Protocol):
    @property
    def key(self) -> Any: ...


class SigningKeyProvider(Protocol):
    def get_signing_key_from_jwt(self, token: str | bytes) -> SigningKey: ...


class UnconfiguredUserAuthenticator:
    async def authenticate(self, token: str) -> AuthContext:
        del token
        raise AppError(
            code="temporarily_unavailable",
            title="Authentication is unavailable",
            detail="User authentication has not been configured.",
            status_code=503,
        )


class FakeUserAuthenticator:
    """Explicit test/development authenticator; never selected implicitly."""

    def __init__(self, tokens: dict[str, AuthContext]) -> None:
        self._tokens = dict(tokens)

    async def authenticate(self, token: str) -> AuthContext:
        auth = self._tokens.get(token)
        if auth is None:
            raise AppError(
                code="unauthorized",
                title="Unauthorized",
                detail="The user access token is invalid.",
                status_code=401,
            )
        return auth


class JWKSUserAuthenticator:
    """Validate Twitter user access tokens against the Auth service JWKS."""

    def __init__(
        self,
        *,
        jwks_url: str,
        issuer: str | None = None,
        audience: str | None = None,
        timeout_seconds: float = 2.0,
        cache_seconds: int = 300,
        key_provider: SigningKeyProvider | None = None,
    ) -> None:
        self._issuer = issuer.strip() if issuer and issuer.strip() else None
        self._audience = audience.strip() if audience and audience.strip() else None
        self._key_provider = key_provider or PyJWKClient(
            jwks_url,
            cache_keys=True,
            cache_jwk_set=True,
            lifespan=cache_seconds,
            timeout=timeout_seconds,
        )

    async def authenticate(self, token: str) -> AuthContext:
        try:
            signing_key = await asyncio.to_thread(
                self._key_provider.get_signing_key_from_jwt,
                token,
            )
            options: Options = {
                "require": ["exp", "iat", "nbf", "user_id"],
                "verify_aud": self._audience is not None,
                "verify_iss": self._issuer is not None,
            }
            claims = await asyncio.to_thread(
                decode,
                token,
                signing_key.key,
                algorithms=["RS256"],
                options=options,
                audience=self._audience,
                issuer=self._issuer,
            )
        except PyJWKClientConnectionError as exc:
            raise AppError(
                code="temporarily_unavailable",
                title="Authentication is unavailable",
                detail="The user identity provider is temporarily unavailable.",
                status_code=503,
            ) from exc
        except (InvalidTokenError, PyJWKClientError, TypeError, ValueError) as exc:
            raise _unauthorized() from exc

        actor_user_id = str(claims.get("user_id", "")).strip()
        if not actor_user_id.isdigit() or int(actor_user_id) <= 0:
            raise _unauthorized()
        token_id_claim = claims.get("jti")
        token_id = (
            str(token_id_claim).strip()
            if isinstance(token_id_claim, str) and token_id_claim.strip()
            else f"sha256:{hashlib.sha256(token.encode('utf-8')).hexdigest()}"
        )
        try:
            return AuthContext(
                actor_user_id=actor_user_id,
                token_id=token_id,
                scopes=_token_scopes(claims),
            )
        except ValueError as exc:
            raise _unauthorized() from exc


def build_user_authenticator(settings: Settings) -> UserAuthenticator:
    if not settings.user_jwks_url:
        return UnconfiguredUserAuthenticator()
    return JWKSUserAuthenticator(
        jwks_url=settings.user_jwks_url,
        issuer=settings.user_jwt_issuer,
        audience=settings.user_jwt_audience,
        timeout_seconds=settings.user_jwks_timeout_seconds,
        cache_seconds=settings.user_jwks_cache_seconds,
    )


def _token_scopes(claims: dict[str, Any]) -> frozenset[str]:
    raw_scopes = claims.get("scope", claims.get("scopes", []))
    if isinstance(raw_scopes, str):
        values = raw_scopes.split()
    elif isinstance(raw_scopes, list):
        values = [value for value in cast(list[object], raw_scopes) if isinstance(value, str)]
    else:
        values = []
    return frozenset(value for value in values if value)


def _unauthorized() -> AppError:
    return AppError(
        code="unauthorized",
        title="Unauthorized",
        detail="The user access token is invalid.",
        status_code=401,
    )


async def require_user(
    request: Request,
    credentials: Annotated[HTTPAuthorizationCredentials | None, Depends(bearer_scheme)],
) -> AuthContext:
    if credentials is None or credentials.scheme.casefold() != "bearer":
        raise AppError(
            code="unauthorized",
            title="Unauthorized",
            detail="A valid bearer token is required.",
            status_code=401,
        )
    authenticator = cast(UserAuthenticator, request.app.state.user_authenticator)
    return await authenticator.authenticate(credentials.credentials)

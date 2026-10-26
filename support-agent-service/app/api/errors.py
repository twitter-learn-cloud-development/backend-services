from typing import Any

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from app.api.request_context import request_id, trace_id
from app.core.errors import AppError


def _problem_response(
    request: Request,
    *,
    status_code: int,
    code: str,
    title: str,
    detail: str,
    errors: list[dict[str, Any]] | None = None,
) -> JSONResponse:
    body: dict[str, Any] = {
        "type": f"https://errors.example.com/support/{code.replace('_', '-')}",
        "title": title,
        "status": status_code,
        "code": code,
        "detail": detail,
        "request_id": str(request_id(request)),
        "trace_id": trace_id(request),
    }
    if errors:
        body["errors"] = errors
    return JSONResponse(
        status_code=status_code,
        content=body,
        media_type="application/problem+json",
    )


def install_exception_handlers(app: FastAPI) -> None:
    @app.exception_handler(AppError)
    async def handle_app_error(request: Request, exc: AppError) -> JSONResponse:
        return _problem_response(
            request,
            status_code=exc.status_code,
            code=exc.code,
            title=exc.title,
            detail=exc.detail,
            errors=exc.errors,
        )

    @app.exception_handler(RequestValidationError)
    async def handle_validation_error(
        request: Request, exc: RequestValidationError
    ) -> JSONResponse:
        oversized = any(
            error["type"] in {"string_too_long", "bytes_too_long"}
            or (error["type"] == "value_error" and "context" in error["loc"])
            for error in exc.errors()
        )
        errors = [
            {
                "field": ".".join(str(part) for part in error["loc"]),
                "reason": error["type"],
            }
            for error in exc.errors()
        ]
        return _problem_response(
            request,
            status_code=413 if oversized else 422,
            code="request_too_large" if oversized else "invalid_request",
            title="Request too large" if oversized else "Invalid request",
            detail=(
                "The request exceeds the supported size limits."
                if oversized
                else "The request does not match the expected schema."
            ),
            errors=errors,
        )

    @app.exception_handler(Exception)
    async def handle_unexpected_error(request: Request, exc: Exception) -> JSONResponse:
        del exc
        return _problem_response(
            request,
            status_code=503,
            code="temporarily_unavailable",
            title="Support service unavailable",
            detail="The support request could not be completed.",
        )

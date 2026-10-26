from collections.abc import Iterator

import pytest
import structlog
from fastapi.testclient import TestClient

from app.core.config import Settings
from app.main import create_app


@pytest.fixture(autouse=True)
def isolate_structlog_configuration() -> Iterator[None]:
    """Prevent app/test logging configured against pytest capture from leaking."""
    structlog.reset_defaults()
    structlog.contextvars.clear_contextvars()
    try:
        yield
    finally:
        structlog.reset_defaults()
        structlog.contextvars.clear_contextvars()


@pytest.fixture
def client() -> Iterator[TestClient]:
    settings = Settings(app_env="test", json_logs=False)
    with TestClient(create_app(settings)) as test_client:
        yield test_client

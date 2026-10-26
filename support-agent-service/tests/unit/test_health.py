from fastapi.testclient import TestClient


def test_liveness(client: TestClient) -> None:
    response = client.get("/health/live")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_readiness(client: TestClient) -> None:
    response = client.get("/health/ready")

    assert response.status_code == 200
    assert response.json() == {"status": "ready"}


def test_trace_context_and_prometheus_metrics(client: TestClient) -> None:
    trace_id = "4bf92f3577b34da6a3ce929d0e0e4736"
    response = client.get(
        "/health/live",
        headers={"traceparent": f"00-{trace_id}-00f067aa0ba902b7-01"},
    )
    metrics = client.get("/metrics/")

    assert response.headers["X-Trace-ID"] == trace_id
    assert response.headers["X-Request-ID"]
    assert metrics.status_code == 200
    assert "support_agent_http_requests_total" in metrics.text
    assert 'route="/health/live"' in metrics.text

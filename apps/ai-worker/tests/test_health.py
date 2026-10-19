from ai_worker import health


def test_health_reports_service() -> None:
    assert health() == {"status": "ok", "service": "ai-worker"}

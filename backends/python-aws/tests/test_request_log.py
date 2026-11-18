from dashboard_api.request_log import emit


def test_emit_includes_the_structured_fields():
    entry = emit(
        level="info",
        backend="aws-python",
        cloud="aws",
        color="blue",
        route="/api/v1/stock/TSLA",
        status=200,
        trace_id="abc",
        message="GET /api/v1/stock/TSLA 200",
    )
    assert entry["backend"] == "aws-python"
    assert entry["route"] == "/api/v1/stock/TSLA"
    assert entry["status"] == 200
    assert "TSLA" in entry["message"]
    assert entry["cloud"] == "aws"
    assert entry["color"] == "blue"

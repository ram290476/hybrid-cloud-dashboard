import json
from pathlib import Path

from fastapi.testclient import TestClient

from dashboard_api.auth import mint
from dashboard_api.main import app

ROOT = Path(__file__).resolve().parents[3]
TOKEN = mint("local-dev-secret-change-me", "user-1", "http://localhost/dev-auth", "hcd-local", 1_800_000_000)
HEADERS = {"Authorization": f"Bearer {TOKEN}"}


def test_data_health_shape_and_warning_states():
    from datetime import datetime, timezone

    from dashboard_api.data_health import FRESH_WITHIN_SECONDS, build_data_health
    from dashboard_api.main import _identity, store

    client = TestClient(app)
    response = client.get("/api/v1/data-health", headers=HEADERS)
    assert response.status_code == 200
    body = response.json()
    assert body["backend"] == "aws-python"
    assert body["cloud"] == "aws"
    assert body["fresh_within_seconds"] == FRESH_WITHIN_SECONDS
    assert body["freshness"] in {"fresh", "stale", "missing"}
    assert body["files"]["dashboard"]["generated_at"] == "2026-10-09T22:00:00Z"
    assert body["files"]["dashboard"]["present"] is True
    assert body["collectors"][0]["name"] == "prices_daily"
    assert set(body["counts"]) == {"chart", "stock", "funds"}
    age = body["files"]["dashboard"]["age_seconds"]
    assert body["freshness"] == ("stale" if age > FRESH_WITHIN_SECONDS else "fresh")
    denied = client.get("/api/v1/data-health")
    assert denied.status_code == 401

    fresh_now = datetime(2026, 10, 10, 10, 0, tzinfo=timezone.utc)
    fresh = build_data_health(store, _identity(), fresh_now)
    assert fresh["freshness"] == "fresh"
    assert fresh["files"]["dashboard"]["age_seconds"] == 43200
    assert fresh["counts"]["chart"] >= 1
    assert "TSLA" in fresh["tickers"]
    assert fresh["import"] is None

    store.put_object(
        "serving/_import.json",
        b'{"source":"dash-import","origin":"investor-dashboard","at":"2026-10-10T09:00:00Z"}',
    )
    marked = build_data_health(store, _identity(), fresh_now)
    assert marked["import"]["source"] == "dash-import"
    assert marked["import"]["age_seconds"] == 3600

    stale = build_data_health(store, _identity(), datetime(2026, 11, 18, 20, 0, tzinfo=timezone.utc))
    assert stale["freshness"] == "stale"
    assert any("stale" in warning for warning in stale["warnings"])

    from dashboard_api.store import Memory

    missing = build_data_health(Memory(), _identity(), fresh_now)
    assert missing["freshness"] == "missing"
    assert missing["collectors"] == []
    assert any("serving/dashboard.json is missing" in warning for warning in missing["warnings"])


def test_dashboard_matches_fixture(monkeypatch):
    monkeypatch.setenv("FACADE_KEY", "")
    client = TestClient(app)
    response = client.get("/api/v1/dashboard", headers=HEADERS)
    assert response.status_code == 200
    assert response.headers["x-backend-lang"] == "python"
    assert response.headers["x-backend-runtime"] == "lambda"
    assert response.headers["x-app-color"] == "blue"
    want = json.loads((ROOT / "contract/fixtures/serving/dashboard.json").read_text())
    assert response.json() == want


def test_stock_and_fund_match_chart(monkeypatch):
    monkeypatch.setenv("FACADE_KEY", "")
    client = TestClient(app)
    chart = client.get("/api/v1/chart/TSLA", headers=HEADERS)
    stock = client.get("/api/v1/stock/TSLA", headers=HEADERS)
    fund = client.get("/api/v1/fund/ARKK", headers=HEADERS)
    assert chart.status_code == stock.status_code == fund.status_code == 200
    for name in (
        "content-type",
        "x-backend-env",
        "x-backend-lang",
        "x-backend-runtime",
        "x-app-color",
        "x-app-version",
        "x-data-version",
        "access-control-allow-origin",
        "access-control-allow-headers",
    ):
        assert stock.headers[name] == chart.headers[name]
        assert fund.headers[name] == chart.headers[name]
    assert stock.headers["content-type"].startswith("application/json")
    want_stock = json.loads((ROOT / "contract/fixtures/serving/stock/TSLA.json").read_text())
    want_fund = json.loads((ROOT / "contract/fixtures/serving/funds/ARKK.json").read_text())
    assert stock.json() == want_stock
    assert fund.json() == want_fund
    for path in ("/api/v1/stock/NOPE", "/api/v1/fund/NOPE"):
        missing = client.get(path, headers=HEADERS)
        assert missing.status_code == 404
        assert missing.headers["content-type"].startswith("application/problem+json")
        problem = missing.json()
        assert problem["type"] == "about:blank"
        assert problem["status"] == 404
        assert problem["title"]
        assert problem["detail"]


def test_prefs_keeps_unrelated_field():
    client = TestClient(app)
    current = client.get("/api/v1/prefs", headers=HEADERS).json()
    current["display"]["theme"] = "charting-navy"
    current["display"]["panel_mode"] = "all"
    current["chart_settings"] = {"TSLA": {"overlays": ["MA20"], "lanes": ["VOL"]}}
    saved = client.put("/api/v1/prefs", headers=HEADERS, json=current)
    assert saved.status_code == 200
    body = saved.json()
    assert body["display"]["theme"] == "charting-navy"
    assert body["display"]["panel_mode"] == "all"
    assert body["display"]["time_zone"] == "America/New_York"
    assert body["chart_settings"]["TSLA"]["overlays"] == ["MA20"]
    assert "TSLA" in body["tickers"]
    body["chart_settings"] = {}
    cleared = client.put("/api/v1/prefs", headers=HEADERS, json=body)
    assert cleared.status_code == 200
    assert cleared.json()["chart_settings"] == {}
    assert cleared.json()["display"]["panel_mode"] == "all"


def test_merge_vectors():
    from dashboard_api.merge import merge_docs

    vectors = json.loads((ROOT / "contract/fixtures/merge/vectors.json").read_text())
    for vector in vectors:
        assert merge_docs(vector["docs"]) == vector["want"]

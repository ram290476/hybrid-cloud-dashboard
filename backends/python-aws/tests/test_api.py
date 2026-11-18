import json
from pathlib import Path

from fastapi.testclient import TestClient

from dashboard_api.auth import mint
from dashboard_api.main import app

ROOT = Path(__file__).resolve().parents[3]
TOKEN = mint("local-dev-secret-change-me", "user-1", "http://localhost/dev-auth", "hcd-local", 1_800_000_000)
HEADERS = {"Authorization": f"Bearer {TOKEN}"}


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

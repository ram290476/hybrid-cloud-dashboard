"""Python dashboard API. Same contract as the Go and .NET backends."""

from __future__ import annotations

import json
import os
import time
from pathlib import Path

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse

from dashboard_api.auth import parse
from dashboard_api.merge import empty_doc, project
from dashboard_api.store import Memory, S3Dynamo

APP_BACKEND = os.environ.get("APP_BACKEND", "aws-python")
APP_LANG = os.environ.get("APP_LANG", "python")
APP_RUNTIME = os.environ.get("APP_RUNTIME", "ecs-fargate")
APP_COLOR = os.environ.get("APP_COLOR", "blue")
APP_VERSION = os.environ.get("APP_VERSION", "dev")
DATA_VERSION = os.environ.get("DATA_VERSION", "fixture-2026.10.09")
AUTH_MODE = os.environ.get("AUTH_MODE", "dev")
DEV_SECRET = os.environ.get("DEV_JWT_SECRET", "local-dev-secret-change-me")
ISSUER = os.environ.get("DEV_JWT_ISSUER", "http://localhost/dev-auth")
AUDIENCE = os.environ.get("DEV_JWT_AUDIENCE", "hcd-local")
FACADE_KEY = os.environ.get("FACADE_KEY", "")
def _fixtures_dir() -> Path:
    if os.environ.get("FIXTURES_DIR"):
        return Path(os.environ["FIXTURES_DIR"])
    for parent in Path(__file__).resolve().parents:
        candidate = parent / "contract" / "fixtures"
        if candidate.is_dir():
            return candidate
    return Path("contract/fixtures")


FIXTURES = _fixtures_dir()

memory = Memory()
store = S3Dynamo(memory)


def _seed() -> None:
    for key in (
        "serving/dashboard.json",
        "serving/status.json",
        "serving/chart/TSLA.json",
        "serving/chart/SPCX.json",
    ):
        path = FIXTURES / key
        if path.exists():
            memory.put_object(key, path.read_bytes())


_seed()

app = FastAPI(title="dashboard-api", version="v1")


def _headers() -> dict[str, str]:
    return {
        "x-backend-env": APP_BACKEND,
        "x-backend-lang": APP_LANG,
        "x-backend-runtime": APP_RUNTIME,
        "x-app-color": APP_COLOR,
        "x-app-version": APP_VERSION,
        "x-data-version": DATA_VERSION,
        "access-control-allow-origin": "*",
        "access-control-allow-headers": "authorization,content-type,x-facade-key,x-request-id,traceparent",
    }


def _identity() -> dict:
    return {
        "status": "ok",
        "backend": APP_BACKEND,
        "lang": APP_LANG,
        "runtime": APP_RUNTIME,
        "color": APP_COLOR,
        "version": APP_VERSION,
        "data_version": DATA_VERSION,
    }


def _problem(status: int, detail: str) -> JSONResponse:
    return JSONResponse(
        {"type": "about:blank", "title": detail, "status": status, "detail": detail},
        status_code=status,
        headers=_headers(),
        media_type="application/problem+json",
    )


def _guard(request: Request) -> str | JSONResponse:
    if FACADE_KEY and request.headers.get("x-facade-key") != FACADE_KEY:
        return _problem(403, "missing facade key")
    header = request.headers.get("authorization", "")
    if not header.startswith("Bearer "):
        return _problem(401, "missing bearer token")
    if AUTH_MODE not in ("", "dev"):
        return _problem(401, "cognito validation needs COGNITO_ISSUER; local mode is AUTH_MODE=dev")
    try:
        return parse(DEV_SECRET, header.removeprefix("Bearer "), ISSUER, AUDIENCE)
    except ValueError as exc:
        return _problem(401, str(exc))


def _default_doc(sub: str) -> dict:
    doc = empty_doc(sub)

    def scalar(value: object, wall: int) -> dict:
        return {"value": value, "hlc": {"wall_ms": wall, "counter": 0, "origin": "seed"}}

    doc["fields"] = {
        "display.theme": scalar("industrial-dark", 1),
        "display.time_zone": scalar("America/New_York", 1),
        "display.updown_palette": scalar("green-red", 1),
        "display.chart_period": scalar("1M", 1),
    }
    doc["tickers"]["adds"] = {
        "seed-tsla": {"value": "TSLA", "hlc": {"wall_ms": 1, "counter": 0, "origin": "seed"}},
        "seed-spcx": {"value": "SPCX", "hlc": {"wall_ms": 2, "counter": 0, "origin": "seed"}},
    }
    doc["pinned"]["adds"] = {
        "seed-tsla": {"value": "TSLA", "hlc": {"wall_ms": 1, "counter": 0, "origin": "seed"}},
        "seed-spcx": {"value": "SPCX", "hlc": {"wall_ms": 2, "counter": 0, "origin": "seed"}},
    }
    return doc


@app.get("/healthz")
def healthz() -> JSONResponse:
    return JSONResponse(_identity(), headers=_headers())


@app.get("/readyz")
def readyz() -> JSONResponse:
    try:
        store.ready()
    except Exception as exc:  # noqa: BLE001 - surface emulator errors to the probe
        return _problem(503, str(exc))
    return JSONResponse({"status": "ready"}, headers=_headers())


@app.get("/version")
def version() -> JSONResponse:
    return JSONResponse(_identity(), headers=_headers())


@app.get("/api/v1/status")
def status(request: Request) -> Response:
    guard = _guard(request)
    if isinstance(guard, JSONResponse):
        return guard
    return _object("serving/status.json")


@app.get("/api/v1/dashboard")
def dashboard(request: Request) -> Response:
    guard = _guard(request)
    if isinstance(guard, JSONResponse):
        return guard
    return _object("serving/dashboard.json")


@app.get("/api/v1/chart/{ticker}")
def chart(ticker: str, request: Request) -> Response:
    guard = _guard(request)
    if isinstance(guard, JSONResponse):
        return guard
    return _object(f"serving/chart/{ticker}.json")


@app.get("/api/v1/prefs")
def get_prefs(request: Request) -> Response:
    guard = _guard(request)
    if isinstance(guard, JSONResponse):
        return guard
    try:
        raw = store.get_prefs(guard)
        doc = json.loads(raw)
    except KeyError:
        doc = _default_doc(guard)
        store.put_prefs(guard, json.dumps(doc).encode())
    return JSONResponse(project(doc), headers=_headers())


@app.put("/api/v1/prefs")
async def put_prefs(request: Request) -> Response:
    guard = _guard(request)
    if isinstance(guard, JSONResponse):
        return guard
    incoming = await request.json()
    try:
        doc = json.loads(store.get_prefs(guard))
    except KeyError:
        doc = _default_doc(guard)
    if incoming.get("version", 0) != doc.get("version", 0):
        return _problem(409, "version conflict")
    # Reuse the Go merge by applying a small local update for the fields the UI sends.
    from dashboard_api.merge import project as project_doc

    current = project_doc(doc)
    doc["version"] = doc.get("version", 0) + 1
    wall = int(time.time() * 1000)
    stamp = {"wall_ms": wall, "counter": 0, "origin": APP_BACKEND}
    display = incoming.get("display") or {}
    for key in ("theme", "time_zone", "updown_palette", "chart_period", "panels", "panel_mode"):
        if key in display and display[key] != current["display"].get(key):
            doc["fields"][f"display.{key}"] = {"value": display[key], "hlc": stamp}
    if isinstance(incoming.get("chart_settings"), dict):
        incoming_charts = incoming["chart_settings"]
        current_charts = current.get("chart_settings") or {}
        for ticker, settings in incoming_charts.items():
            if settings != current_charts.get(ticker):
                doc["fields"][f"chart.{ticker}"] = {"value": settings, "hlc": stamp}
        for ticker in current_charts:
            if ticker not in incoming_charts:
                doc["fields"][f"chart.{ticker}"] = {"value": None, "hlc": stamp, "tombstone": True}
    _apply_set(doc["tickers"], current["tickers"], incoming.get("tickers") or [], stamp)
    _apply_set(doc["pinned"], current["pinned"], incoming.get("pinned") or [], stamp)
    store.put_prefs(guard, json.dumps(doc).encode())
    return JSONResponse(project_doc(doc), headers=_headers())


def _apply_set(orset: dict, before: list, after: list, stamp: dict) -> None:
    prev, nxt = set(before), set(after)
    for value in prev - nxt:
        for tag, add in orset["adds"].items():
            if add["value"] == value and tag not in orset["removes"]:
                orset["removes"][tag] = stamp
    for value in nxt - prev:
        tag = f"{stamp['origin']}-{stamp['wall_ms']}-{value}"
        orset["adds"][tag] = {"value": value, "hlc": stamp}


def _object(key: str) -> Response:
    try:
        body = store.get_object(key)
    except Exception:
        try:
            body = memory.get_object(key)
        except KeyError:
            return _problem(404, f"missing {key}")
    return Response(body, media_type="application/json", headers=_headers())

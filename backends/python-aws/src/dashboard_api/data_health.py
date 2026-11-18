"""Store report for GET /api/v1/data-health. Same shape as the Go and .NET backends."""

from __future__ import annotations

import json
from datetime import datetime, timezone

from dashboard_api.store import _missing

# Daily collectors run about once a day. 36 hours still counts as fresh.
FRESH_WITHIN_SECONDS = 36 * 60 * 60
_PREFIXES = (
    ("chart", "serving/chart/"),
    ("stock", "serving/stock/"),
    ("funds", "serving/funds/"),
)


def cloud_of(backend: str) -> str:
    if backend.startswith("gcp"):
        return "gcp"
    if backend.startswith("aws"):
        return "aws"
    if backend.startswith("azure"):
        return "azure"
    return "unknown"


def build_data_health(store, identity: dict, now: datetime) -> dict:
    if now.tzinfo is None:
        now = now.replace(tzinfo=timezone.utc)
    warnings: list[str] = []
    try:
        listed = store.list_objects("serving/")
    except Exception as exc:  # noqa: BLE001 - a down emulator is a warning, not a 500
        warnings.append(f"object store unavailable: {exc}")
        listed = []
    counts = {"chart": 0, "stock": 0, "funds": 0}
    modified: dict[str, datetime | None] = {}
    tickers: set[str] = set()
    newest: datetime | None = None
    oldest: datetime | None = None
    for item in listed:
        key = item["key"]
        stamp = _aware(item.get("modified"))
        modified[key] = stamp
        kind = _prefix(key)
        if kind:
            counts[kind] += 1
            tickers.add(key.rsplit("/", 1)[-1].removesuffix(".json"))
        if stamp is not None:
            newest = stamp if newest is None or stamp > newest else newest
            oldest = stamp if oldest is None or stamp < oldest else oldest
    dashboard, dash_stale = _file(store, now, "serving/dashboard.json", modified.get("serving/dashboard.json"), warnings)
    status, status_stale, status_body = _file(
        store, now, "serving/status.json", modified.get("serving/status.json"), warnings, keep_body=True
    )
    if not dashboard["present"] or not status["present"]:
        freshness = "missing"
    elif dash_stale or status_stale:
        freshness = "stale"
    else:
        freshness = "fresh"
    return {
        "backend": identity.get("backend", ""),
        "lang": identity.get("lang", ""),
        "runtime": identity.get("runtime", ""),
        "color": identity.get("color", ""),
        "version": identity.get("version", ""),
        "data_version": identity.get("data_version", ""),
        "cloud": cloud_of(identity.get("backend", "")),
        "checked_at": _iso(now),
        "freshness": freshness,
        "fresh_within_seconds": FRESH_WITHIN_SECONDS,
        "counts": counts,
        "files": {"dashboard": dashboard, "status": status},
        "tickers": sorted(tickers),
        "newest_at": _iso(newest) if newest else None,
        "oldest_at": _iso(oldest) if oldest else None,
        "collectors": _collectors(status_body, now, warnings),
        "import": _import(store, now, warnings),
        "warnings": warnings,
    }


def _file(store, now, key, modified, warnings, keep_body=False):
    try:
        body = store.get_object(key)
    except KeyError:
        warnings.append(f"{key} is missing")
        file = _empty_file(key)
        return (file, False, b"") if keep_body else (file, False)
    except Exception as exc:  # noqa: BLE001
        if _missing(exc):
            warnings.append(f"{key} is missing")
        else:
            warnings.append(f"could not read {key}: {exc}")
        file = _empty_file(key)
        return (file, False, b"") if keep_body else (file, False)
    generated = None
    try:
        doc = json.loads(body)
    except (json.JSONDecodeError, UnicodeError):
        doc = None
    if isinstance(doc, dict) and isinstance(doc.get("generated_at"), str) and doc["generated_at"]:
        generated = doc["generated_at"]
    when = _parse(generated) if generated else None
    if when is None:
        when = modified
    file = {
        "key": key,
        "present": True,
        "generated_at": generated,
        "last_modified": _iso(modified) if modified else None,
        "age_seconds": None,
    }
    if when is None:
        warnings.append(f"{key} has no timestamp")
        stale = True
    else:
        age = _age(now, when)
        file["age_seconds"] = age
        stale = age > FRESH_WITHIN_SECONDS
        if stale:
            warnings.append(f"{key} is stale")
    if keep_body:
        return file, stale, body
    return file, stale


def _empty_file(key: str) -> dict:
    return {"key": key, "present": False, "generated_at": None, "last_modified": None, "age_seconds": None}


def _collectors(body: bytes, now: datetime, warnings: list[str]) -> list[dict]:
    if not body:
        return []
    try:
        doc = json.loads(body)
    except (json.JSONDecodeError, UnicodeError):
        return []
    jobs = doc.get("jobs") if isinstance(doc, dict) else None
    if not isinstance(jobs, list):
        return []
    rows = []
    for job in jobs:
        if not isinstance(job, dict):
            continue
        status = str(job.get("status") or "")
        last_run = job.get("last_run") if isinstance(job.get("last_run"), str) else ""
        when = _parse(last_run)
        rows.append(
            {
                "name": str(job.get("name") or ""),
                "status": status,
                "last_run": last_run,
                "age_seconds": _age(now, when) if when else None,
            }
        )
        if status.lower() not in {"ok", "success", "ready"}:
            warnings.append(f"collector {job.get('name') or ''} status is {status}")
    return rows


def _import(store, now: datetime, warnings: list[str]):
    try:
        body = store.get_object("serving/_import.json")
    except Exception as exc:  # noqa: BLE001
        if isinstance(exc, KeyError) or _missing(exc):
            return None
        warnings.append(f"could not read serving/_import.json: {exc}")
        return None
    try:
        doc = json.loads(body)
    except (json.JSONDecodeError, UnicodeError):
        warnings.append("serving/_import.json is not valid JSON")
        return None
    if not isinstance(doc, dict):
        warnings.append("serving/_import.json is not valid JSON")
        return None
    at = doc.get("at") if isinstance(doc.get("at"), str) else ""
    source = doc.get("source") if isinstance(doc.get("source"), str) else ""
    origin = doc.get("origin") if isinstance(doc.get("origin"), str) else ""
    if not source:
        warnings.append("serving/_import.json has no source")
    when = _parse(at)
    return {"source": source, "origin": origin, "at": at, "age_seconds": _age(now, when) if when else None}


def _prefix(key: str) -> str | None:
    for name, prefix in _PREFIXES:
        if not key.startswith(prefix) or not key.endswith(".json"):
            continue
        rest = key[len(prefix):]
        if rest and "/" not in rest:
            return name
    return None


def _parse(value: str | None) -> datetime | None:
    if not value:
        return None
    text = value.replace("Z", "+00:00")
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    return _aware(parsed)


def _aware(value):
    if value is None:
        return None
    if not isinstance(value, datetime):
        return None
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def _age(now: datetime, ts: datetime) -> int:
    seconds = int((now - ts).total_seconds())
    return 0 if seconds < 0 else seconds


def _iso(value: datetime) -> str:
    return value.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

"""Current alerts from this backend's data-health report, plus a short in-memory history."""

from __future__ import annotations

import threading
from datetime import datetime, timezone

from dashboard_api.data_health import FRESH_WITHIN_SECONDS, build_data_health

_LOCK = threading.Lock()
_ITEMS: dict[str, dict] = {}
_ORDER: list[str] = []


def alerts_page(store, identity: dict, now: datetime) -> dict:
    report = build_data_health(store, identity, now)
    return _sync(_firing(report), now)


def _firing(report: dict) -> list[dict]:
    backend = report.get("backend") or ""
    rows = []
    freshness = report.get("freshness")
    if freshness == "stale":
        rows.append(_row(f"freshness:{backend}", "warning", "data-freshness", backend, f"{backend} serving data is stale"))
    elif freshness == "missing":
        rows.append(_row(f"freshness:{backend}", "warning", "data-freshness", backend, f"{backend} serving data is missing"))
    for job in report.get("collectors") or []:
        name = job.get("name") or ""
        status = job.get("status") or ""
        if status.lower() not in {"ok", "success", "ready"}:
            rows.append(_row(f"collector:{backend}:{name}", "warning", "collector", backend, f"collector {name} status is {status}"))
        limit = 2 * 60 * 60 if "hourly" in name.lower() else FRESH_WITHIN_SECONDS
        age = job.get("age_seconds")
        if age is None or age > limit:
            rows.append(_row(f"collector-age:{backend}:{name}", "warning", "collector", backend, f"collector {name} last run is older than its interval"))
    return rows


def _row(alert_id: str, severity: str, source: str, backend: str, message: str) -> dict:
    return {"id": alert_id, "severity": severity, "source": source, "backend": backend, "message": message}


def _sync(firing: list[dict], now: datetime) -> dict:
    stamp = now.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
    seen = set()
    with _LOCK:
        for alert in firing:
            seen.add(alert["id"])
            prev = _ITEMS.get(alert["id"])
            if prev is None:
                alert = {**alert, "first_seen": stamp, "last_seen": stamp, "state": "firing"}
                _ITEMS[alert["id"]] = alert
                _ORDER.append(alert["id"])
            else:
                prev.update(severity=alert["severity"], source=alert["source"], message=alert["message"], last_seen=stamp, state="firing")
        for alert_id, alert in list(_ITEMS.items()):
            if alert_id not in seen and alert["state"] != "resolved":
                alert["state"] = "resolved"
                alert["last_seen"] = stamp
        while len(_ORDER) > 200:
            _ITEMS.pop(_ORDER.pop(0), None)
        alerts = [_ITEMS[alert_id] for alert_id in _ORDER if alert_id in _ITEMS]
    return {"alerts": alerts, "firing": sum(1 for alert in alerts if alert["state"] == "firing")}

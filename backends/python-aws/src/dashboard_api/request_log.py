"""Structured request logs. Stdout always; OTLP/HTTP when the collector endpoint is set."""

from __future__ import annotations

import json
import os
import threading
import urllib.request
from datetime import datetime, timezone


def emit(*, level: str, backend: str, cloud: str, color: str, route: str, status: int, trace_id: str, message: str) -> dict:
    entry = {
        "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ"),
        "level": level or "info",
        "backend": backend,
        "cloud": cloud,
        "color": color,
        "route": route,
        "status": status,
        "trace_id": trace_id,
        "message": message,
    }
    print(json.dumps(entry, separators=(",", ":")), flush=True)
    endpoint = os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT", "").rstrip("/")
    if endpoint:
        threading.Thread(target=_post, args=(endpoint, entry), daemon=True).start()
    return entry


def _post(endpoint: str, entry: dict) -> None:
    line = json.dumps(entry, separators=(",", ":"))
    payload = {
        "resourceLogs": [{
            "resource": {"attributes": [
                {"key": "service.name", "value": {"stringValue": entry["backend"]}},
                {"key": "cloud", "value": {"stringValue": entry["cloud"]}},
                {"key": "color", "value": {"stringValue": entry["color"]}},
            ]},
            "scopeLogs": [{"logRecords": [{
                "timeUnixNano": str(int(datetime.now(timezone.utc).timestamp() * 1_000_000_000)),
                "severityText": entry["level"].upper(),
                "body": {"stringValue": line},
                "attributes": [
                    {"key": key, "value": {"stringValue": str(entry[key])}}
                    for key in ("backend", "cloud", "color", "route", "status", "trace_id", "level", "message")
                ],
            }]}],
        }]
    }
    req = urllib.request.Request(
        endpoint + "/v1/logs",
        data=json.dumps(payload).encode(),
        headers={"content-type": "application/json"},
        method="POST",
    )
    try:
        urllib.request.urlopen(req, timeout=0.3).close()
    except Exception:
        return

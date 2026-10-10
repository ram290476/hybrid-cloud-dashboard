#!/usr/bin/env python3
"""Static contract check, plus a live facade probe when one is listening."""

import json
import os
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    spec = (ROOT / "contract/openapi.yaml").read_text()
    for path in ("/api/v1/dashboard", "/api/v1/status", "/api/v1/prefs", "/healthz", "/readyz", "/version"):
        if path not in spec:
            raise SystemExit(f"openapi missing {path}")
    dashboard = json.loads((ROOT / "contract/fixtures/serving/dashboard.json").read_text())
    if "data_version" not in json.dumps(dashboard) and not dashboard:
        raise SystemExit("dashboard fixture empty")
    base = os.environ.get("FACADE_URL", "")
    if not base:
        print("contract files ok")
        return
    token = os.popen("python3 scripts/dev_token.py").read().strip()
    req = urllib.request.Request(base.rstrip("/") + "/api/v1/status", headers={"Authorization": f"Bearer {token}"})
    with urllib.request.urlopen(req, timeout=5) as resp:
        lang = resp.headers.get("x-backend-lang")
        if lang not in {"go", "python", "dotnet"}:
            raise SystemExit(f"unexpected lang {lang}")
        print(f"live status via {lang}")


if __name__ == "__main__":
    main()

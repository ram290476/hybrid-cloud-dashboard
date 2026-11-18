#!/usr/bin/env python3
"""Static contract check, a live data-health probe of all three backends, and a facade probe when one is listening."""

import json
import os
import shutil
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA_HEALTH_KEYS = (
    "backend",
    "lang",
    "runtime",
    "color",
    "version",
    "data_version",
    "cloud",
    "checked_at",
    "freshness",
    "fresh_within_seconds",
    "counts",
    "files",
    "tickers",
    "newest_at",
    "oldest_at",
    "collectors",
    "import",
    "warnings",
)


def check_data_health(body: dict, where: str) -> None:
    missing = [key for key in DATA_HEALTH_KEYS if key not in body]
    if missing:
        raise SystemExit(f"{where} data-health missing {missing}: {json.dumps(body)[:500]}")
    if body["freshness"] not in {"fresh", "stale", "missing"}:
        raise SystemExit(f"{where} freshness {body['freshness']}")
    if body["cloud"] not in {"gcp", "aws", "azure"}:
        raise SystemExit(f"{where} cloud {body['cloud']}")
    if body["fresh_within_seconds"] != 129600:
        raise SystemExit(f"{where} fresh_within_seconds {body['fresh_within_seconds']}")
    for name in ("chart", "stock", "funds"):
        if not isinstance(body["counts"].get(name), int) or body["counts"][name] < 0:
            raise SystemExit(f"{where} counts {body['counts']}")
    for name in ("dashboard", "status"):
        file = body["files"][name]
        for key in ("key", "present", "generated_at", "last_modified", "age_seconds"):
            if key not in file:
                raise SystemExit(f"{where} files.{name} missing {key}")
    if not isinstance(body["tickers"], list) or not isinstance(body["collectors"], list) or not isinstance(body["warnings"], list):
        raise SystemExit(f"{where} list fields are not lists")
    for job in body["collectors"]:
        for key in ("name", "status", "last_run", "age_seconds"):
            if key not in job:
                raise SystemExit(f"{where} collector missing {key}")
    if body["import"] is not None:
        for key in ("source", "at"):
            if key not in body["import"]:
                raise SystemExit(f"{where} import missing {key}")


def _free_port() -> int:
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()
    return port


def _dotnet() -> str:
    found = shutil.which("dotnet")
    if found:
        return found
    home = Path.home() / ".dotnet" / "dotnet"
    if home.is_file():
        return str(home)
    raise SystemExit("dotnet is not installed")


def _wait(url: str, proc: subprocess.Popen, log_path: Path) -> None:
    deadline = time.time() + 120
    while time.time() < deadline:
        if proc.poll() is not None:
            tail = log_path.read_text(errors="replace")[-2000:] if log_path.exists() else ""
            raise SystemExit(f"{url} exited {proc.returncode}\n{tail}")
        try:
            with urllib.request.urlopen(url + "/healthz", timeout=1) as resp:
                if resp.status == 200:
                    return
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            time.sleep(0.4)
    tail = log_path.read_text(errors="replace")[-2000:] if log_path.exists() else ""
    raise SystemExit(f"{url} did not become ready\n{tail}")


def live_data_health() -> None:
    """Start Go, Python, and .NET on memory fixtures and check the shared shape."""
    sys.path.insert(0, str(ROOT / "backends/python-aws/src"))
    from dashboard_api.auth import mint

    token = mint("local-dev-secret-change-me", "user-1", "http://localhost/dev-auth", "hcd-local", 1_900_000_000)
    base_env = os.environ.copy()
    for key in (
        "STORAGE_EMULATOR_HOST",
        "AWS_ENDPOINT_URL",
        "AWS_ENDPOINT_URL_S3",
        "AWS_ENDPOINT_URL_DYNAMODB",
        "AZURE_STORAGE_CONNECTION",
        "Storage__ConnectionString",
        "FACADE_KEY",
    ):
        base_env.pop(key, None)
    base_env["FIXTURES_DIR"] = str(ROOT / "contract/fixtures")
    base_env["AUTH_MODE"] = "dev"
    base_env["DEV_JWT_SECRET"] = "local-dev-secret-change-me"
    base_env["DEV_JWT_ISSUER"] = "http://localhost/dev-auth"
    base_env["DEV_JWT_AUDIENCE"] = "hcd-local"
    log_dir = Path("/tmp/hcd-contract")
    log_dir.mkdir(parents=True, exist_ok=True)
    ports = {"go": _free_port(), "python": _free_port(), "dotnet": _free_port()}
    procs: list[tuple[str, subprocess.Popen, Path]] = []
    try:
        go_log = log_dir / "go.log"
        go_env = dict(base_env, PORT=str(ports["go"]))
        procs.append(("go", subprocess.Popen(
            ["go", "run", "./cmd/api"], cwd=ROOT / "backends/go-gcp", env=go_env,
            stdout=go_log.open("w"), stderr=subprocess.STDOUT,
        ), go_log))
        py_log = log_dir / "python.log"
        procs.append(("python", subprocess.Popen(
            [sys.executable, "-m", "uvicorn", "dashboard_api.main:app", "--host", "127.0.0.1", "--port", str(ports["python"]), "--app-dir", str(ROOT / "backends/python-aws/src")],
            cwd=ROOT, env=base_env, stdout=py_log.open("w"), stderr=subprocess.STDOUT,
        ), py_log))
        dn_log = log_dir / "dotnet.log"
        dn_env = dict(base_env, ASPNETCORE_URLS=f"http://127.0.0.1:{ports['dotnet']}")
        procs.append(("dotnet", subprocess.Popen(
            [_dotnet(), "run", "--no-launch-profile", "--project", str(ROOT / "backends/dotnet-azure")],
            cwd=ROOT, env=dn_env, stdout=dn_log.open("w"), stderr=subprocess.STDOUT,
        ), dn_log))
        expect = {
            "go": ("gcp-go", "gcp", "go"),
            "python": ("aws-python", "aws", "python"),
            "dotnet": ("azure-dotnet", "azure", "dotnet"),
        }
        for name, proc, log_path in procs:
            url = f"http://127.0.0.1:{ports[name]}"
            _wait(url, proc, log_path)
            req = urllib.request.Request(url + "/api/v1/data-health", headers={"Authorization": f"Bearer {token}"})
            with urllib.request.urlopen(req, timeout=10) as resp:
                body = json.loads(resp.read().decode())
            check_data_health(body, name)
            backend, cloud, lang = expect[name]
            if body["backend"] != backend or body["cloud"] != cloud or body["lang"] != lang:
                raise SystemExit(f"{name} identity {body['backend']} {body['cloud']} {body['lang']}")
            if body["files"]["dashboard"]["present"] is not True:
                raise SystemExit(f"{name} did not load dashboard.json")
            if not any(job.get("name") == "prices_daily" for job in body["collectors"]):
                raise SystemExit(f"{name} missing prices_daily: {body['collectors']}")
            bare = urllib.request.Request(url + "/api/v1/data-health")
            try:
                urllib.request.urlopen(bare, timeout=5)
            except urllib.error.HTTPError as exc:
                if exc.code != 401:
                    raise SystemExit(f"{name} unauthenticated status {exc.code}")
            else:
                raise SystemExit(f"{name} data-health allowed a request with no token")
        print("data-health ok on go, python, dotnet")
    finally:
        for _, proc, _ in procs:
            proc.terminate()
        for _, proc, _ in procs:
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()


def main() -> None:
    spec = (ROOT / "contract/openapi.yaml").read_text()
    for path in (
        "/api/v1/data-health",
        "/api/v1/dashboard",
        "/api/v1/status",
        "/api/v1/chart/{ticker}",
        "/api/v1/stock/{ticker}",
        "/api/v1/fund/{ticker}",
        "/api/v1/prefs",
        "/ops/logs",
        "/healthz",
        "/readyz",
        "/version",
    ):
        if path not in spec:
            raise SystemExit(f"openapi missing {path}")
    for token in ("getDataHealth", "getOpsLogs", "getStock", "getFund", "DataHealth", "OpsLogs", "fresh_within_seconds", "DataHealthCollector"):
        if token not in spec:
            raise SystemExit(f"openapi missing {token}")
    dashboard = json.loads((ROOT / "contract/fixtures/serving/dashboard.json").read_text())
    if "data_version" not in json.dumps(dashboard) and not dashboard:
        raise SystemExit("dashboard fixture empty")
    samples = (
        ("serving/stock/TSLA.json", "TSLA"),
        ("serving/stock/SPCX.json", "SPCX"),
        ("serving/funds/ARKK.json", "ARKK"),
    )
    for rel, ticker in samples:
        body = json.loads((ROOT / "contract/fixtures" / rel).read_text())
        if body.get("ticker") != ticker:
            raise SystemExit(f"{rel} ticker is {body.get('ticker')}")
        if not body:
            raise SystemExit(f"{rel} empty")
    print("contract files ok")
    live_data_health()
    base = os.environ.get("FACADE_URL", "")
    if not base:
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

#!/usr/bin/env bash
# Process-mode stack for a machine without kind. Ctrl-C stops every child.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD
export DEV_JWT_SECRET="${DEV_JWT_SECRET:-local-dev-secret-change-me}"
export AUTH_MODE=dev
mkdir -p /tmp/hcd-demo
pids=()
cleanup() { for pid in "${pids[@]:-}"; do kill "$pid" 2>/dev/null || true; done; }
trap cleanup EXIT INT TERM

(cd backends/go-gcp && PORT=8081 APP_COLOR=blue FIXTURES_DIR="$ROOT/contract/fixtures" go run ./cmd/api > /tmp/hcd-demo/go.log 2>&1) &
pids+=($!)
PORT=8082 APP_COLOR=blue FIXTURES_DIR="$ROOT/contract/fixtures" python3 -m uvicorn dashboard_api.main:app --app-dir backends/python-aws/src --host 127.0.0.1 --port 8082 > /tmp/hcd-demo/python.log 2>&1 &
pids+=($!)
if command -v dotnet >/dev/null 2>&1 || [[ -x "$HOME/.dotnet/dotnet" ]]; then
  if command -v dotnet >/dev/null 2>&1; then DOTNET=dotnet; else DOTNET="$HOME/.dotnet/dotnet"; fi
  (cd backends/dotnet-azure && FIXTURES_DIR="$ROOT/contract/fixtures" ASPNETCORE_URLS=http://127.0.0.1:8083 "$DOTNET" run --no-launch-profile > /tmp/hcd-demo/dotnet.log 2>&1) &
  pids+=($!)
fi
(cd facade && PORT=8080 FRONTEND_DIR="$ROOT/frontend" DEV_INJECT_TOKEN=1 ROUTING_POLICY="$ROOT/facade/policy/routing-policy.local.yaml" go run ./cmd/facade > /tmp/hcd-demo/facade.log 2>&1) &
pids+=($!)
echo "demo listening on http://127.0.0.1:8080/hybrid.html"
wait

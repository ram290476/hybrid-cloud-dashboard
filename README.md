# Hybrid Cloud Dashboard

One operations UI and three backends that serve the same `/api/v1` contract. The source of truth is Hybrid Cloud Architecture v2 (decision D-native, 9 Oct 2026): no managed Kubernetes on the default path.

| Piece | Runtime | Where it runs |
| --- | --- | --- |
| Shared UI | Static JS copied from `ram290476/investor-dashboard` | kind, or the facade in `make demo` |
| Go API | Cloud Run, scale to zero | `backends/go-gcp` |
| Python API | ECS Fargate (arm64, 0.25 vCPU / 0.5 GB, no ALB, cloudflared sidecar) | `backends/python-aws` |
| .NET API | App Service for Linux, F1, two apps (`dotnet-api-blue` / `dotnet-api-green`) | `backends/dotnet-azure` |
| Facade | Go policy engine locally; Envoy sample in `facade/envoy` | kind + k3s, not EKS/AKS/GKE |
| Sync | `hcd-sync` on the Mac (launchd) | `sync/` |

Blue/green is two layers. Facade weights are per environment (`gcp-go`, `aws-python`, `azure-dotnet`), never per color. The color flip stays inside each environment: a Cloud Run revision, the ECS service pair, or the two F1 apps.

## Repo map

```
frontend/                 copied market UI + hybrid.html operations page
contract/openapi.yaml     shared API
contract/fixtures/        serving snapshots and merge vectors
backends/go-gcp           GCP
backends/python-aws       AWS (container-first; Lambda module is unwired)
backends/dotnet-azure     Azure (JIT Dockerfile; Dockerfile.aot is the cloud publish sketch)
facade/                   cost/perf policy, health, audit log
sync/                     field-level merge across the three object stores
deploy/                   kind config, Kustomize for kind and the local facade
infra/terraform/          OpenTofu-compatible HCL for gcp, aws, azure, cloudflare
```

Section 11 of the architecture copies `apps/web` as zero-build ES modules. The market screens stay that app. `frontend/hybrid.html` is the hybrid operations page and uses the same Industrial Dark tokens (`theme.js`, IBM Plex, `styles.css`).

## Run locally

Process mode (no cluster). Needs Go, Python 3.12 or 3.13, and the .NET 10 SDK. A Mac system Python 3.14 does not have these packages. Use [uv](https://docs.astral.sh/uv/) with 3.13 (`backends/python-aws/.python-version`):

```bash
uv python install 3.13
uv venv --python 3.13
uv pip install fastapi uvicorn boto3 pytest httpx
make demo
```

`make test` uses whichever `python3` is on `PATH`. Point that at the 3.13 venv before running it. CI installs the same packages on Python 3.12.

Open http://127.0.0.1:8080/hybrid.html . The facade probes the three APIs, routes `/api`, and prints one JSON audit line per request. `DEV_INJECT_TOKEN=1` lets the browser call the API with the local HMAC issuer `http://localhost/dev-auth`.

```bash
make dev-token
make test
```

`make dev-token` prints only the token.

Kind on the Mac mini (OrbStack, cluster name `hcd`, registry `localhost:5001`):

```bash
make cluster-up          # kind + local registry + Envoy Gateway + kubectl apply -k deploy/overlays/local
make pf-emulators
make local-seed          # sets dummy AWS creds itself and seeds fake-gcs, SeaweedFS, DynamoDB, and Azurite
tilt up
make contract-test
```

`make cluster-up` is safe to re-run when the cluster already exists. `make cluster-down` deletes the kind cluster and the `kind-registry` container.

kind does not implement Service type LoadBalancer. The Envoy proxy Service is NodePort `30080`, and `deploy/local/kind-config.yaml` publishes that on the Mac as port 80, so `http://127.0.0.1/` is the Gateway. A cluster created before that mapping does not pick it up until `make cluster-down && make cluster-up`. Until then, or if port 80 is already taken:

```bash
make pf-gateway
```

That forwards the Envoy Service in namespace `envoy-gateway-system` (Envoy Gateway creates it there, not in `hcd`) to `127.0.0.1:8088`. Tilt's facade forward on 8080 skips the Gateway.

`make pf-emulators` forwards host ports that stay off the process-mode API ports (facade 8080, Go 8081, Python 8082, .NET 8083):

| Service | Host | In cluster |
| --- | --- | --- |
| SeaweedFS (`svc/minio`) | 9000 | 9000 |
| fake-gcs | 4443 | 4443 |
| Azurite | 10000 | 10000 |
| DynamoDB Local | 8000 | 8000 |
| Firestore emulator | 8787 | 8080 |
| Cosmos gateway / health / explorer | 18081 / 18080 / 11234 | 8081 / 8080 / 1234 |

`make sync-status` sources `scripts/hcd-sync-env.sh` before the one-shot sync.

Emulators live in `hcd-deps`: SeaweedFS (S3). The Deployment is `seaweedfs`; Services are `seaweedfs` and `minio` (the alias setup-plan commands use). fake-gcs-server, Azurite, DynamoDB Local, the Firestore emulator, and the Cosmos emulator `vnext-latest` (linux/arm64 and amd64). MinIO is not used.

Local Kubernetes names are not the cloud names. Deployments are `go-gcp-blue`, `python-aws-blue`, and `dotnet-azure-blue` (green deployments exist at 0 replicas). Services are `go-gcp`, `python-aws`, and `dotnet-azure`, so `kubectl exec deploy/python-aws` does not match; use `deploy/python-aws-blue`. ECS service names in Terraform stay `py-api-blue` and `py-api-green`.

The Go pods set `STORAGE_EMULATOR_HOST` and `FIRESTORE_EMULATOR_HOST` (host `firestore.hcd-deps.svc.cluster.local:8080`). The Python pods set `AWS_ENDPOINT_URL_S3` to `http://minio.hcd-deps.svc.cluster.local:9000`. The .NET pods set `AZURE_STORAGE_CONNECTION` and the same value as `Storage__ConnectionString` (the API reads either). They also set `Cosmos__Endpoint`, `Cosmos__Database`, and `Cosmos__Container` so a setup-plan `printenv` sees them. The Cosmos client is not wired; prefs stay on the blob `prefs/<sub>.json`.

`hcd-sync` polls the three object stores for `prefs/<sub>.json` and merges with the shared Go library. On the Mac:

```bash
cp sync/launchd/com.ram.hcd-sync.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.ram.hcd-sync.plist
```

VS Code: open `hybrid-cloud-dashboard.code-workspace`. Tasks wrap the make targets. Launch configs cover Delve, debugpy, and coreclr.

## Deploy each cloud

Terraform and OpenTofu both read this HCL (`required_version >= 1.6.0`). Validate only. Do not apply from this repo until the new accounts exist.

```bash
make terraform-validate
```

| Root | What a later apply would create |
| --- | --- |
| `infra/terraform/gcp` | Cloud Run `go-api` (min instances 0), two buckets, Firestore database `hcd`, Workload Identity pool, budget only when `billing_account` is set |
| `infra/terraform/aws` | ECS cluster, one ARM64 task definition (API + cloudflared), services `py-api-blue` and `py-api-green` with `desired_count` default **0**, S3, DynamoDB, Cognito pool, GitHub OIDC role for `ram290476/hybrid-cloud-dashboard` |
| `infra/terraform/azure` | Linux F1 plan, two web apps, storage, Cosmos with `free_tier_enabled`, user-assigned identity |
| `infra/terraform/cloudflare` | One CNAME. Health-checked DNS failover is not turned on |
| `infra/terraform/labs` | EKS / AKS / GKE sketches. Not referenced by the cloud roots |

GitHub Actions (`.github/workflows/ci.yml`) runs the tests and `terraform validate` with actions pinned to commit SHAs. `release.yml` is `workflow_dispatch` only and exits before any cloud call until the OIDC variable for that cloud is set. No cloud keys are stored.

Auth in the cloud is a new Cognito pool in a new AWS account. `AUTH_MODE=cognito` returns 401 until `COGNITO_ISSUER` exists. Local mode is `AUTH_MODE=dev`.

## What is stubbed

- Cognito JWKS validation. The dev HMAC token is the working local issuer.
- Firestore, Cosmos query, and DynamoDB are not the sync transport. Each backend reads and writes the prefs document at `prefs/<sub>.json` in its object store, and `hcd-sync` merges those objects. Python also mirrors that document in DynamoDB. A read merges the object and the Dynamo row with the same field-level HLC rules, so a stale Dynamo item cannot hide the synced object. `Cosmos__*` on the .NET Deployment is present for local inspection only.
- OpenTelemetry SDKs are not linked into the three processes yet. JSON logs and the response headers (`x-backend-env`, `x-backend-lang`, `x-backend-runtime`, `x-app-color`, `x-app-version`, `x-data-version`) are. The collector and Grafana LGTM manifests are in `deploy/overlays/local/observability`. Point `OTEL_EXPORTER_OTLP_ENDPOINT` at the collector when the SDK lands. Grafana Cloud stays the free-tier destination for real accounts.
- .NET Native AOT (`Dockerfile.aot`) is not the local image. Local kind uses the JIT SDK image, per section 9.4.2.
- The Lambda URL module under `infra/terraform/aws/modules/lambda-url` is not called by the AWS root.

## Decisions still open

- Terraform or OpenTofu as the tool of record. The HCL is written so either validates. D-6 is unchanged.
- AWS runtime: Fargate is what the root creates. Lambda stays a module until you confirm it.
- The public DNS name, and whether `app.example.com` should pay for health-checked failover.
- The new AWS account id for Cognito. Do not reuse `308639168050`. Every account id, token, and billing account in this repo is a placeholder.
- Cosmos and the Firestore emulator are the heavy local pods. Drop them from the deps overlay if the 24 GB Mac is tight; the APIs still serve fixtures from memory.

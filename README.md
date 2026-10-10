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

Process mode (no cluster). Needs Go, Python 3.12, and the .NET 10 SDK.

```bash
python3 -m pip install fastapi uvicorn boto3 pytest httpx
make demo
```

Open http://127.0.0.1:8080/hybrid.html . The facade probes the three APIs, routes `/api`, and prints one JSON audit line per request. `DEV_INJECT_TOKEN=1` lets the browser call the API with the local HMAC issuer `http://localhost/dev-auth`.

```bash
make dev-token
make test
```

Kind on the Mac mini (OrbStack, cluster name `hcd`, registry `localhost:5001`):

```bash
make cluster-up          # kind + local registry + Envoy Gateway + kubectl apply -k deploy/overlays/local
make pf-emulators
make local-seed
tilt up
make contract-test
```

Emulators live in `hcd-deps`: SeaweedFS (S3; the Service is also named `minio` so `aws --endpoint-url http://localhost:9000` still works), fake-gcs-server, Azurite, DynamoDB Local, the Firestore emulator, and the Cosmos emulator vNext. MinIO is not used; it no longer publishes a prebuilt image.

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
- Firestore, Cosmos query, and DynamoDB are not the sync transport. Each backend mirrors the prefs document to `prefs/<sub>.json` in its object store, and `hcd-sync` merges those objects. Python still writes DynamoDB when `AWS_ENDPOINT_URL_DYNAMODB` is set.
- OpenTelemetry SDKs are not linked into the three processes yet. JSON logs and the response headers (`x-backend-env`, `x-backend-lang`, `x-backend-runtime`, `x-app-color`, `x-app-version`, `x-data-version`) are. The collector and Grafana LGTM manifests are in `deploy/overlays/local/observability`. Point `OTEL_EXPORTER_OTLP_ENDPOINT` at the collector when the SDK lands. Grafana Cloud stays the free-tier destination for real accounts.
- .NET Native AOT (`Dockerfile.aot`) is not the local image. Local kind uses the JIT SDK image, per section 9.4.2.
- The Lambda URL module under `infra/terraform/aws/modules/lambda-url` is not called by the AWS root.

## Decisions still open

- Terraform or OpenTofu as the tool of record. The HCL is written so either validates. D-6 is unchanged.
- AWS runtime: Fargate is what the root creates. Lambda stays a module until you confirm it.
- The public DNS name, and whether `app.example.com` should pay for health-checked failover.
- The new AWS account id for Cognito. Do not reuse `308639168050`. Every account id, token, and billing account in this repo is a placeholder.
- Cosmos and the Firestore emulator are the heavy local pods. Drop them from the deps overlay if the 24 GB Mac is tight; the APIs still serve fixtures from memory.

# Local ports

Ports used by the Hybrid Cloud Dashboard on the local kind + Tilt setup (Mac mini, OrbStack, cluster `hcd`). Values were checked on the Mac on 18 Nov 2026 (America/Los_Angeles) unless a row says otherwise.

This page is documentation only. It does not change how the stack is started. The same tables are on the wiki [Local Ports](https://github.com/ram290476/hybrid-cloud-dashboard/wiki/Local-Ports) page.

## Host port reservation

- **8080** on the Mac is reserved for this project's Tilt facade forward (`k8s_resource("facade", port_forwards = "8080:8080")` in the [`Tiltfile`](../Tiltfile)). Do not bind another app to host 8080 while that Tilt session is running.
- **8081** on the Mac is reserved for the separate **job-search-local** dashboard. That project's kind cluster `jobs` maps host 8081 to NodePort 30080. The mapping was already in place on 18 Nov 2026.

Do not run `make demo` (process mode) at the same time as the kind + Tilt session: both use host 8080. Process-mode Go also listens on 8081 and would collide with job-search-local.

## Host ports

These are the ports that show up on the Mac when cluster `hcd` and `tilt up` are running.

| Host port | Bound on | What | How it is published |
| --- | --- | --- | --- |
| 80 | all addresses | Envoy Gateway: market UI, `/hybrid.html`, `/api`, `/healthz` | kind [`deploy/local/kind-config.yaml`](../deploy/local/kind-config.yaml) maps host 80 to NodePort 30080. This is the primary Gateway URL: `http://127.0.0.1/` |
| 443 | all addresses | kind mapping to container 443 | Same kind config. The TCP mapping was present; HTTPS was not exercised on 18 Nov 2026 |
| 53232 | 127.0.0.1 | kind cluster API (container 6443) | Assigned by kind for this cluster. It is **not** pinned in `kind-config.yaml` and can change if the cluster is recreated. Confirm with `docker ps` on `hcd-control-plane` |
| 5001 | 127.0.0.1 | `kind-registry` (`registry:2`, container 5000) | `make cluster-up`. Tilt pushes `localhost:5001/hcd-*` images here |
| 8080 | 127.0.0.1 | Facade (also serves the UI, including `/hybrid.html`) | Tilt port-forward of `svc/facade`. Skips the Gateway |
| 3000 | 127.0.0.1 | Grafana (LGTM) | Tilt port-forward of `svc/lgtm` |
| 10350 | 127.0.0.1 | Tilt UI | `tilt up` |

On 18 Nov 2026, `http://127.0.0.1/`, `http://127.0.0.1/hybrid.html`, `http://127.0.0.1:8080/`, `http://127.0.0.1:8080/hybrid.html`, `http://127.0.0.1:3000/`, and `http://127.0.0.1:10350/` all returned HTTP 200. Host 8081 was in use by job-search-local and also returned 200.

### Optional host forwards

These are not started by Tilt. They only listen after someone runs the matching `make` target or `kubectl port-forward`.

| Host port | What | How | Notes from 18 Nov 2026 |
| --- | --- | --- | --- |
| 8088 | Envoy Gateway (same as host 80) | `make pf-gateway`: port-forward of the Envoy Service in `envoy-gateway-system` to `127.0.0.1:8088` | Fallback when the kind host-80 mapping is missing, or port 80 is already taken. Not listening that day: screen `pfg` was looping the target, but the default kubectl context was `orbstack`, which has no `envoy-gateway-system` resources. Use `kubectl --context kind-hcd` (or switch context) for this forward. Prefer `http://127.0.0.1/` when the kind mapping is present |
| 8083 | `svc/dotnet-azure` (cluster port 8080) | Detached screen `pfd` runs `kubectl -n hcd port-forward svc/dotnet-azure 8083:8080` (not a Makefile target) | Not listening that day, same kubectl context issue |
| 9000 | SeaweedFS S3 (`svc/minio`) | `make pf-emulators` | Not running that day |
| 4443 | fake-gcs | `make pf-emulators` | Not running that day |
| 10000 | Azurite | `make pf-emulators` | Not running that day |
| 8000 | DynamoDB Local | `make pf-emulators` | Not running that day |
| 8787 | Firestore emulator (cluster port 8080) | `make pf-emulators` | Host 8787 is used so this forward does not take 8080 |
| 18081 / 18080 / 11234 | Cosmos gateway / health / Data Explorer (cluster ports 8081 / 8080 / 1234) | `make pf-emulators` | Host ports stay off process-mode 8080–8083 |

Detached `screen` sessions on the Mac (`tilt`, `pfg`, `pfd`) are a convenience, not something the repo starts.

### Process mode (`make demo`)

Used only when the kind + Tilt session is **not** running. [`scripts/demo.sh`](../scripts/demo.sh) binds:

| Host port | Process |
| --- | --- |
| 8080 | Facade (serves the UI) |
| 8081 | Go backend |
| 8082 | Python backend |
| 8083 | .NET backend (if `dotnet` is on `PATH`) |

## In-cluster service ports

Namespace `hcd`. Every app Service is ClusterIP port **8080**.

| Service | Port | Notes |
| --- | --- | --- |
| `facade` | 8080 | Policy engine; also serves `frontend/` |
| `frontend` | 8080 | nginx UI |
| `python-aws` | 8080 | Blue (`version=blue`) |
| `python-aws-preview` | 8080 | Green; no endpoints while green is at 0 replicas |
| `go-gcp` | 8080 | Blue |
| `go-gcp-preview` | 8080 | Green; no endpoints at 0 replicas |
| `dotnet-azure` | 8080 | Blue |
| `dotnet-azure-preview` | 8080 | Green; no endpoints at 0 replicas |

The Envoy data-plane Service lives in `envoy-gateway-system` (not `hcd`). It is a NodePort: container/Service port **80**, node port **30080**. kind publishes that node port on the Mac as host 80.

## Dependency ports

Namespace `hcd-deps`. These ports are in-cluster only until `make pf-emulators` is run.

| Service | In-cluster ports | Role |
| --- | --- | --- |
| `cosmos` | 8081, 8080, 1234 | Cosmos emulator (gateway, health, Data Explorer) |
| `firestore` | 8080 | Firestore emulator |
| `dynamodb` | 8000 | DynamoDB Local |
| `azurite` | 10000 | Azure Blob emulator |
| `seaweedfs` | 9000 | S3-compatible store |
| `minio` | 9000 | Alias Service for the same SeaweedFS pods. There is no separate MinIO Deployment |
| `fake-gcs` | 4443 | GCS emulator |

## Observability ports

Namespace `observability`.

| Service | In-cluster ports | Role |
| --- | --- | --- |
| `lgtm` | 3000, 4317, 4318 | Grafana UI plus OTLP. Tilt forwards only 3000 to the Mac |
| `otel-collector` | 4317, 4318 | OTLP gRPC and HTTP |

## kubectl context

The kind context is `kind-hcd`. If the current context is `orbstack` (OrbStack's own cluster), bare `kubectl -n hcd …` and `make pf-gateway` will not see these Services. Use `kubectl --context kind-hcd` without changing the default context unless you intend to.

## Related

- Issue [#78](https://github.com/ram290476/hybrid-cloud-dashboard/issues/78)
- Wiki: [Local Ports](https://github.com/ram290476/hybrid-cloud-dashboard/wiki/Local-Ports), [Running Locally](https://github.com/ram290476/hybrid-cloud-dashboard/wiki/Running-Locally)

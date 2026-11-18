# Dash Import

`make dash-import` copies investor-dashboard `serving/` objects into the three local object stores used by the kind + Tilt stack: fake-gcs (Go / GCP), SeaweedFS (Python / AWS), and Azurite (.NET / Azure). The command landed in [PR #81](https://github.com/ram290476/hybrid-cloud-dashboard/pull/81) as [`scripts/dash-import.py`](../scripts/dash-import.py). The same page is on the wiki at [Dash Import](https://github.com/ram290476/hybrid-cloud-dashboard/wiki/Dash-Import).

The copy is read-only on AWS. The only AWS calls are `ListObjectsV2` and `GetObject`. Uploads go only to the same local emulator endpoints that `make local-seed` uses. The script refuses any local endpoint whose host looks like AWS, and it wraps the source S3 client so every other API call raises `PermissionError`.

This page does not name a bucket, a profile, or an account. Never commit those values, and never use a bucket name that contains an account id.

## Prerequisites

1. The kind cluster `hcd` is up, and Tilt has created the emulator pods in namespace `hcd-deps`.
2. The `make pf-emulators` port forwards are listening on the Mac, at least on ports **9000** (SeaweedFS, Service `minio`), **4443** (fake-gcs), and **10000** (Azurite). DynamoDB on 8000 is used by `make local-seed`, not by `dash-import`.
3. The kubectl context for those forwards is **`kind-hcd`**. The Mac's default context is often `orbstack`, which does not have the `hcd-deps` Services. `make pf-emulators` does not pass `--context`, so either run the forwards with `kubectl --context kind-hcd` or run `make pf-emulators` from a shell whose current context is already `kind-hcd`. Do not change the default context unless you intend to. See [`docs/local-ports.md`](local-ports.md).
4. AWS credentials that can read the investor-dashboard serving prefix: a valid `aws login` session, or a named profile. Prefer a **least-privilege read-only IAM profile limited to `serving/*`** over the account root user.
5. Python 3 with boto3, the same environment used for `make local-seed`.

## Environment variables

Required:

| Variable | Purpose |
| --- | --- |
| `DASH_BUCKET` | The investor-dashboard serving bucket. Nothing in the repo provides this name; you must export it. The script exits if it is unset. |
| `AWS_PROFILE` or `AWS_DEFAULT_PROFILE` | Profile used to read `DASH_BUCKET`. If `AWS_ACCESS_KEY_ID` is the local SeaweedFS key (`minioadmin`) and no profile is set, the script exits and asks you to export `AWS_PROFILE`. When a profile is set, the script temporarily unsets `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` so dummy local keys do not shadow it. |
| `AWS_REGION` or `AWS_DEFAULT_REGION` | Required unless the profile already has a region. |

Optional local endpoint overrides. The defaults match `make pf-emulators` and `make local-seed`. Leave them unset unless a forward is on a different port. The script will refuse to publish if any of these hosts looks like AWS.

| Variable | Default |
| --- | --- |
| `GCS_EMULATOR_HOST` | `http://127.0.0.1:4443` |
| `GCS_BUCKET` | `hcd-serving` |
| `S3_ENDPOINT` | `http://127.0.0.1:9000` |
| `S3_BUCKET` | `hcd-serving` |
| `AZURE_BLOB_ENDPOINT` | `http://127.0.0.1:10000` |
| `BLOB_CONTAINER` | `hcd-serving` |

## What is copied and skipped

Only these source keys are imported, and only when they are JSON objects directly under the listed prefixes (no nested paths):

| Source key | Local key |
| --- | --- |
| `serving/dashboard.json` | `serving/dashboard.json` |
| `serving/status.json` | `serving/status.json` |
| `serving/chart_data/*.json` | `serving/chart/*.json` |
| `serving/stock/*.json` | `serving/stock/*.json` |
| `serving/funds/*.json` | `serving/funds/*.json` |

The `chart_data` → `chart` rename is required: the three backends and the UI read `serving/chart/{TICKER}.json`, not `chart_data`.

Skipped, even if they sit under `serving/`:

- `*.gz` objects
- any key whose path has a `parquet` segment or a `.parquet` file
- keys that contain `_lease` or `_debounce`
- any other prefix or nested key

The script does not write to AWS in any of those cases. It simply does not copy the object.

## Run

```bash
export DASH_BUCKET=<investor-dashboard serving bucket>
export AWS_PROFILE=<read-only profile>
export AWS_REGION=<bucket region>
# pf-emulators must already be listening, with context kind-hcd
make dash-import
```

A successful run prints a source count, then one line per local store:

```text
read N objects from DASH_BUCKET (chart=X stock=Y funds=Z)
imported N objects into fake-gcs
imported N objects into seaweedfs
imported N objects into azurite
```

The three local publishes are attempted even if one fails. If any store fails, the command exits non-zero and names the failures. Nothing is written to AWS in either case.

`make dash-import` fills all three stores because the facade can serve `/api/v1` from any healthy backend. A copy that reached only SeaweedFS would still leave the Go and .NET backends empty.

## Verify

- The `read N objects` line is non-zero, and the three `imported N objects` lines match that count.
- Through the Gateway (or Tilt's facade forward on 8080):

```bash
curl -s -o /tmp/hcd-dashboard.json -w "%{http_code}\n" http://127.0.0.1/api/v1/dashboard
curl -s -o /tmp/hcd-tsla.json -w "%{http_code}\n" http://127.0.0.1/api/v1/stock/TSLA
curl -s -o /tmp/hcd-arkk.json -w "%{http_code}\n" http://127.0.0.1/api/v1/fund/ARKK
```

`make local-seed` already places a sample `serving/stock/TSLA.json` (and `SPCX`, plus `serving/funds/ARKK.json`) in the stores. After a successful import, those URLs should return the investor-dashboard payload rather than only the local fixture. `/` and `/hybrid.html` should load; stock and fund pages in the market UI should stop returning 404 once the [PR #81](https://github.com/ram290476/hybrid-cloud-dashboard/pull/81) endpoints are in the running backend pods.

## Troubleshooting

### `python3` has no boto3

`make local-seed` and `make dash-import` call `python3` from `PATH`. On the Mac mini the default `python3` may be a Homebrew version that does not have boto3 or the other project packages. Use the project virtualenv instead:

```bash
./.venv/bin/python scripts/local-seed.py
./.venv/bin/python scripts/dash-import.py
```

Activate that venv first if you prefer to keep using `make local-seed` and `make dash-import`. The same 3.13 environment is described in the README.

### Expired AWS session


`aws sts get-caller-identity` fails, or `make dash-import` exits with no credentials. Run `aws login` (or refresh the named profile) and retry. Do not paste passwords or codes into chat.

### `AWS_ACCESS_KEY_ID` is the local SeaweedFS key

`make local-seed` sets dummy AWS keys in that shell. Export `AWS_PROFILE` for the read of `DASH_BUCKET` and run `make dash-import` again. The script will use the profile and ignore those dummy keys.

### Port forwards are not listening, or the import cannot reach the emulators

Check `lsof` (or equivalent) for ports 9000, 4443, and 10000, and check `kubectl config current-context`. If the context is `orbstack`, the forwards will not see `hcd-deps`. Start them with `kubectl --context kind-hcd` and keep that shell (or a detached `screen` session) open. See [`docs/local-ports.md`](local-ports.md).

If Tilt later recreates the emulator pods, existing forwards die. Restart the forwards, then re-seed and re-import as needed.

### Data lost after a pod restart

[PR #81](https://github.com/ram290476/hybrid-cloud-dashboard/pull/81) mounts fake-gcs and Azurite on the kind node (`hostPath` under `/var/lib/hcd/`). A restart of those two pods keeps their files. Deleting the kind cluster still wipes them.

SeaweedFS, DynamoDB, Firestore, and Cosmos still use memory or an `emptyDir`, so a Tilt or pod restart that recreates those pods wipes SeaweedFS. After that, re-run the port forwards, then `make local-seed`, then `make dash-import` if you need the investor-dashboard snapshot again.

### The script refuses an endpoint

`S3_ENDPOINT`, `GCS_EMULATOR_HOST`, or `AZURE_BLOB_ENDPOINT` must be a local HTTP(S) URL. The script exits rather than write if the host is an AWS endpoint. Leave those variables unset unless you are pointing at a local forward.

### No objects matched

The script exits without writing if `DASH_BUCKET` has no keys under the prefixes above. Check that the bucket is the investor-dashboard *serving* bucket and that your profile can list `serving/`.

## Related

- Issue [#82](https://github.com/ram290476/hybrid-cloud-dashboard/issues/82)
- Feature PR [#81](https://github.com/ram290476/hybrid-cloud-dashboard/pull/81)
- [`docs/local-ports.md`](local-ports.md) and wiki [Local Ports](https://github.com/ram290476/hybrid-cloud-dashboard/wiki/Local-Ports)
- Wiki [Running Locally](https://github.com/ram290476/hybrid-cloud-dashboard/wiki/Running-Locally) and [Dash Import](https://github.com/ram290476/hybrid-cloud-dashboard/wiki/Dash-Import)

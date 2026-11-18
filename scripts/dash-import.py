#!/usr/bin/env python3
"""Copy investor-dashboard serving objects into the local emulators.

Reads DASH_BUCKET with the AWS profile and region already in the environment.
The only AWS calls are ListObjects and GetObject. Uploads go to the same
SeaweedFS, fake-gcs, and Azurite endpoints as make local-seed.
"""

from __future__ import annotations

import importlib.util
import json
import os
import sys
import urllib.parse
import urllib.request
from pathlib import Path

_AWS_HOSTS = ("amazonaws.com", "amazonaws.com.cn", "aws.amazon.com")
_SKIP_MARKERS = ("_lease", "_debounce")
_EXACT = {"serving/dashboard.json", "serving/status.json"}
_PREFIXES = (
    ("serving/chart_data/", "serving/chart/"),
    ("serving/stock/", "serving/stock/"),
    ("serving/funds/", "serving/funds/"),
)


def _seed():
    path = Path(__file__).with_name("local-seed.py")
    spec = importlib.util.spec_from_file_location("local_seed", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def skipped(key: str) -> bool:
    lower = key.lower()
    if lower.endswith(".gz"):
        return True
    parts = lower.split("/")
    if any(part == "parquet" or part.endswith(".parquet") for part in parts):
        return True
    return any(marker in lower for marker in _SKIP_MARKERS)


def destination_key(key: str) -> str | None:
    """Local object key, or None when this source key is not part of the import."""
    if skipped(key):
        return None
    if key in _EXACT:
        return key
    for src, dest in _PREFIXES:
        if not key.startswith(src) or not key.endswith(".json"):
            continue
        rest = key[len(src):]
        if rest and "/" not in rest:
            return dest + rest
        return None
    return None


class ReadOnlyS3:
    """Blocks every S3 call except list and get. The import holds this, not the raw client."""

    _ALLOWED = frozenset({"list_objects_v2", "get_object"})

    def __init__(self, client):
        self._client = client

    def __getattr__(self, name):
        if name.startswith("_"):
            raise AttributeError(name)
        if name not in self._ALLOWED:
            raise PermissionError(f"dash-import never writes to AWS ({name})")
        return getattr(self._client, name)


def assert_local_endpoint(url: str) -> str:
    parsed = urllib.parse.urlsplit(url)
    host = (parsed.hostname or "").lower().rstrip(".")
    if parsed.scheme not in {"http", "https"} or not host:
        raise SystemExit(f"refusing endpoint {url}")
    if any(host == marker or host.endswith("." + marker) for marker in _AWS_HOSTS):
        raise SystemExit(f"refusing to write to AWS endpoint {url}")
    return url


def iter_serving_keys(client, bucket: str):
    token = None
    while True:
        kwargs = {"Bucket": bucket, "Prefix": "serving/"}
        if token:
            kwargs["ContinuationToken"] = token
        page = client.list_objects_v2(**kwargs)
        for item in page.get("Contents") or []:
            yield item["Key"]
        if not page.get("IsTruncated"):
            return
        token = page.get("NextContinuationToken")


def load_objects(client, bucket: str) -> list[tuple[str, bytes]]:
    chosen: list[tuple[str, bytes]] = []
    for key in iter_serving_keys(client, bucket):
        dest = destination_key(key)
        if dest is None:
            continue
        body = client.get_object(Bucket=bucket, Key=key)["Body"].read()
        chosen.append((dest, body))
    return chosen


def publish_gcs(host: str, bucket: str, objects: list[tuple[str, bytes]]) -> None:
    assert_local_endpoint(host)
    create = urllib.request.Request(
        host.rstrip("/") + "/storage/v1/b?project=hcd-local",
        data=json.dumps({"name": bucket}).encode(),
        method="POST",
    )
    create.add_header("content-type", "application/json")
    try:
        urllib.request.urlopen(create, timeout=10)
    except Exception as exc:
        if "409" not in str(exc):
            raise
    for key, body in objects:
        name = urllib.parse.quote(key, safe="/")
        upload = urllib.request.Request(
            host.rstrip("/") + f"/upload/storage/v1/b/{bucket}/o?uploadType=media&name={name}",
            data=body,
            method="POST",
        )
        upload.add_header("content-type", "application/json")
        urllib.request.urlopen(upload, timeout=30)


def publish_s3(client, bucket: str, objects: list[tuple[str, bytes]]) -> None:
    try:
        client.create_bucket(Bucket=bucket)
    except Exception:
        pass
    for key, body in objects:
        client.put_object(Bucket=bucket, Key=key, Body=body, ContentType="application/json")


def publish_azure(endpoint: str, container: str, objects: list[tuple[str, bytes]]) -> None:
    assert_local_endpoint(endpoint)
    seed = _seed()
    base = endpoint.rstrip("/") + "/" + seed.AZURITE_ACCOUNT + "/" + container
    seed._azure("PUT", base + "?restype=container", seed.AZURITE_ACCOUNT, seed.AZURITE_KEY, b"", False, [("restype", "container")])
    for key, body in objects:
        quoted = urllib.parse.quote(key, safe="/")
        seed._azure("PUT", base + "/" + quoted, seed.AZURITE_ACCOUNT, seed.AZURITE_KEY, body, True, [])


def local_s3_client(endpoint: str):
    assert_local_endpoint(endpoint)
    import boto3

    return boto3.client(
        "s3",
        endpoint_url=endpoint,
        aws_access_key_id="minioadmin",
        aws_secret_access_key="minioadmin",
        region_name="us-east-1",
    )


def _source_session():
    profile = (os.environ.get("AWS_PROFILE") or os.environ.get("AWS_DEFAULT_PROFILE") or "").strip()
    if not profile and os.environ.get("AWS_ACCESS_KEY_ID") == "minioadmin":
        raise SystemExit(
            "AWS_ACCESS_KEY_ID is the local SeaweedFS key. Export AWS_PROFILE for a read of DASH_BUCKET."
        )
    import boto3
    saved = {}
    if profile:
        for key in ("AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"):
            if key in os.environ:
                saved[key] = os.environ.pop(key)
    try:
        session = boto3.Session(profile_name=profile or None)
        creds = session.get_credentials()
        if creds is None:
            raise SystemExit("no AWS credentials. Export AWS_PROFILE or a read-only key. The bucket name is DASH_BUCKET.")
        frozen = creds.get_frozen_credentials()
        region = session.region_name or os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION")
    finally:
        os.environ.update(saved)
    if not region:
        raise SystemExit("AWS region is unset. Set it on the profile or export AWS_REGION.")
    return boto3.Session(
        aws_access_key_id=frozen.access_key,
        aws_secret_access_key=frozen.secret_key,
        aws_session_token=frozen.token,
        region_name=region,
    )


def source_client() -> ReadOnlyS3:
    session = _source_session()
    saved = {}
    for key in ("AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3"):
        if key in os.environ:
            saved[key] = os.environ.pop(key)
    try:
        client = session.client("s3")
    finally:
        os.environ.update(saved)
    return ReadOnlyS3(client)


def with_import_marker(objects: list[tuple[str, bytes]]) -> list[tuple[str, bytes]]:
    """Record that this copy came from investor-dashboard, without the bucket name."""
    marker = _seed().import_marker("dash-import", "investor-dashboard")
    kept = [(key, body) for key, body in objects if key != marker[0]]
    kept.append(marker)
    return kept


def publish_all(objects: list[tuple[str, bytes]]) -> None:
    gcs_host = os.environ.get("GCS_EMULATOR_HOST", "http://127.0.0.1:4443")
    gcs_bucket = os.environ.get("GCS_BUCKET", "hcd-serving")
    s3_endpoint = os.environ.get("S3_ENDPOINT", "http://127.0.0.1:9000")
    s3_bucket = os.environ.get("S3_BUCKET", "hcd-serving")
    azure_endpoint = os.environ.get("AZURE_BLOB_ENDPOINT", "http://127.0.0.1:10000")
    azure_container = os.environ.get("BLOB_CONTAINER", "hcd-serving")
    errors: list[str] = []
    try:
        publish_gcs(gcs_host, gcs_bucket, objects)
        print(f"imported {len(objects)} objects into fake-gcs")
    except Exception as exc:
        errors.append(f"fake-gcs: {exc}")
        print(f"fake-gcs failed: {exc}", file=sys.stderr)
    try:
        publish_s3(local_s3_client(s3_endpoint), s3_bucket, objects)
        print(f"imported {len(objects)} objects into seaweedfs")
    except Exception as exc:
        errors.append(f"seaweedfs: {exc}")
        print(f"seaweedfs failed: {exc}", file=sys.stderr)
    try:
        publish_azure(azure_endpoint, azure_container, objects)
        print(f"imported {len(objects)} objects into azurite")
    except Exception as exc:
        errors.append(f"azurite: {exc}")
        print(f"azurite failed: {exc}", file=sys.stderr)
    if errors:
        raise SystemExit("dash-import did not seed every local store: " + "; ".join(errors))


def main() -> None:
    bucket = os.environ.get("DASH_BUCKET", "").strip()
    if not bucket:
        raise SystemExit("DASH_BUCKET is required. It is the investor-dashboard serving bucket. This script does not contain a bucket name.")
    client = source_client()
    objects = load_objects(client, bucket)
    if not objects:
        raise SystemExit("no serving objects matched. Check DASH_BUCKET. Nothing was written.")
    chart = sum(1 for key, _ in objects if key.startswith("serving/chart/"))
    stock = sum(1 for key, _ in objects if key.startswith("serving/stock/"))
    funds = sum(1 for key, _ in objects if key.startswith("serving/funds/"))
    print(f"read {len(objects)} objects from DASH_BUCKET (chart={chart} stock={stock} funds={funds})")
    publish_all(with_import_marker(objects))


if __name__ == "__main__":
    main()

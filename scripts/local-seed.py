#!/usr/bin/env python3
"""Create local buckets/tables and upload contract fixtures. Safe to re-run."""

import base64
import hashlib
import hmac
import json
import os
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from email.utils import formatdate
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "contract" / "fixtures"


def gcs_bucket(host: str, name: str) -> None:
    body = json.dumps({"name": name}).encode()
    req = urllib.request.Request(host.rstrip("/") + "/storage/v1/b?project=hcd-local", data=body, method="POST")
    req.add_header("content-type", "application/json")
    try:
        urllib.request.urlopen(req, timeout=5)
    except Exception as exc:
        if "409" not in str(exc):
            raise
    for path in FIXTURES.rglob("*.json"):
        key = str(path.relative_to(FIXTURES)).replace("\\", "/")
        upload = urllib.request.Request(
            host.rstrip("/") + f"/upload/storage/v1/b/{name}/o?uploadType=media&name={key}",
            data=path.read_bytes(),
            method="POST",
        )
        upload.add_header("content-type", "application/json")
        urllib.request.urlopen(upload, timeout=5)


# Public Azurite well-known key. Local emulator only.
AZURITE_ACCOUNT = "devstoreaccount1"
AZURITE_KEY = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="


def _shared_key(method: str, account: str, key_b64: str, path: str, query: list[tuple[str, str]], body: bytes, blob: bool, date: str | None = None) -> dict[str, str]:
    """Shared Key for the Azurite path-style URL.

    Since API version 2015-02-21 a Content-Length of 0 is an empty field in the
    string-to-sign. Path-style URLs already start with /<account>. Azurite still
    canonicalizes the resource as /<account> plus that path, so the account
    appears twice (the same string the Azure SDKs sign). Content-Type is set
    explicitly because urllib would otherwise add
    application/x-www-form-urlencoded without it being signed.
    """
    if date is None:
        date = formatdate(timeval=datetime.now(timezone.utc).timestamp(), usegmt=True)
    content_type = "application/octet-stream"
    length = "" if len(body) == 0 else str(len(body))
    ms = {"x-ms-date": date, "x-ms-version": "2020-10-02"}
    if blob:
        ms["x-ms-blob-type"] = "BlockBlob"
    canon = "\n".join(f"{name}:{ms[name]}" for name in sorted(ms))
    resource_path = path if path.startswith("/") else "/" + path
    resource = "/" + account + resource_path
    if query:
        resource += "\n" + "\n".join(f"{key}:{value}" for key, value in sorted(query))
    to_sign = f"{method}\n\n\n{length}\n\n{content_type}\n\n\n\n\n\n\n{canon}\n{resource}"
    sig = base64.b64encode(hmac.new(base64.b64decode(key_b64), to_sign.encode(), hashlib.sha256).digest()).decode()
    headers = {"Content-Type": content_type, "Authorization": f"SharedKey {account}:{sig}", **ms}
    return headers


def _azure(method: str, endpoint: str, account: str, key_b64: str, body: bytes, blob: bool, query: list[tuple[str, str]]) -> None:
    parsed = urllib.parse.urlsplit(endpoint)
    headers = _shared_key(method, account, key_b64, parsed.path, query, body, blob)
    req = urllib.request.Request(endpoint, data=body if method == "PUT" else None, method=method, headers=headers)
    try:
        urllib.request.urlopen(req, timeout=5)
    except urllib.error.HTTPError as exc:
        if exc.code not in (409,):
            raise


def azurite_seed(endpoint: str, container: str) -> None:
    base = endpoint.rstrip("/") + "/" + AZURITE_ACCOUNT + "/" + container
    _azure("PUT", base + "?restype=container", AZURITE_ACCOUNT, AZURITE_KEY, b"", False, [("restype", "container")])
    for path in FIXTURES.rglob("*.json"):
        key = str(path.relative_to(FIXTURES)).replace("\\", "/")
        quoted = urllib.parse.quote(key)
        _azure("PUT", base + "/" + quoted, AZURITE_ACCOUNT, AZURITE_KEY, path.read_bytes(), True, [])


def main() -> None:
    os.environ.setdefault("AWS_ACCESS_KEY_ID", "minioadmin")
    os.environ.setdefault("AWS_SECRET_ACCESS_KEY", "minioadmin")
    os.environ.setdefault("AWS_DEFAULT_REGION", "us-east-1")
    os.environ.setdefault("AWS_REGION", "us-east-1")
    host = os.environ.get("GCS_EMULATOR_HOST", "http://127.0.0.1:4443")
    try:
        gcs_bucket(host, os.environ.get("GCS_BUCKET", "hcd-serving"))
        print("seeded fake-gcs")
    except Exception as exc:
        print(f"fake-gcs skipped: {exc}")
    try:
        import boto3

        s3 = boto3.client("s3", endpoint_url=os.environ.get("S3_ENDPOINT", "http://127.0.0.1:9000"))
        bucket = os.environ.get("S3_BUCKET", "hcd-serving")
        try:
            s3.create_bucket(Bucket=bucket)
        except Exception:
            pass
        for path in FIXTURES.rglob("*.json"):
            key = str(path.relative_to(FIXTURES)).replace("\\", "/")
            s3.put_object(Bucket=bucket, Key=key, Body=path.read_bytes())
        dynamo = boto3.client("dynamodb", endpoint_url=os.environ.get("DYNAMODB_ENDPOINT", "http://127.0.0.1:8000"))
        try:
            dynamo.create_table(
                TableName="hcd-prefs",
                AttributeDefinitions=[{"AttributeName": "user_sub", "AttributeType": "S"}],
                KeySchema=[{"AttributeName": "user_sub", "KeyType": "HASH"}],
                BillingMode="PAY_PER_REQUEST",
            )
        except Exception:
            pass
        print("seeded seaweedfs and dynamodb")
    except Exception as exc:
        print(f"aws emulators skipped: {exc}")
    try:
        azurite_seed(os.environ.get("AZURE_BLOB_ENDPOINT", "http://127.0.0.1:10000"), os.environ.get("BLOB_CONTAINER", "hcd-serving"))
        print("seeded azurite")
    except Exception as exc:
        print(f"azurite skipped: {exc}")


if __name__ == "__main__":
    main()

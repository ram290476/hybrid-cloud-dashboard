#!/usr/bin/env python3
"""Create local buckets/tables and upload contract fixtures. Safe to re-run."""

import json
import os
import urllib.request
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


def main() -> None:
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


if __name__ == "__main__":
    main()

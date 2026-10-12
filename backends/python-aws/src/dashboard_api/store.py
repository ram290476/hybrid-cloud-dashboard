"""Memory store, plus S3 and DynamoDB when the emulator endpoints are set."""

from __future__ import annotations

import json
import os
from typing import Protocol

from dashboard_api.merge import merge_docs


class Store(Protocol):
    def ready(self) -> None: ...
    def get_object(self, key: str) -> bytes: ...
    def put_object(self, key: str, body: bytes) -> None: ...
    def get_prefs(self, sub: str) -> bytes: ...
    def put_prefs(self, sub: str, body: bytes) -> None: ...


class Memory:
    def __init__(self) -> None:
        self.objs: dict[str, bytes] = {}
        self.prefs: dict[str, bytes] = {}

    def ready(self) -> None:
        return None

    def get_object(self, key: str) -> bytes:
        if key not in self.objs:
            raise KeyError(key)
        return self.objs[key]

    def put_object(self, key: str, body: bytes) -> None:
        self.objs[key] = body

    def get_prefs(self, sub: str) -> bytes:
        if sub not in self.prefs:
            raise KeyError(sub)
        return self.prefs[sub]

    def put_prefs(self, sub: str, body: bytes) -> None:
        self.prefs[sub] = body


class S3Dynamo:
    """boto3 against SeaweedFS (S3) and DynamoDB Local. Falls back per call."""

    def __init__(self, memory: Memory) -> None:
        self.memory = memory
        self.bucket = os.environ.get("S3_BUCKET", "hcd-serving")
        self.table = os.environ.get("PREFS_TABLE", "hcd-prefs")
        self.s3 = None
        self.dynamo = None
        if os.environ.get("AWS_ENDPOINT_URL_S3"):
            import boto3

            self.s3 = boto3.client("s3", endpoint_url=os.environ["AWS_ENDPOINT_URL_S3"])
        if os.environ.get("AWS_ENDPOINT_URL_DYNAMODB"):
            import boto3

            self.dynamo = boto3.client("dynamodb", endpoint_url=os.environ["AWS_ENDPOINT_URL_DYNAMODB"])

    def ready(self) -> None:
        if self.s3:
            self._ensure_bucket()
        if self.dynamo:
            self._ensure_table()

    def _ensure_bucket(self) -> None:
        try:
            self.s3.head_bucket(Bucket=self.bucket)
            return
        except Exception as exc:
            if not _absent(exc):
                raise
        try:
            self.s3.create_bucket(Bucket=self.bucket)
        except Exception as exc:
            if not _already_exists(exc):
                raise

    def _ensure_table(self) -> None:
        try:
            self.dynamo.describe_table(TableName=self.table)
            return
        except Exception as exc:
            if not _absent(exc):
                raise
        try:
            self.dynamo.create_table(
                TableName=self.table,
                AttributeDefinitions=[{"AttributeName": "user_sub", "AttributeType": "S"}],
                KeySchema=[{"AttributeName": "user_sub", "KeyType": "HASH"}],
                BillingMode="PAY_PER_REQUEST",
            )
        except Exception as exc:
            if not _already_exists(exc):
                raise

    def get_object(self, key: str) -> bytes:
        if not self.s3:
            return self.memory.get_object(key)
        obj = self.s3.get_object(Bucket=self.bucket, Key=key)
        return obj["Body"].read()

    def put_object(self, key: str, body: bytes) -> None:
        if not self.s3:
            self.memory.put_object(key, body)
            return
        self.s3.put_object(Bucket=self.bucket, Key=key, Body=body)

    def get_prefs(self, sub: str) -> bytes:
        # hcd-sync writes the merged document to the object store only.
        # DynamoDB is a local mirror and must not hide that object. When both
        # exist, field-level HLC merge matches the other two backends.
        docs: list[dict] = []
        if self.s3:
            raw = self._s3_prefs(sub)
            if raw is not None:
                docs.append(json.loads(raw))
        if self.dynamo:
            raw = self._dynamo_prefs(sub)
            if raw is not None:
                docs.append(json.loads(raw))
        if docs:
            return json.dumps(merge_docs(docs)).encode()
        return self.memory.get_prefs(sub)

    def _s3_prefs(self, sub: str) -> bytes | None:
        try:
            obj = self.s3.get_object(Bucket=self.bucket, Key=f"prefs/{sub}.json")
            return obj["Body"].read()
        except Exception as exc:
            if _missing(exc):
                return None
            raise

    def _dynamo_prefs(self, sub: str) -> bytes | None:
        item = self.dynamo.get_item(TableName=self.table, Key={"user_sub": {"S": sub}})
        if "Item" not in item:
            return None
        return item["Item"]["doc"]["S"].encode()

    def put_prefs(self, sub: str, body: bytes) -> None:
        # Object copy is what hcd-sync merges. DynamoDB stays a mirror of that document.
        if self.s3:
            self.s3.put_object(Bucket=self.bucket, Key=f"prefs/{sub}.json", Body=body)
        if self.dynamo:
            self.dynamo.put_item(TableName=self.table, Item={"user_sub": {"S": sub}, "doc": {"S": body.decode()}})
            return
        self.memory.put_prefs(sub, body)


def _error_code(exc: Exception) -> str:
    response = getattr(exc, "response", None)
    if isinstance(response, dict):
        return str(response.get("Error", {}).get("Code", ""))
    return ""


def _missing(exc: Exception) -> bool:
    return _error_code(exc) in ("NoSuchKey", "404", "NoSuchBucket", "NotFound")


def _absent(exc: Exception) -> bool:
    return _error_code(exc) in ("NoSuchKey", "404", "NoSuchBucket", "NotFound", "ResourceNotFoundException")


def _already_exists(exc: Exception) -> bool:
    return _error_code(exc) in ("BucketAlreadyOwnedByYou", "BucketAlreadyExists", "ResourceInUseException")

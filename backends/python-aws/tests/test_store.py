"""S3 is the sync transport. A stale Dynamo row must not hide it."""

import json

from dashboard_api.store import Memory, S3Dynamo


class _Body:
    def __init__(self, raw: bytes) -> None:
        self._raw = raw

    def read(self) -> bytes:
        return self._raw


class FakeS3:
    def __init__(self) -> None:
        self.objects: dict[str, bytes] = {}
        self.buckets: set[str] = set()

    def head_bucket(self, Bucket: str) -> dict:
        if Bucket not in self.buckets:
            raise FakeMissing("NoSuchBucket")
        return {}

    def create_bucket(self, Bucket: str) -> None:
        self.buckets.add(Bucket)

    def get_object(self, Bucket: str, Key: str) -> dict:
        if Key not in self.objects:
            raise FakeMissing()
        return {"Body": _Body(self.objects[Key])}

    def put_object(self, Bucket: str, Key: str, Body: bytes) -> None:
        self.objects[Key] = Body


class FakeMissing(Exception):
    def __init__(self, code: str = "NoSuchKey") -> None:
        super().__init__(code)
        self.response = {"Error": {"Code": code}}


class FakeDynamo:
    def __init__(self) -> None:
        self.items: dict[str, str] = {}
        self.tables: set[str] = set()

    def describe_table(self, TableName: str) -> dict:
        if TableName not in self.tables:
            raise FakeMissing("ResourceNotFoundException")
        return {"Table": {"TableName": TableName}}

    def create_table(self, TableName: str, **_kwargs: object) -> None:
        self.tables.add(TableName)

    def get_item(self, TableName: str, Key: dict) -> dict:
        sub = Key["user_sub"]["S"]
        if sub not in self.items:
            return {}
        return {"Item": {"doc": {"S": self.items[sub]}}}

    def put_item(self, TableName: str, Item: dict) -> None:
        self.items[Item["user_sub"]["S"]] = Item["doc"]["S"]


def _doc(sub: str, theme: str, wall: int, ticker: str | None = None) -> dict:
    doc = {
        "user_sub": sub,
        "fields": {"display.theme": {"value": theme, "hlc": {"wall_ms": wall, "counter": 0, "origin": "test"}}},
        "tickers": {"adds": {}, "removes": {}},
        "pinned": {"adds": {}, "removes": {}},
        "version": wall,
    }
    if ticker:
        doc["tickers"]["adds"][ticker] = {"value": ticker, "hlc": {"wall_ms": wall, "counter": 0, "origin": "test"}}
    return doc


def test_object_store_wins_over_stale_dynamo():
    store = S3Dynamo(Memory())
    s3 = FakeS3()
    dynamo = FakeDynamo()
    store.s3 = s3
    store.dynamo = dynamo
    s3.objects["prefs/user-1.json"] = json.dumps(_doc("user-1", "midnight-slate", 20)).encode()
    dynamo.items["user-1"] = json.dumps(_doc("user-1", "industrial-dark", 5, ticker="TSLA"))
    merged = json.loads(store.get_prefs("user-1"))
    assert merged["fields"]["display.theme"]["value"] == "midnight-slate"
    assert "TSLA" in merged["tickers"]["adds"]


def test_object_store_alone_is_visible():
    store = S3Dynamo(Memory())
    s3 = FakeS3()
    store.s3 = s3
    store.dynamo = FakeDynamo()
    s3.objects["prefs/user-1.json"] = json.dumps(_doc("user-1", "midnight-slate", 9)).encode()
    assert json.loads(store.get_prefs("user-1"))["fields"]["display.theme"]["value"] == "midnight-slate"


def test_ready_creates_missing_bucket_and_table():
    store = S3Dynamo(Memory())
    s3 = FakeS3()
    dynamo = FakeDynamo()
    store.s3 = s3
    store.dynamo = dynamo
    store.ready()
    assert s3.buckets == {"hcd-serving"}
    assert dynamo.tables == {"hcd-prefs"}
    store.ready()


def test_put_prefs_writes_object_and_mirror():
    store = S3Dynamo(Memory())
    s3 = FakeS3()
    dynamo = FakeDynamo()
    store.s3 = s3
    store.dynamo = dynamo
    store.put_prefs("user-1", b'{"user_sub":"user-1"}')
    assert s3.objects["prefs/user-1.json"] == b'{"user_sub":"user-1"}'
    assert dynamo.items["user-1"] == '{"user_sub":"user-1"}'

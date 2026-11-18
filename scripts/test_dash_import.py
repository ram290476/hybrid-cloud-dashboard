"""dash-import selects serving JSON and never writes to the source bucket."""

import importlib.util
import io
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

import pytest


def _load():
    path = Path(__file__).with_name("dash-import.py")
    spec = importlib.util.spec_from_file_location("dash_import", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


dash = _load()


def test_destination_keeps_serving_json_and_renames_chart_data():
    keys = [
        "serving/dashboard.json",
        "serving/status.json",
        "serving/chart_data/TSLA.json",
        "serving/chart_data/TSLA.json.gz",
        "serving/chart_data/TSLA.parquet",
        "serving/parquet/prices.json",
        "serving/chart_data/nested/TSLA.json",
        "serving/stock/TSLA.json",
        "serving/stock/TSLA.json.gz",
        "serving/stock/_lease",
        "serving/funds/ARKK.json",
        "serving/funds/ARKK.parquet",
        "serving/_debounce/status.json",
        "serving/prefs/user.json",
        "raw/prices/TSLA.parquet",
    ]
    got = {key: dash.destination_key(key) for key in keys}
    assert got["serving/dashboard.json"] == "serving/dashboard.json"
    assert got["serving/status.json"] == "serving/status.json"
    assert got["serving/chart_data/TSLA.json"] == "serving/chart/TSLA.json"
    assert got["serving/stock/TSLA.json"] == "serving/stock/TSLA.json"
    assert got["serving/funds/ARKK.json"] == "serving/funds/ARKK.json"
    assert got["serving/chart_data/TSLA.json.gz"] is None
    assert got["serving/chart_data/TSLA.parquet"] is None
    assert got["serving/parquet/prices.json"] is None
    assert got["serving/chart_data/nested/TSLA.json"] is None
    assert got["serving/stock/TSLA.json.gz"] is None
    assert got["serving/stock/_lease"] is None
    assert got["serving/funds/ARKK.parquet"] is None
    assert got["serving/_debounce/status.json"] is None
    assert got["serving/prefs/user.json"] is None
    assert got["raw/prices/TSLA.parquet"] is None


class _Recording:
    def __init__(self, pages, bodies):
        self.pages = list(pages)
        self.bodies = bodies
        self.calls = []

    def list_objects_v2(self, **kwargs):
        self.calls.append(("list_objects_v2", kwargs.get("Prefix"), kwargs.get("ContinuationToken")))
        return self.pages.pop(0)

    def get_object(self, **kwargs):
        self.calls.append(("get_object", kwargs["Key"]))
        return {"Body": io.BytesIO(self.bodies[kwargs["Key"]])}

    def put_object(self, **kwargs):
        self.calls.append(("put_object", kwargs.get("Key")))
        raise AssertionError("put_object on the source")

    def delete_object(self, **kwargs):
        self.calls.append(("delete_object", kwargs.get("Key")))
        raise AssertionError("delete_object on the source")


def test_load_reads_selected_keys_only():
    pages = [
        {
            "Contents": [
                {"Key": "serving/dashboard.json"},
                {"Key": "serving/chart_data/TSLA.json.gz"},
                {"Key": "serving/stock/_lease"},
            ],
            "IsTruncated": True,
            "NextContinuationToken": "next",
        },
        {
            "Contents": [
                {"Key": "serving/chart_data/TSLA.json"},
                {"Key": "serving/stock/TSLA.json"},
                {"Key": "serving/_debounce"},
                {"Key": "serving/funds/ARKK.json"},
            ],
            "IsTruncated": False,
        },
    ]
    bodies = {
        "serving/dashboard.json": b'{"data_version":"x"}',
        "serving/chart_data/TSLA.json": b'{"ticker":"TSLA"}',
        "serving/stock/TSLA.json": b'{"ticker":"TSLA"}',
        "serving/funds/ARKK.json": b'{"ticker":"ARKK"}',
    }
    raw = _Recording(pages, bodies)
    client = dash.ReadOnlyS3(raw)
    loaded = dash.load_objects(client, "source-bucket")
    assert loaded == [
        ("serving/dashboard.json", bodies["serving/dashboard.json"]),
        ("serving/chart/TSLA.json", bodies["serving/chart_data/TSLA.json"]),
        ("serving/stock/TSLA.json", bodies["serving/stock/TSLA.json"]),
        ("serving/funds/ARKK.json", bodies["serving/funds/ARKK.json"]),
    ]
    kinds = [name for name, *_ in raw.calls]
    assert kinds == ["list_objects_v2", "get_object", "list_objects_v2", "get_object", "get_object", "get_object"]
    with pytest.raises(PermissionError):
        client.put_object(Bucket="source-bucket", Key="serving/dashboard.json", Body=b"{}")
    with pytest.raises(PermissionError):
        client.delete_object(Bucket="source-bucket", Key="serving/dashboard.json")
    assert not any(name in {"put_object", "delete_object"} for name, *_ in raw.calls)


def test_local_endpoint_refuses_aws():
    with pytest.raises(SystemExit):
        dash.assert_local_endpoint("https://s3.us-east-1.amazonaws.com")
    with pytest.raises(SystemExit):
        dash.assert_local_endpoint("https://bucket.s3.amazonaws.com")
    assert dash.assert_local_endpoint("http://127.0.0.1:9000") == "http://127.0.0.1:9000"


def test_main_requires_bucket(monkeypatch):
    monkeypatch.delenv("DASH_BUCKET", raising=False)
    with pytest.raises(SystemExit, match="DASH_BUCKET"):
        dash.main()


def test_emulator_key_is_not_sent_to_aws(monkeypatch):
    monkeypatch.delenv("AWS_PROFILE", raising=False)
    monkeypatch.delenv("AWS_DEFAULT_PROFILE", raising=False)
    monkeypatch.setenv("AWS_ACCESS_KEY_ID", "minioadmin")
    with pytest.raises(SystemExit, match="SeaweedFS"):
        dash._source_session()


class _Store(BaseHTTPRequestHandler):
    seen = []

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        _Store.seen.append(("POST", self.path, body))
        if "storage/v1/b?" in self.path and json.loads(body).get("name"):
            self.send_response(200)
            self.end_headers()
            return
        self.send_response(200)
        self.end_headers()

    def log_message(self, fmt, *args):
        return


def test_publish_gcs_uses_the_renamed_key():
    _Store.seen = []
    server = ThreadingHTTPServer(("127.0.0.1", 0), _Store)
    port = server.server_address[1]
    import threading

    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        dash.publish_gcs(f"http://127.0.0.1:{port}", "hcd-serving", [("serving/chart/TSLA.json", b'{"ticker":"TSLA"}')])
    finally:
        server.shutdown()
    assert any("name=serving/chart/TSLA.json" in path for _, path, _ in _Store.seen)
    assert not any("chart_data" in path for _, path, _ in _Store.seen)


class _Azure(BaseHTTPRequestHandler):
    seen = []

    def do_PUT(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        _Azure.seen.append((urlsplit(self.path).path, body))
        self.send_response(201)
        self.end_headers()

    def log_message(self, fmt, *args):
        return


def test_publish_azure_writes_the_local_container():
    _Azure.seen = []
    server = ThreadingHTTPServer(("127.0.0.1", 0), _Azure)
    port = server.server_address[1]
    import threading

    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        dash.publish_azure(
            f"http://127.0.0.1:{port}",
            "hcd-serving",
            [("serving/funds/ARKK.json", b'{"ticker":"ARKK"}')],
        )
    finally:
        server.shutdown()
    assert _Azure.seen[-1][0].endswith("/hcd-serving/serving/funds/ARKK.json")
    assert _Azure.seen[-1][1] == b'{"ticker":"ARKK"}'


def test_with_import_marker_records_source_without_a_bucket():
    import re

    marked = dash.with_import_marker([("serving/status.json", b"{}"), ("serving/_import.json", b"old")])
    assert [key for key, _ in marked] == ["serving/status.json", "serving/_import.json"]
    doc = json.loads(marked[-1][1])
    assert doc["source"] == "dash-import"
    assert doc["origin"] == "investor-dashboard"
    assert doc["at"].endswith("Z")
    assert "bucket" not in doc
    assert re.search(r"\d{12}", json.dumps(doc)) is None


def test_publish_s3_puts_only_on_the_given_client():
    class Local:
        def __init__(self):
            self.puts = []

        def create_bucket(self, **kwargs):
            self.puts.append(("create_bucket", kwargs["Bucket"]))

        def put_object(self, **kwargs):
            self.puts.append(("put_object", kwargs["Bucket"], kwargs["Key"], kwargs["Body"]))

    local = Local()
    dash.publish_s3(local, "hcd-serving", [("serving/stock/TSLA.json", b"{}")])
    assert local.puts == [
        ("create_bucket", "hcd-serving"),
        ("put_object", "hcd-serving", "serving/stock/TSLA.json", b"{}"),
    ]

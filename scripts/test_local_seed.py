"""Azurite Shared Key must match the headers urllib actually sends."""

import base64
import hashlib
import hmac
import importlib.util
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit


def _load():
    path = Path(__file__).with_name("local-seed.py")
    spec = importlib.util.spec_from_file_location("local_seed", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


seed = _load()


def _expected(method: str, path: str, query: str, headers, key_b64: str, account: str) -> str:
    length = headers.get("Content-Length", "")
    if length in ("", "0"):
        length = ""
    content_type = headers.get("Content-Type", "")
    ms = sorted((name.lower(), value) for name, value in headers.items() if name.lower().startswith("x-ms-"))
    canon = "\n".join(f"{name}:{value}" for name, value in ms)
    resource = path
    if query:
        pairs = sorted(part.split("=", 1) for part in query.split("&"))
        resource += "\n" + "\n".join(f"{key}:{value}" for key, value in pairs)
    to_sign = f"{method}\n\n\n{length}\n\n{content_type}\n\n\n\n\n\n\n{canon}\n{resource}"
    sig = base64.b64encode(hmac.new(base64.b64decode(key_b64), to_sign.encode(), hashlib.sha256).digest()).decode()
    return f"SharedKey {account}:{sig}"


class _Store(BaseHTTPRequestHandler):
    seen = []

    def do_PUT(self):
        length = int(self.headers.get("Content-Length", "0"))
        self.rfile.read(length)
        want = _expected("PUT", urlsplit(self.path).path, urlsplit(self.path).query, self.headers, seed.AZURITE_KEY, seed.AZURITE_ACCOUNT)
        got = self.headers.get("Authorization")
        _Store.seen.append((self.path, got == want, self.headers.get("Content-Type"), self.headers.get("Content-Length")))
        if got != want:
            self.send_response(403)
            self.end_headers()
            self.wfile.write(b"signature mismatch")
            return
        self.send_response(201)
        self.end_headers()

    def log_message(self, fmt, *args):
        return


def test_azurite_signature_accepts_container_and_blob():
    _Store.seen = []
    server = ThreadingHTTPServer(("127.0.0.1", 0), _Store)
    port = server.server_address[1]
    import threading

    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        endpoint = f"http://127.0.0.1:{port}"
        seed._azure("PUT", f"{endpoint}/{seed.AZURITE_ACCOUNT}/hcd-serving?restype=container", seed.AZURITE_ACCOUNT, seed.AZURITE_KEY, b"", False, [("restype", "container")])
        seed._azure("PUT", f"{endpoint}/{seed.AZURITE_ACCOUNT}/hcd-serving/serving/status.json", seed.AZURITE_ACCOUNT, seed.AZURITE_KEY, b'{"ok":true}', True, [])
    finally:
        server.shutdown()
    assert len(_Store.seen) == 2
    assert all(ok for _, ok, _, _ in _Store.seen), _Store.seen
    assert _Store.seen[0][2] == "application/octet-stream"
    assert _Store.seen[0][3] == "0"
    assert _Store.seen[1][3] == "11"

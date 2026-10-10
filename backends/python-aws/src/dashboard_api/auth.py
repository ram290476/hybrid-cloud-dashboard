"""Dev HMAC JWT. Production sets AUTH_MODE=cognito and checks the Cognito JWKS."""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import time


def _b64(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()


def _unb64(raw: str) -> bytes:
    pad = "=" * (-len(raw) % 4)
    return base64.urlsafe_b64decode(raw + pad)


def mint(secret: str, sub: str, issuer: str, audience: str, exp: int) -> str:
    header = _b64(json.dumps({"alg": "HS256", "typ": "JWT"}, separators=(",", ":")).encode())
    payload = _b64(json.dumps({"sub": sub, "iss": issuer, "aud": audience, "exp": exp, "iat": exp - 3600}, separators=(",", ":")).encode())
    unsigned = f"{header}.{payload}"
    sig = hmac.new(secret.encode(), unsigned.encode(), hashlib.sha256).digest()
    return unsigned + "." + _b64(sig)


def parse(secret: str, token: str, issuer: str, audience: str, now: float | None = None) -> str:
    parts = token.split(".")
    if len(parts) != 3:
        raise ValueError("malformed token")
    unsigned = parts[0] + "." + parts[1]
    expected = hmac.new(secret.encode(), unsigned.encode(), hashlib.sha256).digest()
    if not hmac.compare_digest(expected, _unb64(parts[2])):
        raise ValueError("bad signature")
    claims = json.loads(_unb64(parts[1]))
    if issuer and claims.get("iss") != issuer:
        raise ValueError("bad issuer")
    if audience and claims.get("aud") != audience:
        raise ValueError("bad audience")
    if claims.get("exp", 0) and (now or time.time()) >= claims["exp"]:
        raise ValueError("expired")
    if not claims.get("sub"):
        raise ValueError("missing sub")
    return claims["sub"]

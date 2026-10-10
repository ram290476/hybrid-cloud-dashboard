#!/usr/bin/env python3
"""Mint the local HMAC JWT used by checklist step 11."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "backends/python-aws/src"))
from dashboard_api.auth import mint  # noqa: E402

print(mint("local-dev-secret-change-me", "user-1", "http://localhost/dev-auth", "hcd-local", 1_800_000_000))

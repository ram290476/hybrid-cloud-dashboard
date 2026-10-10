"""Field-level prefs merge. Behavior matches libs/merge and contract/fixtures/merge."""

from __future__ import annotations

import hashlib
import json
from typing import Any

MAX_TICKERS = 25
MAX_PINNED = 6


def _cmp(a: dict, b: dict) -> int:
    for key in ("wall_ms", "counter"):
        if a.get(key, 0) != b.get(key, 0):
            return -1 if a.get(key, 0) < b.get(key, 0) else 1
    if a.get("origin", "") != b.get("origin", ""):
        return -1 if a.get("origin", "") < b.get("origin", "") else 1
    return 0


def empty_doc(sub: str = "") -> dict:
    return {
        "user_sub": sub,
        "fields": {},
        "tickers": {"adds": {}, "removes": {}},
        "pinned": {"adds": {}, "removes": {}},
        "version": 0,
    }


def _norm(doc: dict) -> dict:
    doc.setdefault("fields", {})
    doc.setdefault("tickers", {"adds": {}, "removes": {}})
    doc["tickers"].setdefault("adds", {})
    doc["tickers"].setdefault("removes", {})
    doc.setdefault("pinned", {"adds": {}, "removes": {}})
    doc["pinned"].setdefault("adds", {})
    doc["pinned"].setdefault("removes", {})
    doc.setdefault("version", 0)
    doc.setdefault("user_sub", "")
    return doc


def merge_docs(docs: list[dict]) -> dict:
    out = empty_doc()
    for raw in docs:
        doc = _norm(json.loads(json.dumps(raw)))
        if doc["user_sub"]:
            out["user_sub"] = doc["user_sub"]
        out["version"] = max(out["version"], doc["version"])
        for key, field in doc["fields"].items():
            cur = out["fields"].get(key)
            if cur is None or _cmp(field["hlc"], cur["hlc"]) > 0:
                out["fields"][key] = field
        _merge_set(out["tickers"], doc["tickers"])
        _merge_set(out["pinned"], doc["pinned"])
    return out


def _merge_set(dst: dict, src: dict) -> None:
    for tag, add in src["adds"].items():
        dst["adds"].setdefault(tag, add)
    for tag, hlc in src["removes"].items():
        cur = dst["removes"].get(tag)
        if cur is None or _cmp(hlc, cur) > 0:
            dst["removes"][tag] = hlc


def _present(orset: dict) -> list[dict]:
    items = []
    for tag, add in orset["adds"].items():
        if tag in orset["removes"]:
            continue
        items.append({"tag": tag, "value": add["value"], "hlc": add["hlc"]})
    items.sort(key=lambda item: (item["hlc"].get("wall_ms", 0), item["hlc"].get("counter", 0), item["hlc"].get("origin", ""), item["tag"]))
    best: dict[str, dict] = {}
    for item in items:
        cur = best.get(item["value"])
        if cur is None or _cmp(item["hlc"], cur["hlc"]) > 0:
            best[item["value"]] = item
    out = list(best.values())
    out.sort(key=lambda item: (item["hlc"].get("wall_ms", 0), item["hlc"].get("counter", 0), item["hlc"].get("origin", ""), item["tag"]))
    return out


def _decode(value: Any) -> Any:
    # Go stores json.RawMessage, which arrives as a JSON text value. Python seeds store the object.
    if not isinstance(value, str):
        return value
    try:
        return json.loads(value)
    except json.JSONDecodeError:
        return value


def _trim(items: list[dict], limit: int) -> list[dict]:
    if len(items) > limit:
        return items[len(items) - limit :]
    return items


def project(doc: dict) -> dict:
    doc = _norm(doc)
    tickers = _trim(_present(doc["tickers"]), MAX_TICKERS)
    allowed = {item["value"] for item in tickers}
    pins = _trim([item for item in _present(doc["pinned"]) if item["value"] in allowed], MAX_PINNED)
    display: dict[str, Any] = {}
    charts: dict[str, Any] = {}
    updated = 0
    for key, field in doc["fields"].items():
        if field.get("tombstone"):
            continue
        updated = max(updated, field["hlc"].get("wall_ms", 0))
        value = _decode(field.get("value"))
        if key.startswith("display."):
            display[key[8:]] = value
        elif key.startswith("chart."):
            if isinstance(value, dict):
                charts[key[6:]] = value
    display.setdefault("theme", "industrial-dark")
    display.setdefault("updown_palette", "green-red")
    display.setdefault("chart_period", "1M")
    display.setdefault("time_zone", "America/New_York")
    return {
        "tickers": [item["value"] for item in tickers],
        "pinned": [item["value"] for item in pins],
        "display": display,
        "chart_settings": charts,
        "version": doc["version"],
    }

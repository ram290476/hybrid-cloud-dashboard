import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { logsRequest, readFilters, renderFilterBar, writeFilters } from "./ops-filters.js";
import { mergeEntries, renderLogs, traceHref } from "./ops-logs.js";

const line = (over = {}) => ({
  timestamp: "2026-11-18T20:00:00Z",
  level: "info",
  backend: "gcp-go",
  cloud: "gcp",
  color: "blue",
  route: "/api/v1/stock/TSLA",
  status: 200,
  trace_id: "abc",
  message: "GET /api/v1/stock/TSLA 200",
  ...over,
});

test("filters round-trip through the URL", () => {
  const search = writeFilters("?tab=logs", {
    backends: ["gcp-go", "facade"],
    range: "1h",
    q: "TSLA",
    level: "error",
    live: true,
  });
  const filters = readFilters(search);
  assert.deepEqual(filters.backends, ["gcp-go", "facade"]);
  assert.equal(filters.range, "1h");
  assert.equal(filters.q, "TSLA");
  assert.equal(filters.level, "error");
  assert.equal(filters.live, true);
  assert.match(search, /tab=logs/);
  const custom = writeFilters(search, { range: "custom", from: "2026-11-18T19:00", to: "2026-11-18T20:00" });
  assert.equal(readFilters(custom).from, "2026-11-18T19:00");
  assert.match(logsRequest(readFilters(custom)), /from=2026-11-18T19%3A00/);
  assert.match(logsRequest(filters), /from=1h/);
  assert.match(logsRequest(filters), /backend=gcp-go/);
  assert.match(logsRequest(filters), /q=TSLA/);
  assert.match(renderFilterBar(filters, { level: true, live: true }), /placeholder="Search logs"/);
  assert.match(renderFilterBar(filters, { level: true }), /value="gcp-go"/);
});

test("logs render the empty state, a row, and the next page", () => {
  assert.match(renderLogs({ entries: [] }), /No log lines in this window/);
  const html = renderLogs({ entries: [line()], next_cursor: "2026-11-18T19:00:00Z" });
  assert.match(html, /TSLA/);
  assert.match(html, /class="badge level-info"/);
  assert.match(html, /log-gcp/);
  assert.match(html, /Load more/);
  assert.match(html, /data-cursor="2026-11-18T19:00:00Z"/);
  assert.match(traceHref("abc"), /abc/);
  const more = mergeEntries([line()], [line(), line({ trace_id: "def", timestamp: "2026-11-18T19:00:00Z" })]);
  assert.equal(more.length, 2);
  assert.equal(more[1].trace_id, "def");
});

test("the operations page hosts the logs tab and the shared filter bar", () => {
  const html = readFileSync(new URL("./hybrid.html", import.meta.url), "utf8");
  const js = readFileSync(new URL("./hybrid.js", import.meta.url), "utf8");
  assert.match(html, /data-tab="logs"/);
  assert.match(js, /ops-logs\.js/);
  assert.match(js, /ops-filters\.js/);
  assert.match(js, /logsRequest/);
});

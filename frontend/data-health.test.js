import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { collectorClass, disagreements, formatAge, loadReports, missingReport, renderDataHealth } from "./data-health.js";
import { searchWithTab, tabFromSearch } from "./ops-tabs.js";

function report(backend, overrides = {}) {
  return {
    ...missingReport(backend),
    freshness: "fresh",
    lang: "go",
    runtime: "cloud-run",
    counts: { chart: 2, stock: 2, funds: 1 },
    files: {
      dashboard: { key: "serving/dashboard.json", present: true, generated_at: "2026-10-10T00:00:00Z", last_modified: "2026-10-10T00:00:00Z", age_seconds: 3600 },
      status: { key: "serving/status.json", present: true, generated_at: "2026-10-10T00:00:00Z", last_modified: "2026-10-10T00:00:00Z", age_seconds: 3600 },
    },
    tickers: ["ARKK", "TSLA"],
    newest_at: "2026-10-10T00:00:00Z",
    oldest_at: "2026-10-09T00:00:00Z",
    collectors: [{ name: "prices_daily", status: "ok", last_run: "2026-10-10T00:00:00Z", age_seconds: 3600 }],
    import: { source: "local-seed", origin: "contract-fixtures", at: "2026-10-10T09:00:00Z", age_seconds: 60 },
    warnings: [],
    ...overrides,
  };
}

test("data health renders freshness, collectors, and disagreement", () => {
  const azure = missingReport("azure-dotnet", "backend unreachable");
  const python = report("aws-python", {
    lang: "python",
    runtime: "lambda",
    freshness: "stale",
    counts: { chart: 1, stock: 2, funds: 1 },
    files: {
      dashboard: { key: "serving/dashboard.json", present: true, generated_at: "2026-10-09T00:00:00Z", last_modified: null, age_seconds: 900000 },
      status: { key: "serving/status.json", present: true, generated_at: "2026-10-10T00:00:00Z", last_modified: null, age_seconds: 3600 },
    },
    collectors: [{ name: "prices_daily", status: "error", last_run: "2026-10-01T00:00:00Z", age_seconds: 900000 }],
    warnings: ["serving/dashboard.json is stale", "collector prices_daily status is error"],
  });
  const html = renderDataHealth([report("gcp-go"), python, azure]);
  assert.match(html, /The backends disagree on dashboard generated_at and status generated_at and chart count/);
  assert.match(html, /data-backend="gcp-go"/);
  assert.match(html, /data-freshness="fresh"/);
  assert.match(html, /data-freshness="stale"/);
  assert.match(html, /data-freshness="missing"/);
  assert.match(html, /class="badge fresh"/);
  assert.match(html, /class="badge stale"/);
  assert.match(html, /class="badge missing"/);
  assert.match(html, /class="job ok"/);
  assert.match(html, /class="job bad"/);
  assert.match(html, /prices_daily/);
  assert.match(html, /backend unreachable/);
  assert.match(html, /local-seed/);
  assert.match(html, /contract-fixtures/);
  assert.match(html, /class="health-row disagree"/);
  assert.match(html, /class="disagree"/);
  assert.equal(collectorClass("ok"), "ok");
  assert.equal(collectorClass("failed"), "bad");
  assert.equal(collectorClass("running"), "warn");
  assert.equal(formatAge(90), "1m");
  assert.equal(formatAge(null), "unknown");
  assert.deepEqual(disagreements([report("gcp-go"), report("aws-python")]), []);
});

test("a missing report renders a warning instead of throwing", () => {
  const html = renderDataHealth([missingReport("gcp-go", "serving/status.json is missing")]);
  assert.match(html, /data-freshness="missing"/);
  assert.match(html, /serving\/status.json is missing/);
  assert.match(html, /No collectors/);
  assert.match(html, /not recorded/);
  assert.doesNotMatch(html, /health-alert/);
  assert.equal(renderDataHealth([]), `<p class="muted">No backends to compare.</p>`);
});

test("loadReports asks each backend and keeps a warning when one fails", async () => {
  const calls = [];
  const fetchImpl = async (url) => {
    calls.push(url);
    if (url.endsWith("azure-dotnet")) throw new Error("down");
    return {
      ok: true,
      json: async () => report(url.split("/").pop(), { freshness: "fresh" }),
    };
  };
  const reports = await loadReports(
    [
      { name: "gcp-go", endpoint: "http://127.0.0.1:8081" },
      { name: "aws-python", endpoint: "http://127.0.0.1:8082" },
      { name: "azure-dotnet", endpoint: "http://127.0.0.1:8083" },
    ],
    fetchImpl,
  );
  assert.deepEqual(calls, [
    "/api/v1/facade/data-health/gcp-go",
    "/api/v1/facade/data-health/aws-python",
    "/api/v1/facade/data-health/azure-dotnet",
  ]);
  assert.equal(reports[0].freshness, "fresh");
  assert.equal(reports[0].endpoint, "http://127.0.0.1:8081");
  assert.equal(reports[2].freshness, "missing");
  assert.match(reports[2].warnings[0], /down/);
  const html = renderDataHealth(reports);
  assert.match(html, /127\.0\.0\.1:8083/);
  assert.match(html, /data-freshness="missing"/);
});

test("operations page has a data health tab", () => {
  const html = readFileSync(new URL("./hybrid.html", import.meta.url), "utf8");
  const js = readFileSync(new URL("./hybrid.js", import.meta.url), "utf8");
  assert.match(html, /data-tab="health"/);
  assert.match(html, /Data health/);
  assert.match(js, /data-health\.js/);
  assert.match(js, /loadReports/);
  assert.equal(tabFromSearch("?tab=health"), "health");
  assert.equal(tabFromSearch(""), "ops");
  assert.equal(searchWithTab("?tab=health", "ops"), "");
  assert.equal(searchWithTab("", "health"), "?tab=health");
});

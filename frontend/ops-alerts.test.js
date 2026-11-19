import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { alertHref, renderAlerts, renderAlertsPanel } from "./ops-alerts.js";
import { alertsRequest, readFilters, writeFilters } from "./ops-filters.js";

const firing = {
  id: "freshness:gcp-go",
  severity: "warning",
  source: "data-freshness",
  backend: "gcp-go",
  message: "gcp-go serving data is stale",
  first_seen: "2026-11-18T20:00:00Z",
  last_seen: "2026-11-18T20:05:00Z",
  state: "firing",
};

test("alert filters combine backend, range, search, severity, and state", () => {
  const search = writeFilters("?tab=alerts", {
    backends: ["gcp-go"],
    range: "24h",
    q: "stale",
    severity: "warning",
    state: "firing",
  });
  const filters = readFilters(search);
  assert.deepEqual(filters.backends, ["gcp-go"]);
  assert.equal(filters.range, "24h");
  assert.equal(filters.q, "stale");
  assert.equal(filters.severity, "warning");
  assert.equal(filters.state, "firing");
  const query = alertsRequest(filters);
  assert.match(query, /\/api\/v1\/alerts\?/);
  assert.match(query, /backend=gcp-go/);
  assert.match(query, /from=24h/);
  assert.match(query, /q=stale/);
  assert.match(query, /severity=warning/);
  assert.match(query, /state=firing/);
});

test("alerts render firing rows, resolved rows, links, and the empty state", () => {
  assert.match(renderAlerts({ alerts: [], firing: 0 }), /No active alerts/);
  const html = renderAlerts({
    firing: 1,
    alerts: [firing, { ...firing, id: "health:gcp-go", source: "health", state: "resolved", severity: "critical", message: "gcp-go health check is failing" }],
  });
  assert.match(html, /serving data is stale/);
  assert.match(html, /class="firing severity-warning"/);
  assert.match(html, /class="resolved severity-critical"/);
  assert.match(html, /href="\?tab=health"/);
  assert.equal(alertHref({ source: "health" }), "?tab=ops");
  assert.equal(alertHref({ source: "audit" }), "?tab=ops#audit");
  assert.match(renderAlerts({ alerts: [{ ...firing, state: "resolved" }], firing: 0 }), /No active alerts/);
});

test("the alerts search box does not reuse the logs placeholder", () => {
  const filters = { backends: [], range: "15m", from: "", to: "", q: "", severity: "", state: "" };
  const html = renderAlertsPanel(filters);
  assert.match(html, /placeholder="Search alerts"/);
  assert.doesNotMatch(html, /placeholder="Search logs"/);
});

test("the local gateway sends /ops to the facade before the frontend catch-all", () => {
  const yaml = readFileSync(new URL("../deploy/overlays/local/app/gateway.yaml", import.meta.url), "utf8");
  const ops = yaml.indexOf("value: /ops");
  const root = yaml.indexOf("value: /\n");
  assert.ok(ops > 0, "missing /ops path");
  assert.ok(root > ops, "/ops must be matched before the frontend catch-all");
  const rule = yaml.slice(ops, root);
  assert.match(rule, /name: facade/);
});

test("the operations page adds the alerts tab beside logs", () => {
  const html = readFileSync(new URL("./hybrid.html", import.meta.url), "utf8");
  const js = readFileSync(new URL("./hybrid.js", import.meta.url), "utf8");
  assert.match(html, /data-tab="alerts"/);
  assert.match(html, /id="alert-count"/);
  assert.match(js, /ops-alerts\.js/);
  assert.match(js, /alertsRequest/);
});

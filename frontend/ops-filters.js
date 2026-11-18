export const FILTER_BACKENDS = ["gcp-go", "aws-python", "azure-dotnet", "facade"];
export const RANGE_PRESETS = ["15m", "1h", "24h", "7d", "custom"];

export function readFilters(search) {
  const params = new URLSearchParams(String(search || "").replace(/^\?/, ""));
  const range = params.get("range") || "15m";
  return {
    backends: params.getAll("backend").filter((name) => FILTER_BACKENDS.includes(name)),
    range: RANGE_PRESETS.includes(range) ? range : "15m",
    from: params.get("from") || "",
    to: params.get("to") || "",
    q: params.get("q") || "",
    level: params.get("level") || "",
    live: params.get("live") === "1",
    severity: params.get("severity") || "",
    state: params.get("state") || "",
  };
}

export function writeFilters(search, patch) {
  const params = new URLSearchParams(String(search || "").replace(/^\?/, ""));
  const current = readFilters(search);
  const next = { ...current, ...patch };
  params.delete("backend");
  for (const name of next.backends || []) params.append("backend", name);
  if (!next.range || next.range === "15m") params.delete("range");
  else params.set("range", next.range);
  if (next.range === "custom") {
    if (next.from) params.set("from", next.from);
    else params.delete("from");
    if (next.to) params.set("to", next.to);
    else params.delete("to");
  } else {
    params.delete("from");
    params.delete("to");
  }
  if (next.q) params.set("q", next.q);
  else params.delete("q");
  if (next.level) params.set("level", next.level);
  else params.delete("level");
  if (next.live) params.set("live", "1");
  else params.delete("live");
  if (next.severity) params.set("severity", next.severity);
  else params.delete("severity");
  if (next.state) params.set("state", next.state);
  else params.delete("state");
  const text = params.toString();
  return text ? `?${text}` : "";
}

export function alertsRequest(filters) {
  const params = new URLSearchParams();
  for (const name of filters.backends || []) params.append("backend", name);
  if (filters.range === "custom") {
    if (filters.from) params.set("from", filters.from);
    if (filters.to) params.set("to", filters.to);
  } else {
    params.set("from", filters.range || "15m");
  }
  if (filters.q) params.set("q", filters.q);
  if (filters.severity) params.set("severity", filters.severity);
  if (filters.state) params.set("state", filters.state);
  return `/api/v1/alerts?${params.toString()}`;
}

export function logsRequest(filters, cursor) {
  const params = new URLSearchParams();
  for (const name of filters.backends || []) params.append("backend", name);
  if (filters.range === "custom") {
    if (filters.from) params.set("from", filters.from);
    if (filters.to) params.set("to", filters.to);
  } else {
    params.set("from", filters.range || "15m");
  }
  if (filters.q) params.set("q", filters.q);
  if (filters.level) params.set("level", filters.level);
  if (cursor) params.set("cursor", cursor);
  params.set("limit", "50");
  return `/ops/logs?${params.toString()}`;
}

function esc(value) {
  return String(value ?? "").replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
}

export function renderFilterBar(filters, { level = false, live = false, extras = "" } = {}) {
  const selected = new Set(filters.backends || []);
  const checks = FILTER_BACKENDS.map((name) => `<label><input type="checkbox" name="backend" value="${esc(name)}" ${selected.has(name) ? "checked" : ""}> ${esc(name)}</label>`).join("");
  const presets = RANGE_PRESETS.map((name) => `<button type="button" data-range="${esc(name)}" aria-pressed="${filters.range === name ? "true" : "false"}">${esc(name)}</button>`).join("");
  const levelSelect = level ? `<label>Level <select name="level">
      <option value="" ${filters.level ? "" : "selected"}>any</option>
      ${["debug", "info", "warn", "error"].map((name) => `<option value="${name}" ${filters.level === name ? "selected" : ""}>${name}</option>`).join("")}
    </select></label>` : "";
  const liveBox = live ? `<label><input type="checkbox" name="live" ${filters.live ? "checked" : ""}> Live tail</label>` : "";
  return `<form class="ops-filters" data-range="${esc(filters.range)}">
    <fieldset><legend>Backend</legend>${checks}</fieldset>
    <div class="ranges" role="group" aria-label="Date range">${presets}</div>
    <label class="custom-range">From <input type="datetime-local" name="from" value="${esc(filters.from)}"></label>
    <label class="custom-range">To <input type="datetime-local" name="to" value="${esc(filters.to)}"></label>
    <label>Search <input type="search" name="q" value="${esc(filters.q)}" placeholder="Search logs"></label>
    ${levelSelect}
    ${liveBox}
    ${extras}
  </form>`;
}

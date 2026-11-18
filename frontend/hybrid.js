import { loadReports, renderDataHealth } from "./data-health.js";
import { logsRequest, readFilters, renderFilterBar, writeFilters } from "./ops-filters.js";
import { mergeEntries, renderLogs } from "./ops-logs.js";
import { OPS_TABS, searchWithTab, tabFromSearch } from "./ops-tabs.js";
import { applyTheme } from "./theme.js";

applyTheme({ theme: "industrial-dark", updown_palette: "green-red" });

const cards = document.querySelector("#cards");
const routes = document.querySelector("#routes");
const audit = document.querySelector("#audit");
const health = document.querySelector("#health");
const logsRoot = document.querySelector("#logs");
let logEntries = [];
let searchTimer = 0;

function esc(value) {
  return String(value ?? "").replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
}

function card(backend) {
  const weight = Number(backend.weight || 0);
  return `<article class="panel">
    <header class="panel-header">
      <h2><span class="dot ${backend.healthy ? "up" : ""}"></span>${esc(backend.name)}</h2>
      <span class="chip">${esc(backend.lang)} · ${esc(backend.color || "blue")}</span>
    </header>
    <div class="panel-body metric">
      <p>${esc(backend.runtime)} · ${esc(backend.capacity_tier || "free")}</p>
      <p>p95 ${Math.round(backend.p95_ms || 0)} ms · errors ${(Number(backend.error_rate || 0) * 100).toFixed(2)}%</p>
      <p>cost $${Number(backend.month_to_date_cost_usd || 0).toFixed(4)} · weight ${weight}</p>
      <div class="bar" aria-hidden="true"><span style="width:${weight}%"></span></div>
    </div>
  </article>`;
}

function render(state) {
  cards.innerHTML = (state.backends || []).map(card).join("");
  routes.innerHTML = `<table><thead><tr><th>Path</th><th>Policy</th><th>Reason</th><th>Weights</th></tr></thead><tbody>${
    (state.routes || []).map((route) => `<tr><td>${esc(route.path)}</td><td>${esc(route.policy)}</td><td>${esc(route.reason)}</td><td class="metric">${esc(JSON.stringify(route.weights || {}))}</td></tr>`).join("")
  }</tbody></table>`;
  const rows = (state.audit || []).slice(-12).reverse();
  audit.innerHTML = rows.length ? `<table><thead><tr><th>When</th><th>Backend</th><th>Reason</th><th>Status</th><th>ms</th></tr></thead><tbody>${
    rows.map((row) => `<tr><td class="metric">${esc(row.timestamp)}</td><td>${esc(row.backend_environment)} ${esc(row.backend_lang)}</td><td>${esc(row.decision_reason)}</td><td>${esc(row.upstream_status)}</td><td class="metric">${esc(row.latency_ms && row.latency_ms.total)}</td></tr>`).join("")
  }</tbody></table>` : `<p class="muted">No requests yet. Open the market dashboard or call /api/v1/status.</p>`;
}

function filtersFromForm(form) {
  const data = new FormData(form);
  return {
    backends: data.getAll("backend"),
    range: form.getAttribute("data-range") || "15m",
    from: data.get("from") || "",
    to: data.get("to") || "",
    q: data.get("q") || "",
    level: data.get("level") || "",
    live: Boolean(form.querySelector('[name="live"]')?.checked),
  };
}

function paintLogs() {
  if (!logsRoot) return;
  const filters = readFilters(location.search);
  logsRoot.innerHTML = `${renderFilterBar(filters, { level: true, live: true })}<div id="logs-results"></div>`;
  const form = logsRoot.querySelector("form");
  form.addEventListener("change", (event) => {
    if (event.target.name === "q") return;
    pushFilters(filtersFromForm(form));
  });
  form.querySelector('[name="q"]').addEventListener("input", (event) => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => pushFilters({ ...filtersFromForm(form), q: event.target.value }), 300);
  });
  form.querySelectorAll("[data-range]").forEach((button) => {
    button.addEventListener("click", () => {
      pushFilters({ ...filtersFromForm(form), range: button.getAttribute("data-range") });
    });
  });
  logsRoot.querySelector("#logs-results").addEventListener("click", (event) => {
    const more = event.target.closest("#logs-more");
    if (more) loadLogs(more.getAttribute("data-cursor"));
  });
}

function pushFilters(patch) {
  const next = writeFilters(location.search, patch);
  history.replaceState(null, "", `${location.pathname}${next}`);
  logEntries = [];
  paintLogs();
  loadLogs();
}

async function loadLogs(cursor) {
  const results = document.querySelector("#logs-results");
  if (!results) return;
  try {
    const response = await fetch(logsRequest(readFilters(location.search), cursor || ""));
    if (!response.ok) throw new Error(String(response.status));
    const page = await response.json();
    logEntries = cursor ? mergeEntries(logEntries, page.entries || []) : (page.entries || []);
    results.innerHTML = renderLogs({ ...page, entries: logEntries });
  } catch (error) {
    results.innerHTML = `<p class="muted">Logs are not reachable (${esc(error.message)}).</p>`;
  }
}

function showTab(tab) {
  const selected = OPS_TABS.includes(tab) ? tab : "ops";
  for (const name of OPS_TABS) {
    const panel = document.querySelector(`#panel-${name}`);
    const button = document.querySelector(`[data-tab="${name}"]`);
    if (panel) panel.hidden = name !== selected;
    if (button) button.setAttribute("aria-selected", name === selected ? "true" : "false");
  }
  if (selected === "logs" && logsRoot && !logsRoot.querySelector("form")) {
    paintLogs();
    loadLogs();
  }
}

document.querySelectorAll("[data-tab]").forEach((button) => {
  button.addEventListener("click", () => {
    const tab = button.getAttribute("data-tab");
    const next = searchWithTab(location.search, tab);
    history.replaceState(null, "", `${location.pathname}${next}`);
    showTab(tab);
  });
});
showTab(tabFromSearch(location.search));

async function tick() {
  try {
    const response = await fetch("/api/v1/facade/state");
    if (!response.ok) throw new Error(String(response.status));
    const state = await response.json();
    render(state);
    try {
      health.innerHTML = renderDataHealth(await loadReports(state.backends || []));
    } catch (error) {
      health.innerHTML = `<p class="muted">Data health is not reachable (${esc(error.message)}).</p>`;
    }
    if (tabFromSearch(location.search) === "logs" && readFilters(location.search).live) loadLogs();
  } catch (error) {
    cards.innerHTML = `<p class="muted">Facade state is not reachable (${esc(error.message)}). Start it with <span class="metric">make demo</span>.</p>`;
    health.innerHTML = `<p class="muted">Data health needs the facade (${esc(error.message)}).</p>`;
  }
}

tick();
setInterval(tick, 2000);
fetch("/api/v1/status").catch(() => {});

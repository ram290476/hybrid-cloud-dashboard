import { applyTheme } from "./theme.js";

applyTheme({ theme: "industrial-dark", updown_palette: "green-red" });

const cards = document.querySelector("#cards");
const routes = document.querySelector("#routes");
const audit = document.querySelector("#audit");

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

async function tick() {
  try {
    const response = await fetch("/api/v1/facade/state");
    if (!response.ok) throw new Error(String(response.status));
    render(await response.json());
  } catch (error) {
    cards.innerHTML = `<p class="muted">Facade state is not reachable (${esc(error.message)}). Start it with <span class="metric">make demo</span>.</p>`;
  }
}

tick();
setInterval(tick, 2000);
fetch("/api/v1/status").catch(() => {});

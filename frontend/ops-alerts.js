import { alertsRequest, renderFilterBar } from "./ops-filters.js";

function esc(value) {
  return String(value ?? "").replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
}

export function alertHref(alert) {
  if (alert.source === "audit" || alert.source === "failover") return "?tab=ops#audit";
  if (alert.source === "health") return "?tab=ops";
  return "?tab=health";
}

function option(value, label, current) {
  return `<option value="${esc(value)}"${current === value ? " selected" : ""}>${esc(label)}</option>`;
}

export function alertExtras(filters) {
  return `<label>Severity <select name="severity">${option("", "any", filters.severity || "")}${["critical", "warning", "info"].map((name) => option(name, name, filters.severity)).join("")}</select></label>
    <label>State <select name="state">${option("", "any", filters.state || "")}${["firing", "resolved"].map((name) => option(name, name, filters.state)).join("")}</select></label>`;
}

export function renderAlerts(page) {
  const alerts = page?.alerts || [];
  const firing = alerts.filter((alert) => alert.state === "firing");
  const rows = alerts.map((alert) => `<tr class="${alert.state === "resolved" ? "resolved" : "firing"} severity-${esc(alert.severity)}">
      <td><span class="badge severity-${esc(alert.severity)}">${esc(alert.severity)}</span></td>
      <td>${esc(alert.state)}</td>
      <td>${esc(alert.backend)}</td>
      <td>${esc(alert.source)}</td>
      <td>${esc(alert.message)}</td>
      <td class="metric">${esc(alert.last_seen)}</td>
      <td><a href="${esc(alertHref(alert))}">Open</a></td>
    </tr>`).join("");
  const empty = firing.length ? "" : `<p class="muted empty-alerts">No active alerts</p>`;
  const table = alerts.length
    ? `<table class="alerts"><thead><tr><th>Severity</th><th>State</th><th>Backend</th><th>Source</th><th>Message</th><th>Last seen</th><th></th></tr></thead><tbody>${rows}</tbody></table>`
    : "";
  return `${empty}${table}`;
}

export function renderAlertsPanel(filters) {
  return `${renderFilterBar(filters, { extras: alertExtras(filters), placeholder: "Search alerts" })}<div id="alerts-results"></div>`;
}

export { alertsRequest };

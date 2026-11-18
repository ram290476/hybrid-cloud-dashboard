import { logsRequest, renderFilterBar } from "./ops-filters.js";

const BACKEND_CLASS = {
  "gcp-go": "log-gcp",
  "aws-python": "log-aws",
  "azure-dotnet": "log-azure",
  facade: "log-facade",
};

function esc(value) {
  return String(value ?? "").replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
}

export function traceHref(traceId, grafana = "http://127.0.0.1:3000") {
  if (!traceId) return "";
  const panes = JSON.stringify({
    trace: { datasource: "tempo", queries: [{ query: traceId, queryType: "traceql" }] },
  });
  return `${grafana}/explore?schemaVersion=1&panes=${encodeURIComponent(panes)}`;
}

export function mergeEntries(current, next) {
  const seen = new Set((current || []).map(keyOf));
  return (current || []).concat((next || []).filter((entry) => !seen.has(keyOf(entry))));
}

function keyOf(entry) {
  return `${entry.timestamp}|${entry.trace_id}|${entry.backend}|${entry.route}|${entry.status}`;
}

export function renderLogs(page) {
  const entries = page?.entries || [];
  const warning = page?.warning ? `<p class="health-alert">${esc(page.warning)}</p>` : "";
  if (!entries.length) {
    return `${warning}<p class="muted empty-logs">No log lines in this window.</p>`;
  }
  const rows = entries.map((entry) => {
    const level = String(entry.level || "info").toLowerCase();
    const href = traceHref(entry.trace_id);
    const trace = href ? `<a href="${esc(href)}">${esc(entry.trace_id)}</a>` : esc(entry.trace_id);
    return `<tr class="${BACKEND_CLASS[entry.backend] || ""} level-${esc(level)}">
      <td class="metric">${esc(entry.timestamp)}</td>
      <td>${esc(entry.backend)}</td>
      <td><span class="badge level-${esc(level)}">${esc(level)}</span></td>
      <td class="metric">${esc(entry.status)}</td>
      <td>${esc(entry.route)}</td>
      <td>${esc(entry.message)}</td>
      <td class="metric">${trace}</td>
      <td><details><summary>JSON</summary><pre>${esc(JSON.stringify(entry, null, 2))}</pre></details></td>
    </tr>`;
  }).join("");
  const more = page?.next_cursor
    ? `<button type="button" id="logs-more" data-cursor="${esc(page.next_cursor)}">Load more</button>`
    : "";
  return `${warning}<table class="logs"><thead><tr><th>When</th><th>Backend</th><th>Level</th><th>Status</th><th>Route</th><th>Message</th><th>Trace</th><th></th></tr></thead><tbody>${rows}</tbody></table>${more}`;
}

export function renderLogsPanel(filters) {
  return `${renderFilterBar(filters, { level: true, live: true })}<div id="logs-results"></div>`;
}

export { logsRequest };

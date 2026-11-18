export const FRESH_WITHIN_SECONDS = 129600;

const COUNT_KEYS = ["chart", "stock", "funds"];

export function cloudOf(backend) {
  const name = String(backend || "");
  if (name.startsWith("gcp")) return "gcp";
  if (name.startsWith("aws")) return "aws";
  if (name.startsWith("azure")) return "azure";
  return "unknown";
}

export function missingReport(name, message) {
  const file = (key) => ({ key, present: false, generated_at: null, last_modified: null, age_seconds: null });
  return {
    backend: name || "",
    lang: "",
    runtime: "",
    color: "",
    version: "",
    data_version: "",
    cloud: cloudOf(name),
    checked_at: null,
    freshness: "missing",
    fresh_within_seconds: FRESH_WITHIN_SECONDS,
    counts: { chart: 0, stock: 0, funds: 0 },
    files: {
      dashboard: file("serving/dashboard.json"),
      status: file("serving/status.json"),
    },
    tickers: [],
    newest_at: null,
    oldest_at: null,
    collectors: [],
    import: null,
    warnings: [message || "backend unreachable"],
  };
}

export async function loadReports(backends, fetchImpl = globalThis.fetch) {
  return Promise.all((backends || []).map(async (backend) => {
    const name = backend?.name || "";
    try {
      const response = await fetchImpl(`/api/v1/facade/data-health/${encodeURIComponent(name)}`);
      if (!response.ok) throw new Error(String(response.status));
      const report = await response.json();
      return { ...missingReport(name), ...report, endpoint: backend.endpoint || "" };
    } catch (error) {
      return { ...missingReport(name, error.message), endpoint: backend.endpoint || "" };
    }
  }));
}

export function disagreements(reports) {
  if (!reports || reports.length < 2) return [];
  const notes = [];
  const values = (pick) => [...new Set(reports.map(pick))];
  if (values((report) => report.files?.dashboard?.generated_at ?? null).length > 1) notes.push("dashboard generated_at");
  if (values((report) => report.files?.status?.generated_at ?? null).length > 1) notes.push("status generated_at");
  for (const key of COUNT_KEYS) {
    if (values((report) => report.counts?.[key] ?? null).length > 1) notes.push(`${key} count`);
  }
  return notes;
}

export function collectorClass(status) {
  const value = String(status || "").toLowerCase();
  if (value === "ok" || value === "success" || value === "ready") return "ok";
  if (value === "error" || value === "failed" || value === "fail") return "bad";
  return "warn";
}

export function formatAge(seconds) {
  if (seconds == null || !Number.isFinite(Number(seconds))) return "unknown";
  const value = Math.max(0, Number(seconds));
  if (value < 60) return `${Math.round(value)}s`;
  if (value < 3600) return `${Math.floor(value / 60)}m`;
  if (value < 86400) return `${Math.floor(value / 3600)}h`;
  return `${Math.floor(value / 86400)}d`;
}

function esc(value) {
  return String(value ?? "").replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
}

function fileRow(label, file, disagree) {
  const when = file?.generated_at || file?.last_modified || "missing";
  const age = file?.present ? formatAge(file.age_seconds) : "missing";
  const klass = disagree ? "health-row disagree" : "health-row";
  return `<p class="${klass}"><span>${esc(label)}</span><span class="metric">${esc(when)} · ${esc(age)}</span></p>`;
}

export function renderDataHealth(reports) {
  const rows = reports || [];
  if (!rows.length) return `<p class="muted">No backends to compare.</p>`;
  const notes = disagreements(rows);
  const banner = notes.length
    ? `<p class="health-alert">The backends disagree on ${esc(notes.join(" and "))}.</p>`
    : "";
  const cards = rows.map((report) => {
    const notesFor = new Set(notes);
    const counts = COUNT_KEYS.map((key) => {
      const klass = notesFor.has(`${key} count`) ? "disagree" : "";
      return `<span class="${klass}">${esc(key)} ${esc(report.counts?.[key] ?? 0)}</span>`;
    }).join(" · ");
    const jobs = (report.collectors || []).map((job) => `<li><span>${esc(job.name)}</span> <span class="job ${collectorClass(job.status)}">${esc(job.status || "unknown")}</span> <span class="metric">${esc(formatAge(job.age_seconds))}</span></li>`).join("");
    const imported = report.import
      ? `${esc(report.import.source || "unknown")}${report.import.origin ? ` · ${esc(report.import.origin)}` : ""} · <span class="metric">${esc(report.import.at || "unknown")} · ${esc(formatAge(report.import.age_seconds))}</span>`
      : "not recorded";
    const warnings = (report.warnings || []).map((warning) => `<li>${esc(warning)}</li>`).join("");
    const freshness = ["fresh", "stale", "missing"].includes(report.freshness) ? report.freshness : "missing";
    return `<article class="panel" data-backend="${esc(report.backend)}" data-freshness="${esc(freshness)}">
      <header class="panel-header">
        <h2>${esc(report.backend || "backend")}</h2>
        <span class="badge ${esc(freshness)}">${esc(freshness)}</span>
      </header>
      <div class="panel-body">
        <p class="muted">${esc(report.cloud || cloudOf(report.backend))} · ${esc(report.lang)} · ${esc(report.runtime)}</p>
        <p class="metric">${counts}</p>
        ${fileRow("dashboard", report.files?.dashboard, notesFor.has("dashboard generated_at"))}
        ${fileRow("status", report.files?.status, notesFor.has("status generated_at"))}
        <p class="health-row"><span>tickers</span><span>${esc((report.tickers || []).join(", ") || "none")}</span></p>
        <p class="health-row"><span>oldest</span><span class="metric">${esc(report.oldest_at || "—")}</span></p>
        <p class="health-row"><span>newest</span><span class="metric">${esc(report.newest_at || "—")}</span></p>
        <p class="health-row"><span>import</span><span>${imported}</span></p>
        ${report.endpoint ? `<p class="muted metric">${esc(report.endpoint)}</p>` : ""}
        <ul class="collectors">${jobs || `<li class="muted">No collectors</li>`}</ul>
        ${warnings ? `<ul class="warnings">${warnings}</ul>` : ""}
      </div>
    </article>`;
  }).join("");
  return `${banner}<div class="ops-grid">${cards}</div>`;
}

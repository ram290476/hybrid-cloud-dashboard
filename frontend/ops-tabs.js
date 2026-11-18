export const OPS_TABS = ["ops", "health"];

export function tabFromSearch(search) {
  const params = new URLSearchParams(String(search || "").replace(/^\?/, ""));
  const tab = params.get("tab");
  return OPS_TABS.includes(tab) ? tab : "ops";
}

export function searchWithTab(search, tab) {
  const params = new URLSearchParams(String(search || "").replace(/^\?/, ""));
  if (!tab || tab === "ops") params.delete("tab");
  else params.set("tab", tab);
  const text = params.toString();
  return text ? `?${text}` : "";
}

// A module script and fetch are cross-origin from a page without an origin; the preview's folder allows both.
const r = await fetch("data/info.json");
document.body.dataset.fetch = (await r.json()).ok ? "ok" : "bad";
// Outside the folder is closed, even on Fyr's own host.
try {
  await fetch("/api/state");
  document.body.dataset.api = "open";
} catch {
  document.body.dataset.api = "closed";
}
parent.postMessage({ jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { height: 300 } }, "*");

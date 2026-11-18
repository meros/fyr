// Phone layout invariants (SPEC 0.5 V0.17), checked in headless Chrome over CDP against web/dist in
// mock mode. Run `npm run build` first; `npm run test:layout` (and `make test`) run this file.
//
// For every phone screen and the sizes 360x640, 390x844 and 430x932, with emulated safe-area insets
// (top 47, bottom 34):
//   1. at rest: the header is below the top inset; the bottom bar, the composer, the sticky commit
//      button and any sheet's buttons are inside the viewport and above the bottom inset;
//   2. keyboard: the visual viewport shrinks to 60 % (iOS keeps the layout viewport and scrolls it,
//      so the fake visualViewport also gets an offsetTop); the focused field and the composer or
//      commit button are inside the visible area;
//   3. resume: the page is hidden, the size changes without any resize event (as after the app
//      returns from the background), then it is visible again; the invariants of 1 hold.
//
// Insets: Emulation.setSafeAreaInsetsOverride (Chrome 136 or later). Keyboard and resume: a scripted
// window.visualViewport (an EventTarget whose height and offsetTop the test sets), installed before
// the app runs, because CDP cannot shrink only the visual viewport.
//
// Speed: the three sizes run at the same time, one tab each (each in its own browser context, so their
// localStorage is apart); the desktop and timing checks run after them, alone. No step sleeps a fixed time to
// let the page catch up: each waits for the state it needs (waitFor polls a page expression, settle waits for a
// DOM without changes and without running animations). The few fixed waits left measure time on purpose.

import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, extname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dist = join(dirname(fileURLToPath(import.meta.url)), "..", "dist");
if (!existsSync(join(dist, "index.html"))) {
  console.error("layout test: web/dist is not built; run npm run build first");
  process.exit(1);
}

const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".woff2": "font/woff2", ".png": "image/png", ".json": "application/json", ".webmanifest": "application/manifest+json" };
// V0.47: the mock folder preview, served as the host serves one: its files under /api/previews/pv_site/, with the
// host's CSP (TestPreviewCSP keeps preview-csp.txt equal to it) and Access-Control-Allow-Origin.
const fixtures = join(dirname(fileURLToPath(import.meta.url)), "fixtures");
const site = join(fixtures, "preview-site");
const [cspTemplate, cdns] = readFileSync(join(fixtures, "preview-csp.txt"), "utf8").trim().split("\n");
const server = createServer((req, res) => {
  const path = decodeURIComponent(new URL(req.url, "http://x").pathname);
  if (path.startsWith("/api/previews/pv_site/")) {
    const file = join(site, path.slice("/api/previews/pv_site/".length));
    if (!file.startsWith(site + "/") || !existsSync(file) || statSync(file).isDirectory()) {
      res.writeHead(404).end();
      return;
    }
    const csp = cspTemplate.replaceAll("{base}", `http://${req.headers.host}/api/previews/pv_site/`).replaceAll("{cdn}", cdns);
    res.writeHead(200, { "Content-Type": TYPES[extname(file)] ?? "application/octet-stream", "Content-Security-Policy": csp, "Access-Control-Allow-Origin": "*", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store" });
    res.end(readFileSync(file));
    return;
  }
  // V0.116: project image icons, as the host serves them: a fixture per project id (FYR_PROJECT_ICONS first, so
  // screenshots can use icons that are not committed); none is a 404 and the client draws the glyph.
  const icon = path.match(/^\/api\/h\/[A-Za-z0-9-]+\/projects\/([A-Za-z0-9_-]+)\/icon$/); // decision 0031: under the host's prefix
  if (icon) {
    const f = [process.env.FYR_PROJECT_ICONS, join(fixtures, "project-icons")].filter(Boolean).map((d) => join(d, `${icon[1]}.png`)).find((x) => existsSync(x));
    if (!f) return void res.writeHead(404).end();
    res.writeHead(200, { "Content-Type": "image/png", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store" });
    return void res.end(readFileSync(f));
  }
  let file = join(dist, path === "/" ? "index.html" : path);
  if (!file.startsWith(dist) || !existsSync(file) || statSync(file).isDirectory()) file = join(dist, "index.html");
  res.writeHead(200, { "Content-Type": TYPES[extname(file)] ?? "application/octet-stream", "Cache-Control": "no-store" });
  res.end(readFileSync(file));
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = `http://127.0.0.1:${server.address().port}`;

// macOS has no google-chrome on PATH; use the app bundle's binary.
const macChrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const chromeBin = process.env.FYR_CHROME || (process.platform === "darwin" && existsSync(macChrome) ? macChrome : "google-chrome");
const profile = mkdtempSync(join(tmpdir(), "fyr-layout-"));
// The files a screen attaches (s.files), written once: tabs that run at the same time read them.
writeFileSync(join(profile, "shot.png"), Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64"));
writeFileSync(join(profile, "build-output-with-a-very-long-name-that-does-not-fit.log"), "log line\n".repeat(200));
const port = 9400 + Math.floor(Math.random() * 400);
const chrome = spawn(chromeBin, ["--headless=new", "--disable-gpu", "--enable-unsafe-swiftshader", "--no-sandbox", "--hide-scrollbars", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore", detached: true });
const cleanup = () => {
  try {
    process.kill(-chrome.pid, "SIGKILL"); // the whole group: the wrapper script leaves Chrome's children otherwise
  } catch {}
  server.close();
  rmSync(profile, { recursive: true, force: true });
};
const watchdog = setTimeout(() => {
  console.error("layout test: timed out");
  cleanup();
  process.exit(2);
}, 420_000); // a hang guard, not a speed check: about 35 s on a quiet 8-core machine, 140 s and more at a load average of 80 (a deploy beside other sessions' test suites, 16 Nov)

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// Chrome's debug port: up to 60 s, since make test starts Chrome beside the Go tests, the typecheck and the unit tests
// (CI, 16 Nov: not up after 10 s on a busy runner).
let version;
let chromeExit = null;
chrome.on("exit", (code, signal) => (chromeExit = `exit ${code ?? signal}`));
for (const t0 = Date.now(); Date.now() - t0 < 60_000 && !chromeExit; ) {
  try {
    version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
    if (version.webSocketDebuggerUrl) break;
  } catch {}
  await sleep(100);
}
if (!version?.webSocketDebuggerUrl) {
  console.error(`layout test: Chrome (${chromeBin}) did not open its debug port ${port}${chromeExit ? ` (${chromeExit})` : " within 60 s"}`);
  cleanup();
  process.exit(1);
}
// One browser connection; each tab is a flat session on it (Target.attachToTarget with flatten), so the
// sandboxed frames a tab auto-attaches come over the same socket.
const ws = new WebSocket(version.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let seq = 0;
const pending = new Map();
const tabsBySession = new Map();
const pageErrors = [];
ws.addEventListener("message", (m) => {
  const j = JSON.parse(m.data);
  if (j.id && pending.has(j.id)) {
    pending.get(j.id)(j);
    pending.delete(j.id);
  }
  const tab = j.method && tabsBySession.get(j.sessionId);
  if (tab) tab.event(j);
});
const sendRaw = (method, params = {}, sessionId) =>
  new Promise((r) => {
    const i = ++seq;
    pending.set(i, r);
    ws.send(JSON.stringify({ id: i, method, params, ...(sessionId ? { sessionId } : {}) }));
  });

const INSETS = { top: 47, bottom: 34, left: 0, right: 0 };

// A tab in its own browser context (its own localStorage, so tabs that run at the same time do not see each
// other's projectFilter or drafts) and its own window (so it is a visible page that draws frames).
async function newTab() {
  const ctx = await sendRaw("Target.createBrowserContext", { disposeOnDetach: true });
  const target = await sendRaw("Target.createTarget", { url: "about:blank", browserContextId: ctx.result.browserContextId, newWindow: true });
  const att = await sendRaw("Target.attachToTarget", { targetId: target.result.targetId, flatten: true });
  const sessionId = att.result.sessionId;
  const waiters = [];
  const tab = {
    targetId: target.result.targetId,
    browserContextId: ctx.result.browserContextId,
    sessionId,
    attached: [], // child targets (the sandboxed live view runs out of process)
    send: (method, params = {}, sid = sessionId) => sendRaw(method, params, sid),
    // The next event of this name on this tab.
    next(method, timeout) {
      return new Promise((ok, fail) => {
        const w = { method, ok, timer: setTimeout(() => (waiters.splice(waiters.indexOf(w), 1), fail(new Error(`no ${method} within ${timeout} ms`))), timeout) };
        waiters.push(w);
      });
    },
    event(j) {
      if (j.method === "Target.attachedToTarget") tab.attached.push(j.params);
      if (j.method === "Runtime.exceptionThrown") pageErrors.push(j.params.exceptionDetails?.exception?.description ?? j.params.exceptionDetails?.text);
      for (const w of waiters.filter((x) => x.method === j.method)) {
        clearTimeout(w.timer);
        waiters.splice(waiters.indexOf(w), 1);
        w.ok(j.params);
      }
    },
  };
  tabsBySession.set(sessionId, tab);
  await tab.send("Page.enable");
  await tab.send("Runtime.enable");
  // The tab behaves as the focused window although several windows are open.
  await tab.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  // A scriptable visualViewport, before any page script runs.
  await tab.send("Page.addScriptToEvaluateOnNewDocument", {
    source: `(() => {
    const vv = new EventTarget();
    window.__vv = { h: null, top: 0 };
    for (const [k, get] of Object.entries({
      height: () => window.__vv.h ?? window.innerHeight,
      width: () => window.innerWidth,
      offsetTop: () => window.__vv.top,
      offsetLeft: () => 0, pageTop: () => window.__vv.top, pageLeft: () => 0, scale: () => 1,
    })) Object.defineProperty(vv, k, { get });
    Object.defineProperty(window, "visualViewport", { value: vv, configurable: true });
    let hidden = false;
    Object.defineProperty(document, "visibilityState", { get: () => (hidden ? "hidden" : "visible"), configurable: true });
    Object.defineProperty(document, "hidden", { get: () => hidden, configurable: true });
    window.__setHidden = (h) => { hidden = h; document.dispatchEvent(new Event("visibilitychange")); };
  })();`,
  });
  // env(safe-area-inset-*) is always defined in a browser (0 without a notch), so CSS variables cannot
  // stand in for it; this Chrome must support the override.
  const trySafeArea = await tab.send("Emulation.setSafeAreaInsetsOverride", { insets: INSETS });
  if (trySafeArea.error) {
    console.error(`layout test: this Chrome cannot emulate safe-area insets (${trySafeArea.error.message}); use Chrome 136 or later`);
    cleanup();
    process.exit(1);
  }
  return tab;
}

const SIZES = [
  [360, 640],
  [390, 844],
  [430, 932],
];

// The PR tab has its detail from GitHub (the mock answers after 120 ms), not only the thread's own PR line.
const PR_LOADED = "!!document.querySelector('.prv .prv-st') && !document.querySelector('.prv .prv-empty')";
const SCREENS = [
  { name: "inbox", url: "?mock=rail" },
  { name: "update banner (V0.39)", url: "?mock=rail&deploy=drain" },
  { name: "thread", url: "?mock=rail&open=t_mapperf", field: ".m-cmp .in" },
  { name: "project screen", url: "?mock=rail&project=p_web" },
  { name: "inbox filtered by project", url: "?mock=rail", storage: { "fyr.projectFilter": "p_web" } },
  { name: "opened tool group", url: "?mock=rail&open=t_mapperf", field: ".m-cmp .in", openTools: true },
  { name: "thread with previews", url: "?mock=1&open=t_w3", field: ".m-cmp .in", previews: true },
  { name: "composer with chips", url: "?mock=rail&open=t_mapperf", field: ".m-cmp .in", files: ".m-cmp input[data-picker=files]" },
  { name: "question Q1", url: "?mock=rail&open=t_lasso", openOther: true },
  { name: "approval", url: "?mock=1&open=t_approval" },
  { name: "effect card (V0.27)", url: "?mock=1&open=t_update" },
  { name: "effect", url: "?mock=1&open=t_effect" },
  { name: "new thread", url: "?mock=rail&new", field: ".m-ta" },
  { name: "snooze sheet", url: "?mock=rail&open=t_mapperf", sheet: "snooze" },
  { name: "action sheet", url: "?mock=rail", sheet: "actions" },
  // SPEC 0.5 V0.55: a thread without a project; its details have no Make project button.
  { name: "details without a project", url: "?mock=1&open=t_s4", sheet: "details" },
  // SPEC 0.5 V0.30: the project picker from the header chip, the one-step New project, and a new thread whose
  // project has no folder here (the folder field shows and is required).
  { name: "project picker", url: "?mock=1&open=t_s4", sheet: "project", field: ".m-sheet .pq" },
  { name: "new project step", url: "?mock=1&open=t_s4", sheet: "newproject", field: ".m-sheet .pform input" },
  // SPEC 0.5 V0.42: a project without a folder starts in a fresh scratch folder; "Change" opens the folder picker
  // sheet. SPEC 0.5 V0.55: the project screen shows its one folder with Change….
  { name: "new thread, project without a folder", url: "?mock=rail&new", storage: { "fyr.lastProject": "p_home", "fyr.projectFilter": "" }, field: ".m-ta" },
  { name: "folder picker sheet", url: "?mock=rail&new", storage: { "fyr.lastProject": "p_web", "fyr.projectFilter": "" }, sheet: "folder", field: ".fsheet .pq" },
  { name: "project screen with its folder", url: "?mock=rail&project=p_web" },
  // SPEC 0.5 V0.37: a thread with a pinned live view; the composer stays inside the safe areas.
  { name: "thread with a live view", url: "?mock=1&open=t_view", field: ".m-cmp .in" },
  // SPEC 0.5 V0.25: the Changes tab with one file's diff open.
  { name: "changes with a diff", url: "?mock=rail&open=t_mapperf&tab=changes", openDiff: true },
  // SPEC 0.5 V0.66: a failing PR's CI annotations: a count on the file rows, notes in the opened file's diff; a live change.
  { name: "changes with CI annotations", url: "?mock=1&open=t_done&tab=changes", openDiff: "web/src/map/render.ts", wholeBranch: true },
  // SPEC 0.5 V0.65: New since you looked and Whole branch, the agent's notes, Viewed checkboxes; every target 44 px.
  { name: "changes since you looked", url: "?mock=1&open=t_done&tab=changes", openDiff: true },
  // SPEC 0.5 V0.43: the PR tab with a failing check and 2 unresolved review threads, and the Merge dialog.
  { name: "PR tab with a failing check", url: "?mock=1&open=t_block&tab=pr", prTab: true, ready: PR_LOADED },
  { name: "PR merge dialog", url: "?mock=1&open=t_s2&tab=pr", sheet: "merge", ready: PR_LOADED },
  // SPEC 0.5 V0.46: quiet hours. ?quiet=p_home puts Garden in quiet hours: its thread leaves the inbox, comes back
  // with the project filter (whose line says until when), and the project screen's Quiet hours row fits the phone.
  { name: "inbox with a quiet project", url: "?mock=rail&quiet=p_home", storage: { "fyr.projectFilter": "" } },
  // SPEC 0.5 V0.114: the chip alone no longer opens it: the gate (until HH:MM, the 30 s hold) shows, not the thread;
  // after the hold (the unlock in browser storage) the chip shows it; a link (?open=) to its thread shows the gate.
  { name: "quiet project through the filter", url: "?mock=rail&quiet=p_home", storage: { "fyr.projectFilter": "p_home", "fyr.quietShowUntil": "" } },
  { name: "quiet project unlocked through the filter", url: "?mock=rail&quiet=p_home", storage: { "fyr.projectFilter": "p_home", "fyr.quietShowUntil": "9999999999999" } },
  { name: "link to a quiet thread", url: "?mock=rail&quiet=p_home&open=t_dish", storage: { "fyr.projectFilter": "", "fyr.quietShowUntil": "" } },
  { name: "project screen with quiet hours", url: "?mock=rail&project=p_home&quiet=p_home", storage: { "fyr.projectFilter": "" } },
  // SPEC 0.5 V0.45: the handover sheet from the thread's ⋯; its buttons stay above the bottom inset.
  { name: "handover sheet", url: "?mock=rail&open=t_triage", sheet: "handover" },
  // SPEC V0.119 (amends V0.50): Settings is the nav and four pages, with a provider, Add provider and a project as
  // sub-pages. On every one: no control under 44 px, nothing past the right edge, the last control above the bottom
  // inset; with the keyboard up a focused field and its button stay in view.
  { name: "settings nav (V0.119)", url: "?mock=rail&settings", ready: "document.querySelectorAll('.m-settings .stn-s').length === 6 && [...document.querySelectorAll('.m-settings .stn-s')].every((e) => e.textContent)" },
  { name: "settings providers", url: "?mock=rail&settings=providers", ready: "document.querySelectorAll('.m-settings .prow').length === 3 && document.querySelectorAll('section[aria-label=\"System agents\"] select').length === 4" },
  { name: "settings provider login (V0.119)", url: "?mock=rail&settings=providers/work", ready: "!!document.querySelector('.m-settings .login a.loginlink')" },
  { name: "settings provider (V0.119)", url: "?mock=rail&settings=providers/claude", ready: "document.querySelectorAll('.m-settings .model').length === 3", field: ".m-settings .fieldrow .inl input" },
  { name: "settings provider without a key (V0.119)", url: "?mock=rail&providers=nokey&settings=providers/claudeDeepseek", ready: "!!document.querySelector('.m-settings .keyrow input')", field: ".m-settings .keyrow .inl input" },
  { name: "settings add provider (V0.119)", url: "?mock=rail&settings=providers/new", ready: "!!document.querySelector('.m-settings [role=radiogroup][aria-label=Kind]')" },
  { name: "settings automation (V0.119)", url: "?mock=rail&settings=automation", ready: "!!document.querySelector('[aria-label=\"Auto-update Fyr\"]') && document.querySelectorAll('section[aria-label=\"Updates\"] .sr').length >= 3 && document.querySelectorAll('section[aria-label=\"Heavy jobs\"] .sr').length === 4 && document.querySelectorAll('section[aria-label=\"Heavy jobs\"] button').length === 3", field: ".m-settings .fieldrow .inl input" },
  { name: "settings projects (V0.119)", url: "?mock=rail&settings=projects", ready: "document.querySelectorAll('.m-settings .pli').length > 3" },
  { name: "settings device (V0.119)", url: "?mock=rail&settings=device", ready: "!!document.querySelector('.m-settings [role=switch][aria-label=Notifications]')" },
  // V0.125: the Hosts page and a host's page with a changed id, at every phone size.
  { name: "settings signed-in devices (V0.157)", url: "?mock=rail&settings=devices", ready: "document.querySelectorAll('.m-settings .dev').length === 4 && !!document.querySelector('.m-settings input[aria-label=\"Approval code\"]')" },
  { name: "settings hosts (V0.125)", url: "?mock=rail&hosts=2&settings=hosts", ready: "document.querySelectorAll('.m-settings .sr.host').length === 4" },
  { name: "settings paired installs (V0.159)", url: "?mock=rail&hosts=2&settings=hosts", ready: "document.querySelectorAll('.m-settings .peers .peerrow').length >= 3 && !!document.querySelector('.m-settings .peers input[aria-label=\"Invite from another install\"]')" },
  { name: "settings host changed (V0.125)", url: "?mock=rail&hosts=2&settings=hosts/lab", ready: "!!document.querySelector('.m-settings .note.bad')" },
  { name: "board of changes", url: "?mock=1&changes" },
  // V0.116: a project with an image icon the setup agent set: Set by agent, the image buttons 44 px.
  { name: "project screen with an image icon (V0.116)", url: "?mock=rail&project=p_fyr", field: ".pedit .pname .inl input" },
  // V0.101: the inbox row of a thread in Needs you carries the beam and its button; the Changes tab shows the ask
  // inline with its answer and Not now, Ready with Merge bound to the head, and Refresh now.
  { name: "inbox with a ready change (V0.101)", url: "?mock=1" },
  { name: "thread Changes tab with the causes inline (V0.101)", url: "?mock=1&open=t_done&tab=changes" },
  // V0.73 (2): six change cards (five ended) above a git checkout with no diff; the diff section flows after the cards.
  { name: "ended changes over an empty diff", url: "?mock=1&open=t_ended&tab=changes", ready: "!!document.querySelector('.chtab .chg-empty')" },
  // SPEC 0.5 V0.68: Fyr's own confirmation sheets: a script in a scratch folder, and Delete project (red).
  { name: "confirm sheet for a script", url: "?mock=1&open=t_done", sheet: "confirm" },
  { name: "confirm sheet for Delete project", url: "?mock=rail&project=p_web", sheet: "confirm-delete" },
  // V0.54: a front with two hosts, one offline: rows name the host; the offline thread says so under its chips.
  { name: "inbox with two hosts", url: "?mock=rail&hosts=2" },
  { name: "thread on an offline host", url: "?mock=rail&hosts=2&open=t_triage", field: ".m-cmp .in" },
];

// V0.73 (2): the Changes tab's cards and the diff section below them. A card's box is cut to the list when the list
// scrolls by itself (the desktop); every card and the diff section must not overlap, and the diff starts below the list.
const FLOW = `(() => {
  const tab = document.querySelector('.chtab'), chl = tab?.querySelector('.chl'), chg = tab?.querySelector('.chg');
  if (!tab || !chl || !chg) return null;
  const box = (e) => { const r = e.getBoundingClientRect(); return { top: r.top, bottom: r.bottom, left: r.left, right: r.right }; };
  const clip = getComputedStyle(chl).overflowY !== 'visible';
  const l = box(chl), d = box(chg);
  const cards = [...chl.querySelectorAll('.chc')].map(box).map((c) => clip ? { ...c, top: Math.max(c.top, l.top), bottom: Math.min(c.bottom, l.bottom) } : c).filter((c) => c.bottom > c.top);
  const hit = (a, b) => a.top < b.bottom - 0.5 && b.top < a.bottom - 0.5 && a.left < b.right - 0.5 && b.left < a.right - 0.5;
  const overlaps = [];
  cards.forEach((c, i) => { if (hit(c, d)) overlaps.push('card ' + (i + 1) + ' and the diff'); cards.slice(i + 1).forEach((o, j) => hit(c, o) && overlaps.push('cards ' + (i + 1) + ' and ' + (i + j + 2))); });
  const last = cards[cards.length - 1];
  return { cards: chl.querySelectorAll('.chc').length, lastBottom: last ? last.bottom : null, listBottom: clip ? l.bottom : (last ? last.bottom : l.bottom), diffTop: d.top, overlaps,
    rails: [...chl.querySelectorAll('.chc.end .chrail')].length, ended: chl.querySelectorAll('.chc.end').length, doneRows: chl.querySelectorAll('.chdone .chdrow').length,
    empty: chg.querySelector('.chg-empty')?.textContent ?? '', prChip: [...document.querySelectorAll('.chip.pr, .m-chip.prc')].map((e) => e.textContent).join('|') };
})()`;

// What must be visible, and where, on the current screen.
const MEASURE = `(() => {
  const vv = window.visualViewport;
  const top = vv.offsetTop, bottom = vv.offsetTop + vv.height;
  const rect = (sel) => { const el = document.querySelector(sel); if (!el) return null; const r = el.getBoundingClientRect(); return r.height ? { top: r.top, bottom: r.bottom } : null; };
  const lastSheetItem = (() => { const items = document.querySelectorAll('.m-sheet .m-mi, .m-sheet .m-bt, .m-sheet .pformb .btn, .m-sheet .fbar .btn, .m-sheet .df .btn'); const el = items[items.length - 1]; if (!el) return null; el.scrollIntoView({ block: 'nearest' }); const r = el.getBoundingClientRect(); return { top: r.top, bottom: r.bottom }; })();
  const active = document.activeElement && document.activeElement !== document.body ? document.activeElement.getBoundingClientRect() : null;
  return {
    top, bottom,
    header: rect('.m-ih, .m-nh, .m-tth'),
    bar: rect('.m-bn .m-bnew'),
    composer: rect('.m-cmp'),
    commit: rect('.m-abar .m-bt'),
    sheet: rect('.m-sheet'),
    chend: [...document.querySelectorAll('.chend')].map((b) => { const r = b.getBoundingClientRect(); return { h: r.height, right: r.right }; }),
    // V0.84: Not now, inside its card (left, right and bottom within the card's box).
    chpark: [...document.querySelectorAll('.chpark')].map((b) => { const r = b.getBoundingClientRect(), c = b.closest('.chc')?.getBoundingClientRect(); return { h: r.height, w: r.width, inside: !!c && r.left >= c.left - 0.5 && r.right <= c.right + 0.5 && r.top >= c.top - 0.5 && r.bottom <= c.bottom + 0.5, right: r.right }; }),
    cfm: (() => { const s = document.querySelector('.m-sheet.cfm'); if (!s) return null; const r = s.getBoundingClientRect(); const bs = [...s.querySelectorAll('.cfm-f .m-bt, .cfm-copy')].map((b) => b.getBoundingClientRect()); const p = s.querySelector('.cfm-f .m-bt.p'); return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, vw: innerWidth, minBtn: Math.min(...bs.map((b) => b.height)), btnBottom: Math.max(...bs.map((b) => b.bottom)), title: s.querySelector('.cfm-t')?.textContent ?? '', code: s.querySelector('.cfm-code code')?.textContent ?? '', folder: s.querySelector('.cfm-in')?.textContent ?? '', primary: p?.textContent ?? '', danger: !!p?.classList.contains('danger'), focused: document.activeElement === p, codeOverflow: s.querySelector('.cfm-code') ? s.querySelector('.cfm-code').scrollWidth - s.querySelector('.cfm-code').clientWidth : 0 }; })(),
    sheetItem: lastSheetItem,
    chips: rect('.m-cmpw .att-strip'),
    camera: rect('.m-cmp .cam'),
    chipCount: document.querySelectorAll('.att-chip.done').length,
    previewCards: document.querySelectorAll('.pv').length,
    previewOverflow: Math.max(0, ...[...document.querySelectorAll('.pv, .pv-frame, .pv-img img, .fs')].map(e => e.getBoundingClientRect().right)) - window.innerWidth,
    pageOverflow: document.documentElement.scrollWidth - window.innerWidth,
    innerWidth: window.innerWidth,
    projectEditor: !!document.querySelector('.pedit'),
    filterLine: !!document.querySelector('.m-screen .pfrow .pfc.on'),
    otherProjectRows: [...document.querySelectorAll('.m-list .rpi')].filter(e => e.getAttribute('title') !== 'acme/web').length, // V0.135: the row's project is its icon
    saveButton: [...document.querySelectorAll('.pedit button')].some((b) => b.textContent.trim() === 'Save' && b.getBoundingClientRect().height > 0),
    projectGroups: [...document.querySelectorAll('.pedit > .sg > .sg-h .sg-t')].map((e) => e.textContent),
    // V0.119: the button beside a focused text field (Save, Add, Save key) stays in view with the keyboard up.
    fieldButton: (() => { const b = document.activeElement?.closest?.('.inl')?.querySelector('.btn'); if (!b) return null; const r = b.getBoundingClientRect(); return { top: r.top, bottom: r.bottom }; })(),
    toolOutput: (() => { const o = document.querySelector('.tlout'); if (!o) return null; const r = o.getBoundingClientRect(); return { right: r.right, lines: o.textContent.split(String.fromCharCode(10)).length }; })(),
    changeRows: document.querySelectorAll('.chg-file').length,
    flow: ${FLOW},
    since: (() => {
      const el = document.querySelector('.chg'); if (!el) return null;
      const targets = [...el.querySelectorAll('.chg-mode, .chg-vw, .chg-file')].filter(e => e.getBoundingClientRect().height > 0);
      const small = targets.map(e => ({ t: (e.textContent || e.className).trim().slice(0, 30), h: e.getBoundingClientRect().height, w: e.getBoundingClientRect().width })).filter(x => x.h < 43.5 || x.w < 43.5);
      const right = Math.max(0, ...[...el.querySelectorAll('.chg-modes, .chg-notes, .chg-vw, .chg-mode')].map(e => e.getBoundingClientRect().right));
      return { modes: [...el.querySelectorAll('.chg-mode')].map(e => e.textContent), pressed: el.querySelector('.chg-mode.on')?.textContent ?? '', count: el.querySelector('.chg-vcount')?.textContent ?? '', notes: el.querySelectorAll('.chg-notes li').length, boxes: el.querySelectorAll('.chg-vw input').length, small, right };
    })(),
    diffLines: document.querySelectorAll('.chg-diff .dl').length,
    ciCounts: [...document.querySelectorAll('.chg-ci')].map(e => e.textContent).join(","),
    ciNotes: [...document.querySelectorAll('.chg-diff .dl.nt')].filter(e => /[✖▲] lint/.test(e.textContent)).length,
    changeRight: Math.max(0, ...[...document.querySelectorAll('.chg, .chg-file, .chg-diff')].map(e => e.getBoundingClientRect().right)),
    focused: active ? { top: active.top, bottom: active.bottom } : null,
    pickerRows: [...document.querySelectorAll('.m-sheet .pli .pln')].slice(0, 2).map(e => e.textContent),
    stepName: document.querySelector('.m-sheet .pform input')?.value ?? null,
    stepSources: document.querySelectorAll('.m-sheet .pform .src').length,
    newProject: document.querySelector('.m-new .m-ppick .pln')?.textContent ?? null,
    folderLine: document.querySelector('.m-new .m-fline .ell')?.textContent ?? null,
    stepFolder: document.querySelector('.m-sheet .pform .pfolder .ell')?.textContent ?? '',
    folderSections: [...document.querySelectorAll('.fsheet .fsec h4')].map(e => e.textContent),
    browseRows: document.querySelectorAll('.fsheet .fsec.browse .fpr').length,
    projFolder: document.querySelector('.pedit .pfolder .ell')?.textContent ?? '',
    projFolderLine: document.querySelector('.pedit .pfolder .srcl')?.textContent ?? '',
    projFolderBtn: document.querySelector('.pedit .pfchg')?.textContent ?? '',
    stars: document.querySelectorAll('.pedit .star').length,
    noOldWords: !/\b(sources?|main folder|clones)\b/i.test(document.querySelector('.pedit, .m-new, .m-sheet')?.innerText ?? ''),
    makeProject: !!document.querySelector('.m-mkp, .chip.mkp'),
    startDisabled: !!document.querySelector('.m-new .m-abar .m-bt')?.disabled,
    pr: (() => {
      const v = document.querySelector('.prv'); if (!v) return null;
      const targets = [...v.querySelectorAll('button, .prv-det, .prv-thh a, a.btn')].filter(e => e.getBoundingClientRect().height > 0);
      const small = targets.map(e => ({ t: e.textContent.trim().slice(0, 30), h: e.getBoundingClientRect().height })).filter(x => x.h < 43.5);
      const status = v.querySelector('.prv-st')?.textContent ?? '';
      const right = Math.max(0, ...[...v.querySelectorAll('*')].map(e => e.getBoundingClientRect().right));
      return { status, failed: v.querySelectorAll('.prv-ck.fail').length, ask: v.querySelectorAll('.prv-ask').length, threads: v.querySelectorAll('.prv-th').length,
        askAll: v.querySelector('.prv-askall')?.textContent ?? '', merge: !!v.querySelector('.prv-merge'), mergeDisabled: !!v.querySelector('.prv-merge')?.disabled, small, right };
    })(),
    settings: (() => {
      // V0.119: every Settings page and sub-page, and the project screen (the same page).
      const el = document.querySelector('.m-settings .set, .m-project .set'); if (!el) return null;
      const vis = (e) => { const r = e.getBoundingClientRect(); return r.height > 0 && r.width > 0; };
      const targets = [...el.querySelectorAll('button, select, input:not([type=file]), a.btn')].filter(vis);
      // A switch's target is its row (a label, 56 px): the whole row toggles it.
      const box = (e) => (e.matches('[role=switch]') ? e.closest('label, .sr') : null) || e;
      const name = (e) => (e.textContent || e.getAttribute('aria-label') || e.getAttribute('placeholder') || e.tagName).trim().slice(0, 30);
      const small = targets.map((e) => ({ t: name(e), h: box(e).getBoundingClientRect().height })).filter((x) => x.h < 43.5);
      const right = Math.max(0, ...[...el.querySelectorAll('*')].filter(vis).map((e) => e.getBoundingClientRect().right));
      const lg = el.querySelector('.login');
      const login = lg ? { link: lg.querySelector('a.loginlink')?.getAttribute('href') ?? '', code: !!lg.querySelector('input'), cancel: [...lg.querySelectorAll('button')].some(b => b.textContent.trim() === 'Cancel') } : null;
      const adb = el.querySelector('section[aria-label="Auto-update"] [role=switch]');
      const upb = el.querySelector('section[aria-label="Updates"] .sr .btn');
      const autoDeploy = adb ? { role: adb.getAttribute('role'), checked: adb.getAttribute('aria-checked'), label: adb.getAttribute('aria-label'), rowH: adb.closest('.sr').getBoundingClientRect().height } : null;
      const sas = el.querySelector('section[aria-label="System agents"]');
      const systemAgents = sas ? { selects: [...sas.querySelectorAll('select')].map((x) => x.getAttribute('aria-label')), lines: [...sas.querySelectorAll('.sajob .sr-d')].map(e => e.textContent), same: [...sas.querySelectorAll('select[aria-label="Thread titles"] option')].map((o) => o.textContent)[0] ?? '' } : null;
      const nav = [...el.querySelectorAll('.stn-i')].map((b) => ({ t: b.querySelector('.stn-t').textContent, s: b.querySelector('.stn-s').textContent, h: b.getBoundingClientRect().height }));
      const device = { push: !!el.querySelector('[role=switch][aria-label="Notifications"]'), theme: el.querySelectorAll('[role=radiogroup][aria-label="Theme"] [role=radio]').length };
      const last = targets[targets.length - 1];
      // Scrolled to the end, as far as the screen scrolls: its padding keeps the last control above the home indicator.
      const sc = el.closest('.m-nbody'); if (sc) sc.scrollTop = sc.scrollHeight;
      const lr = last ? box(last).getBoundingClientRect() : null;
      const updateNow = upb ? { text: upb.textContent.trim(), h: upb.getBoundingClientRect().height } : null;
      return { nav, device, systemAgents, autoDeploy, updateNow, rows: el.querySelectorAll('.prow').length, found: el.querySelectorAll('.sr.found').length,
        imports: [...el.querySelectorAll('.sr.found button')].filter(b => b.textContent.trim() === 'Import').length,
        needs: [...el.querySelectorAll('.pst')].map(e => e.textContent), small, login, right,
        last: lr ? { t: name(last), top: lr.top, bottom: lr.bottom } : null };
    })(),
    // Content that paints outside its box: an element whose overflow is visible while its height is capped
    // (a max-height, or a flex item shrunk below its content). The rows then draw over the sections below.
    // Up to 8 px is allowed: .dlb enlarges its touch target with -6 px margins on purpose.
    spills: [...document.querySelectorAll('.m-app *')].filter((e) => {
      const cs = getComputedStyle(e);
      if (cs.overflowY !== 'visible' || cs.display === 'inline' || cs.display === 'contents' || e.clientHeight === 0) return false;
      return e.scrollHeight - e.clientHeight > 8;
    }).map((e) => e.tagName.toLowerCase() + '.' + [...e.classList].join('.') + ' ' + e.clientHeight + '<' + e.scrollHeight),
    dlg: rect('.dlg'),
    dlgButton: (() => { const b = document.querySelector('.dlg .df .btn.p'); if (!b) return null; const r = b.getBoundingClientRect(); return { top: r.top, bottom: r.bottom, h: r.height, text: b.textContent }; })(),
    dlgRows: [...document.querySelectorAll('.dlg .prv-m')].map(e => e.getBoundingClientRect().height),
    liveView: rect('.m-thread .lv'),
    liveFrame: rect('.m-thread .lv-f'),
    innerHeight: window.innerHeight,
    dishwasher: (document.querySelector('.m-app')?.innerText ?? '').includes('Pick a dishwasher'),
    quietNote: document.querySelector('.m-screen .pfc.on .pfq')?.textContent ?? null,
    quietGate: (() => { const g = document.querySelector('.m-app .qgate'); if (!g) return null; const h = g.querySelector('.qhold'); const r = h?.getBoundingClientRect();
      return { head: g.querySelector('.qgh')?.textContent ?? '', hold: h?.textContent ?? '', h: r?.height ?? 0, right: g.getBoundingClientRect().right }; })(),
    quietRow: (() => {
      const el = document.querySelector('.pedit .qfld'); if (!el) return null;
      el.scrollIntoView({ block: 'center' });
      // Each row and the add bar, scrolled into view one by one: two rows and the presets are taller than a small phone.
      const parts = [...el.querySelectorAll('.qwin, .qadd')].map((p) => { p.scrollIntoView({ block: 'center' }); const b = p.getBoundingClientRect(); return { top: b.top, bottom: b.bottom }; });
      el.scrollIntoView({ block: 'center' });
      const r = el.getBoundingClientRect();
      const box = (b) => b.getBoundingClientRect();
      const days = [...el.querySelectorAll('.qdays button')].map(box);
      const times = [...el.querySelectorAll('.qtime select')].map(box);
      const others = [...el.querySelectorAll('.qrm, .qtog, .qadd button')].map(box); // .qtog: the All day switch's label, the switch's target
      const all = [...days, ...times, ...others];
      return { parts, right: Math.max(r.right, ...all.map(d => d.right)), rows: el.querySelectorAll('.qwin').length,
        add: [...el.querySelectorAll('.qadd button')].map(b => b.textContent.replace(/\\s+/g, ' ').trim()),
        line: el.querySelector('.sg-f')?.textContent ?? '', note: el.querySelector('.sg-h .end')?.textContent ?? '', days: days.length, times: times.length, small: all.filter(d => d.height < 43.5).length };
    })(),
  };
})()`;

const failures = [];
let checks = 0;
const check = (ok, what) => {
  checks++;
  if (!ok) failures.push(what);
};
// The timing budgets (V0.38, V0.32) measure wall time, so a machine busy with other work is slower; they scale with a
// calibration run (see "Calibration of the timing budgets"), up to a ceiling, and a heavier load still fails them (#50). The deploy
// gate (`make test`) runs with FYR_LAYOUT_TIMING=0 and skips only these budgets; CI and `make test-timing` keep them.
const TIMING = process.env.FYR_LAYOUT_TIMING !== "0";
let timingSkipped = 0;
const timed = (ok, what) => {
  if (TIMING) check(ok, what);
  else timingSkipped++;
};

const window_innerWidth = (m) => m.innerWidth;
// The class of #84: text outside its parent. Every element that holds text must stay inside the content box of its
// nearest bordered ancestor (a card, a chip, a button, a field) and inside the window, unless an ancestor on the way
// clips or scrolls it (overflow other than visible), which is how a row, a chip line or a scroller keeps it in.
const OUTSIDE = `(() => {
  const out = [], W = innerWidth;
  const clips = (cs) => cs.overflowX !== 'visible' || cs.overflowY !== 'visible';
  for (const e of document.querySelectorAll('body *')) {
    if (!e.offsetParent && getComputedStyle(e).position !== 'fixed') continue;
    if (![...e.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim())) continue;
    const r = e.getBoundingClientRect();
    if (!r.width || !r.height) continue;
    let a = e.parentElement, box = null;
    for (; a && a !== document.body; a = a.parentElement) {
      const cs = getComputedStyle(a);
      if (clips(cs)) break;
      if (parseFloat(cs.borderRightWidth) > 0 && parseFloat(cs.borderLeftWidth) > 0) { box = a; break; }
    }
    const name = (x) => x.tagName.toLowerCase() + (x.className && typeof x.className === 'string' ? '.' + x.className.trim().split(/\\s+/).join('.') : '');
    const t = e.textContent.trim().slice(0, 40);
    if (box) {
      const b = box.getBoundingClientRect(), cs = getComputedStyle(box);
      if (r.right > b.right - parseFloat(cs.borderRightWidth) + 1 || r.left < b.left + parseFloat(cs.borderLeftWidth) - 1) out.push(name(e) + ' outside ' + name(box) + ': ' + t);
    } else if ((!a || a === document.body) && (r.right > W + 1 || r.left < -1)) out.push(name(e) + ' outside the window: ' + t);
  }
  return out.slice(0, 6);
})()`;
// SPEC 0.5 V0.50, decision 0015 (#28): the UI says provider, never profile. Reads Fyr's own chrome (labels, options,
// buttons, menu items, dialog text, aria-labels and placeholders); the conversation's text is the agent's, not Fyr's.
const PROFILE_WORDS = `(() => { const out = []; const see = (s) => { if (s && /\\bprofiles?\\b/i.test(s)) out.push(s.trim().slice(0, 80)); };
  const own = (e) => !e.closest('.convo, .msg, .you, .md, pre, code');
  for (const e of document.querySelectorAll('label, .fld > span, option, button, [role=menuitem], .ho-from, .ho-b, .dh, h1, h2, h3')) if (own(e)) see(e.textContent);
  for (const e of document.querySelectorAll('[aria-label], [placeholder]')) if (own(e)) { see(e.getAttribute('aria-label')); see(e.getAttribute('placeholder')); }
  return out; })()`;
const ctxWantsChips = (ctx) => ctx.startsWith("composer with chips");

// V0.73 (2): six cards, five ended without a rail, the diff section below the last card and nothing overlapping;
// the header has no chip for the ended main PR.
function checkFlow(f, ctx) {
  // V0.98 (3): the five ended changes are done: they fold under Done here; the open one stays a card.
  check(!!f && f.cards === 1 && f.ended === 0 && f.doneRows === 5, `${ctx}: expected 1 change card and 5 rows under Done here (${JSON.stringify(f)})`);
  if (!f) return;
  check(f.empty === "No changes against origin/main.", `${ctx}: the diff section says "${f.empty}"`);
  check(f.diffTop >= f.listBottom - 0.5 && (f.lastBottom === null || f.diffTop >= f.lastBottom - 0.5), `${ctx}: the diff section starts at ${f.diffTop.toFixed(1)}, above the last card's bottom ${f.lastBottom?.toFixed(1)} (list ${f.listBottom.toFixed(1)})`);
  check(f.overlaps.length === 0, `${ctx}: overlapping boxes: ${f.overlaps.join(", ")}`);
  check(f.rails === 0, `${ctx}: ${f.rails} ended cards still draw a phase rail`);
  check(!/#88/.test(f.prChip), `${ctx}: the header shows the ended main PR's chip "${f.prChip}"`);
}

function assertLayout(m, ctx, { keyboard = false } = {}) {
  const insetTop = keyboard ? 0 : INSETS.top;
  const insetBottom = keyboard ? 0 : INSETS.bottom;
  const visTop = m.top;
  const visBottom = m.bottom;
  if (m.header && !m.sheet) check(m.header.top >= visTop + insetTop - 0.5, `${ctx}: header top ${m.header.top.toFixed(1)} is above the top inset (${visTop + insetTop})`);
  for (const [name, r] of [
    ["bottom bar", m.bar],
    ["composer", m.composer],
    ["commit button", m.commit],
    ["last sheet item", m.sheetItem],
    ["attachment chips", m.chips],
    ["camera button", m.camera],
  ]) {
    if (!r) continue;
    check(r.top >= visTop - 0.5, `${ctx}: ${name} starts above the visible area (${r.top.toFixed(1)} < ${visTop})`);
    check(r.bottom <= visBottom - insetBottom + 0.5, `${ctx}: ${name} bottom ${r.bottom.toFixed(1)} is below ${visBottom - insetBottom} (viewport ${visBottom}, inset ${insetBottom})`);
  }
  if (ctx.startsWith("thread with previews")) {
    check(m.previewCards >= 4, `${ctx}: expected the 4 preview cards (found ${m.previewCards})`);
    check(m.previewOverflow <= 0.5, `${ctx}: a preview card runs ${m.previewOverflow.toFixed(1)} px past the right edge`);
  }
  if (ctx.startsWith("project screen")) check(m.projectEditor, `${ctx}: no project editor`);
  // V0.119: every Settings page and sub-page, and the project screen.
  if (m.settings) {
    const st = m.settings;
    check(st.small.length === 0, `${ctx}: targets under 44 px ${JSON.stringify(st.small)}`);
    check(st.right <= m.innerWidth + 0.5, `${ctx}: Settings runs ${(st.right - m.innerWidth).toFixed(1)} px past the right edge`);
    if (st.last && !keyboard) check(st.last.top >= m.top + insetTop - 0.5 && st.last.bottom <= m.bottom - insetBottom + 0.5, `${ctx}: the last control "${st.last.t}" (${st.last.top.toFixed(1)}–${st.last.bottom.toFixed(1)}) is outside the safe areas`);
  }
  if (ctx.startsWith("settings nav")) {
    const n = m.settings?.nav ?? [];
    check(n.map((x) => x.t).join("|") === "Providers|Automation|Projects|Hosts|Devices|This device" && n.every((x) => x.h >= 55.5 && x.s), `${ctx}: the nav is wrong ${JSON.stringify(n)}`);
  }
  if (ctx.startsWith("settings providers")) {
    const st = m.settings;
    check(!!st && st.rows === 3 && st.found >= 1 && st.imports === st.found && st.needs.includes("Needs a login") && !st.login, `${ctx}: the Providers page is wrong ${JSON.stringify(st)}`);
    // SPEC V0.115: the phone has System agents too; the labels are unchanged and a job reads "Same as default".
    check(!!st?.systemAgents && st.systemAgents.selects.join("|") === "Provider|Model|Thread titles|Project setup" && st.systemAgents.lines.length === 2 && st.systemAgents.same === "Same as default", `${ctx}: System agents are wrong ${JSON.stringify(st?.systemAgents)}`);
  }
  // SPEC 0.5 V0.53, V0.119: a running login shows on the provider's page: the sign-in link, the code field and Cancel, all 44 px.
  if (ctx.startsWith("settings provider login")) check(!!m.settings?.login && /^https:\/\/claude\.com\//.test(m.settings.login.link) && m.settings.login.code && m.settings.login.cancel, `${ctx}: the login panel is wrong ${JSON.stringify(m.settings?.login)}`);
  // SPEC V0.111, V0.119, V0.126: the Auto-update switch, on by default, its row 44 px or more; Update now a 44 px target.
  if (ctx.startsWith("settings automation")) {
    const a = m.settings?.autoDeploy;
    check(!!a && a.role === "switch" && a.checked === "true" && a.label === "Auto-update Fyr" && a.rowH >= 43.5, `${ctx}: no Auto-update switch ${JSON.stringify(a)}`);
    const u = m.settings?.updateNow;
    check(!!u && u.text === "Update now" && u.h >= 43.5, `${ctx}: no 44 px Update now button ${JSON.stringify(u)}`);
  }
  if (ctx.startsWith("settings device")) check(!!m.settings && m.settings.device.push && m.settings.device.theme === 3, `${ctx}: This device is wrong ${JSON.stringify(m.settings?.device)}`);
  if (ctx.startsWith("project screen")) check(!m.saveButton && m.projectGroups.join("|") === "Identity|Threads|Quiet hours", `${ctx}: the project page has a Save button (${m.saveButton}) or its groups are ${JSON.stringify(m.projectGroups)}`);
  if (ctx.startsWith("project picker")) check(m.pickerRows[0] === "New project from this thread" && m.pickerRows[1] === "No project", `${ctx}: the picker's first rows are ${JSON.stringify(m.pickerRows)}`);
  if (ctx.startsWith("new project step")) check(m.stepName === "lasso-app" && m.stepSources === 1 && m.stepFolder === "~/src/lasso-app", `${ctx}: the step is not prefilled from the thread (name ${m.stepName}, ${m.stepSources} folders, folder "${m.stepFolder}")`);
  if (ctx.startsWith("new thread, project without")) check(m.newProject === "Garden" && m.folderLine === "A fresh scratch folder", `${ctx}: expected Garden in a fresh scratch folder (${m.newProject}, ${m.folderLine})`);
  if (ctx.startsWith("folder picker sheet")) check(m.folderSections.join("|") === "Recent|Repos found|Browse" && m.browseRows >= 4, `${ctx}: sections ${JSON.stringify(m.folderSections)}, ${m.browseRows} browse rows`);
  if (ctx.startsWith("project screen with its folder")) check(m.projFolder === "~/src/acme/web" && m.projFolderLine === "Threads started in this folder or below land here." && m.projFolderBtn === "Change…" && m.stars === 0 && m.noOldWords, `${ctx}: folder "${m.projFolder}", line "${m.projFolderLine}", button "${m.projFolderBtn}", ${m.stars} stars, old words gone ${m.noOldWords}`);
  if (ctx.startsWith("details without a project")) check(!m.makeProject, `${ctx}: a Make project button still shows`);
  if (ctx.startsWith("inbox with a quiet project")) check(!m.dishwasher && !m.quietNote, `${ctx}: the quiet project's thread shows in the inbox (${m.dishwasher}, note ${m.quietNote})`);
  if (ctx.startsWith("quiet project through the filter")) {
    check(!m.dishwasher && /^Quiet until \d\d:\d\d$/.test(m.quietNote ?? ""), `${ctx}: the chip shows the quiet project's thread without the hold (${m.dishwasher}) or no note ("${m.quietNote}")`);
    check(!!m.quietGate && /^Garden is quiet until \d\d:\d\d\.$/.test(m.quietGate.head) && /Hold here for 30 s/.test(m.quietGate.hold) && m.quietGate.h >= 44 && m.quietGate.right <= m.innerWidth + 0.5, `${ctx}: the gate is missing or wrong ${JSON.stringify(m.quietGate)}`);
  }
  if (ctx.startsWith("quiet project unlocked through the filter")) check(m.dishwasher && !m.quietGate, `${ctx}: after the hold the chip does not show the quiet project's thread (${m.dishwasher}, gate ${JSON.stringify(m.quietGate)})`);
  if (ctx.startsWith("link to a quiet thread")) check(!m.dishwasher && !!m.quietGate && /Hold here for 30 s/.test(m.quietGate.hold), `${ctx}: the link opens the quiet thread without the hold (${m.dishwasher}, gate ${JSON.stringify(m.quietGate)})`);
  if (ctx.startsWith("project screen with quiet hours")) {
    const q = m.quietRow;
    // Decision 0014 addendum: two windows (the mock's daily one and a weekend one), each with its 24-hour times and
    // seven days, "+ Add quiet hours" and both presets, and one line for all of them.
    check(!!q && q.rows === 2 && q.days === 14 && q.times === 8 && q.add.join("|") === "Add quiet hours|Weeknights 17:00–06:00|All weekend Sat–Sun"
      && /^Threads hide and send no notifications daily \d\d:\d\d–\d\d:\d\d and weekends \d\d:\d\d–\d\d:\d\d\.$/.test(q.line) && /^Quiet (now, until|until) \d\d:\d\d/.test(q.note), `${ctx}: the Quiet hours rows are wrong ${JSON.stringify(q)}`);
    if (q) {
      for (const [i, p] of q.parts.entries())
        check(p.top >= m.top + insetTop - 0.5 && p.bottom <= m.bottom - insetBottom + 0.5, `${ctx}: quiet hours part ${i + 1} (${p.top.toFixed(1)}–${p.bottom.toFixed(1)}) is outside the safe areas`);
      check(q.parts.length === 3, `${ctx}: expected two rows and the add bar, found ${q.parts.length} parts`);
      check(q.right <= m.innerWidth + 0.5, `${ctx}: the Quiet hours row runs ${(q.right - m.innerWidth).toFixed(1)} px past the right edge`);
      check(q.small === 0, `${ctx}: ${q.small} quiet-hours targets are under 44 px`);
    }
  }
  if (ctx.startsWith("inbox filtered")) {
    check(m.filterLine, `${ctx}: the picked project chip is missing`);
    check(m.otherProjectRows === 0, `${ctx}: ${m.otherProjectRows} rows of other projects show under the filter`);
  }
  if (ctx.startsWith("opened tool group")) {
    check(!!m.toolOutput, `${ctx}: the opened row shows no output`);
    if (m.toolOutput) {
      check(m.toolOutput.lines === 200, `${ctx}: long output shows ${m.toolOutput.lines} lines, expected the 200-line cut`);
      check(m.toolOutput.right <= window_innerWidth(m) + 0.5, `${ctx}: the output block runs past the right edge`);
    }
  }
  if (ctx.startsWith("thread with a live view")) {
    check(!!m.liveView && !!m.liveFrame, `${ctx}: no pinned live view`);
    if (m.liveView && m.header) check(m.liveView.top >= m.header.bottom - 0.5, `${ctx}: the live view starts under the header (${m.liveView.top.toFixed(1)} < ${m.header.bottom.toFixed(1)})`);
    if (m.liveFrame) {
      const fh = m.liveFrame.bottom - m.liveFrame.top;
      // V0.37: in its View tab the view fills the pane down to the composer (nothing else shares the tab).
      check(fh >= 40, `${ctx}: the live view is only ${fh.toFixed(0)} px tall`);
      if (m.composer) check(m.composer.top - m.liveFrame.bottom <= 24, `${ctx}: the live view leaves ${(m.composer.top - m.liveFrame.bottom).toFixed(0)} px empty above the composer`);
      if (m.composer) check(m.liveFrame.bottom <= m.composer.top + 0.5, `${ctx}: the live view overlaps the composer`);
    }
  }
  if (ctx.startsWith("changes")) {
    check(m.changeRows >= (ctx.includes("since") ? 2 : 4), `${ctx}: expected the changed files (found ${m.changeRows})`);
    if (ctx.includes("since")) {
      const v = m.since;
      check(!!v && v.modes.length === 2 && v.pressed === "New since you looked · 2" && v.count === "0 of 2 viewed" && v.notes === 3 && v.boxes === 2, `${ctx}: the since view is wrong ${JSON.stringify(v)}`);
      if (v) {
        check(v.small.length === 0, `${ctx}: targets under 44 px ${JSON.stringify(v.small)}`);
        check(v.right <= window_innerWidth(m) + 0.5, `${ctx}: the since controls run ${(v.right - window_innerWidth(m)).toFixed(1)} px past the right edge`);
      }
    }
    check(m.diffLines >= 5, `${ctx}: the opened file shows no diff lines (${m.diffLines})`);
    if (ctx.startsWith("changes with CI")) {
      check(m.ciCounts === "2,1", `${ctx}: expected annotation counts 2 and 1 on the file rows (found "${m.ciCounts}")`);
      check(m.ciNotes === 2, `${ctx}: expected 2 lint notes in render.ts's diff (found ${m.ciNotes})`);
    }
    if (ctx.startsWith("changes since")) {
      // SPEC 0.5 V0.84: the failing c_scale card offers Not now, a 44 px target inside its card and the screen.
      check(m.chpark.length >= 1, `${ctx}: no Not now on the failing change`);
      check(m.chpark.every((b) => b.h >= 43.5 && b.w >= 43.5 && b.inside && b.right <= window_innerWidth(m) + 0.5), `${ctx}: Not now is under 44 px or outside its card ${JSON.stringify(m.chpark)}`);
    }
    check(m.changeRight <= window_innerWidth(m) + 0.5, `${ctx}: the changes list or diff runs ${(m.changeRight - window_innerWidth(m)).toFixed(1)} px past the right edge`);
  }
  if (ctx.startsWith("ended changes")) checkFlow(m.flow, ctx);
  if (ctx.startsWith("board of changes")) {
    // V0.72: every change that has not ended offers End this change, a 44 px target inside the screen.
    check(m.chend.length >= 1, `${ctx}: no End this change on the board`);
    check(m.chend.every((b) => b.h >= 43.5 && b.right <= window_innerWidth(m) + 0.5), `${ctx}: End this change is under 44 px or past the edge ${JSON.stringify(m.chend)}`);
  }
  if (ctx.startsWith("PR tab")) {
    check(!!m.pr, `${ctx}: no PR tab`);
    if (m.pr) {
      check(m.pr.status === "Blocked: 2 failing checks" || m.pr.status === "Blocked: 1 failing check", `${ctx}: status line "${m.pr.status}"`);
      check(m.pr.failed === 1 && m.pr.ask === 1, `${ctx}: expected 1 failed check with its Ask button (${m.pr.failed}, ${m.pr.ask})`);
      check(m.pr.threads === 2 && /address 2 review comments/.test(m.pr.askAll), `${ctx}: expected 2 open review threads and the ask button (${m.pr.threads}, "${m.pr.askAll}")`);
      check(m.pr.merge && m.pr.mergeDisabled, `${ctx}: Merge must show and be disabled on a blocked PR`);
      check(m.pr.small.length === 0, `${ctx}: targets under 44 px: ${JSON.stringify(m.pr.small)}`);
      check(m.pr.right <= window_innerWidth(m) + 0.5, `${ctx}: the PR tab runs ${(m.pr.right - window_innerWidth(m)).toFixed(1)} px past the right edge`);
    }
  }
  if (ctx.startsWith("PR merge dialog")) {
    check(!!m.dlg && !!m.dlgButton, `${ctx}: the merge dialog did not open`);
    if (m.dlgButton) {
      check(m.dlgButton.text === "Squash and merge #57", `${ctx}: the confirm button reads "${m.dlgButton.text}"`);
      check(m.dlgButton.h >= 43.5 && m.dlgRows.length === 3 && m.dlgRows.every((h) => h >= 43.5), `${ctx}: dialog targets under 44 px (${m.dlgButton.h}, ${m.dlgRows})`);
      check(m.dlgButton.bottom <= m.bottom - (keyboard ? 0 : INSETS.bottom) + 0.5, `${ctx}: the confirm button bottom ${m.dlgButton.bottom.toFixed(1)} is below the bottom inset`);
    }
    if (m.dlg) check(m.dlg.top >= m.top + INSETS.top - 0.5, `${ctx}: the dialog starts above the top inset (${m.dlg.top.toFixed(1)})`);
  }
  check(m.spills.length === 0, `${ctx}: content paints outside its box ${JSON.stringify(m.spills.slice(0, 5))}`);
  check(m.pageOverflow <= 0, `${ctx}: the page scrolls sideways by ${m.pageOverflow} px`);
  if (ctxWantsChips(ctx)) check(m.chipCount >= 2 && !!m.chips && !!m.camera, `${ctx}: expected 2 uploaded chips and the Camera button (chips ${m.chipCount})`);
  if (m.sheet) check(m.sheet.bottom <= visBottom + 0.5, `${ctx}: sheet runs below the viewport`);
  if (ctx.startsWith("confirm sheet")) {
    // SPEC 0.5 V0.68: the sheet fits, its buttons are 44 px and above the bottom inset, focus is on the primary.
    const c = m.cfm;
    check(!!c, `${ctx}: no confirmation sheet`);
    if (c) {
      check(c.top >= visTop + insetTop - 0.5 && c.left >= -0.5 && c.right <= c.vw + 0.5, `${ctx}: the sheet does not fit (${JSON.stringify(c)})`);
      check(c.minBtn >= 43.5, `${ctx}: a button is ${c.minBtn.toFixed(1)} px tall`);
      check(c.btnBottom <= visBottom - insetBottom + 0.5, `${ctx}: the buttons end at ${c.btnBottom.toFixed(1)}, below ${visBottom - insetBottom}`);
      check(c.focused, `${ctx}: focus is not on the primary button`);
      check(c.codeOverflow <= 0.5, `${ctx}: the command runs ${c.codeOverflow} px past its box`);
      if (ctx.startsWith("confirm sheet for a script")) check(c.title === "Set the Acme token" && c.primary === "Run" && !c.danger && c.code === "bash …/20261115-130828-find-the-acme-claude-long-term-token/set-acme-token.sh" && c.folder === "in ~/…/scratch/20261115-130828-find-the-acme-claude-long-term-token", `${ctx}: words ${JSON.stringify(c)}`);
      else check(c.title === "Delete acme/web?" && c.primary === "Delete project" && c.danger, `${ctx}: words ${JSON.stringify(c)}`);
    }
  }
  if (keyboard) {
    check(!!m.focused, `${ctx}: no field is focused`);
    if (m.focused) check(m.focused.top >= visTop - 0.5 && m.focused.bottom <= visBottom + 0.5, `${ctx}: the focused field (${m.focused.top.toFixed(1)}–${m.focused.bottom.toFixed(1)}) is outside the visible area ${visTop}–${visBottom}`);
    if (m.fieldButton) check(m.fieldButton.top >= visTop - 0.5 && m.fieldButton.bottom <= visBottom + 0.5, `${ctx}: the field's button (${m.fieldButton.top.toFixed(1)}–${m.fieldButton.bottom.toFixed(1)}) is outside the visible area ${visTop}–${visBottom}`);
  }
}

// The page is settled when its DOM has not changed for QUIET_MS, at least two frames have drawn since the last
// change, and no view transition or finite animation runs. The mock answers within 30 ms, so 80 ms without a
// change is past any chain of its answers; a longer mock delay (an upload, the PR detail) is waited for by what
// it shows.
const QUIET_MS = 80;
const SETTLE = `new Promise((ok) => {
  let last = performance.now(), frames = 0;
  const mo = new MutationObserver(() => { last = performance.now(); frames = 0; });
  mo.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
  const moving = () => {
    try { if (document.documentElement.matches(':active-view-transition')) return true; } catch {}
    return document.getAnimations().some((a) => a.playState === 'running' && a.effect?.getComputedTiming().endTime !== Infinity);
  };
  const t0 = performance.now();
  const tick = () => {
    frames++;
    const now = performance.now();
    const quiet = now - last >= ${QUIET_MS} && frames >= 2 && !moving();
    if (quiet || now - t0 > 5000) { mo.disconnect(); ok(quiet); }
    else requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
})`;

// The helpers of one tab.
function harness(t) {
  const { send } = t;
  const evaluate = async (expression) => {
    const r = await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
    if (r.result?.exceptionDetails) throw new Error(r.result.exceptionDetails.exception?.description ?? r.result.exceptionDetails.text);
    return r.result?.result?.value;
  };
  // Polls fn every 25 ms until it returns something truthy and returns that. At the timeout it throws (what
  // names the state it waited for), or with soft returns the last value so the check after it reports what it
  // found.
  const poll = async (fn, { timeout = 10_000, what = "a state", soft = false } = {}) => {
    const t0 = Date.now();
    for (;;) {
      const v = await fn();
      if (v) return v;
      if (Date.now() - t0 > timeout) {
        if (soft) return v;
        throw new Error(`timed out after ${timeout} ms waiting for ${what}`);
      }
      await sleep(25);
    }
  };
  // The same for a page expression, in the tab or in one of its frames (sessionId).
  const waitIn = (sessionId, expr, opts = {}) =>
    poll(async () => {
      const r = await send("Runtime.evaluate", { expression: `(() => { try { return (${expr}); } catch { return false; } })()`, returnByValue: true, awaitPromise: true }, sessionId);
      return r.result?.result?.value; // undefined while the page is between documents
    }, { what: expr, ...opts });
  const waitFor = (expr, opts) => waitIn(undefined, expr, opts);
  const frames = () => evaluate("new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(() => r(true), 0))))");
  const settle = async () => {
    if (!(await evaluate(SETTLE))) throw new Error("the page did not settle: its DOM kept changing for 5 s");
  };
  // Navigates and waits for the load event, then for ready (the app has drawn its screen) and a settled DOM.
  const navigate = async (url, ready = "!!document.querySelector('.m-app, .shell')") => {
    const loaded = t.next("Page.loadEventFired", 20_000);
    const r = await send("Page.navigate", { url });
    if (r.error || r.result?.errorText) throw new Error(`navigate ${url}: ${r.error?.message ?? r.result.errorText}`);
    await loaded;
    await waitFor(ready, { what: `the page ${url} to show ${ready}` });
    await settle();
  };
  // A tap, the touch held 40 ms like a finger; until is the state the tap leads to.
  // A long press holds the touch until that state shows: the app opens it after LONG_PRESS_MS (400 ms) of the
  // page's own clock, so a hold timed here would be too short in a page that is slow to take the touch. It
  // ends with touchCancel: headless Chrome has no long-press gesture and turns a lifted touch into a click at
  // the same point, which would land on the sheet that just opened (a phone sends no click after a long press).
  const tapCenter = async (sel, until, { long = false } = {}) => {
    const c = await evaluate(`(() => { const el = document.querySelector(${JSON.stringify(sel)}); if (!el) return null; const r = el.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`);
    if (!c) throw new Error(`no element ${sel}`);
    await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: c[0], y: c[1] }] });
    if (long) await waitFor(until, { what: `${until} during a long press on ${sel}` });
    else await sleep(40);
    await send("Input.dispatchTouchEvent", { type: long ? "touchCancel" : "touchEnd", touchPoints: [] });
    await waitFor(until, { what: `${until} after a tap on ${sel}` });
    await settle();
  };
  const key = async (k, code = k, modifiers = 0) => {
    const vk = { Enter: 13, Escape: 27, ArrowDown: 40, ArrowUp: 38, Tab: 9 }[k] ?? 0;
    await send("Input.dispatchKeyEvent", k === "Enter" ? { type: "keyDown", key: k, code, text: "\r", windowsVirtualKeyCode: vk, modifiers } : { type: "rawKeyDown", key: k, code, windowsVirtualKeyCode: vk, modifiers });
    await send("Input.dispatchKeyEvent", { type: "keyUp", key: k, code, modifiers });
    await settle();
  };
  // The phone layout has applied the visual viewport (useViewportVars in app.tsx): --app-h and --app-top match it.
  const viewportApplied = `(() => { const s = getComputedStyle(document.documentElement); return s.getPropertyValue('--app-h').trim() === Math.round(visualViewport.height) + 'px' && s.getPropertyValue('--app-top').trim() === Math.max(0, Math.round(visualViewport.offsetTop)) + 'px'; })()`;

  async function openScreen(s, w, h) {
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: true });
    await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
    const ready = s.ready ?? "!!document.querySelector('.m-app')";
    await navigate(`${origin}/${s.url}`, ready);
    if (s.storage) {
      await evaluate(`(() => { const s = ${JSON.stringify(s.storage)}; for (const k in s) localStorage.setItem(k, s[k]); })()`);
      await navigate(`${origin}/${s.url}`, ready);
    }
    if (s.files) {
      await send("DOM.enable");
      const doc = await send("DOM.getDocument", {});
      const q = await send("DOM.querySelector", { nodeId: doc.result.root.nodeId, selector: s.files });
      await send("DOM.setFileInputFiles", { nodeId: q.result.nodeId, files: [join(profile, "shot.png"), join(profile, "build-output-with-a-very-long-name-that-does-not-fit.log")] });
      // The mock uploads in four steps of 120 ms.
      await waitFor(`document.querySelectorAll('.att-chip.done').length >= 2`, { soft: true });
      await settle();
    }
    if (s.openTools) {
      // V0.41: the run's steps are folded into one line first
      await evaluate(`document.querySelector('.tl.fold')?.click()`);
      await settle();
      await evaluate(`document.querySelector('.tl.grp')?.click()`);
      await settle();
      await evaluate(`(() => { const rows = document.querySelectorAll('.tl.row'); rows[rows.length - 1]?.click(); })()`);
      await waitFor(`!!document.querySelector('.tlout')`, { soft: true });
      await evaluate(`document.querySelector('.tlout')?.scrollIntoView({ block: 'nearest' })`);
    }
    if (s.wholeBranch) {
      // V0.65 opens on New since you looked when there is a look; this screen checks the whole branch (V0.66).
      await evaluate(`[...document.querySelectorAll('.chg-mode')].find((b) => b.textContent.startsWith('Whole'))?.click()`);
      await waitFor(`document.querySelector('.chg-mode.on')?.textContent.startsWith('Whole')`, { what: "Whole branch pressed" });
      await settle();
    }
    if (s.openDiff) {
      // The file with the longest path: its row must ellipsize, its long lines scroll inside the diff.
      if (typeof s.openDiff === "string") await evaluate(`document.querySelector('.chg-file[title="${s.openDiff}"]')?.click()`);
      else await evaluate(`[...document.querySelectorAll('.chg-file')].sort((a, b) => b.title.length - a.title.length)[0]?.click()`);
      await waitFor(`document.querySelectorAll('.chg-diff .dl').length >= 5`, { soft: true });
      await settle();
    }
    const sheetItem = (word) => `[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('${word}'))`;
    if (s.sheet === "snooze" || s.sheet === "handover") {
      const word = s.sheet === "snooze" ? "Snooze" : "Hand over";
      await tapCenter(".m-tr1 .b44:last-child", `!!${sheetItem(word)}`);
      await evaluate(`${sheetItem(word)}?.click()`);
      await waitFor(`!${sheetItem(word)} && !!document.querySelector('.m-sheet')`, { what: `the ${s.sheet} sheet` });
    } else if (s.sheet === "details") {
      await tapCenter(".m-chipsd", "!!document.querySelector('.m-sheet')");
    } else if (s.sheet === "project" || s.sheet === "newproject") {
      await tapCenter(".m-pjb", "!!document.querySelector('.m-sheet .pli.new')");
      if (s.sheet === "newproject") await tapCenter(".m-sheet .pli.new", "!!document.querySelector('.m-sheet .pform input')");
    } else if (s.sheet === "folder") {
      await tapCenter(".m-new .m-fchg", "!!document.querySelector('.fsheet .fsec.browse .fpr')");
    } else if (s.sheet === "merge") {
      await tapCenter(".prv-merge", "!!document.querySelector('.dlg .df .btn.p')");
    } else if (s.sheet === "confirm") {
      await evaluate(`[...document.querySelectorAll('.offb .btn')].find((b) => b.textContent.includes('Set the Acme token'))?.click()`);
      await waitFor(`!!document.querySelector('.m-sheet.cfm')`, { what: "the confirmation sheet" });
    } else if (s.sheet === "confirm-delete") {
      await evaluate(`document.querySelector('.pdel .btn')?.scrollIntoView({ block: 'center' })`);
      await tapCenter(".pdel .btn", "!!document.querySelector('.m-sheet.cfm')");
    } else if (s.sheet === "actions") {
      await tapCenter("#m-sec-needs .m-swp .m-mv", "!!document.querySelector('.m-sheet')", { long: true });
    }
    await settle();
  }

  const shot = async (name) => process.env.FYR_SHOTS && writeFileSync(join(process.env.FYR_SHOTS, name), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  const calibrate = (rate) => calibrateWith(evaluate, rate);
  return { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached: t.attached, shot, calibrate };
}

// The phone screens at one size.
async function phonePass(t, w, h, screens = SCREENS) {
  const { send, evaluate, waitFor, frames, settle, openScreen, viewportApplied } = harness(t);
  for (const s of screens) {
    const ctx = `${s.name} ${w}x${h}`;
    try {
      await openScreen(s, w, h);
      // 1. At rest.
      assertLayout(await evaluate(MEASURE), `${ctx} at rest`);
      const outside = await evaluate(OUTSIDE);
      check(outside.length === 0, `${ctx}: text outside its parent (${JSON.stringify(outside)})`);
      const words = await evaluate(PROFILE_WORDS);
      check(words.length === 0, `${ctx}: the UI says profile (${JSON.stringify(words)})`);
      if (process.env.FYR_SHOTS && s.name.startsWith("ended changes") && w === 390) {
        await evaluate(`(() => { const t = document.querySelector('.chtab'); t.scrollTop = t.scrollHeight; })()`); // the last cards and the diff
        await settle();
        writeFileSync(join(process.env.FYR_SHOTS, "changes-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
      }
      if (process.env.FYR_SHOTS && s.name.endsWith("(V0.101)") && w === 390) writeFileSync(join(process.env.FYR_SHOTS, s.name.startsWith("inbox") ? "inbox-phone.png" : "changes-tab-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
      if (process.env.FYR_SHOTS && s.name.startsWith("board of changes") && w === 390) writeFileSync(join(process.env.FYR_SHOTS, "board-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
      if (process.env.FYR_SHOTS && s.name.startsWith("settings providers") && w === 390) { // V0.111
        await evaluate(`document.querySelector('section[aria-label="System agents"]')?.scrollIntoView({ block: 'center' })`); // V0.115
        await settle();
        writeFileSync(join(process.env.FYR_SHOTS, "systemagents-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
      }
      if (process.env.FYR_SHOTS && s.sheet === "confirm" && w === 390) writeFileSync(join(process.env.FYR_SHOTS, "confirm-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
      if (s.sheet === "handover") {
        const ho = await evaluate(`({ rung: document.querySelector('.m-sheet .ho-rung')?.textContent ?? '', btn: [...document.querySelectorAll('.m-sheet .df .btn')].map(b => b.textContent.trim()) })`);
        check(!!ho.rung && ho.btn.includes("Hand over"), `${ctx}: the handover sheet has no rung line or no Hand over button ${JSON.stringify(ho)}`);
      }

      // 2. Keyboard: focus the field, shrink the visual viewport (iOS also scrolls the layout viewport).
      if (!s.sheet || s.field) {
        if (s.openOther) {
          await evaluate(`[...document.querySelectorAll('.m-op')].find(b => b.textContent.startsWith('Other'))?.click()`);
          await waitFor(`!!document.querySelector('.m-otherf')`, { what: "the Other field" });
          await settle();
        }
        const field = s.openOther ? ".m-otherf" : s.field;
        if (field) {
          await evaluate(`document.querySelector(${JSON.stringify(field)})?.focus()`);
          const kh = Math.round(h * 0.6);
          await evaluate(`window.__vv.h = ${kh}; window.__vv.top = ${Math.round(h * 0.15)}; visualViewport.dispatchEvent(new Event('resize')); visualViewport.dispatchEvent(new Event('scroll'))`);
          await waitFor(`${viewportApplied} && document.documentElement.classList.contains('kb-open')`, { soft: true });
          await frames(); // the app scrolls the focused field into view on the next frame
          assertLayout(await evaluate(MEASURE), `${ctx} keyboard`, { keyboard: true });
          await evaluate(`document.activeElement?.blur(); window.__vv.h = null; window.__vv.top = 0; visualViewport.dispatchEvent(new Event('resize'))`);
          await waitFor(`${viewportApplied} && !document.documentElement.classList.contains('kb-open')`, { soft: true });
          await settle();
        }
      }

      // 3. Resume: hidden, the window size changes (the visualViewport fires nothing, as when iOS
      // brings the app back), visible again. Then the same with the size restored.
      for (const rh of [Math.round(h * 0.85), h]) {
        await evaluate(`window.__setHidden(true)`);
        await send("Emulation.setDeviceMetricsOverride", { width: w, height: rh, deviceScaleFactor: 1, mobile: true });
        await waitFor(`innerHeight === ${rh}`, { what: `the window to be ${rh} px tall` });
        await evaluate(`window.__setHidden(false); window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`);
        await frames(); // the app re-reads the size on the next frames after a resume
        await waitFor(viewportApplied, { soft: true });
        await settle();
        assertLayout(await evaluate(MEASURE), `${ctx} after resume at ${w}x${rh}`);
      }
    } catch (e) {
      failures.push(`${ctx}: ${e.message}`);
    }
  }
}

console.log(`insets: Emulation.setSafeAreaInsetsOverride (top ${INSETS.top}, bottom ${INSETS.bottom}); keyboard and resume: scripted visualViewport`);

// The jobs: one per phone screen and size, and one per section below. FYR_LAYOUT_TABS workers (default 8) take
// them in turn, each in its own tab and browser context; a job starts with the origin's storage cleared. The test waits
// on the page far more than it computes (the machine is about 30 % busy with 3 tabs), so tabs side by side are the
// speed-up. The timed sections (V0.38, V0.32) run after the pool has drained, alone, so the CPU they measure is
// shared with no other tab; their budgets also scale with a calibration taken in the same tab around each attempt.
const jobs = [];
const timedJobs = [];
// A section: one block of desktop or phone checks. A desktop section starts at 1280 × 800 without touch; one that
// needs another size sets it.
function section(name, opts, fn) {
  if (typeof opts === "function") [fn, opts] = [opts, {}];
  (opts.timed ? timedJobs : jobs).push({ name, run: async (t) => {
    await t.send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await t.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
    await fn(harness(t));
  } });
}
async function closeTab(t) {
  tabsBySession.delete(t.sessionId);
  await sendRaw("Target.closeTarget", { targetId: t.targetId });
  await sendRaw("Target.disposeBrowserContext", { browserContextId: t.browserContextId });
}
const TABS = Math.max(1, Number(process.env.FYR_LAYOUT_TABS) || 8);
const jobTimes = [];
// A worker keeps its tab: a new window per job costs Chrome's browser process more than the job (measured: 218
// windows kept it at 92 % of a core and made the run slower than three tabs). Between jobs the tab goes to
// about:blank and its origin's storage (localStorage, IndexedDB, caches, service workers) is cleared, so a job still
// starts from nothing stored.
async function resetTab(t) {
  await t.send("Emulation.setCPUThrottlingRate", { rate: 1 });
  const blank = t.next("Page.loadEventFired", 20_000);
  await t.send("Page.navigate", { url: "about:blank" });
  await blank;
  await t.send("Storage.clearDataForOrigin", { origin, storageTypes: "all" });
  t.attached.length = 0;
}
async function runJobs(list, width) {
  // FYR_LAYOUT_ONLY=<regex>: only the sections whose name matches (to work on one; a full run sets nothing)
  if (process.env.FYR_LAYOUT_ONLY) list = list.filter((j) => new RegExp(process.env.FYR_LAYOUT_ONLY).test(j.name));
  let next = 0;
  await Promise.all(Array.from({ length: Math.min(width, list.length) }, async () => {
    const t = await newTab();
    try {
      while (next < list.length) {
        const job = list[next++];
        const t0 = Date.now();
        try {
          await resetTab(t);
          await job.run(t);
        } catch (e) {
          failures.push(`${job.name}: ${e.message}`);
        }
        jobTimes.push([Date.now() - t0, job.name]);
      }
    } finally {
      await closeTab(t);
    }
  }));
}

// Calibration of the timing budgets (#50). A budget in wall time also measures the machine: a laptop busy with other
// sessions or a shared CI runner makes the same code slower. So a timed section runs a fixed workload in the same page
// under its own CPU throttle, right before and right after its measurement: short tasks that each build, lay out and
// drop a block of message-like DOM, then run a fixed loop of script. The mean time of one task against
// CALIBRATION_REFERENCE (the same workload on an idle machine) is the machine's speed factor, and each budget of the
// section is multiplied by it, at least 1 (the budgets as written) and at most CALIBRATION_CEILING. The ceiling keeps
// the V0.38 budget under 2.5 s of long tasks: a 100 ms blocking loop in message rendering gives about 4.5 s of them
// during the burst even on an idle machine, so it fails at any load. Load comes and goes within seconds, so a section
// that misses its scaled budget runs again, up to TIMED_ATTEMPTS times, with a fresh calibration; a real regression
// misses every attempt. The factor and the scaled budgets are printed. FYR_LAYOUT_FACTOR=<n> sets the factor by hand
// (1: the budgets as written).
// The reference is the lowest per-task time seen on an 8-core laptop at a load average of 50 to 80 (16 Nov), no idle
// window being available: the true idle time is at most this, so the factor errs low (strict), never lenient. To
// re-measure, run `make test-timing` on an idle machine and take the means printed by the calibration lines.
const CALIBRATION_REFERENCE = { 1: 12, 4: 26 }; // ms, one workload task at CPU throttle 1 and 4
const CALIBRATION_CEILING = 10;
const TIMED_ATTEMPTS = 3;
const calibrateWith = (evaluate, rate) =>
  evaluate(`new Promise((done) => {
    const box = document.createElement('div');
    box.style.cssText = 'position:absolute;left:-20000px;top:0;width:600px;height:4000px;overflow:hidden;contain:strict';
    document.body.appendChild(box);
    const text = 'Calibration text of a message with a few words, some of them long enough to wrap the line. '.repeat(3);
    const once = () => {
      const t0 = performance.now();
      let h = '';
      for (let i = 0; i < 150; i++) h += '<div class="md"><p><b>Message ' + i + '</b> ' + text + '</p><ul><li>one</li><li>two</li></ul></div>';
      box.innerHTML = h;
      let y = 0;
      for (const p of box.querySelectorAll('p')) y += p.getBoundingClientRect().height > 0 ? 1 : 0;
      box.textContent = '';
      let x = 0;
      for (let i = 0; i < 300000; i++) x = (x * 31 + i) % 1000003;
      return performance.now() - t0 + (x < 0 || y < 0 ? 1 : 0);
    };
    // 7 to 20 tasks, at most about 2 s: enough samples on a busy machine without making a quiet run slow.
    const runs = [];
    const start = performance.now();
    const step = () => {
      runs.push(Math.round(once() * 10) / 10);
      if (runs.length < 7 || (runs.length < 20 && performance.now() - start < 2000)) setTimeout(step, 10);
      else { box.remove(); done(runs); }
    };
    setTimeout(step, 10);
  })`);
// The budget factor from the calibration runs around one attempt of a section.
const speedFactor = (rate, label, runs) => {
  const mean = runs.reduce((a, b) => a + b, 0) / runs.length;
  const forced = Number(process.env.FYR_LAYOUT_FACTOR);
  const measured = mean / CALIBRATION_REFERENCE[rate];
  const factor = forced > 0 ? forced : Math.min(CALIBRATION_CEILING, Math.max(1, measured));
  console.log(`calibration ${label}: ${runs.length} workload tasks at CPU throttle ${rate}x, mean ${mean.toFixed(1)} ms (reference ${CALIBRATION_REFERENCE[rate]} ms; ${runs.join(" ")}), speed factor ${measured.toFixed(2)}, budget factor ${factor.toFixed(2)}${forced > 0 ? " (FYR_LAYOUT_FACTOR)" : ""}`);
  return factor;
};

// U8: settle the open thread from its ⋯ menu at 390 × 844; the inbox shows with the Undo toast.
section('U8', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await openScreen({ url: "?mock=rail&open=t_triage" }, 390, 844);
  await tapCenter(".m-tr1 .b44:last-child", "!!document.querySelector('.m-sheet .m-mi')");
  await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('Settle'))?.click()`);
  await waitFor(`!!document.querySelector('.m-strip') && /Undo/.test(document.querySelector('.m-toast')?.textContent ?? '')`, { soft: true });
  const r = await evaluate(`({ inbox: !!document.querySelector('.m-strip'), toast: document.querySelector('.m-toast')?.textContent ?? '', url: location.search })`);
  check(r.inbox, `U8 settle from the thread: the inbox does not show (${r.url})`);
  check(/Settled/.test(r.toast) && /Undo/.test(r.toast), `U8 settle from the thread: no Undo toast on the inbox ("${r.toast}")`);
  // Undo reopens the thread.
  await evaluate(`document.querySelector('.m-toast .tb')?.click()`);
  await waitFor(`!!document.querySelector('.m-tth') && /open=t_triage/.test(location.search)`, { soft: true });
  const u = await evaluate(`({ thread: !!document.querySelector('.m-tth'), url: location.search })`);
  check(u.thread && /open=t_triage/.test(u.url), `U8 Undo does not reopen the thread (${u.url})`);
} catch (e) {
  failures.push(`U8 settle from the thread: ${e.message}`);
}
});

// V0.29: a draft survives a reload (a deploy reloads the page); clearing it removes it.
section('V0.29', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  const typeIn = (v) => evaluate(`(() => { const el = document.querySelector('.m-cmp .in'); el.value = ${JSON.stringify(v)}; el.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  await openScreen({ url: "?mock=rail&open=t_triage" }, 390, 844);
  await typeIn("half a sentence");
  await waitFor(`localStorage.getItem('fyr.draft.t_triage') === 'half a sentence'`, { soft: true });
  await openScreen({ url: "?mock=rail&open=t_triage" }, 390, 844);
  const v = await evaluate(`document.querySelector('.m-cmp .in')?.value ?? ''`);
  check(v === "half a sentence", `V0.29 draft: lost after a reload ("${v}")`);
  await typeIn("");
  await waitFor(`localStorage.getItem('fyr.draft.t_triage') === null`, { soft: true });
} catch (e) {
  failures.push(`V0.29 draft: ${e.message}`);
}
});

// V0.128 (supersedes V0.49, V0.57): a message typed on the View tab is an ordinary send; the thread then shows the
// Conversation tab, scrolled to the end where the new message is. Phone and desktop.
for (const [name, w, h, mobile, field, button, tabs] of [
  ["phone", 390, 844, true, ".m-cmp .in", ".m-send[aria-label=Send]", ".m-tabs button"],
  ["desktop", 1280, 800, false, ".th #composer", ".th .btn.p[title^=Send]", ".th .tabs [role=tab]"],
]) section(`V0.128 ${name}`, async ({ evaluate, waitFor, settle, navigate, openScreen }) => {
  try {
    if (mobile) await openScreen({ url: "?mock=1&open=t_view" }, w, h);
    else await navigate(`${origin}/?mock=1&open=t_view`, "!!document.querySelector('.th #composer')");
    const onTab = `[...document.querySelectorAll('${tabs}')].find((x) => x.classList.contains('on'))?.textContent.trim()`;
    check((await evaluate(onTab)) === "View", `V0.128 ${name}: the thread did not open on View`);
    await evaluate(`(() => { const el = document.querySelector('${field}'); el.focus(); el.value = 'Can you also check iPad?'; el.dispatchEvent(new Event('input', { bubbles: true })); })()`);
    await waitFor(`!document.querySelector('${button}')?.disabled`, { what: "Send enabled" });
    await evaluate(`document.querySelector('${button}').click()`);
    await waitFor(`${onTab} === 'Conversation' && [...document.querySelectorAll('.convo .you')].some((y) => y.textContent.includes('Can you also check iPad?'))`, { soft: true });
    await settle();
    const r = await evaluate(`(() => {
      const c = document.querySelector('.convo'), you = [...document.querySelectorAll('.convo .you')].filter((y) => y.textContent.includes('Can you also check iPad?'));
      const m = you[0]?.getBoundingClientRect(), b = c?.getBoundingClientRect();
      return { tab: ${onTab}, mine: you.length, vtag: you.some((y) => y.classList.contains('vmsg')),
        atEnd: c ? c.scrollHeight - c.scrollTop - c.clientHeight < 24 : false, visible: !!m && !!b && m.top >= b.top - 0.5 && m.bottom <= b.bottom + 0.5,
        field: document.querySelector('${field}')?.value ?? null };
    })()`);
    check(r.tab === "Conversation", `V0.128 ${name}: after a send on the View tab the ${r.tab} tab is shown, not Conversation`);
    check(r.mine === 1 && !r.vtag, `V0.128 ${name}: the message is not one ordinary message in the conversation ${JSON.stringify(r)}`);
    check(r.atEnd && r.visible, `V0.128 ${name}: the conversation is not scrolled to the new message ${JSON.stringify(r)}`);
    check(r.field === "", `V0.128 ${name}: the field kept the text ${JSON.stringify(r)}`);
  } catch (e) {
    failures.push(`V0.128 ${name}: ${e.message}`);
  }
});

// V0.79: on the phone, `/` at the start of the composer lists the commands above it, inside the screen, rows 44 px
// tall; a tap fills `/name ` and the argument hint shows above the field.
section('V0.79', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  const typeIn = (v) => evaluate(`(() => { const el = document.querySelector('.m-cmp .in'); el.focus(); el.value = ${JSON.stringify(v)}; el.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  await openScreen({ url: "?mock=rail&open=t_triage" }, 360, 640);
  await typeIn("/re");
  await waitFor(`document.querySelectorAll('.slash.m .slash-i').length > 0`, { what: "the / list" });
  await settle();
  const r = await evaluate(`(() => {
    const l = document.querySelector('.slash.m').getBoundingClientRect();
    const c = document.querySelector('.m-cmp').getBoundingClientRect();
    const rows = [...document.querySelectorAll('.slash.m .slash-i')];
    return { top: l.top, bottom: l.bottom, left: l.left, right: l.right, cmp: c.top, vw: innerWidth, vh: innerHeight,
      names: rows.map((x) => x.querySelector('.slash-n').textContent), minH: Math.min(...rows.map((x) => x.getBoundingClientRect().height)),
      wide: document.documentElement.scrollWidth > innerWidth };
  })()`);
  check(r.names[0] === "/research" && r.names.includes("/review") && r.names.includes("/security-review"), `V0.79 phone: the list for "/re" is ${JSON.stringify(r.names)}`);
  check(r.top >= 0 && r.left >= 0 && r.right <= r.vw + 0.5 && r.bottom <= r.cmp + 0.5 && !r.wide, `V0.79 phone: the list is not inside the screen above the composer ${JSON.stringify(r)}`);
  check(r.minH >= 44, `V0.79 phone: a row is ${r.minH} px tall`);
  await tapCenter(".slash.m .slash-i", "document.querySelector('.m-cmp .in').value === '/research '");
  const h = await evaluate(`({ list: !!document.querySelector('.slash.m'), hint: document.querySelector('.slash-hint')?.textContent ?? '' })`);
  check(!h.list && /\/research\s+<question>/.test(h.hint), `V0.79 phone: after the tap the list stays or the hint is "${h.hint}"`);
  await typeIn("");
} catch (e) {
  failures.push(`V0.79 phone / list: ${e.message}`);
}
});

// V0.143: an agent that does not respond says so above the composer, since when and why, with Restart; the same
// line on desktop and phone (one component), the button inside the screen and 44 px tall on the phone.
for (const [front, w, h] of [["desktop", 1280, 800], ["phone", 390, 844]]) section(`V0.143 ${front}`, async ({ send, evaluate, navigate, openScreen }) => {
try {
  const row = front === "phone" ? ".m-app .m-status.stuck" : ".th .sys.live.stuck";
  if (front === "desktop") {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=rail&open=t_stuck`, `!!document.querySelector('${row}')`);
  } else await openScreen({ url: "?mock=rail&open=t_stuck", ready: `!!document.querySelector('${row}')` }, w, h);
  const r = await evaluate(`(() => {
    const el = document.querySelector('${row}'), btn = el?.querySelector('button'), b = btn?.getBoundingClientRect(), e = el?.getBoundingClientRect();
    return { text: el?.querySelector('.ell')?.textContent ?? '', btn: btn?.textContent ?? '', disabled: !!btn?.disabled, spin: !!el?.querySelector('.spin'),
      box: b ? { left: b.left, right: b.right, top: b.top, bottom: b.bottom, h: b.height } : null, row: e ? { right: e.right } : null, vw: innerWidth, vh: innerHeight };
  })()`);
  check(/^Not responding since \d\d:\d\d · it did not take a message/.test(r.text), `V0.143 ${front}: the status line says "${r.text}"`);
  check(r.btn === "Restart agent" && !r.disabled && !r.spin, `V0.143 ${front}: no enabled Restart, or it still spins (${JSON.stringify(r)})`);
  check(!!r.box && r.box.left >= 0 && r.box.right <= r.vw + 0.5 && r.box.bottom <= r.vh + 0.5 && r.row.right <= r.vw + 0.5, `V0.143 ${front}: Restart or its line is out of the screen ${JSON.stringify(r)}`);
  if (front === "phone") check(r.box.h >= 44, `V0.143 phone: Restart is ${r.box.h} px tall, under 44`);
} catch (e) {
  failures.push(`V0.143 ${front}: ${e.message}`);
}
});

// V0.187: an entry of the conversation clips what leaves its box (content-visibility, V0.41's list work), so a message's
// Copy has to lie inside its entry to be seen. On the desktop it shows on hover, under the message.
section("V0.187 message copy", async ({ send, evaluate, navigate }) => {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&runs=1&open=t_run`, "!!document.querySelector('.msgw>.msgcp')");
  // Headless Chrome reports (hover: none), so the desktop's rules are in force only once the touch ones are gone; the Run
  // cards of this mock would lie over the message.
  const at = await evaluate(`(() => {
    for (const sheet of document.styleSheets) for (let i = sheet.cssRules.length - 1; i >= 0; i--) {
      const rule = sheet.cssRules[i];
      if (rule instanceof CSSMediaRule && /hover:\\s*none/.test(rule.conditionText)) sheet.deleteRule(i);
    }
    document.head.appendChild(document.createElement('style')).textContent = '.runcs{display:none}';
    const w = [...document.querySelectorAll('.msgw')].filter((m) => m.querySelector(':scope>.msgcp')).pop();
    w.scrollIntoView({ block: "center" });
    const b = w.getBoundingClientRect();
    return { x: b.left + b.width / 2, y: b.top + b.height / 2 };
  })()`);
  await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: at.x, y: at.y });
  await new Promise((r) => setTimeout(r, 250));
  const r = await evaluate(`(() => {
    const w = [...document.querySelectorAll('.msgw')].filter((m) => m.querySelector(':scope>.msgcp')).pop();
    const c = w.querySelector(':scope>.msgcp'), b = c.getBoundingClientRect(), e = c.closest('.convo-in>*').getBoundingClientRect(), m = w.getBoundingClientRect();
    return { opacity: getComputedStyle(c).opacity, inEntry: b.top >= e.top - 0.5 && b.bottom <= e.bottom + 0.5, under: b.top >= m.bottom - 0.5,
      hit: document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2) === c, gap: e.bottom - m.bottom };
  })()`);
  check(r.opacity === "1", `V0.187: Copy does not show on hover ${JSON.stringify(r)}`);
  check(r.inEntry && r.hit, `V0.187: the entry clips the message's Copy ${JSON.stringify(r)}`);
  check(r.under, `V0.187: Copy covers the message ${JSON.stringify(r)}`);
});

// V0.149: a command the agent wants the owner to run is a Run card under the conversation, the same component on the
// desktop and the phone: the full command wraps (no sideways scroll), a Nix store path is shortened until pressed, the
// buttons are inside the screen and 44 px tall on the phone; a `! cmd` block in a message is a Copy-only card.
for (const [front, w, h] of [["desktop", 1280, 800], ["phone", 390, 844]]) section(`V0.149 ${front}`, async ({ send, evaluate, navigate, openScreen, tapCenter, shot }) => {
try {
  const url = "?mock=rail&runs=1&open=t_run";
  const ready = "document.querySelectorAll('.runcs .runc').length === 2 && !!document.querySelector('.runc.prose')";
  if (front === "desktop") {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/${url}`, `${ready}`);
  } else await openScreen({ url, ready }, w, h);
  const r = await evaluate(`(() => {
    const cards = [...document.querySelectorAll('.runc')];
    const cmds = [...document.querySelectorAll('.runc-cmd')];
    const btns = [...document.querySelectorAll('.runc-b button')].filter((b) => b.getBoundingClientRect().height > 0);
    const first = document.querySelector('.runcs .runc');
    return {
      vw: innerWidth, wide: document.documentElement.scrollWidth > innerWidth,
      sideways: cmds.filter((c) => c.scrollWidth > c.clientWidth + 1).length,
      outside: cards.filter((c) => { const b = c.getBoundingClientRect(); return b.width > 0 && (b.left < -0.5 || b.right > innerWidth + 0.5); }).length,
      minH: Math.min(...btns.map((b) => b.getBoundingClientRect().height)),
      title: first?.querySelector('.runc-t')?.textContent ?? '', why: first?.querySelector('.runc-why')?.textContent ?? '',
      cmd: first?.querySelector('.runc-cmd')?.textContent ?? '', labels: [...first.querySelectorAll('.runc-b button')].map((b) => b.textContent),
      second: [...document.querySelectorAll('.runcs .runc')][1]?.querySelector('.runc-res')?.textContent ?? '',
      prose: document.querySelector('.runc.prose .runc-cmd')?.textContent ?? '', proseBtns: [...document.querySelectorAll('.runc.prose button')].map((b) => b.textContent),
      header: [...document.querySelectorAll('.offb button')].map((b) => b.textContent),
    };
  })()`);
  check(!r.wide && r.sideways === 0 && r.outside === 0, `V0.149 ${front}: a command scrolls sideways or a card leaves the screen ${JSON.stringify(r)}`);
  check(r.title === "Run on desk" && r.why === "Mount the SD card and mint the badge onto it", `V0.149 ${front}: the card's title or why ${JSON.stringify(r)}`);
  check(r.cmd.includes("/nix/store/prc4…-nfc-mint") && r.cmd.startsWith("udisksctl mount -b /dev/sdc1"), `V0.149 ${front}: the command is not shortened in the middle: ${r.cmd}`);
  check(r.labels.join("|") === "Run as root|Copy|Not now", `V0.149 ${front}: the buttons are ${r.labels.join("|")}`);
  check(r.second.startsWith("Failed (exit 1)"), `V0.149 ${front}: the failed run says "${r.second}"`);
  check(r.prose.startsWith("sudo /nix/store/prc4…-") && r.proseBtns.join("|") === "Copy", `V0.149 ${front}: the prose card ${JSON.stringify(r)}`);
  check(!r.header.some((x) => /Run/.test(x)), `V0.149 ${front}: a run request shows as a header button ${JSON.stringify(r.header)}`);
  if (front === "phone") check(r.minH >= 44, `V0.149 phone: a button is ${r.minH} px tall, under 44`);
  await shot(`runcard-${front}.png`);
  // A press on the command shows the full store path.
  if (front === "phone") await tapCenter(".runcs .runc .runc-cmd", "document.querySelector('.runcs .runc .runc-cmd').textContent.includes('prc4lv5v5kppd7rgz3h2fh9wq3n4zj9y')");
  else await evaluate("document.querySelector('.runcs .runc .runc-cmd').click()");
  const full = await evaluate("({ t: document.querySelector('.runcs .runc .runc-cmd').textContent, side: document.querySelector('.runcs .runc .runc-cmd').scrollWidth > document.querySelector('.runcs .runc .runc-cmd').clientWidth + 1 })");
  check(full.t.includes("prc4lv5v5kppd7rgz3h2fh9wq3n4zj9y") && !full.side, `V0.149 ${front}: the press does not show the full command, or it scrolls ${JSON.stringify(full)}`);
  // Run sends the press with the digest the card showed.
  await evaluate("document.querySelector('.runcs .runc .runc-b .btn.p').click()");
  const sent = await evaluate("(window.__fyrCommands || []).filter((c) => c.action === 'action.run').map((c) => c.args)");
  check(sent.length === 1 && sent[0].on === "thread" && sent[0].action === "run-1" && sent[0].digest === "d-run-1", `V0.149 ${front}: Run sent ${JSON.stringify(sent)}`);
  // No polkit agent: Run as root is off and the card says why.
  if (front === "desktop") await navigate(`${origin}/${url}&rootprompt=no`, ready);
  else await openScreen({ url: `${url}&rootprompt=no`, ready }, w, h);
  const no = await evaluate("({ off: document.querySelector('.runcs .runc .runc-b button').disabled, line: document.querySelector('.runcs .runc .runc-root')?.textContent ?? '' })");
  check(no.off && /^No password prompt can show on desk: no polkit authentication agent/.test(no.line), `V0.149 ${front}: without a polkit agent ${JSON.stringify(no)}`);
  await shot(`runcard-${front}-nopolkit.png`);
} catch (e) {
  failures.push(`V0.149 ${front}: ${e.message}`);
}
});

// V0.33: on the phone, a message the agent has not read waits after "Agent is here", dimmed, with
// Waiting and a Cancel that fits the screen; Cancel strikes it through with Send again.
section('V0.33', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await openScreen({ url: "?mock=rail&open=t_mapperf" }, 390, 844);
  await waitFor(`!!document.querySelector('.m-app .youw.pend .dlb') && !!document.querySelector('.m-app .here')`, { soft: true });
  const r = await evaluate(`(() => {
    const here = document.querySelector('.m-app .here');
    const pend = [...document.querySelectorAll('.m-app .youw.pend')];
    const last = pend[pend.length - 1];
    const btn = last?.querySelector('.dlb');
    const b = btn?.getBoundingClientRect();
    const hereAbove = !!here && !!last && (here.compareDocumentPosition(last) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;
    const read = [...document.querySelectorAll('.m-app .youw:not(.pend) .dl')].map((x) => x.textContent);
    return { here: here?.textContent ?? '', pend: pend.length, hereAbove, word: last?.querySelector('.dl span')?.textContent ?? '', btn: btn?.textContent ?? '',
      box: b ? { left: b.left, right: b.right, top: b.top, bottom: b.bottom, h: b.height } : null, vw: innerWidth, vh: visualViewport.height,
      cmp: document.querySelector('.m-cmp')?.getBoundingClientRect().top ?? 0, opacity: last ? getComputedStyle(last.querySelector('.you')).opacity : '', read };
  })()`);
  check(/Agent is here/.test(r.here) && r.pend === 1 && r.hereAbove, `V0.33 phone: no pending message after "Agent is here" (${JSON.stringify(r)})`);
  check(r.word === "Waiting" && r.btn === "Cancel", `V0.33 phone: the pending message does not say Waiting with Cancel ("${r.word}", "${r.btn}")`);
  check(Number(r.opacity) < 1, `V0.33 phone: the pending message is not dimmed (opacity ${r.opacity})`);
  check(!!r.box && r.box.left >= 0 && r.box.right <= r.vw + 0.5 && r.box.bottom <= r.cmp + 0.5 && r.box.h >= 36, `V0.33 phone: Cancel is out of reach ${JSON.stringify(r.box)} (composer at ${r.cmp})`);
  check(r.read.length === 1 && r.read[0] === "Read", `V0.33 phone: only the last read message says Read (${JSON.stringify(r.read)})`);
  await tapCenter(".m-app .youw.pend .dlb", "!!document.querySelector('.m-app .youw.gone')");
  const c = await evaluate(`(() => { const g = document.querySelector('.m-app .youw.gone'); return { pend: document.querySelectorAll('.m-app .youw.pend').length, here: !!document.querySelector('.m-app .here'), word: g?.querySelector('.dl span')?.textContent ?? '', again: g?.querySelector('.dlb')?.textContent ?? '', strike: g ? getComputedStyle(g.querySelector('.you')).textDecorationLine : '' }; })()`);
  check(c.pend === 0 && !c.here, `V0.33 phone: after Cancel the message still waits (${JSON.stringify(c)})`);
  check(c.word === "Cancelled" && c.again === "Send again" && /line-through/.test(c.strike), `V0.33 phone: a cancelled message is not struck through with Send again (${JSON.stringify(c)})`);
} catch (e) {
  failures.push(`V0.33 phone delivery: ${e.message}`);
}
});

// V0.24: a message that fails to render shows an error card; the thread, its composer and the
// rest of the conversation stay. (Found live: one bad field blanked the whole client.)
section('V0.24', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await openScreen({ url: "?mock=rail&open=t_mapperf&crash=1" }, 390, 844);
  await waitFor(`!!document.querySelector('.crash')`, { soft: true });
  const r = await evaluate(`({ card: document.querySelector('.crash')?.textContent ?? '', header: !!document.querySelector('.m-tth'), composer: !!document.querySelector('.m-cmp'), body: document.body.innerText.length })`);
  check(/could not be shown/.test(r.card), `V0.24 crash card: no error card ("${r.card}")`);
  check(r.header && r.composer, `V0.24 crash card: the thread did not stay (header ${r.header}, composer ${r.composer})`);
  check(r.body > 200, `V0.24 crash card: the page is nearly empty (${r.body} characters)`);
} catch (e) {
  failures.push(`V0.24 crash card: ${e.message}`);
}
});

// SPEC 0.5 V0.26: on the desktop, a long tool group shows the last 3 calls behind "+N more tools",
// and the thread ⋯ menu moves the thread to a project from the keyboard.
section('V0.26', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&open=t_mapperf`, "!!document.querySelector('.th .tl.fold')");
  // V0.41: the finished run is one folded line; its steps show after one tap
  const f = await evaluate(`({ folds: document.querySelectorAll('.th .tl.fold').length, grp: document.querySelectorAll('.th .tl.grp').length, out: [...document.querySelectorAll('.th .cv.out .cl')].pop()?.textContent ?? '' })`);
  check(f.folds === 1 && f.grp === 1, `V0.41 desktop: expected one folded line and only the live step open (${JSON.stringify(f)})`);
  check(/run that test alone/.test(f.out), `V0.41 desktop: the live run's newest message is not an outcome ("${f.out}")`);
  await evaluate(`document.querySelector('.th .tl.fold')?.click()`);
  await settle();
  await evaluate(`document.querySelector('.th .tl.grp')?.click()`);
  await waitFor(`!!document.querySelector('.th .tl.more')`, { soft: true });
  const g = await evaluate(`({ more: document.querySelector('.th .tl.more')?.textContent ?? '', rows: document.querySelectorAll('.th .tl.row').length })`);
  check(/\+5 more tools/.test(g.more), `V0.26 desktop: no "+5 more tools" row in the 8-call group ("${g.more}")`);
  check(g.rows === 4, `V0.26 desktop: the opened group shows ${g.rows} rows, expected the more row and 3 calls`);
  await evaluate(`document.querySelector('.th .tl.more')?.click()`);
  await waitFor(`!document.querySelector('.th .tl.more')`, { soft: true });
  const g2 = await evaluate(`({ more: !!document.querySelector('.th .tl.more'), rows: document.querySelectorAll('.th .tl.row').length })`);
  check(!g2.more && g2.rows === 8, `V0.26 desktop: "+N more tools" does not show every call (${g2.rows} rows)`);

  await navigate(`${origin}/?mock=rail&open=t_triage`, "!!document.querySelector('.thh .tmenu-b')");
  await evaluate(`document.querySelector('.thh .tmenu-b')?.focus()`);
  await key("Enter");
  const m = await evaluate(`({ menu: !!document.querySelector('.thh [role=menu]'), focused: document.activeElement?.textContent ?? '' })`);
  check(m.menu, "V0.26 desktop: the thread ⋯ menu does not open from the keyboard");
  check(/Project…/.test(m.focused), `V0.26 desktop: the ⋯ menu does not focus "Project…" ("${m.focused}")`);
  await key("Enter");
  const d = await evaluate(`({ dlg: !!document.querySelector('.dlg .plist'), focus: document.activeElement?.classList.contains('pq') })`);
  check(d.dlg && d.focus, "V0.26 desktop: Project… does not open the project picker with the search focused");
  await send("Input.insertText", { text: "garden" }); // V0.61: Enter picks a project by its exact name (any case)
  await settle();
  await key("Enter");
  await waitFor(`!document.querySelector('.dlg') && /Garden/.test(document.querySelector('.thh .chip.proj')?.textContent ?? '')`, { soft: true });
  const r = await evaluate(`({ dlg: !!document.querySelector('.dlg'), chip: document.querySelector('.thh .chip.proj')?.textContent ?? '' })`);
  check(!r.dlg && /Garden/.test(r.chip), `V0.26, V0.61 desktop: Enter on an exact project name does not move the thread there (chip "${r.chip}")`);
} catch (e) {
  failures.push(`V0.26 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.79: on the desktop, `/` in the composer opens the list; ArrowDown moves, Enter picks and does not
// send; Escape closes the list and leaves the text.
section('V0.79', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&open=t_triage`, "!!document.querySelector('#composer')");
  await evaluate(`document.querySelector('#composer').focus()`);
  await send("Input.insertText", { text: "/" });
  await waitFor(`document.querySelectorAll('.cmpw .slash .slash-i').length > 0`, { what: "the / list" });
  const l = await evaluate(`({ names: [...document.querySelectorAll('.cmpw .slash .slash-n')].map((x) => x.textContent), on: document.querySelector('.cmpw .slash .slash-i.on .slash-n')?.textContent ?? '' })`);
  check(l.names.length === 8 && l.on === "/compact", `V0.79 desktop: the "/" list is ${JSON.stringify(l)}`);
  await key("ArrowDown");
  const on = await evaluate(`document.querySelector('.cmpw .slash .slash-i.on .slash-n')?.textContent ?? ''`);
  check(on === "/context", `V0.79 desktop: ArrowDown moved to "${on}"`);
  await key("Enter");
  const p = await evaluate(`({ v: document.querySelector('#composer').value, list: !!document.querySelector('.cmpw .slash') })`);
  check(p.v === "/context " && !p.list, `V0.79 desktop: Enter did not pick /context (${JSON.stringify(p)})`);
  await evaluate(`(() => { const el = document.querySelector('#composer'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  await send("Input.insertText", { text: "/rev" });
  await waitFor(`!!document.querySelector('.cmpw .slash')`, { what: "the / list for /rev" });
  await key("Escape");
  const x = await evaluate(`({ v: document.querySelector('#composer').value, list: !!document.querySelector('.cmpw .slash'), focus: document.activeElement?.id ?? '' })`);
  check(x.v === "/rev" && !x.list && x.focus === "composer", `V0.79 desktop: Escape did not just close the list (${JSON.stringify(x)})`);
  await evaluate(`(() => { const el = document.querySelector('#composer'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); })()`);
} catch (e) {
  failures.push(`V0.79 desktop / list: ${e.message}`);
}
});

// SPEC 0.5 V0.42: on the desktop, the new thread's Change opens the folder picker from the keyboard; a typed path
// completes with Tab, Enter opens the folder, Enter again picks it; Esc closes only the picker.
section('V0.42', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`);
  await evaluate(`localStorage.setItem("fyr.lastProject", "p_web"); localStorage.setItem("fyr.projectFilter", ""); true`);
  await navigate(`${origin}/?mock=rail&dialog=new`, "!!document.querySelector('.dlg .fline .ell')");
  const before = await evaluate(`document.querySelector('.dlg .fline .ell')?.textContent ?? ''`);
  check(before === "~/src/acme/web", `V0.42 desktop: a new thread in acme/web does not start in its folder ("${before}")`);
  await evaluate(`[...document.querySelectorAll('.dlg .fline .btn')].find(b => b.textContent.trim() === 'Change')?.focus()`);
  await key("Enter");
  await waitFor(`!!document.querySelector('.fpick .fsec h4')`, { soft: true });
  const o = await evaluate(`({ picker: !!document.querySelector('.fpick'), focus: document.activeElement?.classList.contains('pq') && !!document.activeElement.closest('.fpick'), first: document.querySelector('.fpick .fsec h4')?.textContent ?? '' })`);
  check(o.picker && o.focus, `V0.42 desktop: Change does not open the folder picker with its field focused (${JSON.stringify(o)})`);
  check(o.first === "Recent", `V0.55 desktop: the picker offers a project folder that is already picked ("${o.first}")`);
  // acme and acme-platform-… share "acme": Tab completes to the common start; Enter opens the exact name.
  await send("Input.insertText", { text: "~/src/ac" });
  await settle();
  await key("Tab");
  await waitFor(`document.querySelector('.fpick .pq')?.value === '~/src/acme'`, { soft: true });
  const v = await evaluate(`document.querySelector('.fpick .pq')?.value ?? ''`);
  check(v === "~/src/acme", `V0.42 desktop: Tab does not complete the common start ("${v}")`);
  await key("Enter");
  await waitFor(`document.querySelector('.fpick .pq')?.value === '~/src/acme/'`, { soft: true });
  await settle();
  const v2 = await evaluate(`document.querySelector('.fpick .pq')?.value ?? ''`);
  check(v2 === "~/src/acme/", `V0.42 desktop: Enter does not open the exact folder ("${v2}")`);
  await send("Input.insertText", { text: "o" });
  await settle();
  await key("Tab");
  await waitFor(`document.querySelector('.fpick .pq')?.value === '~/src/acme/ops/'`, { soft: true });
  const v3 = await evaluate(`document.querySelector('.fpick .pq')?.value ?? ''`);
  check(v3 === "~/src/acme/ops/", `V0.42 desktop: Tab does not complete the one match ("${v3}")`);
  await settle();
  await key("Enter");
  await waitFor(`!document.querySelector('.fpick')`, { soft: true });
  const r = await evaluate(`({ picker: !!document.querySelector('.fpick'), dlg: !!document.querySelector('.dlg'), folder: document.querySelector('.dlg .fline .ell')?.textContent ?? '' })`);
  check(!r.picker && r.dlg && r.folder === "~/src/acme/ops", `V0.42 desktop: Enter does not pick the typed folder (${JSON.stringify(r)})`);
  await evaluate(`[...document.querySelectorAll('.dlg .fline .btn')].find(b => b.textContent.trim() === 'Change')?.focus()`);
  await key("Enter");
  await waitFor(`!!document.querySelector('.fpick .fpr')`, { soft: true });
  await key("ArrowDown");
  const row = await evaluate(`document.activeElement?.classList.contains('fpr') ?? false`);
  check(row, "V0.42 desktop: ArrowDown does not reach the first folder row");
  await key("Escape");
  const e2 = await evaluate(`({ picker: !!document.querySelector('.fpick'), dlg: !!document.querySelector('.dlg') })`);
  check(!e2.picker && e2.dlg, `V0.42 desktop: Esc does not close only the picker (${JSON.stringify(e2)})`);
  await key("Escape");
} catch (e) {
  failures.push(`V0.42 desktop folder picker: ${e.message}`);
}
});

// V0.43: on the desktop, "Ask the agent to fix this check" posts the owner's message to the thread.
section('V0.43', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=1&open=t_block&tab=pr`, "!!document.querySelector('.th .prv-ask')");
  const before = await evaluate(`({ ask: document.querySelector('.th .prv-ask')?.textContent ?? '', status: document.querySelector('.th .prv-st')?.textContent ?? '' })`);
  check(before.ask === "Ask the agent to fix this check", `V0.43 desktop: no "Ask the agent to fix this check" on the failed check ("${before.ask}")`);
  check(/^Blocked: /.test(before.status), `V0.43 desktop: status line "${before.status}"`);
  await evaluate(`document.querySelector('.th .prv-ask')?.click()`);
  await waitFor(`/Sent to the agent/.test(document.querySelector('.th .prv-note')?.textContent ?? '')`, { soft: true });
  const note = await evaluate(`document.querySelector('.th .prv-note')?.textContent ?? ''`);
  check(/Sent to the agent: fix lint/.test(note), `V0.43 desktop: no confirmation after asking ("${note}")`);
  await evaluate(`[...document.querySelectorAll('.th .tabs [role=tab]')].find(e => e.textContent === 'Conversation')?.click()`);
  await waitFor(`[...document.querySelectorAll('.th .you')].some(e => e.textContent.includes('The check lint failed'))`, { soft: true });
  const posted = await evaluate(`[...document.querySelectorAll('.th .you')].map(e => e.textContent).find(t => t.includes('The check lint failed')) ?? ''`);
  check(/The check lint failed: https:\/\/github\.com\/.*\. Find the cause and fix it\./.test(posted), `V0.43 desktop: the owner's message is not in the conversation ("${posted.slice(0, 120)}")`);
} catch (e) {
  failures.push(`V0.43 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.45: the thread ⋯ menu offers Hand over…; the dialog says how the context goes over, and a
// confirmed handover leaves a divider that names both profiles.
section('V0.45', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await navigate(`${origin}/?mock=rail&open=t_triage`, "!!document.querySelector('.thh .tmenu-b')");
  await evaluate(`document.querySelector('.thh .tmenu-b')?.click()`);
  await waitFor(`!!document.querySelector('.thh [role=menuitem]')`, { soft: true });
  const it = await evaluate(`[...document.querySelectorAll('.thh [role=menuitem]')].map(b => b.textContent)`);
  check(it.some((x) => /Hand over…/.test(x)), `V0.45 desktop: no Hand over… in the ⋯ menu (${JSON.stringify(it)})`);
  await evaluate(`[...document.querySelectorAll('.thh [role=menuitem]')].find(b => /Hand over/.test(b.textContent))?.click()`);
  await waitFor(`!!document.querySelector('.dlg .ho-rung b') && !!document.querySelector('.dlg select')?.selectedOptions[0]`, { soft: true });
  const d = await evaluate(`({ dlg: !!document.querySelector('.dlg .ho-rung'), rung: document.querySelector('.dlg .ho-rung b')?.textContent ?? '', to: document.querySelector('.dlg select')?.selectedOptions[0]?.textContent ?? '' })`);
  check(d.dlg && /Full history|reads the old conversation|writes a handover/.test(d.rung) && d.to !== "", `V0.45 desktop: the dialog does not show the target and the rung (${JSON.stringify(d)})`);
  const hw = await evaluate(PROFILE_WORDS);
  check(hw.length === 0, `V0.50 desktop: the handover dialog says profile (${JSON.stringify(hw)})`);
  await evaluate(`[...document.querySelectorAll('.dlg .df .btn')].find(b => b.textContent.trim() === 'Hand over')?.click()`);
  await waitFor(`!document.querySelector('.dlg') && !!document.querySelector('.th .hodiv')`, { soft: true });
  const r = await evaluate(`({ dlg: !!document.querySelector('.dlg'), div: document.querySelector('.th .hodiv')?.textContent ?? '' })`);
  check(!r.dlg && r.div.startsWith("Handed over from ") && r.div.includes(` to ${d.to}`), `V0.45 desktop: no divider after the handover (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.45 desktop: ${e.message}`);
}
});

// SPEC V0.119 (amends V0.50): the gear at the bottom of the desktop rail opens Settings: the nav with four pages on the
// left, Providers open with its three providers; the dialog keeps one size on every page.
const COMMANDS = (action) => `(window.__fyrCommands ?? []).filter((c) => c.action === ${JSON.stringify(action)})`;
section('V0.119 settings', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .rgear')");
  const g = await evaluate(`(() => { const b = document.querySelector('.rail .rgear'); if (!b) return null; const r = b.getBoundingClientRect(), f = document.querySelector('.rail .foot').getBoundingClientRect(); b.click(); return { below: r.top > window.innerHeight / 2, aboveFoot: r.bottom <= f.top + 0.5 }; })()`);
  check(!!g && g.below && g.aboveFoot, `V0.50 desktop: no gear at the bottom of the rail (${JSON.stringify(g)})`);
  await waitFor(`document.querySelectorAll('.dlg .prow').length === 3`, { soft: true });
  await settle();
  const RECT = `(() => { const r = document.querySelector('.dlg.settings')?.getBoundingClientRect(); return r ? [Math.round(r.width), Math.round(r.height)].join('x') : ''; })()`;
  const d = await evaluate(`({ title: document.querySelector('.dlg .dh b')?.textContent ?? '', nav: [...document.querySelectorAll('.dlg .stn-i .stn-t')].map(e => e.textContent), on: document.querySelector('.dlg .stn-i[aria-current=page]')?.dataset.page ?? '', rows: document.querySelectorAll('.dlg .prow').length, size: ${RECT} })`);
  check(d.title === "Settings" && d.nav.join("|") === "Providers|Automation|Projects|Hosts|Devices|This device" && d.on === "providers" && d.rows === 3, `V0.119 desktop: Settings is wrong (${JSON.stringify(d)})`);
  // SPEC V0.115: System agents: a provider and a model for all jobs, a choice per job ("Same as default"), and what each job runs on.
  await waitFor(`document.querySelectorAll('.dlg section[aria-label="System agents"] select').length >= 4`, { soft: true });
  const sa = await evaluate(`(() => { const el = document.querySelector('.dlg section[aria-label="System agents"]'); if (!el) return null;
    return { selects: [...el.querySelectorAll('select')].map(s => s.getAttribute('aria-label')), lines: [...el.querySelectorAll('.sajob .sr-d')].map(e => e.textContent),
      model: el.querySelector('select[aria-label="Model"]')?.selectedOptions[0]?.textContent ?? '', same: el.querySelector('select[aria-label="Thread titles"]')?.selectedOptions[0]?.textContent ?? '' }; })()`);
  check(!!sa && sa.selects.join("|") === "Provider|Model|Thread titles|Project setup" && sa.model === "Haiku (claude-haiku-5-5)" && sa.same === "Same as default" && sa.lines.length === 2 && sa.lines.every((l) => l === "Runs on Claude · claude-haiku-5-5"),
    `V0.115 desktop: the System agents section is wrong (${JSON.stringify(sa)})`);
  await shot("settings-providers-desktop.png");
  // The same size on all four pages; each page shows its heading.
  const sizes = {};
  for (const page of ["automation", "projects", "hosts", "devices", "device", "providers"]) {
    await evaluate(`document.querySelector('.dlg .stn-i[data-page=${page}]').click()`);
    await waitFor(`document.querySelector('.dlg .stp')?.dataset.page === '${page}'`, { soft: true });
    await settle();
    sizes[page] = await evaluate(RECT);
    if (page !== "providers") await shot(`settings-${page}-desktop.png`);
  }
  check(new Set(Object.values(sizes)).size === 1 && sizes.providers === d.size, `V0.119 desktop: the dialog changes size between pages (${JSON.stringify({ first: d.size, ...sizes })})`);
  // ↓ in the nav moves to the next page; the page follows the focus.
  await evaluate(`document.querySelector('.dlg .stn-i[data-page=providers]').focus()`);
  await key("ArrowDown");
  const kn = await evaluate(`({ page: document.querySelector('.dlg .stp')?.dataset.page, focus: document.activeElement?.dataset?.page })`);
  check(kn.page === "automation" && kn.focus === "automation", `V0.119 desktop: ↓ in the nav does not move to Automation (${JSON.stringify(kn)})`);
  // A provider is a sub-page; its models are rows, not a text box; no key is in the page.
  await evaluate(`document.querySelector('.dlg .stn-i[data-page=providers]').click()`);
  await waitFor(`document.querySelectorAll('.dlg .prow').length === 3`, { soft: true });
  await evaluate(`document.querySelector('.dlg .prow[data-sub=claude]').click()`);
  await waitFor(`document.querySelectorAll('.dlg .model').length === 3`, { soft: true });
  await settle();
  const o = await evaluate(`({ ta: !!document.querySelector('.dlg .stp textarea'), keys: [...document.querySelectorAll('.dlg input[type=password]')].map(i => i.value), crumb: document.querySelector('.dlg .crumb')?.textContent ?? '', title: document.querySelector('.dlg .stp-h h2')?.textContent ?? '', models: [...document.querySelectorAll('.dlg .model .sr-t')].map(e => e.textContent), star: document.querySelector('.dlg .model .star[aria-pressed=true]')?.closest('.model')?.querySelector('.sr-t')?.textContent ?? '' })`);
  check(!o.ta && o.keys.every((k) => k === "") && /Providers/.test(o.crumb) && o.title === "Claude" && o.models.join("|") === "Opus 5.5|Sonnet 5.5|Haiku 5.5" && o.star === "Opus 5.5", `V0.119 desktop: the provider page is wrong (${JSON.stringify(o)})`);
  await shot("settings-provider-desktop.png");
  // Reorder from the keyboard: the second model's grip, Alt+↑, one provider.update with the two ids swapped; the focus stays on the moved row's grip.
  await evaluate(`document.querySelectorAll('.dlg .model .grip')[1].focus()`);
  await key("ArrowUp", "ArrowUp", 1);
  await waitFor(`${COMMANDS("provider.update")}.length === 1 && document.querySelector('.dlg .model .sr-t')?.textContent === 'Sonnet 5.5'`, { soft: true });
  const ro = await evaluate(`({ cmds: ${COMMANDS("provider.update")}.map(c => c.args), focus: document.activeElement?.getAttribute('aria-label') ?? '' })`);
  check(ro.cmds.length === 1 && JSON.stringify(ro.cmds[0].models) === JSON.stringify(["claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5"]) && ro.focus === "Move Sonnet 5.5, 1 of 3", `V0.119 desktop: Alt+↑ on a grip does not move the model (${JSON.stringify(ro)})`);
  // Esc on a sub-page goes to its list and focuses the row it came from; Esc again closes Settings.
  await key("Escape");
  await waitFor(`document.querySelector('.dlg .stp')?.dataset.page === 'providers'`, { soft: true });
  const esc = await evaluate(`({ page: document.querySelector('.dlg .stp')?.dataset.page, focus: document.activeElement?.dataset?.sub ?? '' })`);
  check(esc.page === "providers" && esc.focus === "claude", `V0.119 desktop: Esc on a provider does not go back to its row (${JSON.stringify(esc)})`);
  await key("Escape");
  check(await evaluate(`!document.querySelector('.dlg.settings')`), "V0.119 desktop: Esc on a page does not close Settings");
} catch (e) {
  failures.push(`V0.119 desktop: ${e.message}`);
}
});

// SPEC V0.119, V0.24: Settings closes every way, on every page, at desktop heights and on the phone. The owner could
// not close it (17 Nov): a host that never updated itself (a NixOS host) sent "updates": null, the Automation page threw
// on it, and the error left the app unable to render, so Close, Esc and the backdrop did nothing. The mock's second host
// (&hosts=2) sends that null. Desktop at 700 and 900 px: open Settings at Automation (both hosts' Updates show, no crash
// card), then close it by the × (a real mouse click at its centre, which must be the ×), Esc, and a press on the
// backdrop; each time the dialog is gone and the gear opens it again. Back is not a way on the desktop: V0.145 (5) keeps
// dialogs out of the history. Phone: Automation is a screen; its Back goes to the Settings list, and the browser's back
// from there leaves Settings.
for (const [w, hh, phone] of [[1280, 700, false], [1280, 900, false], [390, 844, true]]) section(`V0.119 settings closes ${phone ? "phone" : `desktop ${hh}`}`, async (h) => {
  const { send, evaluate, waitFor, settle, navigate, tapCenter } = h;
  const ctx = `V0.119 settings closes ${phone ? "phone" : `desktop ${w}x${hh}`}`;
try {
  // Both hosts' Updates: desk's list and the null host's line (it runs nix-d8b9560), and no crash card.
  const UPD = `/Fyr runs c705ef536566/.test(document.body.textContent) && /Fyr runs nix-d8b9560/.test(document.body.textContent) && !document.querySelector('.crash')`;
  if (phone) {
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: true });
    await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
    await navigate(`${origin}/?mock=rail&hosts=2&settings=automation`, "!!document.querySelector('.m-settings .stp')");
    await waitFor(UPD, { soft: true });
    check(await evaluate(UPD), `${ctx}: Automation does not show both hosts' Updates without a crash (${await evaluate("[...document.querySelectorAll('.crash')].map((e) => e.textContent).join(' | ')")})`);
    await tapCenter(".m-settings .m-nh .b44[aria-label=Back]", "!document.querySelector('.m-settings .stp')");
    await waitFor("!!document.querySelector('.m-settings .stn') && !document.querySelector('.m-settings .stp')", { soft: true });
    check(await evaluate("!!document.querySelector('.m-settings .stn') && !document.querySelector('.m-settings .stp')"), `${ctx}: Back on Automation does not go to the Settings list`);
    await evaluate("history.back()");
    await waitFor("!document.querySelector('.m-settings') && !!document.querySelector('.m-app')", { soft: true });
    check(await evaluate("!document.querySelector('.m-settings')"), `${ctx}: back from the Settings list does not leave Settings`);
    return;
  }
  await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&hosts=2`, "!!document.querySelector('.rail .rgear')");
  // A page that throws can leave the app re-rendering without end, so a step never returns: the closes have 60 s.
  await Promise.race([closes(), sleep(60_000).then(() => { throw new Error("Settings did not close within 60 s (the page stopped answering)"); })]);
  async function closes() {
  const open = async (how) => {
    await evaluate("document.querySelector('.rail .rgear').click()");
    await waitFor("!!document.querySelector('.dlg.settings .stn-i[data-page=automation]')", { soft: true });
    await evaluate("document.querySelector('.dlg.settings .stn-i[data-page=automation]')?.click()");
    await waitFor(`document.querySelector('.dlg .stp')?.dataset.page === 'automation' && ${UPD}`, { soft: true });
    await settle();
    const s = await evaluate(`({ open: !!document.querySelector('.dlg.settings'), page: document.querySelector('.dlg .stp')?.dataset.page ?? null, updates: ${UPD}, runs: document.querySelector('.dlg .stp')?.textContent.match(/Fyr runs \\S+/g) ?? [], crash: [...document.querySelectorAll('.crash')].map((e) => e.textContent) })`);
    check(s.open && s.page === "automation" && s.updates && !s.crash.length, `${ctx}: Settings at Automation before closing by ${how} (${JSON.stringify(s)})`);
  };
  const mouse = async (x, y) => {
    await send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y });
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", buttons: 1, clickCount: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", buttons: 0, clickCount: 1 });
  };
  const gone = async (how) => {
    await waitFor("!document.querySelector('.dlg.settings')", { soft: true });
    check(await evaluate("!document.querySelector('.dlg.settings')"), `${ctx}: ${how} does not close Settings`);
  };
  // The ×: in the window, and the element at its centre is the × (nothing covers it).
  await open("the ×");
  const x = await evaluate(`(() => { const b = document.querySelector('.dlg.settings .dh .x'); if (!b) return null; const r = b.getBoundingClientRect(), cx = r.left + r.width / 2, cy = r.top + r.height / 2, top = document.elementFromPoint(cx, cy); return { c: [cx, cy], inView: r.top >= 0 && r.bottom <= innerHeight && r.left >= 0 && r.right <= innerWidth, hit: !!top && (top === b || b.contains(top)), top: top?.className ?? null }; })()`);
  check(!!x && x.inView && x.hit, `${ctx}: the × is out of view or covered (${JSON.stringify(x)})`);
  if (x) await mouse(x.c[0], x.c[1]);
  await gone("the ×");
  await open("Esc");
  await h.key("Escape");
  await gone("Esc");
  // The backdrop: a press beside the dialog, in the scrim.
  await open("the backdrop");
  const bd = await evaluate(`(() => { const r = document.querySelector('.dlg.settings').getBoundingClientRect(); const p = [Math.max(8, r.left / 2), innerHeight - 8]; return { p, scrim: document.elementFromPoint(p[0], p[1])?.classList.contains('scrim') ?? false }; })()`);
  check(bd.scrim, `${ctx}: the point beside the dialog is not the backdrop (${JSON.stringify(bd)})`);
  await mouse(bd.p[0], bd.p[1]);
  await gone("a press on the backdrop");
  // And it opens again.
  await open("reopening");
  }
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// SPEC V0.119 (3): save at once. A colour swatch on a project's page sends one project.update with the colour and no
// name, and "Saved" shows beside the title for 2 s.
section('V0.119 (3)', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await navigate(`${origin}/?mock=rail&dialog=settings&page=projects/p_web`, "!!document.querySelector('.dlg .pedit .sw')");
  await settle();
  const pg = await evaluate(`({ save: [...document.querySelectorAll('.dlg .pedit button')].some(b => b.textContent.trim() === 'Save'), groups: [...document.querySelectorAll('.dlg .pedit > .sg > .sg-h .sg-t')].map(e => e.textContent), title: document.querySelector('.dlg .stp-h h2')?.textContent })`);
  check(!pg.save && pg.groups.join("|") === "Identity|Threads|Quiet hours" && pg.title === "acme/web", `V0.119 desktop: the project page (${JSON.stringify(pg)})`);
  await shot("settings-project-desktop.png");
  await evaluate(`document.querySelector('.dlg .pedit .sw:not(.on)').click()`);
  await waitFor(`${COMMANDS("project.update")}.length >= 1 && !!document.querySelector('.dlg .stp-h .saved')`, { soft: true });
  const c = await evaluate(`({ cmds: ${COMMANDS("project.update")}.map(c => Object.keys(c.args).sort().join(',')), saved: !!document.querySelector('.dlg .stp-h .saved') })`);
  check(c.cmds.length === 1 && c.cmds[0] === "color,id" && c.saved, `V0.119 desktop: a swatch does not save its colour alone at once (${JSON.stringify(c)})`);
  await sleep(2300);
  check(await evaluate(`!document.querySelector('.dlg .stp-h .saved')`), "V0.119 desktop: Saved still shows after 2 s");
} catch (e) {
  failures.push(`V0.119 desktop save at once: ${e.message}`);
}
});

// SPEC V0.119 (5): the rail's bell opens Settings on This device.
section('V0.119 (5)', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  // The bell shows once the push check resolves (it starts as unsupported, hidden), so wait for it, not the rail.
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .bell')");
  const bell = await evaluate(`(() => { const b = document.querySelector('.rail .bell'); b?.click(); return !!b; })()`);
  await waitFor(`!!document.querySelector('.dlg.settings')`, { soft: true });
  const b = await evaluate(`document.querySelector('.dlg .stn-i[aria-current=page]')?.dataset.page ?? ''`);
  check(bell && b === "device", `V0.119 desktop: the bell does not open Settings on This device (bell ${bell}, page "${b}")`);
} catch (e) {
  failures.push(`V0.119 desktop bell: ${e.message}`);
}
});

// SPEC V0.119: every page in the dark theme renders (screenshots only).
section('V0.119', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
  for (const page of ["providers", "providers/claude", "providers/new", "automation", "projects", "projects/p_fyr", "hosts", "devices", "device"]) {
    await navigate(`${origin}/?mock=rail&dialog=settings&page=${page}`, "!!document.querySelector('.dlg.settings .stp-h h2')");
    await settle();
    await shot(`settings-${page.replace("/", "-")}-dark-desktop.png`);
  }
} catch (e) {
  failures.push(`V0.119 desktop dark: ${e.message}`);
} finally {
  await send("Emulation.setEmulatedMedia", { features: [] });
}
});

// SPEC V0.119 (4), phone: the Reorder mode shows ↑ and ↓ of 44 px and no × or star; ↓ on the first row swaps it with
// the second (one provider.update); Done brings the rows back. The phone's More sheet lost Notifications and Theme.
section('V0.119 (4), phone', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
  await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await navigate(`${origin}/?mock=rail&settings=providers/claude`, "document.querySelectorAll('.m-settings .model').length === 3");
  await settle();
  await tapCenter(".m-settings .lnk.reo", "!!document.querySelector('.m-settings .model .mv')");
  const r = await evaluate(`({ mv: [...document.querySelectorAll('.m-settings .model .mv .xb')].map(b => Math.round(b.getBoundingClientRect().height)), others: document.querySelectorAll('.m-settings .model [aria-label^="Remove"], .m-settings .model .star, .m-settings .model [role=switch]').length })`);
  check(r.mv.length === 6 && r.mv.every((h) => h >= 44) && r.others === 0, `V0.119 phone: the Reorder mode (${JSON.stringify(r)})`);
  await shot("settings-reorder-phone.png");
  await tapCenter(".m-settings .model .mv .xb[aria-label='Move Opus 5.5 down']", `${COMMANDS("provider.update")}.length === 1`);
  await waitFor(`document.querySelector('.m-settings .model .sr-t')?.textContent === 'Sonnet 5.5'`, { soft: true });
  const sw = await evaluate(`${COMMANDS("provider.update")}.map(c => c.args.models)`);
  check(sw.length === 1 && JSON.stringify(sw[0]) === JSON.stringify(["claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5"]), `V0.119 phone: ↓ on the first model does not swap it (${JSON.stringify(sw)})`);
  await tapCenter(".m-settings .sg-h .btn.p", "!document.querySelector('.m-settings .model .mv')");
  // V0.120: an On row ends in its switch (× is only on a hand-typed model that is off).
  check(await evaluate(`document.querySelectorAll('.m-settings .model [role=switch]').length === 3`), "V0.119 phone: Done does not bring the rows back");

  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.m-ih [aria-label=More]')");
  await tapCenter(".m-ih [aria-label=More]", "!!document.querySelector('.m-sheet .m-mi')");
  await waitFor(`/needs a login/.test([...document.querySelectorAll('.m-sheet .m-mi')].find((b) => b.textContent.startsWith('Settings'))?.textContent ?? '')`, { soft: true });
  const more = await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].map((b) => ({ label: b.querySelector('.ell')?.firstChild?.textContent ?? '', right: b.querySelector('.w')?.textContent ?? '' }))`);
  check(more.map((x) => x.label).join("|") === "Select threads|Changes|Settings|Filter by project…|Adopt from T3|Show tool details" && more.find((x) => x.label === "Settings")?.right === "1 needs a login", `V0.119 phone: the More sheet (${JSON.stringify(more)})`);
} catch (e) {
  failures.push(`V0.119 phone: ${e.message}`);
} finally {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
}
});

// SPEC V0.120: the model chooser. A provider's Models group has On (★, a switch, the default's locked) and Off (by
// name, New marked); a switch sends the whole on-list; Refresh sends provider.refreshModels; Gone and Added by hand
// show on DeepSeek; a provider with 40 models has a search that filters both parts and says "N of 40 match".
section('V0.120', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&dialog=settings&page=providers/claude`, "document.querySelectorAll('.dlg .model').length === 3 && document.querySelectorAll('.dlg .omodel').length === 3");
  const m = await evaluate(`({ subs: [...document.querySelectorAll('.dlg .models .sg-sub')].map(e => e.textContent), on: [...document.querySelectorAll('.dlg .model .sr-t')].map(e => e.textContent), off: [...document.querySelectorAll('.dlg .omodel .sr-t')].map(e => e.textContent),
    newTag: !!document.querySelector('.dlg .omodel[data-model="claude-fable-5-2"] .tag.new'), sw: document.querySelectorAll('.dlg .model [role=switch][aria-checked=true]').length, line: document.querySelector('.dlg .models .mchk')?.textContent ?? '' })`);
  check(m.subs.join("|") === "On · offered in every model picker, in this order|Off · 3" && m.on.join("|") === "Opus 5.5|Sonnet 5.5|Haiku 5.5" && m.off.join("|") === "Fable 5.2New|Haiku 5.4|Sonnet 5.4" && m.newTag && m.sw === 3 && /^Checked \d+ min ago$/.test(m.line),
    `V0.120 desktop: the Models group (${JSON.stringify(m)})`);
  await shot("models-desktop.png");
  // The default's switch is locked: pressing it says why on its row and sends nothing.
  await evaluate(`document.querySelector('.dlg .model[data-model="claude-opus-5-5"] [role=switch]').click()`);
  await waitFor(`/Star another model first/.test(document.querySelector('.dlg .model[data-model="claude-opus-5-5"]')?.textContent ?? '')`, { soft: true });
  const lk = await evaluate(`({ err: document.querySelector('.dlg .model[data-model="claude-opus-5-5"] .sr-d.err')?.textContent ?? '', cmds: ${COMMANDS("provider.update")}.length })`);
  check(lk.err === "Star another model first." && lk.cmds === 0, `V0.120 desktop: the default's switch is not locked (${JSON.stringify(lk)})`);
  // Turning Fable on puts it at the end of On.
  await evaluate(`document.querySelector('.dlg .omodel[data-model="claude-fable-5-2"] [role=switch]').click()`);
  await waitFor(`document.querySelectorAll('.dlg .model').length === 4`, { soft: true });
  const on = await evaluate(`({ cmds: ${COMMANDS("provider.update")}.map(c => c.args.models), last: [...document.querySelectorAll('.dlg .model')].pop()?.dataset.model })`);
  check(on.cmds.length === 1 && JSON.stringify(on.cmds[0]) === JSON.stringify(["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-2"]) && on.last === "claude-fable-5-2", `V0.120 desktop: a switch does not turn the model on at the end (${JSON.stringify(on)})`);
  await evaluate(`[...document.querySelectorAll('.dlg .models .sg-h .lnk')].find(b => b.textContent === 'Refresh')?.click()`);
  await waitFor(`${COMMANDS("provider.refreshModels")}.length === 1`, { soft: true });
  const rf = await evaluate(`({ cmds: ${COMMANDS("provider.refreshModels")}.map(c => c.args.id), line: document.querySelector('.dlg .models .mchk')?.textContent ?? '' })`);
  check(rf.cmds.join() === "claude" && rf.line === "Checked just now", `V0.120 desktop: Refresh (${JSON.stringify(rf)})`);
  // DeepSeek: Gone on an on model, × only on the hand-typed model that is off.
  await navigate(`${origin}/?mock=rail&dialog=settings&page=providers/claudeDeepseek&providers=checkfail`, "document.querySelectorAll('.dlg .model').length === 3");
  const ds = await evaluate(`({ gone: !!document.querySelector('.dlg .model[data-model="deepseek-coder"] .tag.gone'), hand: !!document.querySelector('.dlg .omodel[data-model="my-finetune-v3"] .tag'), x: [...document.querySelectorAll('.dlg .models [aria-label^="Remove"]')].map(b => b.getAttribute('aria-label')), foot: document.querySelector('.dlg .models .sg-f.err')?.textContent ?? '', line: document.querySelector('.dlg .models .mchk')?.textContent ?? '' })`);
  check(ds.gone && ds.hand && ds.x.join() === "Remove my-finetune-v3" && /^Could not list the models: .*401\. The list below is the one from yesterday; it still works\.$/.test(ds.foot) && ds.line === "Last checked yesterday 14:02",
    `V0.120 desktop: DeepSeek's marks and failed check (${JSON.stringify(ds)})`);
  await shot("models-failed-desktop.png");
  // 40 models: search filters both parts; the header counts the matches; reorder is off while it has text.
  await navigate(`${origin}/?mock=rail&dialog=settings&page=providers/router&providers=many`, "!!document.querySelector('.dlg .models input[type=search]')");
  await evaluate(`document.querySelector('.dlg .models input[type=search]').focus()`);
  await send("Input.insertText", { text: "sonnet" });
  await settle();
  const sr = await evaluate(`({ line: document.querySelector('.dlg .models .mchk')?.textContent ?? '', rows: [...document.querySelectorAll('.dlg .models :is(.model,.omodel)')].map(r => r.dataset.model), more: document.querySelector('.dlg .models .more')?.textContent ?? '', grips: document.querySelectorAll('.dlg .models .grip').length })`);
  // While searching, Off shows three matches and "Show N more".
  const hidden = Number(sr.line.split(" ")[0]) - sr.rows.length;
  check(/^\d+ of 40 match$/.test(sr.line) && sr.rows.length > 0 && sr.rows.every((x) => x.includes("sonnet")) && (hidden === 0 ? sr.more === "" : sr.more === `Show ${hidden} more`) && sr.grips === 0, `V0.120 desktop: the search over 40 models (${JSON.stringify(sr)})`);
  await shot("models-search-desktop.png");
  // A thread on a model that is off: its chip and Hand over say "(off)".
  await navigate(`${origin}/?mock=rail&open=t_triage`, "!!document.querySelector('.thh .tmenu-b')");
  const chip = await evaluate(`[...document.querySelectorAll('.chip.m span')].map(e => e.textContent).join('|')`);
  await evaluate(`document.querySelector('.thh .tmenu-b')?.click()`);
  await waitFor(`!!document.querySelector('.thh [role=menuitem]')`, { soft: true });
  await evaluate(`[...document.querySelectorAll('.thh [role=menuitem]')].find(b => /Hand over/.test(b.textContent))?.click()`);
  await waitFor(`!!document.querySelector('.dlg .ho-from')`, { soft: true });
  const ho = await evaluate(`document.querySelector('.dlg .ho-from')?.textContent ?? ''`);
  check(/opus 5\.4 \(off\)/.test(chip) && /From Claude · opus 5\.4 \(off\)\./.test(ho), `V0.120 desktop: a model that is off ("${chip}", "${ho}")`);
  await shot("handover-off-desktop.png");
} catch (e) {
  failures.push(`V0.120 desktop: ${e.message}`);
}
});

// Found live 16 Nov, after the V0.120 deploy (&providers=live is shaped like the live rows): the Providers rows say
// how many of the reported models are on; a Claude login whose check only had Fyr's known list says so in the foot;
// OpenRouter's 380 models render with search, "Show N more" and Refresh, on the desktop and the phone.
for (const [w, h, phone] of [[1280, 800, false], [390, 844, true]]) section(`live providers ${w}`, async ({ send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, shot }) => {
  const at = phone ? ".m-settings" : ".dlg";
  const url = (page) => (phone ? `${origin}/?mock=rail&providers=live&settings=${page}` : `${origin}/?mock=rail&providers=live&dialog=settings&page=${page}`);
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: phone, maxTouchPoints: phone ? 5 : 0 });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: phone });
    await navigate(url("providers"), `document.querySelectorAll('${at} .prow').length === 3`);
    const rows = await evaluate(`[...document.querySelectorAll('${at} .prow .sr-d')].map(e => e.textContent)`);
    check(rows[0]?.includes("3 of 4 models on") && rows[2]?.includes("1 of 380 models on"), `live ${w}: the Providers rows (${JSON.stringify(rows)})`);
    await navigate(url("providers/claudeAlt"), `document.querySelectorAll('${at} .model').length === 3`);
    const c = await evaluate(`({ off: document.querySelectorAll('${at} .omodel').length, foot: document.querySelector('${at} .models .sg-f')?.textContent ?? '' })`);
    check(c.off === 1 && /Fyr's known list/.test(c.foot), `live ${w}: a Claude login on the known list (${JSON.stringify(c)})`);
    await navigate(url("providers/claudeDeepseek"), `document.querySelectorAll('${at} .omodel').length > 0`);
    const o = await evaluate(`({ on: document.querySelectorAll('${at} .model').length, off: document.querySelectorAll('${at} .omodel').length, sub: [...document.querySelectorAll('${at} .models .sg-sub')].map(e => e.textContent), more: document.querySelector('${at} .models .more')?.textContent ?? '', refresh: [...document.querySelectorAll('${at} .models .lnk')].some(b => b.textContent === 'Refresh'), search: document.querySelector('${at} .models input[type=search]')?.placeholder ?? '' })`);
    check(o.on === 1 && o.off === 8 && o.sub[1] === "Off · 379" && o.more === "Show 371 more" && o.refresh && o.search === "Search 380 models", `live ${w}: OpenRouter's 380 models (${JSON.stringify(o)})`);
    await evaluate(`document.querySelector('${at} .models input[type=search]').focus()`);
    await send("Input.insertText", { text: "anthropic" });
    await settle();
    const q = await evaluate(`({ line: document.querySelector('${at} .models .mchk')?.textContent ?? '', rows: [...document.querySelectorAll('${at} .models :is(.model,.omodel)')].map(r => r.dataset.model) })`);
    check(q.line === "38 of 380 match" && q.rows.length === 4 && q.rows.every((x) => x.startsWith("anthropic/")), `live ${w}: the search over 380 models (${JSON.stringify(q)})`);
    await shot(`live-openrouter-${phone ? "phone" : "desktop"}.png`);
  } catch (e) {
    failures.push(`live ${w}: ${e.message}`);
  }
});

// SPEC V0.121: one project order. Settings → Projects has a grip per row on the desktop; Alt+↑ sends
// front.projectOrder with the whole list; the inbox's project chips and New thread's project list follow at once.
section('V0.121', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.pfrow .pfc')");
  const chipsOf = `[...document.querySelectorAll('.pfrow .pfc')].map(b => b.title.split(':')[0])`;
  const before = await evaluate(chipsOf);
  await shot("chips-before-desktop.png");
  await navigate(`${origin}/?mock=rail&dialog=settings&page=projects`, "document.querySelectorAll('.dlg .pli.g .grip').length > 3");
  const rows = await evaluate(`[...document.querySelectorAll('.dlg .pli.g .plb')].map(b => b.dataset.sub)`);
  const names = await evaluate(`[...document.querySelectorAll('.dlg .pli.g .pln')].map(e => e.textContent)`);
  check(JSON.stringify(names) === JSON.stringify([...names].sort((a, b) => a.localeCompare(b))), `V0.121 desktop: with no order set, Settings → Projects is by name (${JSON.stringify(names)})`);
  await shot("projects-desktop.png");
  // Move the second chip's project above the first one's, from the keyboard (Alt+↑ until it passes it).
  const j = names.indexOf(before[1]);
  const k = names.indexOf(before[0]);
  check(j > k && k >= 0, `V0.121 desktop: the chips are not in the Settings order before a reorder (${JSON.stringify({ before, names })})`);
  await evaluate(`document.querySelectorAll('.dlg .pli.g .grip')[${j}].focus()`);
  for (let n = 0; n < j - k; n++) await key("ArrowUp", "ArrowUp", 1);
  await waitFor(`${COMMANDS("front.projectOrder")}.length === ${j - k}`, { soft: true });
  const want = [...rows];
  want.splice(k, 0, ...want.splice(j, 1));
  const po = await evaluate(`({ cmds: ${COMMANDS("front.projectOrder")}.map(c => c.args.ids), rows: [...document.querySelectorAll('.dlg .pli.g .plb')].map(b => b.dataset.sub), focus: document.activeElement?.getAttribute('aria-label') ?? '' })`);
  check(po.cmds.length === j - k && JSON.stringify(po.cmds.at(-1)) === JSON.stringify(want) && JSON.stringify(po.rows) === JSON.stringify(want) && po.focus.startsWith(`Move ${before[1]}, ${k + 1} of `),
    `V0.121 desktop: Alt+↑ on a project's grip (${JSON.stringify({ ...po, want })})`);
  await shot("projects-after-desktop.png");
  await key("Escape");
  await waitFor(`!document.querySelector('.dlg.settings')`, { soft: true });
  await settle();
  // The names in the new order; the chips and New thread's list show a part of it (projects with threads; no quiet
  // ones), in that order.
  const nameOf = Object.fromEntries(rows.map((id, i) => [id, names[i]]));
  const wantNames = want.map((id) => nameOf[id]);
  const inOrder = (xs) => xs.every((x, i) => i === 0 || wantNames.indexOf(xs[i - 1]) < wantNames.indexOf(x));
  const after = await evaluate(chipsOf);
  check(after.length === before.length && after.length > 1 && inOrder(after) && JSON.stringify(after) !== JSON.stringify(before), `V0.121 desktop: the chips do not follow the new order (${JSON.stringify({ before, after, wantNames })})`);
  await shot("chips-after-desktop.png");
  await key("c");
  await waitFor(`!!document.querySelector('.dlg .ppick')`, { soft: true });
  await evaluate(`document.querySelector('.dlg .ppick').click()`);
  await waitFor(`document.querySelectorAll('.dlg .plist .pli:not(.new):not(.none)').length > 1`, { soft: true });
  const nt = await evaluate(`[...document.querySelectorAll('.dlg .plist .pli:not(.new):not(.none) .pln')].map(e => e.textContent)`);
  check(nt.length > 1 && nt.every((n) => wantNames.includes(n)) && inOrder(nt), `V0.121 desktop: New thread's projects do not follow the new order (${JSON.stringify({ nt, wantNames })})`);
  await shot("new-thread-after-desktop.png");
} catch (e) {
  failures.push(`V0.121 desktop: ${e.message}`);
}
});

// SPEC V0.121, phone: Settings → Projects has Reorder beside New project; the mode shows ↑ and ↓ of 44 px in place
// of the thread count; ↓ on the first row sends front.projectOrder with the first two swapped; Done leaves the mode.
// The phone's chips follow a set order (&order=). The provider page shows both parts of the model chooser.
section('V0.121, phone', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
  await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await navigate(`${origin}/?mock=rail&settings=projects`, "document.querySelectorAll('.m-settings .pli').length > 3");
  await shot("projects-phone.png");
  const ids = await evaluate(`[...document.querySelectorAll('.m-settings .pli')].map(b => b.dataset.sub)`);
  await tapCenter(".m-settings .btn.reo", "!!document.querySelector('.m-settings .pli.mvrow .mv')");
  const r = await evaluate(`({ mv: [...document.querySelectorAll('.m-settings .pli.mvrow .mv .xb')].map(b => Math.round(b.getBoundingClientRect().height)), counts: document.querySelectorAll('.m-settings .pli .plc').length, done: !!document.querySelector('.m-settings .btn.p') })`);
  check(r.mv.length === ids.length * 2 && r.mv.every((h) => h >= 44) && r.counts === 0 && r.done, `V0.121 phone: the Reorder mode (${JSON.stringify(r)})`);
  await shot("projects-reorder-phone.png");
  const first = await evaluate(`document.querySelector('.m-settings .pli.mvrow .pln')?.textContent ?? ''`);
  await tapCenter(`.m-settings .pli.mvrow .mv .xb[aria-label="Move ${first} down"]`, `${COMMANDS("front.projectOrder")}.length === 1`);
  const sent = await evaluate(`${COMMANDS("front.projectOrder")}.map(c => c.args.ids)`);
  check(JSON.stringify(sent[0]) === JSON.stringify([ids[1], ids[0], ...ids.slice(2)]), `V0.121 phone: ↓ on the first project (${JSON.stringify({ sent, ids })})`);
  await tapCenter(".m-settings .btn.p", "!document.querySelector('.m-settings .pli.mvrow')");
  // The phone's chips in a set order.
  await navigate(`${origin}/?mock=rail&order=p_dot,p_home,p_web`, "!!document.querySelector('.m-screen .pfrow .pfc')");
  const pc = await evaluate(`[...document.querySelectorAll('.m-screen .pfrow .pfc')].slice(0, 3).map(b => b.title.split(':')[0])`);
  check(pc.join("|") === "dotfiles|Garden|acme/web", `V0.121 phone: the chips do not follow the order (${JSON.stringify(pc)})`);
  await shot("chips-after-phone.png");
  // The provider page on the phone: both parts, the switch 44 px wide at least as a row target.
  await navigate(`${origin}/?mock=rail&settings=providers/claude`, "document.querySelectorAll('.m-settings .model').length === 3 && document.querySelectorAll('.m-settings .omodel').length === 3");
  const pm = await evaluate(`({ subs: document.querySelectorAll('.m-settings .models .sg-sub').length, reo: !!document.querySelector('.m-settings .models .lnk.reo'), refresh: [...document.querySelectorAll('.m-settings .models .lnk')].some(b => b.textContent === 'Refresh') })`);
  check(pm.subs === 2 && pm.reo && pm.refresh, `V0.120 phone: the Models group (${JSON.stringify(pm)})`);
  await shot("models-phone.png");
  await navigate(`${origin}/?mock=rail&open=t_triage`, "!!document.querySelector('.m-app')");
  await shot("thread-off-phone.png");
} catch (e) {
  failures.push(`V0.121 phone: ${e.message}`);
} finally {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
}
});

// SPEC 0.5 V0.80: the rail's load line sits above the quota inside the rail and covers no other rail item; a hot
// working thread's row shows its CPU, coloured, inside its row. V0.99: the figure sits in a ring, the ring carries the
// level; V0.163: the heavy jobs' use as a percentage bar ("95 %", "+1" while one waits), no pips.
section('V0.80', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .rld .rldr') && !!document.querySelector('.one .tcpu')");
  const r = await evaluate(`(() => {
    const rail = document.querySelector('.rail').getBoundingClientRect();
    const ld = document.querySelector('.rail .rld'), b = ld.getBoundingClientRect();
    const fig = ld.querySelector('.rldr .hmr.ld .hmv'), row = ld.querySelector('.rldr'), pips = null, bar = ld.querySelector('.hmr.job');
    const bb = bar?.getBoundingClientRect(), rb = row.getBoundingClientRect();
    const others = [...document.querySelectorAll('.rail > *')].filter((e) => !e.contains(ld)).map((e) => e.getBoundingClientRect());
    const hit = others.filter((o) => o.width && o.height && o.left < b.right - 0.5 && b.left < o.right - 0.5 && o.top < b.bottom - 0.5 && b.top < o.bottom - 0.5).length;
    const q = document.querySelector('.rail .rq')?.getBoundingClientRect();
    const tc = [...document.querySelectorAll('.one')].find((x) => x.dataset.id === 't_mapperf');
    const jr = tc, js = jr?.querySelector('.tslot'), jb = js?.getBoundingClientRect(), jrr = jr?.getBoundingClientRect();
    const c = tc?.querySelector('.tcpu'), cr = c?.getBoundingClientRect(), rr = tc?.getBoundingClientRect();
    return { text: fig.textContent, hot: row.classList.contains('hot'), tip: row.title,
      pips: pips ? pips.querySelectorAll('i').length : 0, held: pips ? pips.querySelectorAll('i.on').length : 0, wait: (pips ?? bar)?.querySelector('em')?.textContent ?? '',
      bar: bar?.querySelector('.hmv')?.textContent ?? '', barHot: !!bar?.classList.contains('hot'), barFill: bar ? Math.round(bar.querySelector('.hmb i').getBoundingClientRect().width / bar.querySelector('.hmb').getBoundingClientRect().width * 100) : -1,
      barIn: !!bb && bb.left >= rb.left - 0.5 && bb.right <= rb.right + 0.5 && bb.bottom <= rb.bottom + 0.5,
      inside: b.left >= rail.left - 0.5 && b.right <= rail.right + 0.5 && b.top >= rail.top && b.bottom <= rail.bottom, hit,
      aboveQuota: !q || b.bottom <= q.top + 0.5,
      cpu: c?.textContent ?? '', cpuHot: !!c?.classList.contains('hot'), inRow: !!cr && cr.top >= rr.top - 0.5 && cr.bottom <= rr.bottom + 0.5,
      job: js?.textContent ?? '', jobIn: !!jb && jb.top >= jrr.top - 0.5 && jb.bottom <= jrr.bottom + 0.5 && jb.right <= Math.min(jrr.right, js.closest('.ell').getBoundingClientRect().right) + 0.5 };
  })()`);
  check(r.text === "130%" && r.hot && /load 10\.4/.test(r.tip) && /CPU pressure 46 %/.test(r.tip), `V0.80 desktop: the rail's load is wrong (${JSON.stringify(r)})`);
  // Decision 0029 S2, V0.152: a host that admits by a budget shows it as a bar (full, hot, "1 waits" under it) in place of
  // the pips.
  const why = await evaluate(`(() => { const w = document.querySelector('#sec-slots .sqwhy'), s = document.querySelector('.list')?.getBoundingClientRect(), b = w?.getBoundingClientRect(); return { text: w?.textContent ?? '', inside: !!b && !!s && b.left >= s.left - 0.5 && b.right <= s.right + 0.5 }; })()`);
  check(why.text === "The next waits: needs ≈ 30 % CPU, 5 % free" && why.inside, `S2 desktop: the queue does not say why the next job waits (${JSON.stringify(why)})`);
  check(r.pips === 0 && r.bar === "95%+1" && r.barHot && r.barFill === 95 && r.barIn, `V0.163 desktop: the rail's heavy-job bar is not a percentage (${JSON.stringify(r)})`);
  check(r.inside && r.hit === 0 && r.aboveQuota, `V0.80 desktop: the load line is outside the rail, covers an item or sits below the quota (${JSON.stringify(r)})`);
  check(r.cpu === "2.6 cores" && r.cpuHot && r.inRow, `V0.80 desktop: the hot thread's CPU is missing or outside its row (${JSON.stringify(r)})`);
  // Decision 0029 S1 (V0.142): the ring is hot from the 3.4× slowdown; the tooltip names it; a holder's row names its job, first on its line and not cut.
  check(/Slowdown 3\.4×/.test(r.tip) && /Signals: cgroups/.test(r.tip), `V0.142 desktop: the tooltip lacks the slowdown or the provider (${JSON.stringify(r)})`);
  check(r.job === "yarn build ≈ 45 %" && r.jobIn, `V0.142 desktop: the holder's job is missing or outside its row (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.80 desktop: ${e.message}`);
}
});

// Decision 0029 S1 (V0.142): the phone's row of a slot holder names its job the same way, first on its line and not
// cut (the phone shows no ring, V0.99 (3)).
section('V0.142', async (h) => {
  const { send, evaluate, navigate, settle, waitFor } = h;
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
    await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
    await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.m-seg')");
    await evaluate(`[...document.querySelectorAll('.m-seg')].find((b) => b.title.startsWith('Working')).click()`);
    await waitFor("[...document.querySelectorAll('.tslot')].some((x) => x.textContent.startsWith('yarn build'))", { what: "the Working section's holder with a job" });
    await settle();
    const r = await evaluate(`(() => {
      const s = [...document.querySelectorAll('.tslot')].find((x) => x.textContent.startsWith('yarn build')), row = s.closest('.one');
      const a = row.getBoundingClientRect(), b = s.getBoundingClientRect();
      return { text: s.textContent, inside: b.top >= a.top - 0.5 && b.bottom <= a.bottom + 0.5 && b.right <= Math.min(a.right, s.closest('.ell').getBoundingClientRect().right) + 0.5, vw: innerWidth };
    })()`);
    check(r.text === "yarn build ≈ 45 %" && r.inside, `V0.142 phone: the holder's job is missing or outside its row (${JSON.stringify(r)})`);
    // Decision 0029 S2: the phone's queue says why the next job waits too, inside the screen.
    const w = await evaluate(`(() => { const e = document.querySelector('#m-sec-slots .sqwhy'), b = e?.getBoundingClientRect(); return { text: e?.textContent ?? '', inside: !!b && b.left >= 0 && b.right <= innerWidth + 0.5 }; })()`);
    check(w.text === "The next waits: needs ≈ 30 % CPU, 5 % free" && w.inside, `S2 phone: the queue does not say why the next job waits (${JSON.stringify(w)})`);
  } catch (e) {
    failures.push(`V0.142 phone: ${e.message}`);
  }
});

// SPEC V0.141: the rail fits the window at any height. With two hosts (two load rings) and four providers (four quota
// lines), at 1280 × 700 and 1280 × 900: the rail's first item (the brand) and last (the host line) are inside the
// window, and so are every section (V0.137's Slots too, while threads wait), List, the bell, Changes and Settings; the document does not scroll; the rail is
// the window's height; the middle (the rings and the quota) is the only part that may scroll, and at 900 it shows
// everything; at 700 the rail is compact (no key hints) and the two rings sit side by side.
// Decision 0029 S5: a frozen job's row says "paused: memory" first on its line, not cut, on the desktop and the
// phone (the phone's Working section).
section('S5 paused', async (h) => {
  const { send, evaluate, navigate, waitFor, settle } = h;
  const read = `(() => { const s = [...document.querySelectorAll('.tslot')].find((x) => x.textContent.includes('paused')); if (!s) return null; const a = s.closest('.one').getBoundingClientRect(), b = s.getBoundingClientRect(), e = s.closest('.ell').getBoundingClientRect(); return { text: s.textContent, inside: b.top >= a.top - 0.5 && b.bottom <= a.bottom + 0.5 && b.right <= Math.min(a.right, e.right) + 0.5 }; })()`;
  try {
    await navigate(`${origin}/?mock=rail&paused=1`, "[...document.querySelectorAll('.tslot')].some((x) => x.textContent.includes('paused'))");
    const r = await evaluate(read);
    check(r && r.text === "yarn build · paused: memory" && r.inside, `S5 desktop: the paused job's row is wrong (${JSON.stringify(r)})`);
  } catch (e) {
    failures.push(`S5 desktop: ${e.message}`);
  }
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
    await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
    await navigate(`${origin}/?mock=rail&paused=1`, "!!document.querySelector('.m-seg')");
    await evaluate(`[...document.querySelectorAll('.m-seg')].find((b) => b.title.startsWith('Working')).click()`);
    await waitFor("[...document.querySelectorAll('.tslot')].some((x) => x.textContent.includes('paused'))", { what: "the paused row on the phone" });
    await settle();
    const r = await evaluate(read);
    check(r && r.text === "yarn build · paused: memory" && r.inside, `S5 phone: the paused job's row is wrong (${JSON.stringify(r)})`);
  } catch (e) {
    failures.push(`S5 phone: ${e.message}`);
  }
});

// SPEC V0.164 (design/rogue-agents/SPEC.md 12.7): undeclared heavy work. At 1280 × 800 and 390 × 844, light and dark: the
// row keeps its height and starts with the mark and the word in the beam ink; the meter has a hatched part and the rail a
// button that opens the thread or a menu (it does not scroll at 1000 px); the strip is one block under the header and the
// conversation keeps its scroll; the phone's strip and Settings buttons are 44 px high; nothing is wider than the screen.
// Add as heavy job… opens Settings → Automation at the suggestion; Dismiss, Add, Remove and the thresholds send the
// owner's commands. ?undeclared=1 (Map perf), =paused, =2 (two threads and a dismissed third) feed it.
const UND_INK = `(() => { const p = document.createElement('i'); p.style.color = 'var(--beam-ink)'; document.body.append(p); const c = getComputedStyle(p).color; p.remove(); return c; })()`;
const UND_COMMANDS = (action) => `(window.__fyrCommands || []).filter((c) => c.action === '${action}').map((c) => c.args)`;
for (const dark of [false, true]) section(`V0.164 desktop${dark ? " dark" : ""}`, async ({ send, evaluate, waitFor, settle, navigate, key, shot }) => {
  const ctx = `V0.164 desktop${dark ? " dark" : ""}`;
  const tag = dark ? "dark" : "light";
  try {
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: dark ? "dark" : "light" }] });
    const rowH = `(() => { const r = document.querySelector('.one[data-id="t_mapperf"]'); return r ? Math.round(r.getBoundingClientRect().height * 10) / 10 : 0; })()`;
    await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.one[data-id=\"t_mapperf\"]')");
    const base = await evaluate(rowH);
    // The rows.
    await navigate(`${origin}/?mock=rail&undeclared=2`, "document.querySelectorAll('.one .tund').length === 2");
    const r = await evaluate(`(() => {
      const ink = ${UND_INK};
      const t = [...document.querySelectorAll('.one .tund')].map((e) => ({ id: e.closest('.one').dataset.id, text: e.textContent, color: getComputedStyle(e).color, iconW: e.querySelector('.und-ic').getBoundingClientRect().width,
        inside: e.getBoundingClientRect().right <= e.closest('.ell').getBoundingClientRect().right + 0.5 }));
      return { ink, t, h: ${rowH}, nix: !!document.querySelector('[data-id="t_nix"] .tund'), why: document.body.innerText.includes('(Map perf uses 52 % undeclared +2)') };
    })()`);
    check(r.t.map((x) => x.id).sort().join() === "t_babysit,t_mapperf" && r.t.every((x) => x.color === r.ink && x.iconW > 8 && x.inside), `${ctx}: the rows' marks are missing, not in the beam ink or cut (${JSON.stringify(r)})`);
    check(r.t.find((x) => x.id === "t_mapperf").text === "undeclared · 2.8 GB · 2.1 cores" && r.t.find((x) => x.id === "t_babysit").text === "undeclared · 1.6 GB · 1.2 cores", `${ctx}: the rows' words are wrong (${JSON.stringify(r.t)})`);
    check(!r.nix, `${ctx}: a dismissed thread's row has a mark`);
    check(r.h === base, `${ctx}: the flagged row is ${r.h} px, the plain one ${base} px`);
    check(r.why, `${ctx}: the wait reason does not name the undeclared use`);
    // The meter and the rail's button.
    const m = await evaluate(`(() => {
      const bar = document.querySelector('.rail .hmr.job'), parts = [...bar.querySelectorAll('.hmb i')], und = bar.querySelector('.hmb i.und');
      const trk = bar.querySelector('.hmb').getBoundingClientRect();
      const b = document.querySelector('.rail .rund'), br = b?.getBoundingClientRect(), rail = document.querySelector('.rail').getBoundingClientRect();
      return { parts: parts.length, hatch: und ? getComputedStyle(und).backgroundImage.startsWith('repeating-linear-gradient') : false, fits: parts.every((p) => p.getBoundingClientRect().right <= trk.right + 0.5),
        tip: bar.title, btn: b?.textContent ?? '', aria: b?.getAttribute('aria-label') ?? '', bright: br ? br.right <= rail.right + 0.5 && br.left >= rail.left - 0.5 : false,
        color: b ? getComputedStyle(b).color : '', ink: ${UND_INK}, outside: !!b && !!b.closest('[role=img]') };
    })()`);
    check(m.parts === 2 && m.hatch && m.fits, `${ctx}: the meter has no hatched undeclared part (${JSON.stringify(m)})`);
    check(/undeclared 87 %/.test(m.tip) && /\nUndeclared: Map perf \(acme\/web\), 2\.8 GB and 2\.1 cores for \d+ min; Babysit PR #4170 \(acme\/web\), 1\.6 GB and 1\.2 cores for \d+ s/.test(m.tip), `${ctx}: the meter's tooltip is wrong (${JSON.stringify(m.tip)})`);
    check(m.btn === "2" && m.aria === "2 threads run undeclared heavy work" && m.bright && m.color === m.ink && !m.outside, `${ctx}: the rail's button is wrong (${JSON.stringify(m)})`);
    await shot(`undeclared-row-${tag}-desktop.png`);
    // The button's menu, then the thread.
    await evaluate(`document.querySelector('.rail .rund').click()`);
    await waitFor(`document.querySelectorAll('.ctxm [role=menuitem]').length === 2`, { what: "the menu of two threads" });
    const items = await evaluate(`[...document.querySelectorAll('.ctxm [role=menuitem]')].map((e) => e.textContent.replace(/\\s+/g, ' ').trim())`);
    check(items.join("|") === "Map perf · 2.8 GB · 2.1 cores|Babysit PR #4170 · 1.6 GB · 1.2 cores", `${ctx}: the rail's menu is ${JSON.stringify(items)}`);
    await shot(`undeclared-rail-menu-${tag}-desktop.png`);
    await evaluate(`document.querySelector('.ctxm [role=menuitem]').click()`);
    await waitFor(`!!document.querySelector('.th .undb')`, { what: "the strip of the opened thread" });
    await settle();
    // The strip.
    const s = await evaluate(`(() => {
      const strips = document.querySelectorAll('.undb'), e = strips[0], th = document.querySelector('.th'), conv = document.querySelector('.convo');
      const b = e.getBoundingClientRect(), t = th.getBoundingClientRect(), c = conv.getBoundingClientRect();
      return { n: strips.length, prev: e.previousElementSibling?.className ?? '', text: e.querySelector('.undb-b').innerText.replace(/\\s+/g, ' '), btns: [...e.querySelectorAll('.btn')].map((x) => x.textContent),
        full: Math.abs(b.width - t.width) < 1, h: Math.round(b.height), convH: Math.round(c.height), convScroll: getComputedStyle(conv).overflowY, below: b.bottom <= c.top + 0.5,
        ql: document.querySelector('.ql.und .qlh')?.textContent ?? '', docW: document.documentElement.scrollWidth <= innerWidth };
    })()`);
    check(s.n === 1 && s.prev === "thh" && s.full && s.h <= 72 && s.below, `${ctx}: the strip is not one block under the header (${JSON.stringify(s)})`);
    check(/^Heavy work Fyr did not admit · 2\.8 GB \(20 % of laptop\) · 2\.1 cores \(52 %\) · \d+ min node test\/layout\.e2e\.mjs · counted against laptop's heavy jobs · agent told 14:29$/.test(s.text), `${ctx}: the strip's text is "${s.text}"`);
    check(s.btns.join("|") === "Add as heavy job…|Dismiss" && s.convH > 150 && /auto|scroll/.test(s.convScroll) && s.docW, `${ctx}: the strip's buttons, the conversation's room or its scroll are wrong (${JSON.stringify(s)})`);
    check(s.ql === "Fyr told the agent: heavy work it did not ask for (2.8 GB, 2.1 cores); ask before a heavy job", `${ctx}: the conversation's machine line is "${s.ql}"`);
    await evaluate(`document.querySelector('.ql.und .qlh').click()`);
    await waitFor(`document.querySelector('.ql.und .qlx')?.textContent.startsWith('This thread runs heavy work Fyr did not admit: about 2.8 GB')`, { what: "the whole notice" });
    await evaluate(`document.querySelector('.ql.und .qlh').click()`);
    await shot(`undeclared-thread-strip-${tag}-desktop.png`);
    await evaluate(`document.querySelector('.ql.und').scrollIntoView({ block: 'center' })`);
    await shot(`undeclared-machine-line-${tag}-desktop.png`);
    // Dismiss hides the mark and the button's count; the meter keeps the hatched part.
    await evaluate(`[...document.querySelectorAll('.undb .btn')].find((b) => b.textContent === 'Dismiss').click()`);
    await waitFor(`!document.querySelector('.undb') && document.querySelectorAll('.one .tund').length === 1`, { what: "Dismiss to hide the mark" });
    const dm = await evaluate(`({ btn: document.querySelector('.rail .rund')?.textContent, hatch: !!document.querySelector('.rail .hmr.job .hmb i.und'), sent: ${UND_COMMANDS("host.undeclaredDismiss")} })`);
    check(dm.btn === "1" && dm.hatch && dm.sent.length === 1 && dm.sent[0].thread === "t_mapperf" && dm.sent[0].on === true, `${ctx}: Dismiss did not hide the mark, keep the meter, or send the command (${JSON.stringify(dm)})`);
    // The rail does not scroll at 1000 px with two hosts and the button.
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 1000, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=rail&hosts=2&undeclared=2`, "!!document.querySelector('.rail .rund') && document.querySelectorAll('.rail .rmid .rldr').length === 2");
    const rs = await evaluate(`(() => { const rail = document.querySelector('.rail'), mid = rail.querySelector('.rmid'); const b = document.querySelector('.rail .rund').getBoundingClientRect();
      return { railScroll: rail.scrollHeight - rail.clientHeight, midScroll: mid.scrollHeight - mid.clientHeight, doc: document.documentElement.scrollHeight - document.documentElement.clientHeight, btn: b.height > 0 && b.bottom <= innerHeight }; })()`);
    check(rs.railScroll <= 0 && rs.midScroll <= 0 && rs.doc <= 0 && rs.btn, `${ctx}: the rail scrolls at 1000 px with the button (${JSON.stringify(rs)})`);
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
    // Paused: the harbour strip without buttons, the row's words.
    await navigate(`${origin}/?mock=rail&undeclared=paused&open=t_mapperf`, "!!document.querySelector('.th .undb.paused')");
    const p = await evaluate(`(() => { const e = document.querySelector('.undb.paused'); return { text: e.querySelector('.undb-b').innerText.replace(/\\s+/g, ' '), btns: e.querySelectorAll('button').length, head: getComputedStyle(e.querySelector('b')).color,
      harbour: (() => { const p = document.createElement('i'); p.style.color = 'var(--harbour)'; document.body.append(p); const c = getComputedStyle(p).color; p.remove(); return c; })(), row: document.querySelector('.one[data-id="t_mapperf"] .tund')?.textContent ?? '', h: Math.round(e.getBoundingClientRect().height) }; })()`);
    check(p.text === "Paused by Fyr: memory is short on laptop (6 % free). Frozen, not killed; it resumes when memory has recovered for 30 s, within 10 min at most. node test/layout.e2e.mjs · 2.8 GB · undeclared" && p.btns === 0 && p.head === p.harbour && p.row === "undeclared · paused: memory" && p.h <= 72, `${ctx}: the paused strip or row is wrong (${JSON.stringify(p)})`);
    await shot(`undeclared-thread-strip-paused-${tag}-desktop.png`);
    // Add as heavy job… opens Settings → Automation at the suggestion.
    await navigate(`${origin}/?mock=rail&undeclared=1&open=t_mapperf`, "!!document.querySelector('.th .undb')");
    await evaluate(`[...document.querySelectorAll('.undb .btn')].find((b) => b.textContent.startsWith('Add as heavy job')).click()`);
    await waitFor(`!!document.querySelector('.dlg .sr.pick')`, { what: "Settings at the suggestion" });
    await settle();
    const st = await evaluate(`(() => { const d = document.querySelector('.dlg'); const sec = (l) => d.querySelector('section[aria-label="' + l + '"]');
      const rows = (l) => [...(sec(l)?.querySelectorAll('.sr') ?? [])];
      const pick = d.querySelector('.sr.pick'); const r = pick.getBoundingClientRect(), dr = d.querySelector('.stp').getBoundingClientRect();
      return { page: d.querySelector('.stp').dataset.page, groups: [...d.querySelectorAll('section.sg')].map((s) => s.getAttribute('aria-label')).filter(Boolean),
        pick: pick.querySelector('.cmd').textContent, pattern: pick.querySelector('input').value, kind: pick.querySelector('select').value, visible: r.top >= dr.top - 1 && r.bottom <= dr.bottom + 1,
        now: rows('When a thread counts as heavy').map((e) => e.querySelector('.sr-d').textContent), vals: rows('When a thread counts as heavy').map((e) => e.querySelector('input').value),
        und: rows('Undeclared now').map((e) => e.innerText.replace(/\\s+/g, ' ').trim()), added: rows('Heavy jobs you added').map((e) => e.innerText.replace(/\\s+/g, ' ').trim()),
        sugg: pick.querySelector('.sr-d').innerText, noSlot: !/slot|reservation/i.test([...d.querySelectorAll('section.sg')].filter((s) => /Undeclared|Suggested|you added|counts as heavy/.test(s.getAttribute('aria-label') || '')).map((s) => s.innerText).join(' ')) }; })()`);
    check(st.page.startsWith("automation") && st.pick === "node test/layout.e2e.mjs" && st.pattern === "node (\\S*/)?layout\\.e2e\\.mjs" && st.kind === "e2e" && st.visible, `${ctx}: Settings did not open at the suggestion (${JSON.stringify(st)})`);
    check(st.groups.join("|").includes("Undeclared now|Suggested heavy jobs|Heavy jobs you added|When a thread counts as heavy|Heavy jobs"), `${ctx}: the groups are ${st.groups.join("|")}`);
    check(st.now.join("|") === "Now 1.0 of laptop's 4 cores|Now 1.1 GB of laptop's 14 GB|Shorter bursts (a typecheck, one test file) never count" && st.vals.join() === "25,8,45", `${ctx}: the thresholds are wrong (${JSON.stringify([st.now, st.vals])})`);
    check(st.und.length === 1 && /^Map perf acme\/web · node test\/layout\.e2e\.mjs · 2\.8 GB \(20 %\) · 2\.1 cores \(52 %\) · \d+ min · agent told 14:29 ?Open ?Dismiss$/.test(st.und[0]), `${ctx}: Undeclared now is ${JSON.stringify(st.und)}`);
    check(st.added.length === 1 && /refused 4 times since/.test(st.added[0]), `${ctx}: the added patterns are ${JSON.stringify(st.added)}`);
    check(/^Seen 3 times in 2 threads, last 14:29 in Map perf · up to 2\.8 GB, 2\.1 cores, 4 min$/.test(st.sugg) && st.noSlot, `${ctx}: the suggestion's line or the words are wrong (${JSON.stringify([st.sugg, st.noSlot])})`);
    await shot(`undeclared-settings-${tag}-desktop.png`);
    await evaluate(`document.querySelector('.dlg section[aria-label="Undeclared now"]').scrollIntoView({ block: 'start' })`);
    await shot(`undeclared-settings-now-${tag}-desktop.png`);
    await evaluate(`document.querySelector('.dlg section[aria-label="When a thread counts as heavy"]').scrollIntoView({ block: 'start' })`);
    await shot(`undeclared-settings-thresholds-${tag}-desktop.png`);
    // Add sends the edited pattern; a threshold saves; Remove.
    await evaluate(`(() => { const i = document.querySelector('.dlg .sr.pick input'); i.value = 'node test/layout\\\\.e2e\\\\.mjs'; i.dispatchEvent(new Event('input', { bubbles: true })); })()`);
    await evaluate(`[...document.querySelectorAll('.dlg .sr.pick .btn')].find((b) => b.textContent === 'Add').click()`);
    await waitFor(`document.querySelectorAll('.dlg section[aria-label="Heavy jobs you added"] .sr').length === 2 && !document.querySelector('.dlg .sr.pick')`, { what: "the suggestion to become an added pattern" });
    const add = await evaluate(UND_COMMANDS("host.heavySuggestion"));
    check(add.length === 1 && add[0].do === "add" && add[0].id === "sg1" && add[0].pattern === "node test/layout\\.e2e\\.mjs" && add[0].kind === "e2e", `${ctx}: Add sent ${JSON.stringify(add)}`);
    await evaluate(`(() => { const i = document.querySelector('.dlg section[aria-label="When a thread counts as heavy"] input'); i.focus(); i.value = '30'; i.dispatchEvent(new Event('input', { bubbles: true })); })()`);
    await key("Enter");
    await waitFor(`${UND_COMMANDS("host.undeclaredSettings")}.length === 1`, { what: "the threshold to be sent" });
    const set = await evaluate(UND_COMMANDS("host.undeclaredSettings"));
    check(set[0].cpu === 0.3 && !("memory" in set[0]), `${ctx}: the threshold sent ${JSON.stringify(set)}`);
    await evaluate(`[...document.querySelectorAll('.dlg section[aria-label="Heavy jobs you added"] .btn')].find((b) => b.textContent === 'Remove').click()`);
    await waitFor(`document.querySelectorAll('.dlg section[aria-label="Heavy jobs you added"] .sr').length === 1`, { what: "Remove to take a pattern out" });
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
  await send("Emulation.setEmulatedMedia", { features: [] });
});

for (const dark of [false, true]) section(`V0.164 phone${dark ? " dark" : ""}`, async ({ send, evaluate, waitFor, settle, openScreen, tapCenter, shot }) => {
  const ctx = `V0.164 phone${dark ? " dark" : ""}`;
  const tag = dark ? "dark" : "light";
  const W = 390;
  try {
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: dark ? "dark" : "light" }] });
    const working = async (url, ready) => {
      await openScreen({ url, ready }, W, 844);
      await evaluate(`[...document.querySelectorAll('.m-seg')].find((b) => b.title.startsWith('Working')).click()`);
      await waitFor(`[...document.querySelectorAll('.one')].some((x) => x.querySelector('.t')?.textContent === 'Map perf')`, { what: "the Working rows" });
      await settle();
    };
    const rowH = `(() => { const r = [...document.querySelectorAll('.one')].find((x) => x.querySelector('.t')?.textContent === 'Map perf'); return r ? Math.round(r.getBoundingClientRect().height * 10) / 10 : 0; })()`;
    await working("?mock=rail", "!!document.querySelector('.m-seg')");
    const base = await evaluate(rowH);
    await working("?mock=rail&undeclared=2", "!!document.querySelector('.m-seg')");
    await waitFor(`document.querySelectorAll('.one .tund').length === 2`, { what: "the marks on the phone's rows" });
    const r = await evaluate(`(() => ({ t: [...document.querySelectorAll('.one .tund')].map((e) => [e.closest('.one').querySelector('.t').textContent, e.textContent, getComputedStyle(e).color === ${UND_INK}, e.getBoundingClientRect().right <= innerWidth]), h: ${rowH}, wide: document.documentElement.scrollWidth > innerWidth }))()`);
    check(r.t.length === 2 && r.t.every((x) => x[2] && x[3]) && r.t.find((x) => x[0] === "Map perf")[1] === "undeclared · 2.8 GB · 2.1 cores", `${ctx}: the rows' marks (${JSON.stringify(r)})`);
    check(r.h === base && !r.wide, `${ctx}: the flagged row is ${r.h} px against ${base} px, or the page is wider than the screen`);
    await shot(`undeclared-row-${tag}-phone.png`);
    // The thread: the strip's buttons sit under the text at 44 px.
    await openScreen({ url: "?mock=rail&undeclared=1&open=t_mapperf", ready: "!!document.querySelector('.m-thread .undb')" }, W, 844);
    const s = await evaluate(`(() => { const e = document.querySelector('.m-thread .undb'), t = e.querySelector('.undb-b').getBoundingClientRect(), bs = [...e.querySelectorAll('.btn')].map((b) => b.getBoundingClientRect());
      const conv = document.querySelector('.m-thread .convo'); return { n: document.querySelectorAll('.undb').length, text: e.querySelector('.undb-b').innerText.replace(/\\s+/g, ' '), labels: [...e.querySelectorAll('.btn')].map((b) => b.textContent), minH: Math.min(...bs.map((b) => b.height)),
        under: bs.every((b) => b.top >= t.bottom - 0.5), inside: [e, ...e.querySelectorAll('*')].every((x) => { const b = x.getBoundingClientRect(); return b.right <= innerWidth + 0.5 && b.left >= -0.5; }),
        convH: conv ? Math.round(conv.getBoundingClientRect().height) : 0, scroll: conv ? getComputedStyle(conv).overflowY : '', wide: document.documentElement.scrollWidth > innerWidth, ql: document.querySelector('.ql.und .qlh')?.textContent ?? '' }; })()`);
    check(s.n === 1 && s.labels.join("|") === "Add as heavy job…|Dismiss" && s.minH >= 44 && s.under && s.inside && !s.wide, `${ctx}: the strip's buttons are under 44 px, over the text or off the screen (${JSON.stringify(s)})`);
    check(s.convH > 150 && /auto|scroll/.test(s.scroll), `${ctx}: the conversation lost its room or scroll (${JSON.stringify(s)})`);
    check(/^Heavy work Fyr did not admit · 2\.8 GB \(20 % of laptop\) · 2\.1 cores \(52 %\) · \d+ min node test\/layout\.e2e\.mjs · counted against laptop's heavy jobs · agent told 14:29$/.test(s.text) && s.ql.startsWith("Fyr told the agent: heavy work it did not ask for (2.8 GB, 2.1 cores)"), `${ctx}: the strip's or the line's text (${JSON.stringify([s.text, s.ql])})`);
    await shot(`undeclared-thread-strip-${tag}-phone.png`);
    await openScreen({ url: "?mock=rail&undeclared=paused&open=t_mapperf", ready: "!!document.querySelector('.m-thread .undb.paused')" }, W, 844);
    const pp = await evaluate(`({ btns: document.querySelectorAll('.undb button').length, wide: document.documentElement.scrollWidth > innerWidth, head: document.querySelector('.undb b').textContent })`);
    check(pp.btns === 0 && !pp.wide && pp.head === "Paused by Fyr: memory is short on laptop (6 % free).", `${ctx}: the paused strip (${JSON.stringify(pp)})`);
    await shot(`undeclared-thread-strip-paused-${tag}-phone.png`);
    // Add as heavy job… goes to Settings → Automation at the suggestion.
    await openScreen({ url: "?mock=rail&undeclared=1&open=t_mapperf", ready: "!!document.querySelector('.m-thread .undb')" }, W, 844);
    await tapCenter(".m-thread .undb .btn", "!!document.querySelector('.set.phone .sr.pick')");
    // Settings: every button and field of the new groups is 44 px high; nothing is wider than the screen.
    await openScreen({ url: "?mock=rail&undeclared=2&settings=automation", ready: "!!document.querySelector('.set.phone section[aria-label=\"Undeclared now\"]')" }, W, 844);
    const g = await evaluate(`(() => { const labels = ['Undeclared now', 'Suggested heavy jobs', 'Heavy jobs you added', 'When a thread counts as heavy'];
      const secs = labels.map((l) => document.querySelector('.set.phone section[aria-label="' + l + '"]'));
      const ctl = secs.flatMap((s) => s ? [...s.querySelectorAll('button, input, select')] : []).filter((e) => e.getBoundingClientRect().height > 0);
      return { have: secs.map((s) => !!s), n: ctl.length, minH: Math.min(...ctl.map((e) => e.getBoundingClientRect().height)), small: ctl.filter((e) => e.getBoundingClientRect().height < 44).map((e) => e.getAttribute('aria-label') || e.textContent),
        off: secs.filter(Boolean).flatMap((s) => [...s.querySelectorAll('*')]).filter((e) => { const b = e.getBoundingClientRect(); return b.width && (b.right > innerWidth + 0.5 || b.left < -0.5); }).map((e) => e.className || e.tagName),
        wide: document.documentElement.scrollWidth > innerWidth, dim: !!secs[0]?.querySelector('.sr.off'), tail: secs[0]?.querySelector('.sr.off .sr-d')?.textContent.endsWith('dismissed, still counted') }; })()`);
    check(g.have.every(Boolean) && g.minH >= 44 && g.off.length === 0 && !g.wide && g.dim && g.tail, `${ctx}: Settings' new groups on the phone (${JSON.stringify(g)})`);
    await evaluate(`document.querySelector('.set.phone section[aria-label="Undeclared now"]').scrollIntoView({ block: 'start' })`);
    await shot(`undeclared-settings-${tag}-phone.png`);
    await evaluate(`document.querySelector('.set.phone section[aria-label="Suggested heavy jobs"]').scrollIntoView({ block: 'start' })`);
    await shot(`undeclared-settings-suggested-${tag}-phone.png`);
    await evaluate(`document.querySelector('.set.phone section[aria-label="When a thread counts as heavy"]').scrollIntoView({ block: 'start' })`);
    await shot(`undeclared-settings-thresholds-${tag}-phone.png`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
  await send("Emulation.setEmulatedMedia", { features: [] });
});

section('V0.141', async (h) => {
  const { send, evaluate, waitFor, settle, navigate } = h;
  for (const ht of [700, 900]) {
    const ctx = `V0.141 desktop 1280x${ht}`;
    try {
      await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: ht, deviceScaleFactor: 1, mobile: false });
      await navigate(`${origin}/?mock=rail&hosts=2`, "!!document.querySelector('.rail .bell') && document.querySelectorAll('.rail .rmid .rldr').length === 2 && document.querySelectorAll('.rail .rmid .rqr').length === 4");
      await settle();
      const r = await evaluate(`(() => {
        const vh = innerHeight, de = document.documentElement;
        const rail = document.querySelector('.rail'), rb = rail.getBoundingClientRect();
        const items = [...rail.children].filter((e) => !e.classList.contains('rmid') && e.getBoundingClientRect().height > 0);
        const box = (e) => { const b = e.getBoundingClientRect(); return { c: e.className, top: Math.round(b.top), bottom: Math.round(b.bottom) }; };
        const out = items.map(box).filter((b) => b.top < 0 || b.bottom > vh);
        const first = box(rail.firstElementChild), last = box(rail.lastElementChild);
        const mid = rail.querySelector('.rmid'), rings = [...mid.querySelectorAll('.rldr')].map((e) => e.getBoundingClientRect());
        const buttons = ['.rail .bell', '.rail .rchg', '.rail .rgear'].map((q) => document.querySelector(q)).filter(Boolean).length;
        return { vh, first, last, out, sections: rail.querySelectorAll('.ri[data-sec]').length, buttons,
          docScroll: de.scrollHeight - de.clientHeight, scrollY, bodyScroll: document.body.scrollHeight - document.body.clientHeight,
          railTop: Math.round(rb.top), railBottom: Math.round(rb.bottom), railScroll: rail.scrollHeight - rail.clientHeight,
          midScroll: mid.scrollHeight - mid.clientHeight, midH: mid.clientHeight,
          sideBySide: rings.length === 2 && Math.abs(rings[0].top - rings[1].top) < 1,
          hints: [...rail.querySelectorAll('.ri .k')].filter((k) => k.getBoundingClientRect().height > 0).length, keys: rail.querySelectorAll('.ri .k').length };
      })()`);
      check(r.first.c === "brand" && r.first.top >= 0 && r.last.c.startsWith("foot") && r.last.bottom <= r.vh, `${ctx}: the rail's first or last item is outside the window (${JSON.stringify(r)})`);
      check(r.out.length === 0 && r.sections >= 5 && r.buttons === 3, `${ctx}: a section or a bottom item is outside the window (${JSON.stringify(r)})`);
      check(r.docScroll <= 0 && r.bodyScroll <= 0 && r.scrollY === 0, `${ctx}: the document scrolls (${JSON.stringify(r)})`);
      check(r.railTop >= 0 && r.railBottom <= r.vh && r.railScroll <= 0, `${ctx}: the rail is not the window's height or scrolls as a whole (${JSON.stringify(r)})`);
      // V0.167: the hosts' meters stack (two rows each), no longer side by side.
      // V0.167, V0.188: the rail is compact under 1010 px, so at 900 the key hints go and the middle shows everything.
      if (ht === 900) check(r.midScroll <= 0 && r.hints === 0, `${ctx}: the rail's middle does not show everything, or the rail is not compact (${JSON.stringify(r)})`);
      else check(r.hints === 0 && r.midH >= 40, `${ctx}: the rail is not compact or its middle has no room (${JSON.stringify(r)})`);
      // The middle scrolls on its own: a wheel over it moves it, not the document.
      await evaluate(`document.querySelector('.rail .rmid').scrollTop = 1e6`);
      const y = await evaluate(`[scrollY, document.documentElement.scrollTop]`);
      check(y[0] === 0 && y[1] === 0, `${ctx}: scrolling the rail's middle scrolled the document (${JSON.stringify(y)})`);
    } catch (e) {
      failures.push(`${ctx}: ${e.message}`);
    }
  }
});

// SPEC V0.166: the phone shows each host's load. With 2 hosts that report a load, one offline and 4 providers, at
// 390 × 844 and 360 × 640: the inbox header holds one strip button (44 px, inside the header, nothing past the screen's
// edge) with a card per host that has a load (two at most, then "+n"; the offline host is named in its label); each
// card's ring figure and heavy-jobs % equal the desktop rail's for the same mock state; the connection keeps its dot
// only. A tap opens the Host load sheet: every host (the offline one says Offline), the rail's tooltip lines (Heavy
// jobs, Running, waiting), and the provider quotas. Back closes it.
for (const [w, ht] of [[390, 844], [360, 640]]) section(`V0.166 phone ${w}x${ht}`, async (h) => {
  const { evaluate, waitFor, settle, navigate, openScreen } = h;
  const ctx = `V0.166 phone ${w}x${ht}`;
  try {
    // the desktop rail first, for the same mock state
    await navigate(`${origin}/?mock=rail&hosts=2`, "document.querySelectorAll('.rail .rmid .rldr').length === 2");
    const desk = await evaluate(`[...document.querySelectorAll('.rail .rmid .rldr')].map((c) => ({ fig: c.querySelector('.hmr.ld .hmv').textContent, heavy: (c.querySelector('.hmr.job .hmv')?.textContent ?? '').replace(/\\s/g, '') }))`);
    await openScreen({ url: "?mock=rail&hosts=2", ready: "!!document.querySelector('.m-load .mlh')" }, w, ht);
    await settle();
    const r = await evaluate(`(() => {
      const b = document.querySelector('.m-load'), bb = b.getBoundingClientRect(), hd = document.querySelector('.m-ih').getBoundingClientRect();
      const cards = [...b.querySelectorAll('.mlh')].map((c) => ({ fig: c.querySelector('.hmr.ld .hmv')?.textContent ?? '', off: c.classList.contains('off'), names: [c.querySelector('.hmn'), c.querySelector('.hmr.job .hmv')].filter(Boolean).map((x) => x.textContent.replace(/\\s/g, '')) }));
      const conn = document.querySelector('.m-conn .ell').getBoundingClientRect();
      return { h: Math.round(bb.height), inHeader: bb.top >= hd.top - 0.5 && bb.bottom <= hd.bottom + 0.5 && bb.left >= 0 && bb.right <= innerWidth, over: document.documentElement.scrollWidth - innerWidth,
        cards, more: b.querySelector('.mlmore')?.textContent ?? '', connHidden: conn.width <= 1, label: b.getAttribute('aria-label') };
    })()`);
    check(r.h >= 44 && r.inHeader && r.over <= 0, `${ctx}: the strip is not a 44 px button inside the header (${JSON.stringify(r)})`);
    check(r.cards.length === 2 && r.cards.every((c) => !c.off) && r.more === "+1" && /host-b: offline/.test(r.label), `${ctx}: the strip does not show the two live hosts and "+1" for the offline one (${JSON.stringify(r)})`);
    check(r.cards[0].names[0] === "desk" && r.cards[1].names[0] === "lab" && r.connHidden, `${ctx}: the cards do not name their hosts, or the connection still writes its name (${JSON.stringify(r)})`);
    check(r.cards.every((c, i) => c.fig === desk[i].fig && (c.names[1] ?? "") === desk[i].heavy), `${ctx}: the strip's figures differ from the desktop rail's (phone ${JSON.stringify(r.cards)}, desktop ${JSON.stringify(desk)})`);
    await evaluate(`document.querySelector('.m-load').click()`);
    await waitFor(`!!document.querySelector('.m-sheet[aria-label="Host load"] .m-lsheet')`);
    await settle();
    const s = await evaluate(`(() => {
      const sh = document.querySelector('.m-sheet[aria-label="Host load"]'), secs = [...sh.querySelectorAll('.mls[aria-label]')];
      const b = sh.getBoundingClientRect();
      return { hosts: secs.map((x) => x.getAttribute('aria-label')), off: secs.filter((x) => x.classList.contains('off')).map((x) => x.querySelector('.mlsn small').textContent),
        lines: sh.querySelector('.mlsl')?.textContent ?? '', quotas: sh.querySelectorAll('.qlist > *').length, fig: sh.querySelector('.mlsh .hmr.ld .hmv')?.textContent ?? '',
        inside: b.left >= 0 && b.right <= innerWidth + 0.5 && b.bottom <= innerHeight + 0.5 };
    })()`);
    check(s.hosts.join() === "desk,host-b,lab,Quota" && s.off.length === 1 && /^Offline/.test(s.off[0]), `${ctx}: the sheet does not list every host with the offline one marked (${JSON.stringify(s)})`);
    check(/Heavy jobs: CPU \d+ %/.test(s.lines) && /Running: /.test(s.lines) && /waiting/.test(s.lines) && s.fig === desk[0].fig, `${ctx}: the sheet does not say what the rail's tooltip says (${JSON.stringify(s)})`);
    check(s.quotas >= 4 && s.inside, `${ctx}: the sheet has no provider quotas or leaves the screen (${JSON.stringify(s)})`);
    await evaluate(`history.back()`);
    await waitFor(`!document.querySelector('.m-sheet[aria-label="Host load"]')`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
});

// SPEC V0.176: leftover processes. ?leftovers=1: Map perf left a Chrome and a mockup server, Nix flake update a
// port-forward under timeout, Babysit PR #4170 a dev server its agent kept, a test binary is in no thread's cgroup, and two
// settled threads' worktrees are listed. Desktop at 1280 × 800, light and dark: the rail shows "5 left" under the host's
// meters (the rail does not scroll at 1000 px) and a press opens Settings → Automation at "Leftover processes"; the open
// thread has one strip under the header with Keep and End; End sends host.leftoverEnd for each and the strip goes.
// Phone at 390 × 844: the same strip with 44 px buttons under the text, the load sheet's line, and Settings' groups
// with 44 px controls; nothing is wider than the screen.
const LFT_COMMANDS = (action) => `(window.__fyrCommands || []).filter((c) => c.action === '${action}').map((c) => c.args)`;
for (const dark of [false, true]) section(`V0.176 desktop${dark ? " dark" : ""}`, async ({ send, evaluate, waitFor, settle, navigate, shot }) => {
  const ctx = `V0.176 desktop${dark ? " dark" : ""}`;
  const tag = dark ? "dark" : "light";
  try {
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: dark ? "dark" : "light" }] });
    await navigate(`${origin}/?mock=rail&leftovers=1`, "!!document.querySelector('.rail .rlft')");
    const r = await evaluate(`(() => { const b = document.querySelector('.rail .rlft'), br = b.getBoundingClientRect(), rail = document.querySelector('.rail').getBoundingClientRect();
      return { text: b.textContent, aria: b.getAttribute('aria-label'), title: b.title, inside: br.right <= rail.right + 0.5 && br.left >= rail.left - 0.5 && br.height > 0 }; })()`);
    check(r.text === "5 left" && r.aria === "laptop: 5 leftover processes · 2.2 GB. Show them" && r.title.startsWith("Processes threads left running: chromium (1.9 GB), vite (180 MB),") && r.inside, `${ctx}: the rail's count is wrong (${JSON.stringify(r)})`);
    await shot(`leftovers-rail-${tag}-desktop.png`);
    await evaluate(`document.querySelector('.rail .rlft').click()`);
    await waitFor(`!!document.querySelector('.dlg section[aria-label="Leftover processes"]')`, { what: "Settings at the leftovers" });
    await settle();
    const st = await evaluate(`(() => { const d = document.querySelector('.dlg'), sec = d.querySelector('section[aria-label="Leftover processes"]'), wt = d.querySelector('section[aria-label="Worktrees of settled threads"]');
      const r = sec.getBoundingClientRect(), dr = d.querySelector('.stp').getBoundingClientRect();
      return { rows: [...sec.querySelectorAll('.sr')].map((e) => e.innerText.replace(/\\s+/g, ' ').trim()), visible: r.top >= dr.top - 1 && r.top <= dr.bottom,
        wt: [...wt.querySelectorAll('.sr')].map((e) => [e.querySelector('.sr-d').textContent, e.querySelectorAll('button').length]) }; })()`);
    check(st.rows.length === 5 && /^chromium · 9 processes · 1\.9 GB Map perf · acme\/web · ends in \d+ min ?Open ?Keep ?End$/.test(st.rows[0]) && st.visible, `${ctx}: Settings' leftovers are wrong (${JSON.stringify(st)})`);
    check(st.rows.some((x) => /^vite · 3 processes · 180 MB .* kept for \d+ h by the agent: the dev server the owner asked for ?Open ?End$/.test(x)) && st.rows.some((x) => /^engine\.test · 8 MB in no thread's cgroup · ends in 1 min ?Keep ?End$/.test(x)), `${ctx}: a kept or an orphan row is wrong (${JSON.stringify(st.rows)})`);
    check(st.wt.length === 2 && st.wt[0][1] === 1 && st.wt[1][1] === 0 && /kept: 2 commits not pushed$/.test(st.wt[1][0]), `${ctx}: the worktrees are wrong (${JSON.stringify(st.wt)})`);
    await shot(`leftovers-settings-${tag}-desktop.png`);
    await evaluate(`[...document.querySelectorAll('.dlg section[aria-label="Worktrees of settled threads"] .btn')].find((b) => b.textContent === 'Remove').click()`);
    await waitFor(`document.querySelectorAll('.dlg section[aria-label="Worktrees of settled threads"] .sr').length === 1`, { what: "Remove to take the worktree out" });
    // The thread's strip.
    await navigate(`${origin}/?mock=rail&leftovers=1&open=t_mapperf`, "!!document.querySelector('.th .lftb')");
    await settle();
    const s = await evaluate(`(() => { const e = document.querySelector('.th .lftb'), th = document.querySelector('.th'), conv = document.querySelector('.convo'), b = e.getBoundingClientRect(), c = conv.getBoundingClientRect();
      return { n: document.querySelectorAll('.lftb').length, text: e.querySelector('.lftb-b').innerText, btns: [...e.querySelectorAll('.btn')].map((x) => x.textContent), full: Math.abs(b.width - th.getBoundingClientRect().width) < 1,
        h: Math.round(b.height), below: b.bottom <= c.top + 0.5, convH: Math.round(c.height), wide: document.documentElement.scrollWidth > innerWidth }; })()`);
    check(s.n === 1 && /^2 leftover processes: chromium \(1\.9 GB\), http\.server · Fyr ends them in \d+ min$/.test(s.text) && s.btns.join("|") === "Keep|End", `${ctx}: the strip is wrong (${JSON.stringify(s)})`);
    check(s.full && s.h <= 48 && s.below && s.convH > 150 && !s.wide, `${ctx}: the strip is not one block under the header (${JSON.stringify(s)})`);
    await shot(`leftovers-thread-strip-${tag}-desktop.png`);
    await evaluate(`[...document.querySelectorAll('.th .lftb .btn')].find((b) => b.textContent === 'End').click()`);
    await waitFor(`!document.querySelector('.th .lftb')`, { what: "End to end the thread's leftovers" });
    const sent = await evaluate(LFT_COMMANDS("host.leftoverEnd"));
    check(sent.map((x) => x.id).join() === "41022@100,41090@101", `${ctx}: End sent ${JSON.stringify(sent)}`);
    // The rail does not scroll at 1000 px with two hosts, the undeclared button and the count.
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 1000, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=rail&hosts=2&undeclared=2&leftovers=1`, "!!document.querySelector('.rail .rlft') && document.querySelectorAll('.rail .rmid .rldr').length === 2");
    const rs = await evaluate(`(() => { const rail = document.querySelector('.rail'), mid = rail.querySelector('.rmid'), b = document.querySelector('.rail .rlft').getBoundingClientRect();
      return { railScroll: rail.scrollHeight - rail.clientHeight, midScroll: mid.scrollHeight - mid.clientHeight, btn: b.height > 0 && b.bottom <= innerHeight }; })()`);
    check(rs.railScroll <= 0 && rs.midScroll <= 0 && rs.btn, `${ctx}: the rail scrolls at 1000 px with the count (${JSON.stringify(rs)})`);
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
  await send("Emulation.setEmulatedMedia", { features: [] });
});

for (const dark of [false, true]) section(`V0.176 phone${dark ? " dark" : ""}`, async ({ send, evaluate, waitFor, settle, openScreen, shot }) => {
  const ctx = `V0.176 phone${dark ? " dark" : ""}`;
  const tag = dark ? "dark" : "light";
  const W = 390;
  try {
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: dark ? "dark" : "light" }] });
    await openScreen({ url: "?mock=rail&leftovers=1&open=t_mapperf", ready: "!!document.querySelector('.m-thread .lftb')" }, W, 844);
    const s = await evaluate(`(() => { const e = document.querySelector('.m-thread .lftb'), t = e.querySelector('.lftb-b').getBoundingClientRect(), bs = [...e.querySelectorAll('.btn')].map((b) => b.getBoundingClientRect());
      return { text: e.querySelector('.lftb-b').innerText, labels: [...e.querySelectorAll('.btn')].map((b) => b.textContent), minH: Math.min(...bs.map((b) => b.height)), under: bs.every((b) => b.top >= t.bottom - 0.5),
        inside: [e, ...e.querySelectorAll('*')].every((x) => { const b = x.getBoundingClientRect(); return b.right <= innerWidth + 0.5 && b.left >= -0.5; }), wide: document.documentElement.scrollWidth > innerWidth }; })()`);
    check(/^2 leftover processes: chromium \(1\.9 GB\), http\.server · Fyr ends them in \d+ min$/.test(s.text) && s.labels.join("|") === "Keep|End" && s.minH >= 44 && s.under && s.inside && !s.wide, `${ctx}: the phone's strip is wrong (${JSON.stringify(s)})`);
    await shot(`leftovers-thread-strip-${tag}-phone.png`);
    // The load sheet says it in a line.
    await openScreen({ url: "?mock=rail&leftovers=1", ready: "!!document.querySelector('.m-load .mlh')" }, W, 844);
    await settle();
    await evaluate(`document.querySelector('.m-load').click()`);
    await waitFor(`!!document.querySelector('.m-sheet[aria-label="Host load"] .m-lsheet li.lft')`, { what: "the sheet's leftovers line" });
    const l = await evaluate(`(() => { const li = document.querySelector('.m-lsheet li.lft'), b = li.getBoundingClientRect(); return { text: li.textContent, inside: b.right <= innerWidth + 0.5 }; })()`);
    check(/^5 leftover processes · 2\.2 GB: processes threads left running: chromium \(1\.9 GB\), vite \(180 MB\), /.test(l.text) && l.inside, `${ctx}: the sheet's line is wrong (${JSON.stringify(l)})`);
    await shot(`leftovers-load-sheet-${tag}-phone.png`);
    await evaluate(`history.back()`);
    await waitFor(`!document.querySelector('.m-sheet[aria-label="Host load"]')`);
    // Settings: 44 px controls, nothing past the screen's edge.
    await openScreen({ url: "?mock=rail&leftovers=1&settings=automation", ready: "!!document.querySelector('.set.phone section[aria-label=\"Leftover processes\"]')" }, W, 844);
    const g = await evaluate(`(() => { const secs = ['Leftover processes', 'Worktrees of settled threads'].map((l) => document.querySelector('.set.phone section[aria-label="' + l + '"]'));
      const ctl = secs.flatMap((s) => s ? [...s.querySelectorAll('button')] : []).filter((e) => e.getBoundingClientRect().height > 0);
      return { have: secs.map((s) => !!s), n: ctl.length, minH: Math.min(...ctl.map((e) => e.getBoundingClientRect().height)),
        off: secs.filter(Boolean).flatMap((s) => [...s.querySelectorAll('*')]).filter((e) => { const b = e.getBoundingClientRect(); return b.width && (b.right > innerWidth + 0.5 || b.left < -0.5); }).map((e) => e.className || e.tagName),
        wide: document.documentElement.scrollWidth > innerWidth }; })()`);
    check(g.have.every(Boolean) && g.n >= 10 && g.minH >= 44 && g.off.length === 0 && !g.wide, `${ctx}: Settings' leftover groups on the phone (${JSON.stringify(g)})`);
    await evaluate(`document.querySelector('.set.phone section[aria-label="Leftover processes"]').scrollIntoView({ block: 'start' })`);
    await shot(`leftovers-settings-${tag}-phone.png`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
  await send("Emulation.setEmulatedMedia", { features: [] });
});

// SPEC V0.167: the rail's middle at 1280 × 900 with 2 hosts and 4 providers, light and dark: no element of it is wider
// than the rail or sticks out of it; no scrollbar shows (none is drawn and nothing scrolls); each host is its two
// labelled meters, "ld" and "job", with the legend first in its tooltip; the parts are at least 8 px apart and no
// rule is drawn between them.
for (const dark of [false, true]) section(`V0.167 desktop 1280x900${dark ? " dark" : ""}`, async (h) => {
  const { send, evaluate, settle, navigate } = h;
  const ctx = `V0.167 desktop 1280x900${dark ? " dark" : ""}`;
  try {
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: dark ? "dark" : "light" }] });
    await navigate(`${origin}/?mock=rail&hosts=2`, "!!document.querySelector('.rail .bell') && document.querySelectorAll('.rail .rmid .rldr').length === 2 && document.querySelectorAll('.rail .rmid .rqr').length === 4");
    await settle();
    const r = await evaluate(`(() => {
      const rail = document.querySelector('.rail').getBoundingClientRect(), mid = document.querySelector('.rail .rmid'), cs = getComputedStyle(mid);
      const out = [...mid.querySelectorAll('*')].filter((e) => { const b = e.getBoundingClientRect(); return b.width && (b.left < rail.left - 0.5 || b.right > rail.right + 0.5 || e.scrollWidth > e.clientWidth + 1 && getComputedStyle(e).overflow === 'visible'); }).map((e) => e.className);
      const hosts = [...mid.querySelectorAll('.rldr')].map((c) => ({ keys: [...c.querySelectorAll('.hmk')].map((k) => k.textContent).join(), tip: c.title.split('\\n')[0] }));
      const ld = mid.querySelector('.rld').getBoundingClientRect(), rq = mid.querySelector('.rq').getBoundingClientRect();
      return { out, hosts, bar: mid.offsetWidth - mid.clientWidth, sw: cs.scrollbarWidth, scroll: mid.scrollHeight - mid.clientHeight, gap: Math.round(rq.top - ld.bottom),
        rule: getComputedStyle(mid.querySelector('.rq')).backgroundImage, borders: [mid.querySelector('.rq'), mid.querySelector('.rld')].map((e) => getComputedStyle(e).borderTopWidth) };
    })()`);
    check(r.out.length === 0, `${ctx}: part of the rail's middle sticks out of the rail (${JSON.stringify(r.out)})`);
    check(r.bar === 0 && r.sw === "none" && r.scroll <= 0, `${ctx}: the rail's middle shows a scrollbar or scrolls (${JSON.stringify(r)})`);
    check(r.hosts.length === 2 && r.hosts[0].keys === "ld,job" && r.hosts.every((x) => x.keys.startsWith("ld") && x.tip.startsWith("ld: the load")), `${ctx}: a host is not its two labelled meters with the legend (${JSON.stringify(r.hosts)})`);
    check(r.gap >= 8 && r.rule === "none" && r.borders.every((b) => b === "0px"), `${ctx}: the parts are uneven or a rule is drawn (${JSON.stringify(r)})`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
  await send("Emulation.setEmulatedMedia", { features: [] });
});

// SPEC V0.152: the rail's middle is one card per host and a quota grid. With two hosts and four providers at 1280 × 1000
// (room for all) and 1280 × 700, in light and dark: each card is centred in the rail and holds its ring (the load as a
// share of the cores, "130%"), its host's short name with the full name on hover, and its slot meter with its own
// tooltip; "1 waits" in the hot colour only while a job waits; the quota lines are one 2-column grid, each with its
// provider's initial (Claude and Claude (work account) apart: C and W), its value right-aligned, red only from 80 %,
// and a tooltip naming the provider and its host; nothing in the middle leaves the rail's width.
section('V0.152', async (h) => {
  const { send, evaluate, settle, navigate } = h;
  for (const [ht, dark] of [[1000, false], [1000, true], [700, false]]) {
    const ctx = `V0.152 desktop 1280x${ht}${dark ? " dark" : ""}`;
    try {
      await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: ht, deviceScaleFactor: 1, mobile: false });
      await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: dark ? "dark" : "light" }] });
      await navigate(`${origin}/?mock=rail&hosts=2`, "document.querySelectorAll('.rail .rmid .rldr').length === 2 && document.querySelectorAll('.rail .rmid .rqr').length === 4");
      await settle();
      const r = await evaluate(`(() => {
        const rail = document.querySelector('.rail').getBoundingClientRect(), mid = document.querySelector('.rail .rmid');
        const cx = (e) => { const b = e.getBoundingClientRect(); return b.left + b.width / 2; };
        const cards = [...mid.querySelectorAll('.rldr')].map((c) => ({ fig: c.querySelector('.hmr.ld .hmv').textContent, name: c.querySelector('.hmn')?.textContent ?? '', full: c.querySelector('.hmn')?.title ?? '',
          meter: !!c.querySelector('.hmr.job'), meterTip: c.querySelector('.hmr.job')?.title ?? '', note: c.querySelector('.hmr.job .hmv')?.textContent ?? '', noteHot: !!c.querySelector('.hmr.job.hot') }));
        const ld = mid.querySelector('.rld'), rq = mid.querySelector('.rq');
        const qs = [...mid.querySelectorAll('.rqr')].map((q) => ({ ini: q.querySelector('i').textContent, v: q.querySelector('span').textContent, hot: q.querySelector('span').classList.contains('hot'), tip: q.title.split('\\n')[0],
          left: Math.round(q.getBoundingClientRect().left), top: Math.round(q.getBoundingClientRect().top), right: q.getBoundingClientRect().right }));
        const wide = [...mid.querySelectorAll('*')].filter((e) => { const b = e.getBoundingClientRect(); return b.width && (b.left < rail.left - 0.5 || b.right > rail.right + 0.5); }).map((e) => e.className);
        return { cards, qs, wide, cols: new Set(qs.map((q) => q.left)).size, rows: new Set(qs.map((q) => q.top)).size,
          centred: Math.abs(cx(ld) - (rail.left + rail.width / 2)) < 2 && Math.abs(cx(rq) - (rail.left + rail.width / 2)) < 2,
          signal: getComputedStyle(document.documentElement).getPropertyValue('--signal-ink').trim(), midScroll: mid.scrollHeight - mid.clientHeight };
      })()`);
      const [desk, lab] = r.cards;
      check(desk.fig === "130%" && /^\d+%$/.test(lab.fig), `${ctx}: a ring's figure is not a share of the cores (${JSON.stringify(r.cards)})`);
      check(desk.name === "desk" && lab.name === "lab" && desk.full === "desk" && lab.full === "lab", `${ctx}: the cards do not name their hosts (${JSON.stringify(r.cards)})`);
      check(desk.meter && r.cards.every((c) => !c.meter || /^Heavy jobs: /.test(c.meterTip)), `${ctx}: a slot meter has no tooltip saying what it is (${JSON.stringify(r.cards)})`);
      check(desk.note === "95%+1" && desk.noteHot && lab.note === "", `${ctx}: the meter's word is wrong or shows when nothing waits (${JSON.stringify(r.cards)})`);
      check(r.qs.map((q) => q.ini).join() === "C,W,C,Z" && r.cols === 2 && r.rows === 2, `${ctx}: the quota grid or its initials are wrong (${JSON.stringify(r.qs)})`);
      check(r.qs.every((q) => q.hot === (parseInt(q.v) >= 80)), `${ctx}: a quota is red below 80 % or plain above (${JSON.stringify(r.qs)})`);
      check(r.qs[2].tip.startsWith("Claude on lab") && r.qs[0].tip.startsWith("Claude on "), `${ctx}: a quota's tooltip does not name its provider and host (${JSON.stringify(r.qs)})`);
      check(r.centred && r.wide.length === 0, `${ctx}: the rail's middle is off-centre or wider than the rail (${JSON.stringify({ centred: r.centred, wide: r.wide })})`);
      if (ht === 1000) check(r.midScroll <= 0, `${ctx}: the rail's middle scrolls with room for all (${r.midScroll} px)`);
    } catch (e) {
      failures.push(`${ctx}: ${e.message}`);
    }
  }
  await send("Emulation.setEmulatedMedia", { features: [] });
});

// SPEC 0.5 V0.118: the update status is a line in the layout, never over a control. In each phase, at the desktop and
// the phone width, with the thread open (its composer and send button) and with the details opened, the status's box
// overlaps no button, field or composer outside it (the rail's buttons included), sits below the top safe area and
// inside the screen; a client-only update shows none; a success goes by itself; a failure stays with Details and ×.
const OVER_CONTROLS = `(() => {
  const e = document.querySelector('.dbn');
  if (!e) return null;
  const b = e.getBoundingClientRect();
  const hits = [...document.querySelectorAll('button, textarea, input, select, [contenteditable=true], .cmp, .m-cmp, .rail .ri, .m-send')]
    .filter((o) => !e.contains(o) && !o.contains(e))
    .map((o) => {
      // the part of the control that shows: its box cut by every scrolling or clipping ancestor
      const r = o.getBoundingClientRect(), v = { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
      for (let a = o.parentElement; a; a = a.parentElement) {
        if (getComputedStyle(a).overflow === 'visible') continue;
        const c = a.getBoundingClientRect();
        v.left = Math.max(v.left, c.left); v.top = Math.max(v.top, c.top); v.right = Math.min(v.right, c.right); v.bottom = Math.min(v.bottom, c.bottom);
      }
      return { o, r: { ...v, width: Math.max(0, v.right - v.left), height: Math.max(0, v.bottom - v.top) } };
    })
    .filter(({ o, r }) => r.width > 0.5 && r.height > 0.5 && getComputedStyle(o).visibility !== 'hidden')
    .filter(({ r }) => r.left < b.right - 0.5 && b.left < r.right - 0.5 && r.top < b.bottom - 0.5 && b.top < r.bottom - 0.5)
    .map(({ o }) => (o.getAttribute('aria-label') || o.textContent || o.className).trim().slice(0, 30));
  const pos = getComputedStyle(e).position;
  return { top: b.top, bottom: b.bottom, left: b.left, right: b.right, vw: innerWidth, vh: innerHeight, pos, hits, text: e.textContent,
    btns: [...e.querySelectorAll('button')].map((x) => x.getAttribute('aria-label') || x.textContent.trim()) };
})()`;
const PHASES = ["build", "test", "drain", "switch", "failed", "done"];
// One section per size and phase: a success waits 5 s to go by itself, so they run side by side.
for (const [label, w, h, mobile] of [["desktop", 1280, 800, false], ["phone", 390, 844, true], ["phone", 360, 640, true]]) {
  for (const ph of PHASES) section(`V0.118 ${label} ${w}×${h} ${ph}`, async ({ send, evaluate, waitFor, settle, navigate, openScreen }) => {
    const ctx = `V0.118 ${label} ${w}×${h} ${ph}`;
    try {
      if (mobile) await openScreen({ url: `?mock=rail&open=t_mapperf&deploy=${ph}` }, w, h);
      else {
        await send("Emulation.setTouchEmulationEnabled", { enabled: false });
        await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
        await navigate(`${origin}/?mock=rail&open=t_mapperf&deploy=${ph}`, "!!document.querySelector('.cmp')");
      }
      await waitFor(`!!document.querySelector('.dbn')`, { soft: true });
      const b = await evaluate(OVER_CONTROLS);
      check(!!b, `${ctx}: no update status`);
      if (!b) return;
      if (process.env.FYR_SHOTS && w !== 360 && (ph === "drain" || ph === "failed")) writeFileSync(join(process.env.FYR_SHOTS, `deploy-${ph}-${label}.png`), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
      check(b.pos !== "fixed" && b.pos !== "absolute", `${ctx}: the status floats (position ${b.pos})`);
      check(b.hits.length === 0, `${ctx}: the status covers ${JSON.stringify(b.hits)} (${JSON.stringify(b)})`);
      check(b.top >= (mobile ? INSETS.top : 0) - 0.5 && b.left >= -0.5 && b.right <= b.vw + 0.5 && b.bottom <= b.vh, `${ctx}: outside the screen or the safe area (${JSON.stringify(b)})`);
      if (ph === "drain") {
        check(/waiting for 2 threads to finish/.test(b.text), `${ctx}: text "${b.text}"`);
        check(b.btns.includes("Restart now"), `${ctx}: no Restart now (V0.83) ${JSON.stringify(b.btns)}`);
      }
      if (ph === "failed") check(b.btns.includes("Details") && b.btns.includes("Dismiss"), `${ctx}: a failure lacks Details or Dismiss ${JSON.stringify(b.btns)}`);
      // the details, opened by a press, are in the layout too
      await evaluate(`(document.querySelector('.dbn .dbn-x') || [...document.querySelectorAll('.dbn button')].find((x) => x.textContent.trim() === 'Details'))?.click()`);
      await settle();
      const o = await evaluate(OVER_CONTROLS);
      check(!!o && o.hits.length === 0 && o.bottom <= o.vh, `${ctx} opened: the status covers ${JSON.stringify(o?.hits)} (${JSON.stringify(o)})`);
      if (ph === "done") {
        await waitFor(`!document.querySelector('.dbn')`, { soft: true, timeout: 9000 });
        check(!(await evaluate(`!!document.querySelector('.dbn')`)), `${ctx}: a success does not go by itself`);
      }
      if (ph === "failed") {
        await evaluate(`document.querySelector('.dbn button[aria-label=Dismiss]')?.click()`);
        await settle();
        check(!(await evaluate(`!!document.querySelector('.dbn')`)), `${ctx}: Dismiss does not hide the failure`);
      }
    } catch (e) {
      failures.push(`${ctx}: ${e.message}`);
    }
  });
  section(`V0.118 ${label} ${w}×${h} client`, async ({ evaluate, settle, navigate, openScreen }) => {
  try {
    if (mobile) await openScreen({ url: `?mock=rail&open=t_mapperf&deploy=client` }, w, h);
    else await navigate(`${origin}/?mock=rail&open=t_mapperf&deploy=client`, "!!document.querySelector('.cmp')");
    await settle();
    check(!(await evaluate(`!!document.querySelector('.dbn')`)), `V0.118 ${label} ${w}: a client-only update shows a status`);
  } catch (e) {
    failures.push(`V0.118 ${label} client: ${e.message}`);
  }
  });
}

// V0.54: with two hosts the phone rows name each thread's host and an offline host's rows say so; the offline
// thread's header carries "<host> offline since HH:MM · read-only" inside the screen; one host shows no host at all.
section('V0.54', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await openScreen({ url: "?mock=rail&hosts=2" }, 390, 844);
  const rows = await evaluate(`[...document.querySelectorAll('.m-mv')].map(r => ({ label: r.getAttribute('aria-label'), off: r.querySelector('.m-off')?.textContent ?? '', host: r.querySelector('.rhm .hmark')?.dataset.host ?? '', right: r.getBoundingClientRect().right }))`);
  const off = rows.filter((r) => r.off);
  check(off.length >= 1 && off.every((r) => r.off === "host-b offline"), `V0.54 rows: offline host not named (${JSON.stringify(off)})`);
  check(rows.some((r) => r.host === "desk"), `V0.54 rows: no row names its host (${JSON.stringify(rows.slice(0, 3))})`);
  check(rows.every((r) => r.right <= 390), "V0.54 rows: a row runs past the edge");
  await openScreen({ url: "?mock=rail&hosts=2&open=t_triage" }, 390, 844);
  const b = await evaluate(`(() => { const e = document.querySelector('.m-offl'); if (!e) return null; const r = e.getBoundingClientRect(); return { top: r.top, left: r.left, right: r.right, bottom: r.bottom, text: e.textContent, vh: innerHeight }; })()`);
  check(!!b, "V0.54 header: no offline line on the phone");
  if (b) {
    check(b.top >= INSETS.top && b.left >= 0 && b.right <= 390 && b.bottom < b.vh, `V0.54 header: offline line outside the screen (${JSON.stringify(b)})`);
    check(/^host-b offline since \d\d:\d\d · read-only$/.test(b.text), `V0.54 header: text "${b.text}"`);
  }
  await openScreen({ url: "?mock=rail&hosts=2&open=t_mapperf" }, 390, 844);
  const chip = await evaluate(`document.querySelector('.m-hostc')?.textContent ?? ''`);
  check(chip === "desk", `V0.54 header: an online thread's host chip reads "${chip}"`);
  await openScreen({ url: "?mock=rail&open=t_mapperf" }, 390, 844);
  const none = await evaluate(`!document.querySelector('.m-hostc, .m-hostn, .m-offl, .rhm')`);
  check(none, "V0.54: one host still shows a host on the phone");
} catch (e) {
  failures.push(`V0.54 hosts: ${e.message}`);
}
});

// SPEC 0.5 V0.125 (decision 0026, amends V0.124 (5) and V0.119): Settings → Hosts is its own page, the same on the
// desktop and the phone. 1 the nav has five pages and Hosts its badge; 2 the Hosts page's rows and foot; 3 a changed
// id on the host's page and Trust; 4 a plain host's page and the way back; 5 This device's new row.
for (const [label, w, ht] of [
  ["desktop", 1280, 800],
  ["phone", 390, 844],
]) section(`V0.125 ${label}`, async (hn) => {
  const { send, evaluate, waitFor, settle, navigate, openScreen, shot, key, tapCenter } = hn;
  const ctx = `V0.125 ${label}`;
  const phone = label === "phone";
  const root = phone ? ".m-settings" : ".dlg";
  const url = (page) => (phone ? `?mock=rail&hosts=2&settings=${page}` : `?mock=rail&hosts=2&dialog=settings&page=${page}`);
  const open = async (page, ready) => {
    if (phone) await openScreen({ url: url(page), ready }, w, ht);
    else await navigate(`${origin}/${url(page)}`, ready);
    await waitFor(ready);
    await settle();
  };
  const step = async (name, fn) => {
    try {
      await fn();
    } catch (e) {
      failures.push(`${ctx} ${name}: ${e.message}`);
    }
  };
  const NAV = `[...document.querySelectorAll('${root} .stn-i')].map(b => ({ t: b.querySelector('.stn-t')?.textContent, s: b.querySelector('.stn-s')?.textContent, badge: b.querySelector('.pst')?.textContent ?? '' }))`;
  // 1. The nav.
  await step("nav", async () => {
    const ready = phone ? `document.querySelectorAll('.m-settings .stn-i').length === 6` : `document.querySelectorAll('.dlg .stn-i').length === 6`;
    await open(phone ? "" : "projects", ready);
    const n = await evaluate(NAV);
    const hosts = n.find((x) => x.t === "Hosts"), projects = n.find((x) => x.t === "Projects");
    check(n.map((x) => x.t).join("|") === "Providers|Automation|Projects|Hosts|Devices|This device", `${ctx}: the nav ${JSON.stringify(n)}`);
    check(hosts?.s === "4 hosts · 1 needs you" && hosts?.badge === "1", `${ctx}: the Hosts nav row ${JSON.stringify(hosts)}`);
    check(/^\d+ projects$/.test(projects?.s ?? ""), `${ctx}: the Projects summary still counts hosts ${JSON.stringify(projects)}`);
    if (!phone) {
      const pj = await evaluate(`({ host: document.querySelectorAll('.dlg .sr.host').length, sub: [...document.querySelectorAll('.dlg .stp-sub')].map(e => e.textContent) })`);
      check(pj.host === 0 && !pj.sub.includes("Hosts"), `${ctx}: Projects still has a Hosts group ${JSON.stringify(pj)}`);
    } else {
      await open("projects", "document.querySelectorAll('.m-settings .pli').length > 3");
      const pj = await evaluate(`document.querySelectorAll('.m-settings .sr.host').length`);
      check(pj === 0, `${ctx}: Projects still has host rows (${pj})`);
    }
  });
  // 2. The Hosts page.
  await step("list", async () => {
    await open("hosts", `document.querySelectorAll('${root} .sr.host').length === 4`);
    const rows = await evaluate(`[...document.querySelectorAll('${root} .sr.host')].map(r => ({ tag: r.tagName, name: r.dataset.host, id: r.dataset.id ?? '', line: r.querySelector('.sr-d')?.textContent ?? '', bad: r.classList.contains('bad'), badge: r.querySelector('.pst')?.textContent ?? '', btns: r.querySelectorAll('button').length, dot: r.querySelector('.hmark')?.className ?? '', h: r.getBoundingClientRect().height, self: r.querySelector('.tag')?.textContent ?? '' }))`);
    const by = Object.fromEntries(rows.map((r) => [r.name, r]));
    check(rows.map((r) => r.name).join("|") === "desk|host-b|lab|old-nuc", `${ctx}: the order ${JSON.stringify(rows.map((r) => r.name))}`);
    check(rows.every((r) => r.tag === "BUTTON" && r.btns === 0), `${ctx}: a row is not one button or holds a button ${JSON.stringify(rows)}`);
    check(/^online since \d\d:\d\d · this machine · id F7QK$/.test(by.desk?.line) && by.desk?.self === "This install" && by.desk?.id === "F7QK-2M9X-AB34-CD56", `${ctx}: desk ${JSON.stringify(by.desk)}`);
    check(/^offline since \d\d:\d\d · tailnet · id M3RS$/.test(by["host-b"]?.line) && !by["host-b"]?.bad && /off/.test(by["host-b"]?.dot), `${ctx}: the offline row ${JSON.stringify(by["host-b"])}`);
    check(/ · LAN · no id$/.test(by["old-nuc"]?.line), `${ctx}: old-nuc ${JSON.stringify(by["old-nuc"])}`);
    check(by.lab?.line === "Another install answers as lab: id B7LA, it was AX2C." && by.lab?.bad && by.lab?.badge === "Needs you" && /^hmark/.test(by.lab?.dot), `${ctx}: lab ${JSON.stringify(by.lab)}`); // V0.170: the mark; the line and the pill say the id changed
    if (phone) check(rows.every((r) => r.h >= 55.5), `${ctx}: a row under 56 px ${JSON.stringify(rows.map((r) => r.h))}`);
    const foot = await evaluate(`[...document.querySelectorAll('${root} section[aria-label="Hosts"] .sg-f')].map(e => e.textContent).join(' ')`); // V0.159: the installs panel has its own groups
    // V0.132 adds the order's sentence before it.
    check(foot === "The first host is the default for New project, New thread and the folder picker. This order is kept on your hosts, so every device shows it. Fyr finds your other installs on the tailnet by itself. A host it cannot see goes in fyrd's config (hosts).", `${ctx}: the foot "${foot}"`);
    await shot(`hosts-${label}.png`);
    if (!phone) {
      await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
      await settle();
      await shot(`hosts-${label}-dark.png`);
      await send("Emulation.setEmulatedMedia", { features: [] });
    }
  });
  // V0.180 (#264): a config that changed under fyrd is said at the top of the installs panel, on both fronts.
  await step("config changed", async () => {
    await open("hosts&config=changed", `!!document.querySelector('${root} .peers .cfgchanged')`);
    const c = await evaluate(`(() => { const e = document.querySelector('${root} .peers .cfgchanged'); const r = e.getBoundingClientRect(); return { t: e.textContent, l: r.left, r: r.right, vw: innerWidth }; })()`);
    check(/^Config changed; restart fyrd to apply\. .*systemctl reload fyr/.test(c.t) && c.l >= 0 && c.r <= c.vw, `${ctx}: the config note ${JSON.stringify(c)}`);
    await shot(`hosts-config-changed-${label}.png`);
  });
  // 3. lab's page: the changed id and Trust.
  await step("changed", async () => {
    await open("hosts/lab", `!!document.querySelector('${root} .note.bad')`);
    const pg = await evaluate(`(() => { const ids = [...document.querySelectorAll('${root} .idf')].map(e => e.getAttribute('aria-label')); const b = [...document.querySelectorAll('${root} .btn')].find(x => x.textContent.trim() === 'Trust B7LA'); const r = b?.getBoundingClientRect();
      return { note: document.querySelector('${root} .note.bad')?.textContent ?? '', ids, trust: r ? { l: r.left, r: r.right, t: r.top, b: r.bottom, h: r.height } : null, vw: innerWidth, vh: innerHeight, pill: document.querySelector('${root} .stp .pst')?.textContent ?? '', titles: [...document.querySelectorAll('${root} .sr .sr-t')].map(e => e.textContent) }; })()`);
    check(pg.note.startsWith("Another install answers as lab.") && pg.ids.includes("id AX2C-7RTM-8PLQ-6VYD") && pg.ids.includes("id B7LA-4QZE-9KDF-3HXW") && pg.pill === "Needs you", `${ctx}: lab's page ${JSON.stringify(pg)}`);
    check(!pg.titles.includes("Trust"), `${ctx}: the ordinary Trust row shows beside a changed id ${JSON.stringify(pg.titles)}`);
    check(!!pg.trust && pg.trust.l >= 0 && pg.trust.r <= pg.vw && pg.trust.b <= pg.vh, `${ctx}: Trust B7LA outside the screen ${JSON.stringify(pg)}`);
    if (phone) check((pg.trust?.h ?? 0) >= 44, `${ctx}: Trust is ${pg.trust?.h} px high`);
    await shot(`host-changed-${label}.png`);
    if (!phone) {
      await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
      await settle();
      await shot(`host-changed-${label}-dark.png`);
      await send("Emulation.setEmulatedMedia", { features: [] });
    }
    await evaluate(`[...document.querySelectorAll('${root} .btn')].find(x => x.textContent.trim() === 'Trust B7LA')?.click()`);
    await waitFor(`!document.querySelector('${root} .note.bad')`, { soft: true });
    const after = await evaluate(`({ note: !!document.querySelector('${root} .note.bad'), id: [...document.querySelectorAll('${root} .idf')].map(e => e.getAttribute('aria-label')), cmds: ${COMMANDS("front.hostId")}.map(c => c.args.host) })`);
    check(!after.note && after.id.join() === "id B7LA-4QZE-9KDF-3HXW" && after.cmds.join() === "B7LA-4QZE-9KDF-3HXW" /* the host's id, not its name */, `${ctx}: after Trust ${JSON.stringify(after)}`);
    // The phone goes back up (a reload would forget the mock's trust): the list, then the nav.
    if (phone) {
      await evaluate(`history.back()`);
      await waitFor(`document.querySelectorAll('.m-settings .sr.host').length === 4`, { soft: true });
      const lab = await evaluate(`document.querySelector('.m-settings .sr.host[data-host=lab]')?.classList.contains('bad')`);
      check(lab === false, `${ctx}: lab's row is still red after Trust`);
      await evaluate(`history.back()`);
      await waitFor(`document.querySelectorAll('.m-settings .stn-i').length === 6`, { soft: true });
    }
    const n = await evaluate(NAV);
    const hosts = n.find((x) => x.t === "Hosts");
    check(hosts?.badge === "" && hosts?.s === "4 hosts · 1 offline", `${ctx}: the badge after Trust ${JSON.stringify(hosts)}`);
  });
  // 4. A plain host's page, and back to the list.
  await step("host", async () => {
    await open("hosts/host-b", `!!document.querySelector('${root} .idf.big')`);
    const pg = await evaluate(`({ id: document.querySelector('${root} .idf.big')?.getAttribute('aria-label'), copy: [...document.querySelectorAll('${root} .btn')].map(b => b.textContent.trim()), rows: [...document.querySelectorAll('${root} .sr')].map(r => [r.querySelector('.sr-t')?.textContent ?? '', r.querySelector('.sr-d')?.textContent ?? '']), pill: document.querySelector('${root} .stp .pst')?.textContent ?? '' })`);
    const row = (t) => pg.rows.find((r) => r[0] === t);
    check(pg.id === "id M3RS-QX7P-5LKA-2WNE" && pg.pill === "Offline", `${ctx}: host-b's id ${JSON.stringify(pg)}`);
    check(pg.copy.includes("Copy") || pg.copy.includes("Copied"), `${ctx}: no Copy ${JSON.stringify(pg.copy)}`);
    check(row("Address")?.[1] === "http://100.64.0.9:7778 · tailnet", `${ctx}: the Address row ${JSON.stringify(row("Address"))}`);
    check(/^Fyr found it by itself\./.test(row("Found on the tailnet")?.[1] ?? ""), `${ctx}: the origin row ${JSON.stringify(pg.rows)}`);
    check(/^Unreachable\. Its threads show as last read/.test(pg.rows.find((r) => /^Offline since/.test(r[0]))?.[1] ?? ""), `${ctx}: the state row ${JSON.stringify(pg.rows)}`);
    await shot(`host-${label}.png`);
    if (!phone) {
      await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
      await settle();
      await shot(`host-${label}-dark.png`);
      await send("Emulation.setEmulatedMedia", { features: [] });
      // Esc goes to the list, the focus on the row it came from.
      await key("Escape");
      await waitFor(`document.querySelector('.dlg .stp')?.dataset.page === 'hosts'`, { soft: true });
      const esc = await evaluate(`({ page: document.querySelector('.dlg .stp')?.dataset.page, focus: document.activeElement?.dataset?.host ?? '' })`);
      check(esc.page === "hosts" && esc.focus === "host-b", `${ctx}: Esc ${JSON.stringify(esc)}`);
      // The crumb too.
      await evaluate(`document.querySelector('.dlg .sr.host[data-host=lab]').click()`);
      await waitFor(`document.querySelector('.dlg .stp')?.dataset.page === 'hosts/lab'`, { soft: true });
      await evaluate(`document.querySelector('.dlg .crumb')?.click()`);
      await waitFor(`document.querySelector('.dlg .stp')?.dataset.page === 'hosts'`, { soft: true });
      check(await evaluate(`document.querySelector('.dlg .stp')?.dataset.page === 'hosts'`), `${ctx}: the crumb does not go back to Hosts`);
    } else {
      await open("hosts", `document.querySelectorAll('.m-settings .sr.host').length === 4`);
      await tapCenter(".m-settings .sr.host[data-host=host-b]", `!!document.querySelector('.m-settings .idf.big')`);
      await evaluate(`history.back()`);
      await waitFor(`document.querySelectorAll('.m-settings .sr.host').length === 4`, { soft: true });
      check(await evaluate(`document.querySelectorAll('.m-settings .sr.host').length === 4`), `${ctx}: Back does not return to the Hosts page`);
    }
  });
  // 5. This device.
  await step("device", async () => {
    await open("device", `!!document.querySelector('${root} .sr.howin')`);
    const d = await evaluate(`({ t: document.querySelector('${root} .sr.howin .sr-t')?.textContent, d: document.querySelector('${root} .sr.howin .sr-d')?.textContent, before: document.querySelector('${root} .sr.howin')?.previousElementSibling?.querySelector('.sr-t')?.textContent })`);
    check(d.t === "How this browser gets in" && /^On this machine itself \(localhost\)\.|^At .+, as owner\./.test(d.d ?? "") && d.before === "Show tool details", `${ctx}: This device ${JSON.stringify(d)}`);
    await shot(`device-${label}.png`);
    if (!phone) {
      await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
      await settle();
      await shot(`device-${label}-dark.png`);
      await send("Emulation.setEmulatedMedia", { features: [] });
    }
  });
});

// SPEC V0.127 (decision 0016): from any front the owner configures every host it follows. Providers (with System
// agents) and Automation show one part per host, this install first, the same on the desktop and the phone. 1 the
// parts, their lines, the offline host read-only with its note; 2 a host's provider is its own page (providers/lab/
// claude) with its host's quota, and Esc goes back to its row; 3 every change names its host (args.host); 4 the phone's
// controls are 44 px and nothing runs past the edge.
for (const [label, w, ht] of [
  ["desktop", 1280, 800],
  ["phone", 390, 844],
]) section(`V0.127 ${label}`, async (hn) => {
  const { send, evaluate, waitFor, settle, navigate, openScreen, shot, key } = hn;
  const ctx = `V0.127 ${label}`;
  const phone = label === "phone";
  const root = phone ? ".m-settings" : ".dlg";
  const url = (page) => (phone ? `?mock=rail&hosts=2&settings=${page}` : `?mock=rail&hosts=2&dialog=settings&page=${page}`);
  const open = async (page, ready) => {
    if (phone) await openScreen({ url: url(page), ready }, w, ht);
    else await navigate(`${origin}/${url(page)}`, ready);
    await waitFor(ready);
    await settle();
  };
  const step = async (name, fn) => {
    try {
      await fn();
    } catch (e) {
      failures.push(`${ctx} ${name}: ${e.message}`);
    }
  };
  const PARTS = `[...document.querySelectorAll('${root} .hpart')].map(p => ({ host: p.dataset.host, line: p.querySelector('.hpart-s')?.textContent ?? '', note: p.querySelector('.hnote')?.textContent ?? '', rows: [...p.querySelectorAll('.prow .sr-t')].map(e => e.textContent), add: p.querySelector('.addp')?.disabled ?? null, selects: [...p.querySelectorAll('select')].map(x => x.disabled) }))`;
  // 1. The Providers page: one part per host.
  await step("providers", async () => {
    await open("providers", `document.querySelectorAll('${root} .hpart').length === 4 && document.querySelectorAll('${root} .hpart .prow').length === 6`);
    const parts = await evaluate(PARTS);
    const by = Object.fromEntries(parts.map((x) => [x.host, x]));
    check(parts.map((x) => x.host).join("|") === "desk|host-b|lab|old-nuc", `${ctx}: the parts ${JSON.stringify(parts.map((x) => x.host))}`);
    check(by.desk?.line === "This install" && by.desk.rows.join("|") === "Claude|Claude (work account)|DeepSeek" && by.desk.add === false, `${ctx}: desk ${JSON.stringify(by.desk)}`);
    check(/^Online since \d\d:\d\d$/.test(by.lab?.line ?? "") && by.lab.rows.join("|") === "Claude|Z.ai" && !by.lab.note, `${ctx}: lab ${JSON.stringify(by.lab)}`);
    const off = by["host-b"];
    check(/^Offline since \d\d:\d\d$/.test(off?.line ?? "") && /^host-b is offline\. These are its settings as Fyr last read them at \d\d:\d\d; nothing here can be changed until it is back\.$/.test(off.note) && off.rows.join() === "Claude" && off.add === true && off.selects.length > 0 && off.selects.every(Boolean), `${ctx}: the offline host ${JSON.stringify(off)}`);
    // The old Host picker is gone: each part has its own System agents.
    const sa = await evaluate(`document.querySelectorAll('${root} section[aria-label="System agents"]').length`);
    check(sa === 3 /* old-nuc reports no install id: nothing is read from it */ && !(await evaluate(`!!document.querySelector('${root} select[aria-label="Host"]')`)), `${ctx}: System agents per host (${sa})`);
    if (!phone) {
      const nav = await evaluate(`document.querySelector('.dlg .stn-i[data-page=providers] .stn-s')?.textContent`);
      check(nav === "6 · 1 needs a login", `${ctx}: the Providers line counts every host "${nav}"`);
    }
    if (phone) {
      const small = await evaluate(`[...document.querySelectorAll('.m-settings .hpart button:not(:disabled), .m-settings .hpart select:not(:disabled)')].map(b => b.getBoundingClientRect()).filter(r => r.height > 0 && r.height < 44).length`);
      const wide = await evaluate(`document.scrollingElement.scrollWidth <= innerWidth && [...document.querySelectorAll('.m-settings .hpart')].every(p => p.getBoundingClientRect().right <= innerWidth)`);
      check(small === 0 && wide, `${ctx}: ${small} controls under 44 px, or past the edge (${wide})`);
    }
    await evaluate(`document.querySelector('${root} .hpart[data-host="lab"]').scrollIntoView({ block: 'start' })`);
    await settle();
    await shot(`hostsettings-providers-${label}.png`);
    await evaluate(`document.querySelector('${root} .hpart[data-host="host-b"]').scrollIntoView({ block: 'start' })`);
    await settle();
    await shot(`hostsettings-offline-${label}.png`);
    if (!phone) {
      await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
      await settle();
      await shot(`hostsettings-offline-${label}-dark.png`);
      await send("Emulation.setEmulatedMedia", { features: [] });
    }
  });
  // 2. lab's Claude is its own page, with lab's quota; Esc goes back to its row.
  await step("provider page", async () => {
    await evaluate(`document.querySelector('${root} .hpart[data-host="lab"] .prow')?.click()`);
    await waitFor(phone ? `/settings=providers\/lab(\/|%2F)claude/.test(location.search)` : `document.querySelector('.dlg .stp')?.dataset.page === 'providers/lab/claude'`, { soft: true });
    await settle();
    const pg = await evaluate(`({ lede: document.querySelector('${root} .stp-lede')?.textContent ?? '', search: location.search, q: document.querySelector('${root} .stp')?.textContent.includes('12 %') })`);
    check(pg.lede === "On lab." && pg.q, `${ctx}: lab's Claude ${JSON.stringify(pg)}`);
    await shot(`hostsettings-provider-${label}.png`);
    if (!phone) {
      await key("Escape");
      await waitFor(`document.querySelector('.dlg .stp')?.dataset.page === 'providers'`, { soft: true });
      const esc = await evaluate(`({ page: document.querySelector('.dlg .stp')?.dataset.page, focus: document.activeElement?.dataset?.sub ?? '' })`);
      check(esc.page === "providers" && esc.focus === "lab/claude", `${ctx}: Esc ${JSON.stringify(esc)}`);
    }
  });
  // 3. Every change names its host.
  await step("commands", async () => {
    await open("providers", `document.querySelectorAll('${root} .hpart .prow').length === 6`);
    await evaluate(`(() => { const s = document.querySelector('${root} .hpart[data-host="lab"] select[aria-label="Thread titles"]'); s.value = s.options[1].value; s.dispatchEvent(new Event('change', { bubbles: true })); })()`);
    await waitFor(`${COMMANDS("host.systemAgents")}.length === 1`, { soft: true });
    const sa = await evaluate(`${COMMANDS("host.systemAgents")}.map(c => ({ ...c.args, host: c.host }))`);
    check(sa.length === 1 && sa[0].host === "B7LA-4QZE-9KDF-3HXW" && sa[0].job === "title", `${ctx}: System agents on lab ${JSON.stringify(sa)}`);
    await open("automation", `document.querySelectorAll('${root} .hpart').length === 4 && !!document.querySelector('${root} .hpart[data-host="lab"] [role=switch]')`);
    const parts = await evaluate(`[...document.querySelectorAll('${root} .hpart')].map(p => ({ host: p.dataset.host, repos: [...p.querySelectorAll('.sr-t.mono, .mono.sr-t')].map(e => e.textContent), switches: [...p.querySelectorAll('[role=switch]')].map(x => x.disabled) }))`);
    const lab = parts.find((x) => x.host === "lab"), off = parts.find((x) => x.host === "host-b");
    check(!!lab && lab.switches.length >= 2 && lab.switches.every((d) => !d) && !!off && off.switches.every(Boolean), `${ctx}: Automation per host ${JSON.stringify(parts)}`);
    await evaluate(`document.querySelector('${root} .hpart[data-host="lab"] [role=switch]').click()`);
    await waitFor(`${COMMANDS("host.autoDeploy")}.length === 1`, { soft: true });
    const dep = await evaluate(`${COMMANDS("host.autoDeploy")}.map(c => ({ ...c.args, host: c.host }))`);
    check(dep.length === 1 && dep[0].host === "B7LA-4QZE-9KDF-3HXW" && dep[0].on === false, `${ctx}: auto-deploy on lab ${JSON.stringify(dep)}`);
    await shot(`hostsettings-automation-${label}.png`);
  });
});

// SPEC V0.132: New project, Change folder and New thread browse the chosen host's folders, the same on the desktop
// and the phone. 1 New project in Settings: the Host field (this install first, the offline host named), lab's folders
// in the picker, project.create with host lab; 2 an offline host says so and nothing is sent; 3 New thread: with no
// project the owner picks the host and the picker is that host's, thread.create names it; a project locks the host.
for (const [label, w, ht] of [
  ["desktop", 1280, 800],
  ["phone", 390, 844],
]) section(`V0.132 ${label}`, async (hn) => {
  const { evaluate, waitFor, settle, navigate, openScreen, shot } = hn;
  const ctx = `V0.132 ${label}`;
  const phone = label === "phone";
  const root = phone ? ".m-app" : ".dlg";
  const open = async (url, ready) => {
    if (phone) await openScreen({ url, ready }, w, ht);
    else await navigate(`${origin}/${url}`, ready);
    await waitFor(ready);
    await settle();
  };
  const step = async (name, fn) => {
    try {
      await fn();
    } catch (e) {
      failures.push(`${ctx} ${name}: ${e.message}`);
    }
  };
  const setHost = (h) => evaluate(`(() => { const s = document.querySelector('${root} .hostf select'); s.value = ${JSON.stringify(h)}; s.dispatchEvent(new Event('change', { bubbles: true })); })()`);
  const click = (sel, text) => evaluate(`(() => { const b = [...document.querySelectorAll(${JSON.stringify(sel)})].find(x => x.textContent.trim().startsWith(${JSON.stringify(text)})); b?.click(); return !!b; })()`);
  const PICKER = `({ title: (document.querySelector('.fsheet .m-shh') ?? document.querySelector('.fmodal .dh b'))?.textContent ?? '', rows: [...document.querySelectorAll('.fpick .fsec.browse .fpr')].map(b => b.textContent.trim()), note: document.querySelector('.fnote')?.textContent ?? '' })`;
  const closePicker = () => evaluate(`(document.querySelector('.fsheet .m-fchg') ?? document.querySelector('.fmodal .dh button'))?.click()`);
  // 1. New project on lab.
  await step("new project", async () => {
    await open(phone ? "?mock=rail&hosts=2&settings=projects" : "?mock=rail&hosts=2&dialog=settings&page=projects", `document.querySelectorAll('${root} .pli').length > 3`);
    if (!(await click(`${root} button`, "New project"))) throw new Error("no New project button");
    await waitFor(`!!document.querySelector('${root} .hostf select')`);
    const opts = await evaluate(`[...document.querySelectorAll('${root} .hostf option')].map(o => o.textContent)`);
    check(opts.join("|") === "desk (this install)|host-b (offline)|lab|old-nuc", `${ctx}: the hosts offered ${JSON.stringify(opts)}`);
    if (phone) {
      const h = await evaluate(`document.querySelector('${root} .hostf select').getBoundingClientRect().height`);
      check(h >= 44, `${ctx}: the Host field is ${h} px high`);
    }
    await setHost("lab");
    await click(`${root} button`, "Choose folder");
    await waitFor(`document.querySelectorAll('.fpick .fsec.browse .fpr').length > 0`);
    await settle();
    const pk = await evaluate(PICKER);
    check(/ on lab$/.test(pk.title) && pk.rows.some((r) => r.includes("lab-notes")) && pk.rows.every((r) => /lab-notes|experiments/.test(r)), `${ctx}: the picker shows lab's folders ${JSON.stringify(pk)}`);
    await shot(`hostfolders-picker-${label}.png`);
    await evaluate(`[...document.querySelectorAll('.fpick .fpr[data-row]:not([data-enter])')].find(b => b.textContent.includes('lab-notes'))?.click()`);
    await waitFor(`!document.querySelector('.fpick')`);
    await evaluate(`(() => { const i = document.querySelector('${root} .pform input'); i.value = 'Lab notes'; i.dispatchEvent(new Event('input', { bubbles: true })); })()`);
    await settle();
    await shot(`hostfolders-newproject-${label}.png`);
    await click(`${root} .pform button`, "Create project");
    await waitFor(`${COMMANDS("project.create")}.length === 1`, { soft: true });
    const c = await evaluate(`${COMMANDS("project.create")}.map(c => ({ ...c.args, host: c.host }))`);
    check(c.length === 1 && c[0].host === "B7LA-4QZE-9KDF-3HXW" /* the call names the host by id, not in args */ && c[0].folder === "/home/owner/lab-notes" && c[0].name === "Lab notes", `${ctx}: project.create ${JSON.stringify(c)}`);
  });
  // 2. An offline host: said plainly, nothing sent, no other host's folders.
  await step("offline", async () => {
    await open(phone ? "?mock=rail&hosts=2&settings=projects" : "?mock=rail&hosts=2&dialog=settings&page=projects", `document.querySelectorAll('${root} .pli').length > 3`);
    await click(`${root} button`, "New project");
    await waitFor(`!!document.querySelector('${root} .hostf select')`);
    await setHost("host-b");
    await settle();
    const note = await evaluate(`document.querySelector('${root} .hostf .hbad')?.textContent ?? ''`);
    check(/^host-b is offline since \d\d:\d\d\. Its folders cannot be read/.test(note), `${ctx}: the offline note "${note}"`);
    await click(`${root} button`, "Choose folder");
    await waitFor(`!!document.querySelector('.fnote')`, { soft: true });
    const pk = await evaluate(PICKER);
    check(/^host-b is offline/.test(pk.note) && pk.rows.length === 0, `${ctx}: the picker of an offline host ${JSON.stringify(pk)}`);
    await shot(`hostfolders-offline-${label}.png`);
    await closePicker();
    await waitFor(`!document.querySelector('.fpick, .fnote')`, { soft: true });
    await evaluate(`(() => { const i = document.querySelector('${root} .pform input'); i.value = 'X'; i.dispatchEvent(new Event('input', { bubbles: true })); })()`);
    await click(`${root} .pform button`, "Create project");
    await settle();
    const after = await evaluate(`({ n: ${COMMANDS("project.create")}.length, err: document.querySelector('${root} .pform .err')?.textContent ?? '' })`);
    check(after.n === 0 && /^host-b is offline/.test(after.err), `${ctx}: a create on an offline host ${JSON.stringify(after)}`);
  });
  // 3. New thread: the host follows the project; with no project the owner picks it.
  await step("new thread", async () => {
    await open(phone ? "?mock=rail&hosts=2&new" : "?mock=rail&hosts=2&dialog=new", `!!document.querySelector('${root} .hostf select')`);
    const locked = await evaluate(`({ dis: document.querySelector('${root} .hostf select').disabled, line: document.querySelector('${root} .hostf .srcl')?.textContent ?? '' })`);
    // V0.139: host first; the Host field is always the owner's pick (it no longer follows the project).
    check(!locked.dis && /^The thread runs there\./.test(locked.line), `${ctx}: the Host field is the owner's pick ${JSON.stringify(locked)}`);
    await evaluate(`document.querySelector('${root} .ppick, ${root} .m-ppick')?.click()`);
    await settle();
    await click(`${root} button, ${root} [role=option]`, "No project");
    await waitFor(`!document.querySelector('${root} .hostf select').disabled`);
    await setHost("lab");
    await settle();
    await evaluate(`(document.querySelector('${root} .m-fchg') ?? [...document.querySelectorAll('${root} .fline .btn')][0])?.click()`);
    await waitFor(`document.querySelectorAll('.fpick .fsec.browse .fpr').length > 0`);
    const pk = await evaluate(PICKER);
    check(/ on lab$/.test(pk.title) && pk.rows.some((r) => r.includes("experiments")), `${ctx}: New thread's picker ${JSON.stringify(pk)}`);
    await evaluate(`[...document.querySelectorAll('.fpick .fpr[data-row]:not([data-enter])')].find(b => b.textContent.includes('experiments'))?.click()`);
    await waitFor(`!document.querySelector('.fpick')`);
    await evaluate(`(() => { const t = document.querySelector('${root} textarea'); t.value = 'Map the arm'; t.dispatchEvent(new Event('input', { bubbles: true })); })()`);
    await settle();
    await shot(`hostfolders-newthread-${label}.png`);
    await evaluate(`[...document.querySelectorAll('${root} button')].find(b => /^Start/.test(b.textContent.trim()))?.click()`);
    await waitFor(`${COMMANDS("thread.create")}.length === 1`, { soft: true });
    const c = await evaluate(`${COMMANDS("thread.create")}.map(c => ({ ...c.args, host: c.host }))`);
    check(c.length === 1 && c[0].host === "B7LA-4QZE-9KDF-3HXW" && c[0].cwd === "/home/owner/experiments" && !c[0].projectId, `${ctx}: thread.create ${JSON.stringify(c)}`);
  });
  // 4. The owner's host order: reorder on the Hosts page (grip and Alt+↑ on the desktop, the Reorder mode on the
  // phone); the first host is the default in New project and New thread, and the per-host groups follow it.
  await step("host order", async () => {
    await open(phone ? "?mock=rail&hosts=2&settings=hosts" : "?mock=rail&hosts=2&dialog=settings&page=hosts", `document.querySelectorAll('${root} .sr.host').length === 4`);
    const ROWS = `[...document.querySelectorAll('${root} .sr.host')].map(r => r.dataset.host).join('|')`;
    if (phone) {
      await click(`${root} button`, "Reorder");
      await waitFor(`!!document.querySelector('${root} .hreo.mvrow')`);
      await evaluate(`document.querySelector('${root} [aria-label="Move lab up"]').click()`);
    } else {
      await evaluate(`document.querySelectorAll('.dlg .hreo .grip')[2].focus()`);
      await hn.key("ArrowUp", "ArrowUp", 1);
    }
    await waitFor(`${COMMANDS("front.hostOrder")}.length === 1`, { soft: true });
    await waitFor(`${ROWS} === 'desk|lab|host-b|old-nuc'`, { soft: true });
    const o = await evaluate(`({ cmds: ${COMMANDS("front.hostOrder")}.map(c => c.args.ids.join('|')), rows: ${ROWS} })`);
    // ids, in the owner's order; old-nuc reports none and is left out (the front keeps it after the named ones)
    check(o.cmds.join() === "F7QK-2M9X-AB34-CD56|B7LA-4QZE-9KDF-3HXW|M3RS-QX7P-5LKA-2WNE" && o.rows === "desk|lab|host-b|old-nuc", `${ctx}: reorder the hosts ${JSON.stringify(o)}`);
    await shot(`hostorder-${label}.png`);
    // lab first (as set on another front): it is the default.
    const q = "?mock=rail&hosts=2&hostOrder=lab,desk";
    await open(phone ? `${q}&settings=projects` : `${q}&dialog=settings&page=projects`, `document.querySelectorAll('${root} .pli').length > 3`);
    await click(`${root} button`, "New project");
    await waitFor(`!!document.querySelector('${root} .hostf select')`);
    const np = await evaluate(`({ v: document.querySelector('${root} .hostf select').value, first: document.querySelector('${root} .hostf option')?.value })`);
    check(np.v === "lab" && np.first === "lab", `${ctx}: New project's default host ${JSON.stringify(np)}`);
    await open(phone ? `${q}&settings=providers` : `${q}&dialog=settings&page=providers`, `document.querySelectorAll('${root} .hpart').length === 4`);
    const parts = await evaluate(`[...document.querySelectorAll('${root} .hpart')].map(p => p.dataset.host).join('|')`);
    check(parts.startsWith("lab|desk"), `${ctx}: the per-host groups follow the order (${parts})`);
  });
});

// SPEC V0.139: every screen that acts on a host says which. 1 New thread is host first: the first host in the owner's
// order, its projects only, its providers; another host switches them. 2 a project's screen: its host read-only under
// the header, its host's providers, Change folder on its host. 3 a project on an offline host is read-only and says
// so. 4 the desktop thread header names the host, as the phone's does.
for (const [label, w, ht] of [
  ["desktop", 1280, 800],
  ["phone", 390, 844],
]) section(`V0.139 ${label}`, async (hn) => {
  const { evaluate, waitFor, settle, navigate, openScreen, shot } = hn;
  const ctx = `V0.139 ${label}`;
  const phone = label === "phone";
  const root = phone ? ".m-app" : ".dlg";
  const open = async (url, ready) => {
    if (phone) await openScreen({ url, ready }, w, ht);
    else await navigate(`${origin}/${url}`, ready);
    await waitFor(ready);
    await settle();
  };
  const step = async (name, fn) => {
    try {
      await fn();
    } catch (e) {
      failures.push(`${ctx} ${name}: ${e.message}`);
    }
  };
  const setHost = (h) => evaluate(`(() => { const s = document.querySelector('${root} .hostf select'); s.value = ${JSON.stringify(h)}; s.dispatchEvent(new Event('change', { bubbles: true })); })()`);
  const chipName = `(document.querySelector('${root} .ppick .pln, ${root} .m-ppick .pln')?.textContent ?? '')`;
  // 1. New thread, host first.
  await step("new thread", async () => {
    await open(phone ? "?mock=rail&hosts=2&hostOrder=lab,desk&new" : "?mock=rail&hosts=2&hostOrder=lab,desk&dialog=new", `!!document.querySelector('${root} .hostf select')`);
    const s = await evaluate(`(() => { const host = document.querySelector('${root} .hostf'); const pick = document.querySelector('${root} .ppick, ${root} .m-ppick');
      return { first: !!host && !!pick && !!(host.compareDocumentPosition(pick) & Node.DOCUMENT_POSITION_FOLLOWING), v: host?.querySelector('select').value, proj: ${chipName},
        provs: [...document.querySelectorAll('${root} .row2 select')][0] ? [...[...document.querySelectorAll('${root} .row2 select')][0].options].map(o => o.value) : null }; })()`);
    check(s.first && s.v === "lab" && s.proj === "Robot arm", `${ctx}: host first, lab, lab's project ${JSON.stringify(s)}`);
    if (!phone) check(s.provs?.join() === "claude,zai", `${ctx}: lab's providers ${JSON.stringify(s.provs)}`);
    await evaluate(`document.querySelector('${root} .ppick, ${root} .m-ppick').click()`);
    await settle();
    const list = await evaluate(`[...document.querySelectorAll('${root} .plist .pln, ${root} .m-pickpanel .pln')].map(e => e.textContent)`);
    check(list.includes("Robot arm") && !list.includes("Server ops") && !list.some((n) => /Garden|dotfiles/.test(n)), `${ctx}: lab's projects only ${JSON.stringify(list)}`);
    await shot(`hostaware-newthread-${label}.png`);
    await evaluate(`document.querySelector('${root} .ppick, ${root} .m-ppick').click()`);
    await setHost("desk");
    await settle();
    const d = await evaluate(`({ proj: ${chipName}, provs: [...document.querySelectorAll('${root} .row2 select')][0] ? [...[...document.querySelectorAll('${root} .row2 select')][0].options].map(o => o.value) : null })`);
    check(d.proj !== "Robot arm" && d.proj !== "", `${ctx}: desk's project after the switch ${JSON.stringify(d)}`);
    if (!phone) check(!d.provs?.includes("zai"), `${ctx}: desk's providers after the switch ${JSON.stringify(d.provs)}`);
  });
  // 2. A project's screen: its host, its providers, its folders.
  await step("project editor", async () => {
    const ed = phone ? ".m-app" : ".dlg";
    await open(phone ? "?mock=rail&hosts=2&project=p_arm" : "?mock=rail&hosts=2&dialog=settings&page=projects/p_arm", `!!document.querySelector('${ed} .hostrow')`);
    const s = await evaluate(`(() => { const r = document.querySelector('${ed} .hostrow'); const head = document.querySelector('${ed} .phead');
      return { name: r.querySelector('.hchip .mono')?.textContent, after: !!head && !!(head.compareDocumentPosition(r) & Node.DOCUMENT_POSITION_FOLLOWING),
        provs: [...document.querySelectorAll('${ed} .pedit select')].map(x => [...x.options].map(o => o.textContent).join('|')) }; })()`);
    check(s.name === "lab" && s.after, `${ctx}: the project's host under its header ${JSON.stringify(s)}`);
    check(s.provs.some((o) => o.includes("Z.ai")), `${ctx}: lab's providers in the defaults ${JSON.stringify(s.provs)}`);
    await shot(`hostaware-project-${label}.png`);
    await evaluate(`[...document.querySelectorAll('${ed} .pedit button')].find(b => b.textContent.trim().startsWith('Change'))?.click()`);
    await waitFor(`document.querySelectorAll('.fpick .fsec.browse .fpr').length > 0`);
    const t = await evaluate(`(document.querySelector('.fsheet .m-shh') ?? document.querySelector('.fmodal .dh b'))?.textContent ?? ''`);
    check(/ on lab$/.test(t), `${ctx}: Change folder browses lab ("${t}")`);
  });
  // 3. A project on an offline host.
  await step("offline project", async () => {
    const ed = phone ? ".m-app" : ".dlg";
    await open(phone ? "?mock=rail&hosts=2&project=p_srv" : "?mock=rail&hosts=2&dialog=settings&page=projects/p_srv", `!!document.querySelector('${ed} .hostrow')`);
    const s = await evaluate(`({ line: document.querySelector('${ed} .hostrow')?.textContent ?? '', name: document.querySelector('${ed} .pname input')?.disabled ?? document.querySelector('${ed} .pname input')?.readOnly, change: [...document.querySelectorAll('${ed} .pedit button')].find(b => b.textContent.trim().startsWith('Change'))?.disabled })`);
    check(/host-b.*offline since \d\d:\d\d/.test(s.line) && /cannot be changed until it is back/.test(s.line) && s.name === true && s.change === true, `${ctx}: an offline host's project ${JSON.stringify(s)}`);
    await shot(`hostaware-offline-${label}.png`);
  });
  // 4. The thread header names the host (the phone did since V0.54).
  if (!phone)
    await step("thread header", async () => {
      await open("?mock=rail&hosts=2&open=t_mapperf", "!!document.querySelector('.thh .chips')");
      const c = await evaluate(`document.querySelector('.thh .hostc')?.textContent ?? ''`);
      check(c === "desk", `${ctx}: the header's host chip "${c}"`);
    });
});

// SPEC V0.140: the display surfaces say which host, with several hosts. 1 each ring of the desktop rail's load names
// its host (under it, and first in its label); one host: no name. 2 a change card on the board names its thread's
// host, the same on the desktop and the phone.
section("V0.140 rail", async (hn) => {
  const { evaluate, waitFor, navigate } = hn;
  const ctx = "V0.140 rail";
  try {
    await navigate(`${origin}/?mock=rail&hosts=2`, "!!document.querySelector('.shell')");
    await waitFor("document.querySelectorAll('.rldr').length === 2");
    const r = await evaluate(`[...document.querySelectorAll('.rldr')].map(x => ({ name: x.querySelector('.hmn')?.textContent ?? '', label: x.getAttribute('aria-label') }))`);
    check(r.length === 2 && r[0].name === "desk" && r[1].name === "lab" && r.every((x) => x.label.startsWith(`${x.name}: load `)), `${ctx}: the rings name their hosts ${JSON.stringify(r)}`);
    await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.shell')");
    await waitFor("document.querySelectorAll('.rldr').length === 1");
    const one = await evaluate(`({ name: !!document.querySelector('.hmn'), label: document.querySelector('.rldr')?.getAttribute('aria-label') ?? '' })`);
    check(!one.name && one.label.startsWith("Load "), `${ctx}: one host names none ${JSON.stringify(one)}`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
});
for (const [label, w, ht] of [
  ["desktop", 1280, 800],
  ["phone", 390, 844],
]) section(`V0.140 board ${label}`, async (hn) => {
  const { evaluate, waitFor, navigate, openScreen, shot } = hn;
  const ctx = `V0.140 board ${label}`;
  const phone = label === "phone";
  try {
    if (phone) await openScreen({ url: "?mock=1&hosts=2&changes", ready: "document.querySelectorAll('.bdfrom').length > 0" }, w, ht);
    else await navigate(`${origin}/?mock=1&hosts=2&dialog=changes`, "document.querySelectorAll('.bdfrom').length > 0");
    const cards = await evaluate(`[...document.querySelectorAll('.bdfrom')].map(b => b.querySelector('.bdhost')?.textContent ?? '')`);
    check(cards.length > 0 && cards.every((h) => h === "desk" || h === "host-b"), `${ctx}: every card names its host ${JSON.stringify(cards)}`);
    await shot(`hostdisplay-board-${label}.png`);
    if (phone) await openScreen({ url: "?mock=1&changes", ready: "document.querySelectorAll('.bdfrom').length > 0" }, w, ht);
    else await navigate(`${origin}/?mock=1&dialog=changes`, "document.querySelectorAll('.bdfrom').length > 0");
    check((await evaluate("document.querySelectorAll('.bdhost').length")) === 0, `${ctx}: one host: no host on the cards`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
});

// V0.35: a table in a message reads as cards on the phone: no cell is squeezed, nothing runs past the edge.
section('V0.35', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await openScreen({ url: "?mock=rail&open=t_triage" }, 390, 844);
  const r = await evaluate(`(() => {
    const tw = document.querySelector('.md .tblw'); if (!tw) return null;
    const cells = [...tw.querySelectorAll('td')].map(td => td.getBoundingClientRect());
    return { cards: getComputedStyle(tw.querySelector('tr')).display, narrow: Math.min(...cells.map(c => c.width)), right: Math.max(...cells.map(c => c.right)), vw: innerWidth, pageX: document.scrollingElement.scrollWidth <= innerWidth };
  })()`);
  check(!!r, "V0.35 table: no table in the message");
  if (r) {
    check(r.cards === "block", `V0.35 table: rows are not cards on the phone (${r.cards})`);
    check(r.narrow > 200, `V0.35 table: a cell is squeezed to ${Math.round(r.narrow)} px`);
    check(r.right <= r.vw && r.pageX, "V0.35 table: the table runs past the edge");
  }
} catch (e) {
  failures.push(`V0.35 table: ${e.message}`);
}
});

// V0.32: native feel on the phone: no page bounce, controls not selectable, message text is,
// and no browser menu on a long press outside text fields.
section('V0.32', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await openScreen({ url: "?mock=rail&open=t_triage" }, 390, 844);
  const r = await evaluate(`(() => {
    const cs = (el) => el ? getComputedStyle(el) : null;
    const btn = document.querySelector('.m-app button');
    const msg = document.querySelector('.m-app .prose, .m-app .msg');
    const ev = new MouseEvent('contextmenu', { bubbles: true, cancelable: true });
    document.querySelector('.m-app .m-tth, .m-app header')?.dispatchEvent(ev);
    return { over: cs(document.documentElement).overscrollBehaviorY, touch: cs(document.querySelector('.m-app')).touchAction,
      btn: cs(btn)?.userSelect, msg: cs(msg)?.userSelect ?? 'none', menuBlocked: ev.defaultPrevented };
  })()`);
  check(r.over === "none", `V0.32 native: the page can still be pulled (overscroll ${r.over})`);
  check(/manipulation/.test(r.touch), `V0.32 native: double tap can zoom (touch-action ${r.touch})`);
  check(r.btn === "none", `V0.32 native: buttons are selectable (${r.btn})`);
  check(r.msg === "text" || r.msg === "auto", `V0.32 native: message text is not selectable (${r.msg})`);
  check(r.menuBlocked, "V0.32 native: a long press opens the browser menu");
} catch (e) {
  failures.push(`V0.32 native: ${e.message}`);
}
});

// V0.31, V0.106: Settle from a row's menu settles that row's thread, in every state, while the open thread streams. The
// menu used to close on any scroll in the page, so the open thread following a run's output closed it under the pointer
// and the click opened the row instead. Desktop: the right-click menu; phone: the long-press sheet.
const SETTLE_ROWS = [
  // [id, title, section it starts in, what Settle does]
  ["t_triage", "Triage sprint issues", "unread", "leaves"], // done, not looked at (V0.188)
  ["t_e2e", "Speed up e2e suite", "needs", "leaves"], // failed
  ["t_nix", "Nix flake update", "seen", "leaves"], // idle
  ["t_babysit", "Babysit PR #4170", "working", "waits"], // running: settles when the run ends (V0.106)
];
for (const [front, w, hh] of [["desktop", 1280, 800], ["phone", 390, 844]]) section(`V0.31 settle from a row ${front}`, async ({ send, evaluate, waitFor, settle, navigate, openScreen }) => {
  const ctx = `V0.31 settle from a row (${front})`;
  const settles = (id) => `(window.__fyrCommands ?? []).filter((c) => c.action === 'thread.settle' && c.threadId === ${JSON.stringify(id)}).length`;
  const allSettles = `(window.__fyrCommands ?? []).filter((c) => c.action === 'thread.settle').map((c) => c.threadId)`;
  const mouse = async (x, y, button = "left") => {
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button, clickCount: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button, clickCount: 1 });
  };
  // The element's point, after scrolling it into view and letting that scroll end (a scroll that is still moving the
  // row closes its menu, rightly).
  const center = async (sel) => {
    if (!(await evaluate(`(() => { const el = ${sel}; if (!el) return false; el.scrollIntoView({ block: 'center', behavior: 'instant' }); return true; })()`))) return null;
    await settle();
    return evaluate(`(() => { const r = (${sel}).getBoundingClientRect(); return { x: r.left + Math.min(60, r.width / 2), y: r.top + r.height / 2 }; })()`);
  };
try {
  if (front === "desktop") {
    // The open thread runs and follows its output (t_mapperf streams below).
    await navigate(`${origin}/?mock=rail&open=t_mapperf`, "!!document.querySelector('.convo') && typeof window.__fyrBurst === 'function'");
    await evaluate(`window.__fyrBurst('t_mapperf', 40)`);
    await waitFor(`document.querySelector('.convo').scrollHeight > document.querySelector('.convo').clientHeight + 200`, { what: "the open thread to be taller than its pane" });
    await settle();
    // The open thread moves while the menu is open, as a streaming run does: its pane scrolls.
    const stream = async () => {
      await evaluate(`(() => { const c = document.querySelector('.convo'); c.scrollTop = 0; c.dispatchEvent(new Event('scroll')); c.scrollTop = 1e9; c.dispatchEvent(new Event('scroll')); })()`);
      await settle();
    };
    const ids = await evaluate(`Object.fromEntries([...document.querySelectorAll('.ib [id^=sec-]')].map((s) => [s.id, [...s.querySelectorAll('[data-id]')].map((r) => r.dataset.id)]))`);
    const pin = await center(`document.querySelector('.ib [data-id="t_copy"]')`);
    if (!pin) throw new Error(`no row t_copy (${JSON.stringify(ids)})`);
    await mouse(pin.x, pin.y, "right");
    await settle();
    const pinItem = await center(`[...document.querySelectorAll('.ctxm-i')].find((b) => b.textContent.startsWith('Pin'))`);
    if (!pinItem) throw new Error(`no Pin in the menu of t_copy (${await evaluate(`JSON.stringify([...document.querySelectorAll('.ctxm-i')].map((b) => b.textContent))`)}, at ${JSON.stringify(pin)})`);
    await mouse(pinItem.x, pinItem.y);
    await waitFor(`!!document.querySelector('#sec-pinned [data-id="t_copy"]')`, { what: "t_copy to be pinned" });
    const rows = [...SETTLE_ROWS, ["t_copy", "Rewrite onboarding copy", "pinned", "leaves"]];
    // A folded section: Snoozed opens on a click on its header; its first row.
    await evaluate(`document.querySelector('#sec-snoozed .sec')?.click()`);
    await waitFor(`!!document.querySelector('#sec-snoozed [data-id]')`, { what: "the Snoozed section to open" });
    rows.push([await evaluate(`document.querySelector('#sec-snoozed [data-id]').dataset.id`), "", "snoozed", "leaves"]);
    for (const [id, , sec, then] of rows) {
      const before = await evaluate(settles(id));
      const r = await center(`document.querySelector('#sec-${sec} [data-id="${id}"]')`);
      check(!!r, `${ctx}: no row ${id} in ${sec}`);
      if (!r) continue;
      await mouse(r.x, r.y, "right");
      await settle();
      await stream();
      const item = await center(`[...document.querySelectorAll('.ctxm .ctxm-i')].find((b) => b.textContent.startsWith('Settle'))`);
      check(!!item, `${ctx}: the menu of ${id} (${sec}) closed when the open thread scrolled, or has no Settle`);
      if (!item) continue;
      await mouse(item.x, item.y);
      await waitFor(`${settles(id)} > ${before}`, { soft: true });
      check((await evaluate(settles(id))) === before + 1, `${ctx}: Settle in the menu of ${id} (${sec}) sent no settle for it (sent: ${JSON.stringify(await evaluate(allSettles))})`);
      check(await evaluate(`!document.querySelector('.ctxm')`), `${ctx}: the menu stays open after Settle on ${id}`);
      if (then === "leaves") {
        await waitFor(`!document.querySelector('#sec-${sec} [data-id="${id}"]')`, { soft: true });
        check(await evaluate(`!document.querySelector('#sec-${sec} [data-id="${id}"]')`), `${ctx}: ${id} stays in ${sec} after Settle`);
      }
    }
    // The open thread itself, and a row whose Settle the host refuses (a question is open): disabled, with the reason.
    const before = await evaluate(settles("t_mapperf"));
    const own = await center(`document.querySelector('.ib [data-id="t_mapperf"]')`);
    await mouse(own.x, own.y, "right");
    await settle();
    await stream();
    const ownItem = await center(`[...document.querySelectorAll('.ctxm .ctxm-i')].find((b) => b.textContent.startsWith('Settle'))`);
    check(!!ownItem, `${ctx}: the open thread's menu closed when it scrolled`);
    if (ownItem) {
      await mouse(ownItem.x, ownItem.y);
      await waitFor(`${settles("t_mapperf")} > ${before}`, { soft: true });
      check((await evaluate(settles("t_mapperf"))) === before + 1, `${ctx}: Settle in the open thread's menu sent no settle for it`);
    }
    const ask = await center(`document.querySelector('.ib [data-id="t_lasso"]')`);
    await mouse(ask.x, ask.y, "right");
    await settle();
    const dis = await evaluate(`(() => { const b = [...document.querySelectorAll('.ctxm .ctxm-i')].find((b) => b.textContent.startsWith('Settle')); return b ? { disabled: b.getAttribute('aria-disabled'), title: b.title } : null; })()`);
    check(dis?.disabled === "true" && /question/i.test(dis.title), `${ctx}: an asking thread's Settle is not disabled with its reason (${JSON.stringify(dis)})`);
  } else {
    await openScreen({ url: "?mock=rail" }, w, hh);
    const rows = [...SETTLE_ROWS];
    await evaluate(`document.querySelector('#m-sec-snoozed .m-sec')?.click()`);
    await waitFor(`!!document.querySelector('#m-sec-snoozed .m-mv')`, { what: "the Snoozed section to open" });
    rows.push(["", await evaluate(`document.querySelector('#m-sec-snoozed .m-mv').getAttribute('aria-label').split(',')[0]`), "snoozed", "leaves"]);
    for (const [id, title, sec, then] of rows) {
      const row = `[...document.querySelectorAll('#m-sec-${sec} .m-mv')].find((r) => r.getAttribute('aria-label').startsWith(${JSON.stringify(title + ",")}))`;
      const n0 = (await evaluate(allSettles)).length;
      const c = await center(row);
      check(!!c, `${ctx}: no row "${title}" in ${sec} (${await evaluate(`JSON.stringify([...document.querySelectorAll('#m-sec-${sec} .m-mv')].map((r) => r.getAttribute('aria-label')))`)})`);
      if (!c) continue;
      // A long press opens the actions sheet (see tapCenter: touchCancel, so no click lands on the sheet).
      await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: c.x, y: c.y }] });
      await waitFor(`!!document.querySelector('.m-sheet .m-mi')`, { what: `the actions sheet of "${title}"` });
      await send("Input.dispatchTouchEvent", { type: "touchCancel", touchPoints: [] });
      await settle();
      await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find((b) => b.textContent.startsWith('Settle'))?.click()`);
      await waitFor(`${allSettles}.length > ${n0}`, { soft: true });
      const sent = (await evaluate(allSettles)).slice(n0);
      check(sent.length === 1 && (!id || sent[0] === id), `${ctx}: Settle in the sheet of "${title}" sent ${JSON.stringify(sent)}, not ${id || "its thread"}`);
      if (then === "leaves") {
        await waitFor(`!(${row})`, { soft: true });
        check(await evaluate(`!(${row})`), `${ctx}: "${title}" stays in ${sec} after Settle`);
      }
      await waitFor(`!document.querySelector('.m-sheet')`, { soft: true });
      await settle();
    }
  }
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// V0.31: a right-click on a thread row opens Fyr's menu; elsewhere nothing opens.
section('V0.31', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('[data-id]')");
  const rightClick = async (x, y) => {
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "right", clickCount: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "right", clickCount: 1 });
    await settle();
  };
  const row = await evaluate(`(() => { const r = document.querySelector('[data-id]').getBoundingClientRect(); return { x: r.left + 40, y: r.top + r.height / 2 }; })()`);
  await rightClick(row.x, row.y);
  const m = await evaluate(`(() => { const el = document.querySelector('.ctxm'); if (!el) return null; const r = el.getBoundingClientRect(); return { items: [...el.querySelectorAll('.ctxm-i .ell')].map(x => x.textContent), inside: r.right <= innerWidth && r.bottom <= innerHeight }; })()`);
  check(!!m && m.items[0] === "Open" && m.items.includes("Copy link"), `V0.31 menu: no Fyr menu on a thread row (${JSON.stringify(m)})`);
  check(!!m && m.inside, "V0.31 menu: the menu runs outside the window");
  await key("Escape");
  const gone = await evaluate(`!document.querySelector('.ctxm')`);
  check(gone, "V0.31 menu: Escape does not close the menu");
  const empty = await evaluate(`(() => { const r = document.querySelector('.th, .empty-th').getBoundingClientRect(); return { x: r.left + r.width / 2, y: r.bottom - 120 }; })()`);
  await rightClick(empty.x, empty.y);
  const none = await evaluate(`!document.querySelector('.ctxm')`);
  check(none, "V0.31 menu: a menu opened where nothing should");
} catch (e) {
  failures.push(`V0.31 menu: ${e.message}`);
}
});

// V0.104: the owner pins a thread from the row's menu; it moves above every section with a pin mark, once. Unpin
// puts it back. The phone pins from the thread's ⋯ sheet and draws the same block on top of its list.
section('V0.104', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
let pinId = "";
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('#sec-working [data-id]')");
  check(!(await evaluate(`!!document.querySelector('#sec-pinned')`)), "V0.104 pin: a Pinned block shows with nothing pinned");
  const row = await evaluate(`(() => { const el = [...document.querySelectorAll('#sec-working [data-id]')].pop(); const r = el.getBoundingClientRect(); return { id: el.dataset.id, x: r.left + 40, y: r.top + r.height / 2 }; })()`);
  pinId = row.id;
  const menuPick = async (label) => {
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x: row.x, y: row.y, button: "right", clickCount: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: row.x, y: row.y, button: "right", clickCount: 1 });
    await settle();
    return evaluate(`(() => { const it = [...document.querySelectorAll('.ctxm-i')].find(x => x.querySelector('.ell')?.textContent === ${JSON.stringify(label)}); it?.click(); return !!it; })()`);
  };
  check(await menuPick("Pin"), "V0.104 pin: the row menu has no Pin");
  await waitFor(`!!document.querySelector('#sec-pinned [data-id="${pinId}"]')`, { soft: true });
  const r = await evaluate(`(() => {
    const blocks = [...document.querySelectorAll('.list > div')].filter(d => d.querySelector('[data-id]'));
    const pinned = document.querySelector('#sec-pinned [data-id="${pinId}"]');
    return { first: blocks[0]?.id, mark: !!pinned?.querySelector('.hd .pin'), once: document.querySelectorAll('.list [data-id="${pinId}"]').length };
  })()`);
  check(r.first === "sec-pinned", `V0.104 pin: the Pinned block is not on top (${r.first})`);
  check(r.mark, "V0.104 pin: the pinned row has no pin mark");
  check(r.once === 1, `V0.104 pin: the pinned row shows ${r.once} times`);
  if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, "pin-desktop.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  // The row now sits in the Pinned block: right-click it there.
  Object.assign(row, await evaluate(`(() => { const r = document.querySelector('#sec-pinned [data-id="${pinId}"]').getBoundingClientRect(); return { x: r.left + 40, y: r.top + r.height / 2 }; })()`));
  check(await menuPick("Unpin"), "V0.104 pin: the pinned row's menu has no Unpin");
  await waitFor(`!document.querySelector('#sec-pinned')`, { soft: true });
  check(await evaluate(`!document.querySelector('#sec-pinned') && !!document.querySelector('#sec-working [data-id="${pinId}"]')`), "V0.104 pin: Unpin does not put the row back");
} catch (e) {
  failures.push(`V0.104 pin desktop: ${e.message}`);
}
try {
  await openScreen({ url: `?mock=rail&open=${pinId}` }, 390, 844);
  await tapCenter(".m-tr1 .b44:last-child", "!!document.querySelector('.m-sheet .m-mi')");
  check(await evaluate(`(() => { const b = [...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.querySelector('.ell')?.firstChild?.textContent === 'Pin'); b?.click(); return !!b; })()`), "V0.104 pin phone: the thread sheet has no Pin");
  await waitFor(`!document.querySelector('.m-sheet')`, { soft: true });
  await tapCenter('button[aria-label="Back to the inbox"]', "!!document.querySelector('.m-list')");
  await waitFor(`!!document.querySelector('#m-sec-pinned')`, { soft: true });
  const r = await evaluate(`(() => { const secs = [...document.querySelectorAll('.m-list > section')]; const p = document.querySelector('#m-sec-pinned'); return { first: secs[0]?.id, rows: p ? p.querySelectorAll('.pin').length : 0 }; })()`);
  check(r.first === "m-sec-pinned", `V0.104 pin phone: the Pinned block is not on top (${r.first})`);
  check(r.rows === 1, `V0.104 pin phone: the Pinned block has ${r.rows} marked rows`);
  if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, "pin-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
} catch (e) {
  failures.push(`V0.104 pin phone: ${e.message}`);
}
});

// V0.185: until work is done is the last choice of the snooze list on both fronts: on for a working thread, off with
// the reason for an idle one. A snoozed row says so instead of a time.
section('V0.185', async (h) => {
  const { send, evaluate, waitFor, settle, navigate, tapCenter, key, openScreen } = h;
  const png = async (name) => process.env.FYR_SHOTS && writeFileSync(join(process.env.FYR_SHOTS, name), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  const last = (sel) => `(() => { const b = [...document.querySelectorAll('${sel}')].pop(); return b ? { text: b.textContent, off: b.disabled } : null; })()`;
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=rail&open=t_mapperf`, "!!document.querySelector('.ib [data-id=t_mapperf]')");
    await key("s");
    await waitFor(`!!document.querySelector('.menu .mi')`, { what: "the snooze menu" });
    const on = await evaluate(last(".menu .mi"));
    check(on?.text.includes("Until work is done") && on.text.includes("at most 24 h") && !on.off, `V0.185 desktop: the choice for a working thread: ${JSON.stringify(on)}`);
    await settle();
    await png("snooze-done-desktop.png");
    await evaluate(`[...document.querySelectorAll('.menu .mi')].pop().click()`);
    await waitFor(`(window.__fyrCommands ?? []).some(c => c.action === 'thread.snoozeWhenDone' && c.threadId === 't_mapperf')`, { what: "the snooze command" });
    // The header toggles, and the fold is remembered: open the section only when it is folded.
    await evaluate(`(() => { const s = document.querySelector('#sec-snoozed'); if (!s.querySelector('[data-id]')) s.querySelector('.sec').click(); })()`);
    await waitFor(`!!document.querySelector('#sec-snoozed [data-id=t_mapperf]')`, { what: "the snoozed row" });
    check((await evaluate(`document.querySelector('#sec-snoozed [data-id=t_mapperf]').textContent`)).includes("until work is done"), "V0.185 desktop: the row does not say until work is done");
    await navigate(`${origin}/?mock=rail&open=t_nix`, "!!document.querySelector('.ib [data-id=t_nix]')");
    await key("s");
    await waitFor(`!!document.querySelector('.menu .mi')`, { what: "the snooze menu" });
    const off = await evaluate(last(".menu .mi"));
    check(off?.off && off.text.includes("Nothing is working"), `V0.185 desktop: the choice for an idle thread: ${JSON.stringify(off)}`);
  } catch (e) {
    failures.push(`V0.185 desktop: ${e.message}`);
  }
  try {
    const sheet = async (id) => {
      await openScreen({ url: `?mock=rail&open=${id}` }, 390, 844);
      await tapCenter(".m-tr1 .b44:last-child", "!!document.querySelector('.m-sheet .m-mi')");
      await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('Snooze'))?.click()`);
      await waitFor(`[...document.querySelectorAll('.m-sheet .m-mi')].some(b => b.textContent.startsWith('Until work is done'))`, { what: "the snooze sheet" });
      return evaluate(`(() => { const b = [...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('Until work is done')); return { text: b.textContent, off: b.disabled }; })()`);
    };
    const off = await sheet("t_nix");
    check(off.off && off.text.includes("Nothing is working"), `V0.185 phone: the choice for an idle thread: ${JSON.stringify(off)}`);
    const on = await sheet("t_mapperf");
    check(!on.off && on.text.includes("at most 24 h"), `V0.185 phone: the choice for a working thread: ${JSON.stringify(on)}`);
    await settle();
    await png("snooze-done-phone.png");
  } catch (e) {
    failures.push(`V0.185 phone: ${e.message}`);
  }
});

// V0.188: a finished thread the owner has not looked at is in Unread, directly above Seen; Mark seen and Mark unseen move
// it, ⇧E leaves it, and both fronts show the same. The row menus are the shared seenToggle.
section('V0.188', async (h) => {
  const { send, evaluate, waitFor, navigate, tapCenter, key, openScreen, settle, shot } = h;
  // The phone's rows carry no data-id: they are found by title.
  const TITLE = { t_triage: "Triage sprint issues" };
  const phoneRow = (sec, id) => `[...document.querySelectorAll('#m-sec-${sec} .m-swp')].find(r => r.textContent.includes(${JSON.stringify(TITLE[id])}))`;
  const inSec = (front, sec, id) => (front ? `!!${phoneRow(sec, id)}` : `!!document.querySelector('#sec-${sec} [data-id=${id}]')`);
  const pressRow = async (sec, id, until) => {
    await evaluate(`(() => { document.querySelectorAll('[data-e2e-row]').forEach(e => e.removeAttribute('data-e2e-row')); const r = ${phoneRow(sec, id)}; r.scrollIntoView({ block: 'center' }); r.querySelector('.m-mv').setAttribute('data-e2e-row', ''); })()`);
    await settle();
    await tapCenter("[data-e2e-row]", until, { long: true });
  };
  const order = (front) => `[...document.querySelectorAll('[id^=${front}sec-]')].map(e => e.id.replace('${front}sec-', '')).filter(k => k !== 'pinned' && k !== 'slots')`;
  const WANT = '["needs","working","unread","seen","snoozed","settled"]';
  try {
    await navigate(`${origin}/?mock=rail`, inSec("", "unread", "t_triage"));
    const d = await evaluate(`({ order: ${order("")}, inSeen: ${inSec("", "seen", "t_triage")}, inNeeds: ${inSec("", "needs", "t_triage")}, rail: document.querySelector('.rail [data-sec=unread]')?.textContent ?? '', railSeen: document.querySelector('.rail [data-sec=seen] .k')?.textContent })`);
    check(JSON.stringify(d.order) === WANT, `V0.188 desktop: the section order (${JSON.stringify(d.order)})`);
    check(!d.inSeen && !d.inNeeds, `V0.188 desktop: an unread thread is only in Unread (${JSON.stringify(d)})`);
    check(/Unread/.test(d.rail) && /g6/.test(d.rail) && d.railSeen === "g3", `V0.188 desktop: the rail's Unread and its keys (${JSON.stringify(d)})`);
    // The chip of an open change on a stopped thread says so, not "working".
    await navigate(`${origin}/?mock=1`, inSec("", "unread", "t_ended"));
    await evaluate(`document.querySelector('#sec-unread').scrollIntoView({ block: 'start' })`);
    await settle();
    const chip = await evaluate(`document.querySelector('#sec-unread [data-id=t_ended] .chgchip')?.textContent ?? ''`);
    check(chip === "change open", `V0.188 desktop: the chip of a stopped thread's open change ("${chip}")`);
    await shot("unread-desktop.png");
    await navigate(`${origin}/?mock=rail`, inSec("", "unread", "t_triage"));
    const menu = async (id, label) => {
      const r = await evaluate(`(() => { const e = document.querySelector('.list [data-id=${id}]'); e.scrollIntoView({ block: 'center' }); const b = e.getBoundingClientRect(); return { x: b.left + 40, y: b.top + b.height / 2 }; })()`);
      await send("Input.dispatchMouseEvent", { type: "mousePressed", x: r.x, y: r.y, button: "right", clickCount: 1 });
      await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: r.x, y: r.y, button: "right", clickCount: 1 });
      await waitFor(`[...document.querySelectorAll('.ctxm-i')].some(x => x.querySelector('.ell')?.textContent === ${JSON.stringify(label)})`, { what: `the row menu's ${label}` });
      await evaluate(`[...document.querySelectorAll('.ctxm-i')].find(x => x.querySelector('.ell')?.textContent === ${JSON.stringify(label)}).click()`);
    };
    await menu("t_triage", "Mark seen");
    await waitFor(inSec("", "seen", "t_triage"), { what: "Mark seen to move the row to Seen" });
    await menu("t_triage", "Mark unseen");
    await waitFor(inSec("", "unread", "t_triage"), { what: "Mark unseen to move the row back to Unread" });
    // Folding is the section's own.
    await evaluate(`document.querySelector('#sec-unread .sec').click()`);
    await waitFor(`!${inSec("", "unread", "t_triage")}`, { what: "Unread to fold" });
    check(await evaluate(`document.querySelectorAll('#sec-seen [data-id]').length > 0`), "V0.188 desktop: folding Unread folded Seen too");
    await evaluate(`document.querySelector('#sec-unread .sec').click()`);
    await waitFor(inSec("", "unread", "t_triage"), { what: "Unread to unfold" });
    // ⇧E from a Seen thread settles Seen and sends nothing for Unread.
    await navigate(`${origin}/?mock=rail&open=t_nix`, inSec("", "seen", "t_nix"));
    const before = await evaluate(`[...document.querySelectorAll('#sec-unread [data-id]')].map(e => e.dataset.id)`);
    check(before.length > 0, "V0.188 desktop: the fixture has an unread thread");
    await evaluate(`document.activeElement?.blur()`);
    await key("E", "KeyE", 8);
    await waitFor(`document.querySelectorAll('#sec-seen [data-id]').length === 0`, { what: "⇧E to settle Seen" });
    const r = await evaluate(`({ left: [...document.querySelectorAll('#sec-unread [data-id]')].map(e => e.dataset.id), sent: (window.__fyrCommands ?? []).filter(c => c.action === 'thread.settle').map(c => c.threadId) })`);
    check(JSON.stringify(r.left) === JSON.stringify(before) && !before.some((id) => r.sent.includes(id)), `V0.188 desktop: ⇧E touched Unread (${JSON.stringify(r)})`);
    // From a row in Unread, ⇧E sweeps nothing and says so; the same row picked with x is the owner's choice and settles.
    // Mark unseen first: it keeps the open thread unread while the checks run.
    await navigate(`${origin}/?mock=rail&open=t_triage`, inSec("", "unread", "t_triage"));
    const sentFor = `(window.__fyrCommands ?? []).filter(c => c.action === 'thread.settle' && c.threadId === 't_triage').length`;
    await evaluate(`document.activeElement?.blur()`);
    await key("u", "KeyU");
    await key("E", "KeyE", 8);
    await waitFor(`/^Nothing settled; 1 unread thread kept/.test(document.querySelector('.toast')?.textContent ?? '')`, { what: "the toast of a sweep that kept an unread thread" });
    check((await evaluate(sentFor)) === 0 && (await evaluate(inSec("", "unread", "t_triage"))), "V0.188 desktop: ⇧E from Unread settled the unread thread");
    await key("x", "KeyX");
    await key("E", "KeyE", 8);
    await waitFor(`${sentFor} === 1`, { what: "the picked unread thread to settle" });
  } catch (e) {
    failures.push(`V0.188 desktop: ${e.message}`);
  }
  try {
    await openScreen({ url: "?mock=rail" }, 390, 844);
    await waitFor(inSec("m-", "unread", "t_triage"), { what: "the phone's Unread" });
    const m = await evaluate(`({ order: ${order("m-")}, strip: [...document.querySelectorAll('.m-seg .l')].map(e => e.textContent), over: [...document.querySelectorAll('.m-seg .l')].filter(e => e.scrollWidth > e.parentElement.clientWidth + 1).map(e => e.textContent), inSeen: ${inSec("m-", "seen", "t_triage")} })`);
    check(JSON.stringify(m.order) === WANT, `V0.188 phone: the section order (${JSON.stringify(m.order)})`);
    check(m.strip.includes("Unread") && !m.over.length && !m.inSeen, `V0.188 phone: the strip and the row (${JSON.stringify(m)})`);
    await openScreen({ url: "?mock=1" }, 390, 844);
    await waitFor(`[...document.querySelectorAll('#m-sec-unread .m-swp')].some(r => r.textContent.includes('Fyr: review'))`, { what: "the phone's unread row with a change" });
    await evaluate(`document.querySelector('#m-sec-unread').scrollIntoView({ block: 'start' })`);
    await settle();
    const mchip = await evaluate(`[...document.querySelectorAll('#m-sec-unread .m-swp')].find(r => r.textContent.includes('Fyr: review')).querySelector('.chgchip')?.textContent ?? ''`);
    check(mchip === "change open", `V0.188 phone: the chip of a stopped thread's open change ("${mchip}")`);
    await shot("unread-phone.png");
    await openScreen({ url: "?mock=rail" }, 390, 844);
    await waitFor(inSec("m-", "unread", "t_triage"), { what: "the phone's Unread again" });
    const item = (label) => `[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.querySelector('.ell')?.firstChild?.textContent === ${JSON.stringify(label)})`;
    await pressRow("unread", "t_triage", `!!${item("Mark seen")}`);
    await evaluate(`${item("Mark seen")}.click()`);
    await waitFor(inSec("m-", "seen", "t_triage"), { what: "the phone's Mark seen to move the row" });
    await waitFor(`!document.querySelector('.m-sheet')`, { what: "the sheet to close" });
    await pressRow("seen", "t_triage", `!!${item("Mark unseen")}`);
    await evaluate(`${item("Mark unseen")}.click()`);
    await waitFor(inSec("m-", "unread", "t_triage"), { what: "the phone's Mark unseen to move the row back" });
    // Select all leaves the unread row out and the toast says so; the row picked by hand settles.
    const menuItem = (label) => `${item(label)}.click()`;
    const sentFor = `(window.__fyrCommands ?? []).filter(c => c.action === 'thread.settle' && c.threadId === 't_triage').length`;
    await pressRow("unread", "t_triage", `!!${item("Select")}`);
    await evaluate(menuItem("Select"));
    await waitFor(`!!document.querySelector('.m-selbar') && !document.querySelector('.m-sheet')`, { what: "the selection" });
    await evaluate(`[...document.querySelectorAll('.m-tbtn')].find(b => b.textContent === 'Select all').click()`);
    await waitFor(`!${phoneRow("unread", "t_triage")}.classList.contains('picked') && document.querySelectorAll('.m-swp.picked').length > 0`, { what: "Select all to leave the unread row out" });
    await evaluate(`document.querySelector('.m-selbar .m-bt.p').click()`);
    await waitFor(`/1 unread thread kept/.test(document.querySelector('.m-toast')?.textContent ?? '')`, { what: "the toast of Select all" });
    check((await evaluate(sentFor)) === 0 && (await evaluate(inSec("m-", "unread", "t_triage"))), "V0.188 phone: Select all settled the unread thread");
    await pressRow("unread", "t_triage", `!!${item("Select")}`);
    await evaluate(menuItem("Select"));
    await waitFor(`!!document.querySelector('.m-selbar') && !document.querySelector('.m-sheet')`, { what: "the selection of one row" });
    await evaluate(`document.querySelector('.m-selbar .m-bt.p').click()`);
    await waitFor(`${sentFor} === 1`, { what: "the picked unread thread to settle" });
  } catch (e) {
    failures.push(`V0.188 phone: ${e.message}`);
  }
});

// V0.162: a bulk settle skips a pinned thread: no settle command for it, the pin stays, and the toast names it. Desktop:
// ⇧E on the Seen section; phone: Select all, then Settle N. Both go through the same settleBulk.
section('V0.162', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
  const settledPinned = `(window.__fyrCommands ?? []).some(c => c.action === 'thread.settle' && c.threadId === 't_poly')`;
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=rail&open=t_nix`, "!!document.querySelector('#sec-seen [data-id=t_poly]')");
    const row = await evaluate(`(() => { const r = document.querySelector('#sec-seen [data-id=t_poly]').getBoundingClientRect(); return { x: r.left + 40, y: r.top + r.height / 2 }; })()`);
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x: row.x, y: row.y, button: "right", clickCount: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: row.x, y: row.y, button: "right", clickCount: 1 });
    await settle();
    await evaluate(`[...document.querySelectorAll('.ctxm-i')].find(x => x.querySelector('.ell')?.textContent === 'Pin')?.click()`);
    await waitFor(`!!document.querySelector('#sec-pinned [data-id=t_poly]')`, { soft: true });
    await evaluate(`document.activeElement?.blur()`);
    await key("E", "KeyE", 8);
    await waitFor(`/pinned thread kept/.test(document.querySelector('.toast')?.textContent ?? '')`, { soft: true });
    const r = await evaluate(`({ toast: document.querySelector('.toast')?.textContent ?? '', pinned: !!document.querySelector('#sec-pinned [data-id=t_poly] .hd .pin'), seen: document.querySelectorAll('#sec-seen [data-id]').length, sent: ${settledPinned} })`);
    check(/^Settled \d+ threads; 1 pinned thread kept/.test(r.toast), `V0.162 desktop: the toast does not name the kept pinned thread ("${r.toast}")`);
    check(r.pinned && !r.sent, `V0.162 desktop: ⇧E settled or unpinned the pinned thread (${JSON.stringify(r)})`);
    check(r.seen === 0, `V0.162 desktop: ⇧E left ${r.seen} unpinned Seen rows`);
    // The owner's own Settle on the pinned thread (the header button, as `e` and the row menu) still settles it and
    // removes the pin: it sends no `now`, so the host's bulk refusal does not apply.
    await evaluate(`document.querySelector('#sec-pinned [data-id=t_poly]')?.click()`);
    await waitFor(`/Lasso polygon perf/.test(document.querySelector('.thh .tt')?.textContent ?? '')`, { soft: true });
    await evaluate(`[...document.querySelectorAll('.thh .acts .btn')].find(b => b.textContent.startsWith('Settle'))?.click()`);
    await waitFor(`!document.querySelector('#sec-pinned')`, { soft: true });
    const o = await evaluate(`({ sends: (window.__fyrCommands ?? []).filter(c => c.action === 'thread.settle' && c.threadId === 't_poly').map(c => !!c.args?.now), pinned: !!document.querySelector('#sec-pinned') })`);
    check(JSON.stringify(o.sends) === "[false]" && !o.pinned, `V0.162 desktop: the owner's own Settle of a pinned thread did not settle it (${JSON.stringify(o)})`);
  } catch (e) {
    failures.push(`V0.162 desktop: ${e.message}`);
  }
  try {
    await openScreen({ url: "?mock=rail&open=t_poly" }, 390, 844);
    await tapCenter(".m-tr1 .b44:last-child", "!!document.querySelector('.m-sheet .m-mi')");
    await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.querySelector('.ell')?.firstChild?.textContent === 'Pin')?.click()`);
    await waitFor(`!document.querySelector('.m-sheet')`, { soft: true });
    await tapCenter('button[aria-label="Back to the inbox"]', "!!document.querySelector('#m-sec-pinned')");
    await tapCenter("#m-sec-pinned .m-mv", "!!document.querySelector('.m-sheet .m-mi')", { long: true });
    await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.querySelector('.ell')?.firstChild?.textContent === 'Select')?.click()`);
    await waitFor(`!!document.querySelector('.m-selbar')`, { soft: true });
    await evaluate(`[...document.querySelectorAll('.m-tbtn')].find(b => b.textContent === 'Select all')?.click()`);
    await settle();
    await evaluate(`document.querySelector('.m-selbar .m-bt.p')?.click()`);
    await waitFor(`/1 pinned/.test(document.querySelector('.m-toast')?.textContent ?? '')`, { soft: true }); // V0.188: Select all also holds unread threads, which it keeps
    const r = await evaluate(`({ toast: document.querySelector('.m-toast')?.textContent ?? '', pinned: document.querySelectorAll('#m-sec-pinned .pin').length, sent: ${settledPinned} })`);
    check(/^Settled \d+ threads; 1 pinned (thread|and \d+ unread threads?) kept/.test(r.toast), `V0.162 phone: the toast does not name the kept pinned thread ("${r.toast}")`);
    check(r.pinned === 1 && !r.sent, `V0.162 phone: Settle N settled or unpinned the pinned thread (${JSON.stringify(r)})`);
    // The owner's own Settle in the thread's sheet still settles the pinned thread and removes the pin.
    await evaluate(`document.querySelector('.m-selbar') && document.querySelector('button[aria-label="Cancel selection"]')?.click()`);
    await openScreen({ url: "?mock=rail&open=t_poly" }, 390, 844);
    await tapCenter(".m-tr1 .b44:last-child", "!!document.querySelector('.m-sheet .m-mi')");
    await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.querySelector('.ell')?.firstChild?.textContent === 'Pin')?.click()`);
    await waitFor(`!document.querySelector('.m-sheet')`, { what: "the sheet to close after Pin" });
    // The sheet sends Pin after it closes: the reopened sheet offers Unpin once the pin is in.
    const sheetItem = (re) => `[...document.querySelectorAll('.m-sheet .m-mi')].find(b => ${re}.test(b.querySelector('.ell')?.firstChild?.textContent ?? ''))`;
    await tapCenter(".m-tr1 .b44:last-child", `!!${sheetItem("/^Unpin$/")}`);
    await evaluate(`${sheetItem("/^Settle/")}?.click()`);
    // The settle leaves the open thread by itself (leaveOpenThread, after the host's answer); the check presses no Back,
    // because a Back pressed before that answer is a second history step, out of the page. It waits for the inbox
    // without a Pinned block, still on the page that sent the settle.
    await waitFor(`(window.__fyrCommands ?? []).some(c => c.action === 'thread.settle' && c.threadId === 't_poly') && !!document.querySelector('.m-list') && !document.querySelector('#m-sec-pinned')`, { what: "the inbox without a Pinned block after the owner's Settle", soft: true });
    const o = await evaluate(`({ sends: (window.__fyrCommands ?? []).filter(c => c.action === 'thread.settle' && c.threadId === 't_poly').map(c => !!c.args?.now), pinned: !!document.querySelector('#m-sec-pinned') })`);
    check(JSON.stringify(o.sends) === "[false]" && !o.pinned, `V0.162 phone: the owner's own Settle of a pinned thread did not settle it (${JSON.stringify(o)})`);
  } catch (e) {
    failures.push(`V0.162 phone: ${e.message}`);
  }
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
});

// V0.108: Regenerate title is in the thread's ⋯ menu on the desktop and in the thread's ⋯ sheet on the phone, the same
// control on both; a press names the thread from its conversation (the mock answers at once).
section('V0.108', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
const REGEN = "Title written from the conversation";
let regenId = "";
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('#sec-working [data-id]')");
  regenId = await evaluate(`document.querySelector('#sec-working [data-id]').dataset.id`);
  await navigate(`${origin}/?mock=rail&open=${regenId}`, "!!document.querySelector('.th .tmenu-b')");
  await evaluate(`document.querySelector('.th .tmenu-b').click()`);
  await waitFor(`!!document.querySelector('.tmenu-l .mi')`);
  const has = await evaluate(`[...document.querySelectorAll('.tmenu-l .mi .ell')].some(x => x.textContent === 'Regenerate title')`);
  check(has, "V0.108 title: the thread's ⋯ menu has no Regenerate title");
  if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, "regen-desktop.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  await evaluate(`[...document.querySelectorAll('.tmenu-l .mi')].find(b => b.querySelector('.ell')?.textContent === 'Regenerate title')?.click()`);
  await waitFor(`document.querySelector('.th .tt')?.textContent === ${JSON.stringify(REGEN)}`, { soft: true });
  check(await evaluate(`document.querySelector('.th .tt')?.textContent === ${JSON.stringify(REGEN)}`), "V0.108 title: the desktop press did not rename the thread");
} catch (e) {
  failures.push(`V0.108 title desktop: ${e.message}`);
}
try {
  await openScreen({ url: `?mock=rail&open=${regenId}` }, 390, 844);
  await tapCenter(".m-tr1 .b44:last-child", "!!document.querySelector('.m-sheet .m-mi')");
  const has = await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].some(b => b.querySelector('.ell')?.firstChild?.textContent === 'Regenerate title')`);
  check(has, "V0.108 title phone: the thread sheet has no Regenerate title");
  if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, "regen-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.querySelector('.ell')?.firstChild?.textContent === 'Regenerate title')?.click()`);
  await waitFor(`document.querySelector('.m-tt')?.textContent === ${JSON.stringify(REGEN)}`, { soft: true });
  check(await evaluate(`document.querySelector('.m-tt')?.textContent === ${JSON.stringify(REGEN)}`), "V0.108 title phone: the press did not rename the thread");
} catch (e) {
  failures.push(`V0.108 title phone: ${e.message}`);
}
});

// SPEC 0.5 V0.30, V0.55: from the keyboard, the project chip opens the picker; ArrowDown reaches New project from this
// thread, Enter opens the one step prefilled with the thread's folder, Enter makes the project and files the thread; p
// and the arrows then unfile it with No project.
section('V0.30, V0.55', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await navigate(`${origin}/?mock=1&open=t_s4`, "!!document.querySelector('.thh .chip.pj')");
  check(!(await evaluate(`!!document.querySelector('.thh .chip.mkp')`)), "V0.55 desktop: the Make project chip still shows");
  await evaluate(`document.querySelector('.thh .chip.pj')?.focus()`);
  await key("Enter");
  await waitFor(`!!document.querySelector('.dlg .plist')`, { soft: true });
  await key("ArrowDown");
  const a = await evaluate(`({ dlg: !!document.querySelector('.dlg .plist'), focused: document.activeElement?.classList.contains('new') ? document.activeElement.textContent : '' })`);
  check(a.dlg && /New project from this thread/.test(a.focused), `V0.30 desktop: the project chip does not open the picker with New project from this thread first ("${a.focused}")`);
  await key("Enter");
  const b = await evaluate(`({ name: document.activeElement?.value ?? '', folder: document.querySelector('.dlg .pform .pfolder .ell')?.textContent ?? '', button: document.querySelector('.dlg .pform .btn.p')?.textContent ?? '' })`);
  check(b.name === "lasso-app" && b.folder === "~/src/lasso-app" && /Create and file this thread/.test(b.button), `V0.30 desktop: the step is not prefilled and focused (${JSON.stringify(b)})`);
  await key("Enter");
  await waitFor(`!document.querySelector('.dlg') && /lasso-app/.test(document.querySelector('.thh .chip.proj')?.textContent ?? '')`, { soft: true });
  const c = await evaluate(`({ dlg: !!document.querySelector('.dlg'), chip: document.querySelector('.thh .chip.proj')?.textContent ?? '' })`);
  check(!c.dlg && /lasso-app/.test(c.chip), `V0.30 desktop: Enter does not make the project and file the thread (${JSON.stringify(c)})`);
  await evaluate(`document.activeElement?.blur()`);
  await send("Input.dispatchKeyEvent", { type: "keyDown", key: "p", code: "KeyP", text: "p", windowsVirtualKeyCode: 80 });
  await send("Input.dispatchKeyEvent", { type: "keyUp", key: "p", code: "KeyP" });
  await waitFor(`!!document.querySelector('.dlg .plist')`, { soft: true });
  await settle();
  await key("ArrowDown");
  await key("ArrowDown");
  const f = await evaluate(`document.activeElement?.textContent ?? ''`);
  check(/^No project/.test(f), `V0.30 desktop: the second row is not No project ("${f}")`);
  await key("Enter");
  await waitFor(`!document.querySelector('.dlg') && document.querySelector('.thh .chip.pj')?.textContent === 'No project'`, { soft: true });
  const u = await evaluate(`({ dlg: !!document.querySelector('.dlg'), chip: document.querySelector('.thh .chip.pj')?.textContent ?? '' })`);
  check(!u.dlg && u.chip === "No project", `V0.30 desktop: No project does not unfile the thread (${JSON.stringify(u)})`);
} catch (e) {
  failures.push(`V0.30 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.37: on the desktop, a press in the live view posts a message from the view, shown as a chip.
// The view's sandboxed frame runs out of process, so the click goes through its own CDP session.
section('V0.37', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Target.setAutoAttach", { autoAttach: true, waitForDebuggerOnStart: false, flatten: true });
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  attached.length = 0;
  await navigate(`${origin}/?mock=1&open=t_view`, "!!document.querySelector('.th .lv-f')");
  await waitFor(`(document.querySelector('.th .lv-f')?.getBoundingClientRect().height ?? 0) > 120`, { soft: true });
  const v = await evaluate(`(() => { const f = document.querySelector('.th .lv-f'); return f ? { h: f.getBoundingClientRect().height, stamp: document.querySelector('.th .lv-s')?.textContent ?? '' } : null; })()`);
  check(!!v && v.h > 120 && /updated 12 min ago/.test(v.stamp), `V0.37 desktop: the live view is missing, not sized by its page, or has no stamp (${JSON.stringify(v)})`);
  const frame = await poll(() => attached.filter((a) => a.targetInfo.type === "iframe").pop(), { soft: true });
  check(!!frame, "V0.37 desktop: no frame target for the live view");
  if (frame) {
    await waitIn(frame.sessionId, `!!document.querySelector('[data-send="Open the PR"]')`, { soft: true });
    const r = await send("Runtime.evaluate", { expression: `(() => { const b = document.querySelector('[data-send="Open the PR"]'); if (!b) return false; b.click(); return true; })()`, returnByValue: true }, frame.sessionId);
    check(r.result?.result?.value === true, `V0.37 desktop: no "Open the PR" button in the view (${JSON.stringify(r.result ?? r.error)})`);
    // V0.37: the pressed button says what happened, inside the view (for 1.8 s).
    const fb = await waitIn(frame.sessionId, `document.querySelector('[data-send="Open the PR"]')?.getAttribute('data-fyr') === 'sent' ? 'sent' : ''`, { soft: true });
    check(fb === "sent", `V0.37 desktop: the pressed button gives no Sent feedback (${JSON.stringify(fb)})`);
    // The message chip is in the Conversation tab; the thread opened on View.
    await evaluate(`[...document.querySelectorAll('.th [role=tab]')].find((x) => x.textContent.trim() === 'Conversation')?.click()`);
    await waitFor(`[...document.querySelectorAll('.th .vchip')].some((c) => /Open the PR/.test(c.textContent))`, { soft: true });
    const chips = await evaluate(`[...document.querySelectorAll('.th .vchip')].map((c) => c.textContent)`);
    check(chips.some((c) => /Open the PR/.test(c)), `V0.37 desktop: the press did not post a message from the view (chips ${JSON.stringify(chips)})`);
  }
  // A message from another window is ignored: the top page posts a press as if it were the view. Nothing
  // changes when it is ignored, so this waits as long as a real press takes to show: the mock posts it after
  // 20 ms, the press above showed its chip within a settle.
  await evaluate(`window.postMessage({ jsonrpc: "2.0", id: 1, method: "ui/message", params: { role: "user", content: [{ type: "text", text: "forged" }] } }, "*")`);
  await sleep(400); // a fixed wait on purpose: nothing changes when the message is ignored, so it gives a wrongly accepted one time to show
  await settle();
  const forged = await evaluate(`[...document.querySelectorAll('.th .vchip')].some((c) => /forged/.test(c.textContent))`);
  check(!forged, "V0.37 desktop: a message from outside the view's frame was sent");
  await send("Target.setAutoAttach", { autoAttach: false, waitForDebuggerOnStart: false, flatten: true });
} catch (e) {
  failures.push(`V0.37 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.47: previews run real pages. A WebGL canvas draws inside the sandboxed frame, and a folder preview
// loads its own script, module, JSON and image by relative URL while the rest of the host stays closed to it.
section('V0.47', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Target.setAutoAttach", { autoAttach: true, waitForDebuggerOnStart: false, flatten: true });
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  attached.length = 0;
  await navigate(`${origin}/?mock=1&open=t_w3`, "!!document.querySelector('.th .pv.html')");
  await evaluate(`(() => { const f = [...document.querySelectorAll('.th .pv.html')].pop(); f?.scrollIntoView({ block: "end" }); return !!f; })()`);
  const CARDS = `[...document.querySelectorAll('.th .pv.html')].map((f) => { const i = f.querySelector('iframe'); return { t: f.querySelector('.pv-t')?.textContent ?? '', w: Math.round(i.getBoundingClientRect().width), h: Math.round(i.getBoundingClientRect().height), allow: i.getAttribute('allow') ?? '', fs: !!f.querySelector('.pv-fs') }; })`;
  // The folder preview reports its height once its page has loaded.
  await waitFor(`${CARDS}.some((c) => c.t === "Folder preview" && c.h === 300)`, { soft: true });
  await settle();
  const cards = await evaluate(CARDS);
  const glCard = cards.find((c) => c.t === "WebGL triangle");
  const siteCard = cards.find((c) => c.t === "Folder preview");
  check(!!glCard && Math.abs(glCard.h - Math.round((glCard.w * 10) / 16)) <= 1, `V0.47 desktop: the WebGL card is not 16:10 of its width (${JSON.stringify(glCard)})`);
  check(!!siteCard && siteCard.h === 300, `V0.47 desktop: the folder preview is not as tall as its page reported (${JSON.stringify(siteCard)})`);
  check(cards.every((c) => /fullscreen/.test(c.allow) && /xr-spatial-tracking/.test(c.allow) && c.fs), `V0.47 desktop: a frame lacks allow="fullscreen; xr-spatial-tracking" or the Full screen button (${JSON.stringify(cards)})`);
  // A frame target is attached before it navigates; its URL comes from the current target list.
  let live = [];
  const urlOf = (a) => live.find((x) => x.targetId === a.targetInfo.targetId)?.url ?? a.targetInfo.url;
  const frameOf = (re) => attached.filter((a) => a.targetInfo.type === "iframe" && re.test(urlOf(a))).pop();
  const previewFrames = async () => {
    live = (await send("Target.getTargets")).result?.targetInfos ?? [];
    const gl = frameOf(/^data:text\/html.*webgl/), st = frameOf(/\/api\/previews\/pv_site\//);
    return gl && st ? { gl, st } : null;
  };
  const found = (await poll(previewFrames, { soft: true })) ?? {};
  const { gl, st } = found;
  check(!!gl && !!st, `V0.47 desktop: no frame target for the WebGL or the folder preview (${attached.map((a) => urlOf(a).slice(0, 40)).join(" | ")})`);
  if (gl) {
    const v = await waitIn(gl.sessionId, `document.body.dataset.gl ?? ''`, { soft: true });
    check(v === "ok", `V0.47 desktop: the WebGL triangle did not draw in the sandbox (${v})`);
  }
  if (st) {
    // The page sets each data attribute when its load or its blocked request has finished.
    const SITE = `({ js: document.body.dataset.js, fetch: document.body.dataset.fetch, api: document.body.dataset.api, img: document.getElementById('dot')?.naturalWidth ?? 0, blocked: document.body.dataset.blocked ?? '', origin: String(self.origin) })`;
    await waitIn(st.sessionId, `(() => { const v = ${SITE}; return v.js && v.fetch && v.api && v.img > 0 && v.blocked; })()`, { soft: true });
    const v = await waitIn(st.sessionId, SITE);
    check(v?.js === "1" && v?.fetch === "ok" && v?.img > 0, `V0.47 desktop: the folder preview did not load its own files (${JSON.stringify(v)})`);
    check(v?.api === "closed" && /\/api\/state/.test(v?.blocked ?? "") && v?.origin === "null", `V0.47 desktop: the folder preview reached the host outside its folder or has an origin (${JSON.stringify(v)})`);
  }
  await send("Target.setAutoAttach", { autoAttach: false, waitForDebuggerOnStart: false, flatten: true });
} catch (e) {
  failures.push(`V0.50 desktop: ${e.message}`);
}
});

// V0.38: never block the main thread. While a burst of 300 thread updates and 300 messages streams in
// (3 s), no task on the page's main thread takes over 50 ms, and the last message shows. The long tasks are
// observed inside the page from the burst's start until its last message has drawn, so the test's own
// navigation and reads are not in the measurement.
// Budget on a quiet machine: no task over 100 ms, under 250 ms of long tasks in all (before V0.38: 10 tasks, 523 ms),
// scaled by the calibration factor (#50).
section('V0.38', { timed: true }, async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  let miss = "";
  let last = "";
  for (let attempt = 1; attempt <= (TIMING ? TIMED_ATTEMPTS : 1); attempt++) {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=rail&open=t_mapperf`, "!!document.querySelector('.th .md') && typeof window.__fyrBurst === 'function'");
    // A mid-range phone's speed: 4 times slower than this machine's CPU.
    await send("Emulation.setCPUThrottlingRate", { rate: 4 });
    const before = TIMING ? await calibrate(4) : [];
    const burst = await evaluate(`new Promise((done) => {
      const long = [];
      const po = new PerformanceObserver((l) => { for (const e of l.getEntries()) long.push(Math.round(e.duration)); });
      po.observe({ type: "longtask", buffered: false });
      window.__fyrBurst("t_mapperf", 300);
      const t0 = performance.now();
      const end = (shown) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(() => {
        for (const e of po.takeRecords()) long.push(Math.round(e.duration));
        po.disconnect();
        done({ long, shown, ms: Math.round(performance.now() - t0) });
      }, 0)));
      const poll = () => {
        const all = document.querySelectorAll('.th .md');
        if (/Burst message 300/.test(all[all.length - 1]?.textContent ?? '')) end(true);
        else if (performance.now() - t0 > 30000) end(false);
        else setTimeout(poll, 100);
      };
      setTimeout(poll, 100);
    })`);
    const after = TIMING ? await calibrate(4) : [];
    await send("Emulation.setCPUThrottlingRate", { rate: 1 });
    last = (await evaluate(`[...document.querySelectorAll('.th .md')].map((x) => x.textContent).filter((t) => /Burst message/.test(t)).pop() ?? ''`)) ?? "";
    if (!TIMING) break;
    const f = speedFactor(4, `V0.38 attempt ${attempt}`, [...before, ...after]);
    const maxTask = Math.round(100 * f);
    const maxTotal = Math.round(250 * f);
    const total = burst.long.reduce((a, b) => a + b, 0);
    const longest = Math.max(0, ...burst.long);
    const ok = longest <= maxTask && total < maxTotal;
    console.log(`V0.38 attempt ${attempt}: ${burst.long.length} long tasks, ${total} ms in all, longest ${longest} ms; budget ${maxTask} ms per task and ${maxTotal} ms in all (100 and 250 ms × ${f.toFixed(2)}): ${ok ? "within" : "over"}`);
    miss = ok ? "" : `V0.38 main thread: long tasks during the burst (${burst.ms} ms): ${burst.long.length}, ${total} ms in all (${burst.long.slice(0, 8).join(", ")} ms), over the budget of ${maxTask} ms per task and ${maxTotal} ms in all (factor ${f.toFixed(2)}), ${attempt} of ${TIMED_ATTEMPTS} attempts`;
    if (ok) break;
  }
  timed(!miss, miss);
  check(/Burst message 300/.test(last), `V0.38 main thread: the last burst message is not shown ("${last.slice(0, 60)}")`);
} catch (e) {
  await send("Emulation.setCPUThrottlingRate", { rate: 1 });
  failures.push(`V0.38 main thread: ${e.message}`);
}
});

// V0.40: in a narrow desktop window the thread list starts hidden; \\ shows it, and the thread keeps its room.
section('V0.40', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 900, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&open=t_triage`, "!!document.querySelector('.th')");
  const a = await evaluate(`({ list: !!document.querySelector('.ib') && getComputedStyle(document.querySelector('.ib')).display !== 'none', th: Math.round(document.querySelector('.th')?.getBoundingClientRect().width ?? 0) })`);
  check(!a.list && a.th > 700, `V0.40 list: a narrow window does not start with the list hidden (list ${a.list}, thread ${a.th} px)`);
  await evaluate(`document.activeElement?.blur(); true`);
  await send("Input.dispatchKeyEvent", { type: "keyDown", key: "\\", code: "Backslash", text: "\\" });
  await send("Input.dispatchKeyEvent", { type: "keyUp", key: "\\", code: "Backslash" });
  const b = await waitFor(`!!document.querySelector('.ib') && getComputedStyle(document.querySelector('.ib')).display !== 'none'`, { soft: true });
  check(b, "V0.40 list: the \\ key does not show the list");
  await evaluate(`try { localStorage.removeItem("fyr.list") } catch {} true`);
} catch (e) {
  failures.push(`V0.40 list: ${e.message}`);
}
});

// V0.32: a screen change on the phone is quick, transition included (found live: open and Back
// took 4 s each, because the transition's update waited for a frame that a transition never draws).
// Budget on a quiet machine: 800 ms each, scaled by the calibration factor (#50).
section('V0.32', { timed: true }, async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 3, mobile: true });
  await send("Emulation.setTouchEmulationEnabled", { enabled: true });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.m-app .m-mv')");
  const inVT = `(() => { try { return document.documentElement.matches(':active-view-transition'); } catch { return false; } })()`;
  const tapAndWait = async (sel, cond) => {
    const c = await evaluate(`(() => { const r = document.querySelector(${JSON.stringify(sel)})?.getBoundingClientRect(); return r ? { x: r.left + r.width / 2, y: r.top + r.height / 2 } : null; })()`);
    if (!c) return -1;
    const t0 = Date.now();
    await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: c.x, y: c.y }] });
    await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    await evaluate(`new Promise((ok) => { const t0 = performance.now(); const tick = () => { if ((${cond}) && !${inVT}) ok(); else if (performance.now() - t0 > 6000) ok(); else requestAnimationFrame(tick); }; requestAnimationFrame(tick); })`);
    return Date.now() - t0;
  };
  let miss = [];
  for (let attempt = 1; attempt <= (TIMING ? TIMED_ATTEMPTS : 1); attempt++) {
    const before = TIMING ? await calibrate(1) : [];
    const open = await tapAndWait(".m-app .m-mv", "!!document.querySelector('.m-tth')");
    await settle();
    const back = await tapAndWait(".m-tth .b44", "!document.querySelector('.m-tth')");
    if (!TIMING) break;
    await settle();
    const f = speedFactor(1, `V0.32 attempt ${attempt}`, [...before, ...(await calibrate(1))]);
    const maxNav = Math.round(800 * f);
    miss = [];
    if (!(open >= 0 && open < maxNav)) miss.push(`V0.32 phone: opening a thread took ${open} ms, over the budget of ${maxNav} ms (factor ${f.toFixed(2)}), ${attempt} of ${TIMED_ATTEMPTS} attempts`);
    if (!(back >= 0 && back < maxNav)) miss.push(`V0.32 phone: Back to the list took ${back} ms, over the budget of ${maxNav} ms (factor ${f.toFixed(2)}), ${attempt} of ${TIMED_ATTEMPTS} attempts`);
    console.log(`V0.32 attempt ${attempt}: open ${open} ms, Back ${back} ms; budget ${maxNav} ms each (800 ms × ${f.toFixed(2)}): ${miss.length ? "over" : "within"}`);
    if (!miss.length) break;
  }
  timed(!miss[0], miss[0]);
  timed(!miss[1], miss[1]);
} catch (e) {
  failures.push(`V0.32 phone navigation: ${e.message}`);
}
});

// U15: the desktop question card always shows a button that says how to finish.
section('U15', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=1&open=t_lasso`, "!!document.querySelector('.th .ask .askf .btn')");
  const BTN = `(() => { const b = document.querySelector('.th .ask .askf .btn'); return b ? { text: b.textContent.replace('↵', '').trim(), disabled: b.disabled } : null; })()`;
  const btn = () => evaluate(BTN);
  const a = await btn();
  check(!!a && a.text === "Pick an answer" && a.disabled, `U15 desktop: the card does not start with a disabled "Pick an answer" (${JSON.stringify(a)})`);
  await evaluate(`document.querySelectorAll('.th .ask .grid4 .op')[0]?.click()`);
  await settle();
  await evaluate(`document.querySelectorAll('.th .ask .grid4 .op')[0]?.click()`);
  await waitFor(`/^Send 2 answers$/.test(${BTN}?.text ?? '')`, { soft: true });
  const b = await btn();
  check(!!b && /^Send 2 answers$/.test(b.text) && !b.disabled, `U15 desktop: after both answers the button is not "Send 2 answers" (${JSON.stringify(b)})`);
  await evaluate(`document.querySelector('.th .ask .askf .btn')?.click()`);
  const gone = await waitFor(`!document.querySelector('.th .ask .askf')`, { soft: true });
  check(gone, "U15 desktop: the card stays after Send");
} catch (e) {
  failures.push(`U15 desktop: ${e.message}`);
}
});

// V0.48: Settled opened from its header shows a clock and closes by itself (30 s; the test
// fast-forwards with a short wait on a patched clock is not possible, so it checks the clock and
// that the rail's g5 keeps it open without one).
section('V0.48', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "[...document.querySelectorAll('.sec.tog')].some((x) => /Settled/.test(x.textContent))");
  const SETTLED = `({ clock: !!document.querySelector('#sec-settled .ahc'), rows: document.querySelectorAll('#sec-settled [data-id]').length })`;
  await evaluate(`[...document.querySelectorAll('.sec.tog')].find((x) => /Settled/.test(x.textContent))?.click()`);
  await waitFor(`${SETTLED}.clock && ${SETTLED}.rows > 0`, { soft: true });
  const a = await evaluate(SETTLED);
  check(a.clock && a.rows > 0, `V0.48 settled: opened from its header without a clock (${JSON.stringify(a)})`);
  await evaluate(`[...document.querySelectorAll('.rail .ri')].find((x) => /Settled/.test(x.textContent))?.click()`);
  await waitFor(`!${SETTLED}.clock && ${SETTLED}.rows > 0`, { soft: true });
  const b = await evaluate(SETTLED);
  check(!b.clock && b.rows > 0, `V0.48 settled: chosen in the rail it still runs down (${JSON.stringify(b)})`);
} catch (e) {
  failures.push(`V0.48 settled: ${e.message}`);
}
});

// #56: a Needs you row whose state word and change word are longer than the rail. No cell passes the row's content
// box or covers another cell, the clipped texts end in an ellipsis, and the list's scrollbar does not cover the row.
section('line 2074', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 400, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.list .rw .m b')");
  const r = await evaluate(`(() => {
    const row = document.querySelector('.list .rw'), list = row.closest('.list');
    row.querySelector('.m b').textContent = "You've hit your weekly limit · resets Mon";
    const pr = row.querySelector('.pr'); pr.className = 'pr chgchip'; pr.textContent = 'nodependabotconfig · landed +1';
    const box = row.getBoundingClientRect(), cs = getComputedStyle(row);
    const inner = { left: box.left + parseFloat(cs.paddingLeft), right: box.right - parseFloat(cs.paddingRight) };
    const cells = [...row.children].map((e) => ({ c: e.className, ...JSON.parse(JSON.stringify(e.getBoundingClientRect())) }));
    const out = cells.filter((c) => c.width > 0 && (c.right > inner.right + 0.5 || c.left < inner.left - 0.5)).map((c) => c.c);
    const hit = [];
    for (let i = 0; i < cells.length; i++) for (let j = i + 1; j < cells.length; j++) {
      const a = cells[i], b = cells[j];
      if (a.width && b.width && a.left < b.right - 0.5 && b.left < a.right - 0.5 && a.top < b.bottom - 0.5 && b.top < a.bottom - 0.5) hit.push(a.c + ' × ' + b.c + ' ' + JSON.stringify([a.left, a.top, a.right, a.bottom, b.left, b.top, b.right, b.bottom].map(Math.round)));
    }
    const cut = (e) => getComputedStyle(e).textOverflow === 'ellipsis' && e.scrollWidth > e.clientWidth;
    const m = row.querySelector('.m');
    const sb = list.offsetWidth - list.clientWidth; // a classic scrollbar takes width; an overlay one takes none
    const gutter = getComputedStyle(list).scrollbarGutter;
    return { out, hit, mCut: cut(m), prCut: cut(pr) || pr.scrollWidth <= pr.clientWidth, // V0.91: on its own line the chip may fit whole
       prRight: pr.getBoundingClientRect().right, listInner: list.getBoundingClientRect().right - sb, gutter };
  })()`);
  check(r.out.length === 0, `#56 desktop: a row cell passes the row (${JSON.stringify(r)})`);
  check(r.hit.length === 0, `#56 desktop: two row cells overlap (${JSON.stringify(r)})`);
  check(r.mCut && r.prCut, `#56 desktop: a clipped row text has no ellipsis (${JSON.stringify(r)})`);
  check(r.gutter.includes("stable"), `#56 desktop: the list does not reserve its scrollbar's width (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`#56 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.91: every inbox row starts with its project tag, then the title, on one line; every chip (PR, change,
// subagents) sits below the title, inside the row; no row says "0 sub".
section('V0.91', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.list .rw .hd .rpi') && !!document.querySelector('.list .one .hd .rpi')");
  const r = await evaluate(`(() => {
    const bad = [];
    const rows = [...document.querySelectorAll('.list .rw, .list .one')];
    for (const row of rows) {
      const id = row.dataset.id, box = row.getBoundingClientRect();
      const tag = row.querySelector('.hd .rpi'), title = row.querySelector('.hd .t'); // V0.135: the project is its icon
      if (!tag || !title) { bad.push(id + ': no project or title'); continue; }
      const a = tag.getBoundingClientRect(), b = title.getBoundingClientRect();
      if (a.left > b.left + 2) bad.push(id + ': project not at the title start');
      if (Math.abs(a.top - b.top) > 4) bad.push(id + ': project and title not on one line');
      for (const c of row.querySelectorAll('.pr:not(.none), .sb')) {
        const cr = c.getBoundingClientRect();
        if (cr.top < b.bottom - 0.5) bad.push(id + ': chip ' + c.textContent + ' beside the title');
        if (cr.bottom > box.bottom + 0.5 || cr.right > box.right + 0.5) bad.push(id + ': chip ' + c.textContent + ' outside the row');
        if (c.textContent.trim() === '0 sub') bad.push(id + ': shows 0 sub');
      }
    }
    return { n: rows.length, bad };
  })()`);
  check(r.n >= 8 && r.bad.length === 0, `V0.91 desktop: inbox rows (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.91 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.135: the title gets the room. At the desktop list and at 390 px on the phone, every inbox row's title
// box is at least 60 % of the row and always two lines high (clamped at two, so a title that streams in does not
// move the row); the project is its icon before the title (the name only on the second line, muted, and only without
// an image icon); a thread started by another shows a quiet ↳ (UI font, not link blue), never "from <parent title>".
for (const [front, w, h, list] of [["desktop", 1280, 900, ".list"], ["phone", 390, 844, ".m-list"]]) section(`V0.135 ${front}`, async ({ send, evaluate, navigate, openScreen }) => {
try {
  if (front === "desktop") {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.list .one .hd .t')");
  } else await openScreen({ url: "?mock=rail", ready: "!!document.querySelector('.m-list .hd .t')" }, w, h);
  const r = await evaluate(`(() => {
    const bad = [], rows = [...document.querySelectorAll('${list} .rw, ${list} .one')];
    let marks = 0, named = 0, clamped = 0;
    for (const row of rows) {
      const id = row.dataset.id || row.textContent.slice(0, 24), box = row.getBoundingClientRect();
      const t = row.querySelector('.hd .t'), icon = row.querySelector('.hd .rpi');
      if (!t || !icon) { bad.push(id + ': no title or project icon'); continue; }
      const tb = t.getBoundingClientRect(), ib = icon.getBoundingClientRect(), cs = getComputedStyle(t), lh = parseFloat(cs.lineHeight);
      if (tb.width < box.width * 0.6) bad.push(id + ': title ' + Math.round(tb.width) + ' of ' + Math.round(box.width) + ' px');
      if (Math.abs(tb.height - 2 * lh) > 1) bad.push(id + ': title box ' + tb.height + ' px, not 2 lines of ' + lh);
      if (cs.webkitLineClamp !== '2') bad.push(id + ': title not clamped at 2 lines (' + cs.webkitLineClamp + ')');
      if (ib.left > tb.left + 2 || Math.abs(ib.top - tb.top) > 6) bad.push(id + ': project icon not at the title start');
      if (row.querySelector('.hd .ptn')) bad.push(id + ': project name on the title line');
      if (t.scrollHeight > t.clientHeight + 1) clamped++;
      if (row.querySelector('.m .rpn')) named++;
      if (/from /.test(row.querySelector('.m')?.textContent ?? '')) bad.push(id + ': repeats "from <parent>"');
      for (const f of row.querySelectorAll('.from')) {
        marks++;
        const fs = getComputedStyle(f);
        if (f.textContent.trim() !== '↳' || !(f.getAttribute('title') || '').startsWith('from ')) bad.push(id + ': parent mark is ' + f.textContent);
        if (/mono/i.test(fs.fontFamily)) bad.push(id + ': parent mark in monospace');
        if (fs.color === getComputedStyle(document.documentElement).getPropertyValue('--harbour').trim() || fs.textDecorationLine === 'underline') bad.push(id + ': parent mark looks like a link');
      }
    }
    return { n: rows.length, marks, named, clamped, bad: bad.slice(0, 12) };
  })()`);
  check(r.n >= 8 && r.marks >= 1 && r.named >= 1 && r.bad.length === 0, `V0.135 ${front}: inbox rows (${JSON.stringify(r)})`);
  if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, `rows-${front}.png`), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
} catch (e) {
  failures.push(`V0.135 ${front}: ${e.message}`);
}
});

// SPEC V0.137: Waiting for a slot. Desktop: the section sits after Working with the deploy's tests pinned as #1 and the
// queued threads as #2… in the queue's order (they leave their own section); the rail counts them; Alt+↑ on a place
// sends host.slotOrder with the host's whole order and the rows renumber; a thread that gets its slot or leaves the
// queue leaves the section and the others renumber; without a queue there is no section and no rail item; with two
// hosts each has its part under its name. Phone: the strip stays one line, Reorder shows ↑ and ↓ of 44 px, ↓ sends
// the order, and nothing is wider than the screen.
section('V0.137', async (h) => {
  const { send, evaluate, waitFor, navigate, tapCenter, key, openScreen } = h;
  const rows = (root) => `[...document.querySelectorAll('${root} .sqh > .sqp, ${root} .sqh > [data-id]')].map(r => (r.dataset.owner ?? r.dataset.id) + ' ' + (r.querySelector('.sqn')?.textContent ?? ''))`;
  const shot = async (name) => {
    if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, name), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  };
try {
  await navigate(`${origin}/?mock=rail&slots=queue`, "!!document.querySelector('#sec-slots .one')");
  const d = await evaluate(`({
    order: [...document.querySelectorAll('.list > div[id^=sec-]')].map(e => e.id),
    rows: ${rows(".list")},
    inWorking: !!document.querySelector('#sec-working [data-id=t_search]'),
    rail: document.querySelector('.rail [data-sec=slots] .n')?.textContent ?? null,
    head: document.querySelector('#sec-slots .sec')?.textContent ?? '' })`);
  const wo = d.order.indexOf("sec-working"), so = d.order.indexOf("sec-slots"), un = d.order.indexOf("sec-unread"), se = d.order.indexOf("sec-seen");
  check(wo >= 0 && so === wo + 1 && un === so + 1 && se === un + 1, `V0.137 desktop: the section sits after Working (${JSON.stringify(d.order)})`);
  check(JSON.stringify(d.rows) === JSON.stringify(["self-deploy #1", "t_search #2", "t_pw #3", "t_nix #4"]) && !d.inWorking && d.rail === "3" && d.head.startsWith("Waiting to run3"),
    `V0.137 desktop: the rows, their places and the rail (${JSON.stringify(d)})`);
  check(!(await evaluate(`!!document.querySelector('#sec-slots .sqp .grip')`)), "V0.137 desktop: the deploy's place is movable");
  await evaluate(`document.querySelector('#sec-slots .one[data-id=t_nix] .sqn.grip').focus()`);
  await key("ArrowUp", "ArrowUp", 1);
  await waitFor(`${COMMANDS("host.slotOrder")}.length === 1`, { soft: true });
  await evaluate(`document.querySelector('#sec-slots .one[data-id=t_nix] .sqn.grip').focus()`);
  await key("ArrowUp", "ArrowUp", 1);
  await waitFor(`${COMMANDS("host.slotOrder")}.length === 2`, { soft: true });
  const mv = await evaluate(`({ sent: ${COMMANDS("host.slotOrder")}.map(c => c.args), rows: ${rows(".list")} })`);
  check(JSON.stringify(mv.sent.at(-1)) === JSON.stringify({ owners: ["t_nix", "t_search", "t_pw"] }) && JSON.stringify(mv.rows) === JSON.stringify(["self-deploy #1", "t_nix #2", "t_search #3", "t_pw #4"]),
    `V0.137 desktop: Alt+↑ twice on t_nix's place (${JSON.stringify(mv)})`);
  await shot("slots-desktop.png");
  // Live: t_nix gets its slot, t_pw's run ends; both leave and t_search is #2.
  await evaluate(`(window.__fyrSlot('t_nix', true), window.__fyrSlot('t_pw', false))`);
  const live = await waitFor(`JSON.stringify(${rows(".list")}) === JSON.stringify(["self-deploy #1", "t_search #2"])`, { soft: true, timeout: 12_000 });
  check(live !== false, `V0.137 desktop: a granted or released thread leaves the section (${JSON.stringify(await evaluate(rows(".list")))})`);
  check(await evaluate(`!!document.querySelector('#sec-seen [data-id=t_pw]') && document.querySelector('.rail [data-sec=slots] .n')?.textContent === '1'`), "V0.137 desktop: t_pw is back in Seen and the rail counts 1");
  // No queue: no section, no rail item.
  await navigate(`${origin}/?mock=rail&slots=none`, "!!document.querySelector('.list .one')");
  check(await evaluate(`!document.querySelector('#sec-slots') && !document.querySelector('.rail [data-sec=slots]') && !!document.querySelector('#sec-working [data-id=t_search]')`), "V0.137 desktop: a section or a rail item without a queue");
  // Two hosts with a queue: one part each, under its name.
  await navigate(`${origin}/?mock=rail&slots=two`, "!!document.querySelector('#sec-slots .sqhn')");
  const two = await evaluate(`[...document.querySelectorAll('#sec-slots .sqh')].map(p => [p.querySelector('.sqhn')?.textContent, [...p.querySelectorAll('.sqp, .one')].map(r => r.dataset.id ?? r.dataset.owner)])`);
  check(JSON.stringify(two) === JSON.stringify([["laptop", ["self-deploy", "t_search", "t_pw", "t_nix"]], ["lab", ["t_poly"]]]), `V0.137 desktop: one part per host (${JSON.stringify(two)})`);

  // Phone.
  await openScreen({ url: "?mock=rail&slots=queue", ready: "!!document.querySelector('#m-sec-slots .one')" }, 390, 844);
  const p = await evaluate(`({
    tops: [...new Set([...document.querySelectorAll('.m-strip .m-seg')].map(b => Math.round(b.getBoundingClientRect().top)))].length,
    segs: document.querySelectorAll('.m-strip .m-seg').length,
    rows: ${rows(".m-list")},
    wide: document.documentElement.scrollWidth > innerWidth })`);
  check(p.tops === 1 && p.segs === 7 && JSON.stringify(p.rows) === JSON.stringify(["self-deploy #1", "t_search #2", "t_pw #3", "t_nix #4"]) && !p.wide, `V0.137 phone: the strip and the rows (${JSON.stringify(p)})`);
  await evaluate(`document.getElementById('m-sec-slots').scrollIntoView()`);
  await tapCenter("#m-sec-slots .m-sbtn", "!!document.querySelector('#m-sec-slots .sqmv .mv')");
  const r = await evaluate(`({ mv: [...document.querySelectorAll('#m-sec-slots .sqmv .mv .xb')].map(b => Math.round(Math.min(b.getBoundingClientRect().width, b.getBoundingClientRect().height))), wide: [...document.querySelectorAll('#m-sec-slots .sqmv, #m-sec-slots .sqmv .xb, #m-sec-slots .m-sec, #m-sec-slots .m-sbtn')].some(e => e.getBoundingClientRect().right > innerWidth + 0.5) })`);
  check(r.mv.length === 6 && r.mv.every((x) => x >= 44) && !r.wide, `V0.137 phone: the Reorder mode (${JSON.stringify(r)})`);
  await shot("slots-phone.png");
  await tapCenter(`#m-sec-slots .sqmv[data-id=t_search] .mv .xb[aria-label$="down"]`, `${COMMANDS("host.slotOrder")}.length === 1`);
  const ps = await evaluate(`({ sent: ${COMMANDS("host.slotOrder")}.map(c => c.args), rows: ${rows(".m-list")} })`);
  check(JSON.stringify(ps.sent[0]) === JSON.stringify({ owners: ["t_pw", "t_search", "t_nix"] }) && JSON.stringify(ps.rows) === JSON.stringify(["self-deploy #1", "t_pw #2", "t_search #3", "t_nix #4"]), `V0.137 phone: ↓ on t_search (${JSON.stringify(ps)})`);
} catch (e) {
  failures.push(`V0.137: ${e.message}`);
}
});

// SPEC 0.5 V0.92, V0.93: the rail's top is the rendered logo (loaded, 32 px, inside the rail), not the text "Fyr".
section('V0.92, V0.93', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .brand img') && document.querySelector('.rail .brand img').complete");
  const r = await evaluate(`(() => {
    const img = document.querySelector('.rail .brand img'), b = img.getBoundingClientRect(), rail = document.querySelector('.rail').getBoundingClientRect();
    return { w: b.width, h: b.height, loaded: img.naturalWidth > 0, alt: img.alt, src: img.getAttribute("src"), alt: img.getAttribute("alt"), text: document.querySelector(".rail .brand").textContent.trim(), inside: b.left >= rail.left && b.right <= rail.right };
  })()`);
  check(r.src === "/logo.png" && r.w === 32 && r.h === 32 && r.loaded && r.alt === "Fyr" && r.text === "" && r.inside, `V0.92 desktop: the rail's mark (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.92 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.116: a project's image icon. Desktop: Settings → Projects draws Fyr's image (28 px, loaded) and a
// project whose image does not load (no fixture) its glyph, never a broken image; Fyr's project screen marks the
// agent's values "Set by agent" and offers Replace… and Remove (V0.119: the Image tab) and Run setup or Run again. Phone: the
// project screen shows the image at 44 px and its buttons are 44 px high.
section('V0.116', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .rgear')");
  await evaluate(`document.querySelector('.rail .rgear').click()`);
  await waitFor(`!!document.querySelector('.dlg .stn-i[data-page=projects]')`);
  await evaluate(`document.querySelector('.dlg .stn-i[data-page=projects]').click()`); // V0.119: Projects is its own page
  await waitFor(`[...document.querySelectorAll('.dlg .pli')].some((b) => b.textContent.includes('Fyr') && b.querySelector('.pi.img img')?.complete)`);
  await waitFor(`![...document.querySelectorAll('.dlg .pli .pi.img img')].some((i) => i.complete && !i.naturalWidth)`, { soft: true });
  const r = await evaluate(`(() => {
    const rows = [...document.querySelectorAll('.dlg .pli')];
    const fyr = rows.find((b) => b.textContent.includes('Fyr')), img = fyr.querySelector('.pi.img img'), b = img.getBoundingClientRect();
    const broken = [...document.querySelectorAll('.dlg .pli .pi.img img')].filter((i) => i.complete && !i.naturalWidth).length;
    const glyphs = rows.filter((x) => x.querySelector('.pi:not(.img)')).length;
    return { w: b.width, h: b.height, loaded: img.naturalWidth > 0, broken, glyphs };
  })()`);
  check(r.loaded && r.w === 28 && r.h === 28 && r.broken === 0 && r.glyphs > 0, `V0.116 desktop: the project image icons in Settings (${JSON.stringify(r)})`);
  if (process.env.FYR_SHOTS) {
    await evaluate(`document.querySelector('.dlg section[aria-label="Projects"]')?.scrollIntoView({ block: 'start' })`);
    await settle();
    writeFileSync(join(process.env.FYR_SHOTS, "project-icons-settings-desktop.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  }
  await evaluate(`[...document.querySelectorAll('.dlg .pli')].find((b) => b.textContent.includes('Fyr')).click()`);
  await waitFor(`!!document.querySelector('.pedit .pimg')`);
  await settle();
  const e = await evaluate(`(() => {
    const ed = document.querySelector('.pedit');
    return { setby: [...ed.querySelectorAll('.sr-t')].filter((s) => s.querySelector('.setby')).map((s) => s.firstChild.textContent.trim()), buttons: [...ed.querySelectorAll('.pimg button, .psetup button')].map((b) => b.textContent.trim()), head: ed.querySelector('.phead .pi.img img')?.naturalWidth > 0 };
  })()`);
  check(e.head && e.setby.join("|") === "Name|Colour|Icon" && /^Replace…\|Remove\|Run (setup|again)$/.test(e.buttons.join("|")), `V0.116 desktop: Fyr's project screen (${JSON.stringify(e)})`);
  if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, "project-screen-desktop.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));

  await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await navigate(`${origin}/?mock=rail&project=p_fyr`, "!!document.querySelector('.pedit .phead .pi.img img') && document.querySelector('.pedit .phead .pi.img img').complete");
  await settle();
  const m = await evaluate(`(() => {
    const img = document.querySelector('.pedit .phead .pi.img img'), b = img.getBoundingClientRect();
    return { w: b.width, loaded: img.naturalWidth > 0, heights: [...document.querySelectorAll('.pedit .pimg button, .pedit .psetup button')].map((x) => x.getBoundingClientRect().height), setby: document.querySelectorAll('.pedit .setby').length };
  })()`);
  check(m.loaded && m.w === 44 && m.heights.length === 3 && m.heights.every((x) => x >= 44) && m.setby === 3, `V0.116 phone: Fyr's project screen (${JSON.stringify(m)})`);
  if (process.env.FYR_SHOTS) {
    writeFileSync(join(process.env.FYR_SHOTS, "project-screen-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
    await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.m-ih')");
    await settle();
    writeFileSync(join(process.env.FYR_SHOTS, "inbox-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  }
} catch (e) {
  failures.push(`V0.116: ${e.message}`);
}
});

// SPEC 0.5 V0.103, V0.113: the phone's inbox header shows the same logo (28 px, loaded, inside the header) and no
// visible name; the image's alt is the accessible name "Fyr".
section('V0.103, V0.113', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.m-ih .m-brand img') && document.querySelector('.m-ih .m-brand img').complete");
  const r = await evaluate(`(() => {
    const img = document.querySelector('.m-ih .m-brand img'), b = img.getBoundingClientRect(), h = document.querySelector('.m-ih').getBoundingClientRect();
    return { w: b.width, h: b.height, loaded: img.naturalWidth > 0, src: img.getAttribute("src"), alt: img.getAttribute("alt"), text: document.querySelector(".m-ih .m-brand").textContent.trim(), inside: b.left >= h.left && b.right <= h.right && b.top >= h.top && b.bottom <= h.bottom };
  })()`);
  check(r.src === "/logo.png" && r.w === 28 && r.h === 28 && r.loaded && r.alt === "Fyr" && !r.text.includes("Fyr") && r.inside, `V0.103 phone: the inbox header's logo (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.103 phone: ${e.message}`);
}
});

// #64: the thread header's blocks (title, chips, offers, tabs) sit 12 px apart, and the conversation starts where
// the header ends: a thread without previews has no band between the tabs and the conversation.
section('line 2218', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1400, height: 575, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=1&open=t_done`, "!!document.querySelector('.th .thh .offers') && !!document.querySelector('.th .convo')");
  await settle();
  const g = await evaluate(`(() => {
    const r = (s) => document.querySelector('.th ' + s)?.getBoundingClientRect();
    const tr = r('.thh .tr'), chips = r('.thh .chips'), offers = r('.thh .offers'), tabs = r('.thh .tabs'), thh = r('.thh'), convo = r('.convo');
    return { trChips: chips.top - tr.bottom, chipsOffers: offers.top - chips.bottom, offersTabs: tabs.top - offers.bottom, headConvo: convo.top - thh.bottom, strip: !!document.querySelector('.th .cv-strip:not(:empty)') };
  })()`);
  const near = (v) => Math.abs(v - 12) <= 0.5;
  check(near(g.trChips) && near(g.chipsOffers) && near(g.offersTabs), `#64 desktop: the header's gaps are not 12 px (${JSON.stringify(g)})`);
  check(Math.abs(g.headConvo) <= 0.5, `#64 desktop: a band between the header and the conversation (${JSON.stringify(g)})`);
} catch (e) {
  failures.push(`#64 desktop: ${e.message}`);
}
});

// #72: without git the Changes tab has no Refresh and no empty diff section; the change cards keep the tab's height.
section('line 2236', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&open=t_deploy&tab=changes`, "!!document.querySelector('.th .chtab .chg .chg-empty')");
  const n = await evaluate(`(() => {
    const tab = document.querySelector('.th .chtab'), chg = tab.querySelector('.chg'), chl = tab.querySelector('.chl');
    return { refresh: !!tab.querySelector('.chg-ref'), chgH: chg.getBoundingClientRect().height, cap: chl ? getComputedStyle(chl).maxHeight : 'none', text: chg.textContent };
  })()`);
  check(!n.refresh && n.chgH <= 48 && n.cap === "none", `#72 desktop: the non-git Changes tab still draws its diff section (${JSON.stringify(n)})`);
} catch (e) {
  failures.push(`#72 desktop: ${e.message}`);
}
});

// V0.118 (amends #81): the busy status is one short line already, so it has no fold; a press on it shows the whole
// sentence and a second press makes it one line again, on the desktop and the phone (44 px there).
section('V0.118 (amends #81)', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
for (const [w, h, mobile] of [[1280, 800, false], [390, 844, true]]) {
  const ctx = `V0.118 press ${mobile ? "phone" : "desktop"}`;
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: mobile });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: mobile ? 3 : 1, mobile });
    await navigate(`${origin}/?mock=rail&deploy=drain`, "!!document.querySelector('.dbn .dbn-x')");
    const one = await evaluate(`(() => { const e = document.querySelector('.dbn .dbn-x'), r = e.getBoundingClientRect(); return { h: r.height, fold: !!document.querySelector('.dbn [aria-label=Fold], .dbn-chip'), lines: Math.round(document.querySelector('.dbn .dbn-t').getBoundingClientRect().height / parseFloat(getComputedStyle(e).lineHeight)) }; })()`);
    check(!one.fold && one.lines === 1 && one.h >= (mobile ? 44 : 28), `${ctx}: the busy status is not one line without a fold (${JSON.stringify(one)})`);
    await evaluate(`document.querySelector('.dbn .dbn-x').click()`);
    await waitFor(`!!document.querySelector('.dbn.open')`, { soft: true });
    const open = await evaluate(`({ open: document.querySelector('.dbn .dbn-x').getAttribute('aria-expanded'), wraps: getComputedStyle(document.querySelector('.dbn .dbn-t')).whiteSpace })`);
    check(open.open === "true" && open.wraps === "normal", `${ctx}: a press does not show the whole sentence (${JSON.stringify(open)})`);
    await evaluate(`document.querySelector('.dbn .dbn-x').click()`);
    await waitFor(`!document.querySelector('.dbn.open')`, { soft: true });
    check(await evaluate(`!document.querySelector('.dbn.open')`), `${ctx}: a second press does not close it`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
}
});

// V0.83: while the update waits for running threads the banner has Restart now inside the screen (44 px on the
// phone); a press opens Fyr's confirmation with a red Restart now, and Cancel sends nothing.
section('V0.83', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
for (const [w, h, mobile] of [[1280, 800, false], [390, 844, true]]) {
  const ctx = `V0.83 ${mobile ? "phone" : "desktop"}`;
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: mobile });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: mobile ? 3 : 1, mobile });
    await navigate(`${origin}/?mock=rail&deploy=drain`, "!!document.querySelector('.dbn')");
    const b = await evaluate(`(() => { const e = document.querySelector('.dbn .dbn-now'); if (!e) return null; const r = e.getBoundingClientRect(); return { text: e.textContent.trim(), w: r.width, h: r.height, in: r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight }; })()`);
    check(!!b && b.text === "Restart now" && b.in, `${ctx}: no Restart now inside the screen (${JSON.stringify(b)})`);
    if (mobile) check(!!b && b.h >= 44 && b.w >= 44, `${ctx}: Restart now is smaller than 44 px (${JSON.stringify(b)})`);
    await evaluate(`document.querySelector('.dbn .dbn-now')?.click()`);
    const sel = mobile ? ".m-sheet.cfm" : ".dlg.cfm";
    const prim = mobile ? ".cfm-f .m-bt.p" : ".df .btn.p";
    await waitFor(`!!document.querySelector('${sel}')`, { soft: true });
    const c = await evaluate(`(() => { const d = document.querySelector('${sel}'); if (!d) return null; const p = d.querySelector('${prim}'); return { title: d.querySelector('.cfm-t')?.textContent ?? '', text: d.querySelector('.cfm-text')?.textContent ?? '', primary: p?.textContent.replace('⏎', '').trim(), danger: !!p?.classList.contains('danger') }; })()`);
    check(!!c && c.title === "Restart Fyr now?" && c.primary === "Restart now" && c.danger && /continue/.test(c.text), `${ctx}: pressing Restart now does not open its confirmation (${JSON.stringify(c)})`);
    await evaluate(`document.querySelector('${sel} ${mobile ? ".cfm-f .m-bt:not(.p)" : ".df .btn:not(.p)"}')?.click()`);
    await waitFor(`!document.querySelector('${sel}')`, { soft: true });
    check(await evaluate(`!document.querySelector('${sel}') && /waiting for/.test(document.querySelector('.dbn')?.textContent ?? '')`), `${ctx}: Cancel does not close the confirmation or the banner changed`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
}
});

// #85: a dismissed "Fyr is updated" stays dismissed after a reload (the mock stamps the report with a new time on
// every load, as a restarted host may).
section('line 2298', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail&deploy=done`, "!!document.querySelector('.dbn.ok [aria-label=Dismiss]')");
  await evaluate(`document.querySelector('.dbn.ok [aria-label=Dismiss]').click()`);
  await waitFor(`!document.querySelector('.dbn')`, { soft: true });
  await navigate(`${origin}/?mock=rail&deploy=done`, "!!document.querySelector('.rail')");
  await settle();
  check(await evaluate(`!document.querySelector('.dbn')`), "#85 desktop: a dismissed update result shows again after a reload");
} catch (e) {
  failures.push(`#85 desktop: ${e.message}`);
}
});

// V0.73 (2): on the desktop, the Changes tab of six cards over an empty diff, in a tall and a short window.
section('V0.73 (2)', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
for (const [w, h] of [[1280, 800], [1024, 600]]) {
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/?mock=1&open=t_ended&tab=changes`, "!!document.querySelector('.th .chtab .chg-empty')");
    checkFlow(await evaluate(FLOW), `ended changes desktop ${w}x${h}`);
  } catch (e) {
    failures.push(`ended changes desktop ${w}x${h}: ${e.message}`);
  }
}
});

// SPEC 0.5 V0.68: on the desktop the confirmation is a dialog that fits the window, with focus on its primary
// button; Escape closes it without a press, Enter presses. A click outside does not close it.
section('V0.68', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=1&open=t_done`, "[...document.querySelectorAll('.th .offb .btn')].some((b) => b.textContent.includes('Set the Acme token'))");
  const press = () => evaluate(`[...document.querySelectorAll('.th .offb .btn')].find((b) => b.textContent.includes('Set the Acme token'))?.click()`);
  const dlg = () => evaluate(`(() => { const d = document.querySelector('.dlg.cfm'); if (!d) return null; const r = d.getBoundingClientRect(); const p = d.querySelector('.df .btn.p'); return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, title: d.querySelector('.cfm-t')?.textContent ?? '', primary: p?.textContent.replace('⏎', '').trim(), focused: document.activeElement === p, hover: d.querySelector('.cfm-code')?.title ?? '', runs: document.querySelectorAll('.th .offrun').length }; })()`);
  // V0.75 (1), #62: an action's result shows on its own button, so the buttons are what this check reads.
  const runs = () => evaluate(`[...document.querySelectorAll('.th .offb .btn')].map((r) => r.textContent).filter((t) => !/^✦|^Set the Acme token$/.test(t) || /✓|failed|running/.test(t)).join('|')`);
  const opened = `document.activeElement === document.querySelector('.dlg.cfm .df .btn.p')`;
  await press();
  await waitFor(opened, { soft: true });
  await settle();
  const a = await dlg();
  check(!!a && a.top >= 0 && a.bottom <= 800 && a.left >= 0 && a.right <= 1280, `V0.68 desktop: the dialog does not fit the window (${JSON.stringify(a)})`);
  check(!!a && a.title === "Set the Acme token" && a.primary === "Run" && a.focused, `V0.68 desktop: title, primary or focus wrong (${JSON.stringify(a)})`);
  check(!!a && a.hover.includes("/home/owner/.local/share/fyr/scratch/20261115-130828-find-the-acme-claude-long-term-token/set-acme-token.sh"), `V0.68 desktop: no full command on hover ("${a?.hover}")`);
  if (process.env.FYR_SHOTS) writeFileSync(join(process.env.FYR_SHOTS, "confirm-desktop.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  await evaluate(`document.querySelector('.cfm-scrim')?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))`);
  await settle();
  check(!!(await dlg()), "V0.68 desktop: a click outside closes the dialog");
  const before = await runs();
  await key("Escape");
  check(!(await dlg()) && (await runs()) === before, "V0.68 desktop: Escape does not close the dialog without a press");
  await press();
  await waitFor(opened, { soft: true });
  await key("Enter");
  await waitFor(`!document.querySelector('.dlg.cfm') && [...document.querySelectorAll('.th .offers .btn')].some((r) => /Set the Acme token.*(✓|failed|running)/.test(r.textContent))`, { soft: true }); // V0.75 (1): the result is on the button
  const after = await runs();
  check(!(await dlg()) && /Set the Acme token/.test(after), `V0.68 desktop: Enter does not run the action (runs "${after}")`);
  if (process.env.FYR_SHOTS) {
    // V0.101: the desktop inbox next to the thread's Changes tab, with the causes inline.
    await navigate(`${origin}/?mock=1&open=t_done&tab=changes`, "!!document.querySelector('.chtab')");
    await settle();
    writeFileSync(join(process.env.FYR_SHOTS, "changes-desktop.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
  }
} catch (e) {
  failures.push(`V0.68 desktop: ${e.message}`);
}
});

// V0.109: the marked row in the thread list is the open thread, whatever path opened it, and the row is in view:
// a link from another thread, ?open= of a thread in a folded section (Settled, V0.186: its header), and k in the list.
section('V0.109', async (h) => {
  const { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached, shot, calibrate } = h;
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 400, deviceScaleFactor: 1, mobile: false });
  const MARK = `(() => {
    const rows = [...document.querySelectorAll('.list [aria-selected="true"]')];
    const list = document.querySelector('.list').getBoundingClientRect(), r = rows[0]?.getBoundingClientRect();
    return { ids: rows.map((x) => x.dataset.id), title: rows[0]?.querySelector('.hd .t')?.textContent ?? null, pane: document.querySelector('.th .tt')?.getAttribute('title') ?? null,
      inView: !!r && r.top >= list.top - 0.5 && r.bottom <= list.bottom + 0.5 };
  })()`;
  const ok = (m) => m.ids.length === 1 && m.title === m.pane && m.inView;
  await navigate(`${origin}/?mock=rail&open=t_babysit`, "!!document.querySelector('.th .fth')");
  await evaluate(`document.querySelector('.th .fth').click()`);
  await waitFor(`${MARK}.pane === 'Map perf'`, { soft: true });
  await settle();
  const a = await evaluate(MARK);
  check(ok(a) && a.ids[0] === "t_mapperf", `V0.109 desktop: after a "From thread" link the marked row is not the open thread in view (${JSON.stringify(a)})`);
  await navigate(`${origin}/?mock=rail&open=t_set7`, "!!document.querySelector('.th .tt')");
  await settle();
  // V0.186: Settled stays folded; its header is the mark (the section V0.186 below checks it).
  await waitFor(`!!document.querySelector('#sec-settled .sec.held')`, { soft: true });
  check(await evaluate(`!!document.querySelector('#sec-settled .sec.held') && !document.querySelector('#sec-settled [data-id]')`), `V0.109 desktop: a thread opened in the folded Settled section did not mark its header`);
  await evaluate(`document.querySelector('.list').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))`);
  await key("k", "KeyK");
  const c = await evaluate(MARK);
  check(ok(c) && c.ids[0] !== "t_set7", `V0.109 desktop: k in the list marks a row that is not the open thread (${JSON.stringify(c)})`);
} catch (e) {
  failures.push(`V0.109 desktop: ${e.message}`);
}
});

// V0.145: one history entry per thread the owner opens on the desktop. Open A, B and C from the list (B on another
// tab), then the mouse's Back button (button 3) and history.back() go to B and A, history.forward() to B on the tab it
// had. Waiting adds no entry (the stream, the view refreshing).
section('V0.145 desktop', async (h) => {
  const { send, evaluate, waitFor, settle, navigate } = h;
try {
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.ib .list [data-id]') && !!document.querySelector('.th .tt')");
  // The tab is shared with earlier sections, and Chrome caps history.length at 50: start from an empty history so
  // three opened threads always add three entries.
  await send("Page.resetNavigationHistory", {});
  const AT = `(() => ({ marked: document.querySelector('.ib .list [aria-selected="true"]')?.dataset.id ?? null, pane: document.querySelector('.th .tt')?.getAttribute('title') ?? null,
    tab: document.querySelector('.th [role=tab].on')?.textContent ?? null, open: new URLSearchParams(location.search).get('open'), n: history.length }))()`;
  const ids = await evaluate(`[...document.querySelectorAll('.ib .list [data-id]')].map((r) => r.dataset.id)`);
  const first = (await evaluate(AT)).marked;
  const [A, B, C] = ids.filter((x) => x !== first);
  const titleOf = (id) => evaluate(`document.querySelector('.ib .list [data-id=${JSON.stringify(id)}] .hd .t')?.textContent ?? null`);
  const titles = { [A]: await titleOf(A), [B]: await titleOf(B), [C]: await titleOf(C) };
  // Real clicks, as the owner's: Chrome marks an entry pushed without user activation (a scripted el.click()) as
  // skippable, and its own Back (the side buttons, Alt+Left) jumps over such entries; history.back() does not. With
  // scripted clicks the press's own mousedown usually activated the page just in time, but not under load, and then
  // Back had nothing to go to (the flake after #205: "the mouse's Back button from C does not show B").
  const realClick = async (sel) => {
    const p = await evaluate(`(() => { const r = document.querySelector(${JSON.stringify(sel)}).getBoundingClientRect(); return [r.left + Math.min(r.width / 2, 40), r.top + r.height / 2]; })()`);
    await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: p[0], y: p[1] });
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x: p[0], y: p[1], button: "left", buttons: 1, clickCount: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: p[0], y: p[1], button: "left", buttons: 0, clickCount: 1 });
  };
  const open = async (id) => {
    await evaluate(`document.querySelector('.ib .list [data-id=${JSON.stringify(id)}]').scrollIntoView({ block: 'center' })`);
    await realClick(`.ib .list [data-id=${JSON.stringify(id)}] .hd .t`);
    await waitFor(`${AT}.marked === ${JSON.stringify(id)}`, { soft: true });
    await settle();
  };
  const n0 = (await evaluate(AT)).n;
  await open(A);
  await open(B);
  // B on a tab that is not its first, when it has one.
  const bTab = await evaluate(`(() => { const t = [...document.querySelectorAll('.th [role=tab]')].find((x) => !x.classList.contains('on')); if (t) t.dataset.pick = '1'; return t?.textContent ?? null; })()`);
  if (bTab) {
    await realClick(`.th [role=tab][data-pick]`);
    await waitFor(`document.querySelector('.th [role=tab].on')?.textContent === ${JSON.stringify(bTab)}`, { soft: true });
  }
  await settle();
  await open(C);
  const atC = await evaluate(AT);
  check(await evaluate(`navigator.userActivation.hasBeenActive`), `V0.145 desktop: the page has no user activation, so Chrome's Back would skip its entries`);
  check(atC.n === n0 + 3 && atC.open === C, `V0.145 desktop: opening three threads does not add three entries (${JSON.stringify({ n0, atC })})`);
  await new Promise((r) => setTimeout(r, 1500));
  await settle();
  check((await evaluate(AT)).n === atC.n, `V0.145 desktop: an entry added while nobody opened a thread`);
  const want = async (id, what) => {
    await waitFor(`${AT}.marked === ${JSON.stringify(id)}`, { soft: true });
    await settle();
    const s = await evaluate(AT);
    check(s.marked === id && s.pane === titles[id] && s.open === id, `V0.145 desktop: ${what} does not show ${id} (${JSON.stringify(s)})`);
    return s;
  };
  // The mouse's Back button.
  const c = await evaluate(`(() => { const r = document.querySelector('.th').getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`);
  await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: c[0], y: c[1] });
  await send("Input.dispatchMouseEvent", { type: "mousePressed", x: c[0], y: c[1], button: "back", buttons: 8, clickCount: 1 });
  await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: c[0], y: c[1], button: "back", buttons: 0, clickCount: 1 });
  await want(B, "the mouse's Back button from C");
  await evaluate(`history.back()`);
  await want(A, "Back twice from C");
  await evaluate(`history.forward()`);
  const atB = await want(B, "Forward from A");
  if (bTab) check(atB.tab === bTab, `V0.145 desktop: Forward to B shows the tab ${atB.tab}, not the one it had (${bTab})`);
  // The mouse's Forward button.
  await send("Input.dispatchMouseEvent", { type: "mousePressed", x: c[0], y: c[1], button: "forward", buttons: 16, clickCount: 1 });
  await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: c[0], y: c[1], button: "forward", buttons: 0, clickCount: 1 });
  await want(C, "the mouse's Forward button from B");
} catch (e) {
  failures.push(`V0.145 desktop: ${e.message}`);
}
});

// SPEC 0.5 V0.134, V0.169: the filter rows, the same on the desktop and the phone. 1 one host: no host row; every project
// chip has a thread in view, and folding the sections takes their projects' chips away. 2 two hosts: row 1 is a switch,
// one segment per host in the owner's order, all on, with its dot and count, above row 2. 3 turning host-b off hides its
// threads, drops the projects only it has from row 2, is remembered, and leaves desk as the last host on, which cannot
// be turned off. 4 a host turned off stays when its threads are folded away; the empty state names it; Clear filter
// turns every host on. 5 V0.134's one-host filter is dropped.
for (const [w, hh, phone] of [[1280, 800, false], [390, 844, true]]) section(`V0.169 ${phone ? "phone" : "desktop"}`, async (h) => {
  const { send, evaluate, waitFor, settle, navigate, openScreen, shot } = h;
  const ctx = `V0.169 ${phone ? "phone" : "desktop"}`;
try {
  const root = phone ? ".m-screen " : ".ib ";
  const go = async (url, storage) => {
    if (phone) await openScreen({ url, storage, ready: "!!document.querySelector('.m-list')" }, w, hh);
    else {
      await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: false });
      await navigate(`${origin}/${url}`, "!!document.querySelector('.ib .list')");
      if (storage) {
        await evaluate(`(() => { const s = ${JSON.stringify(storage)}; for (const k in s) localStorage.setItem(k, s[k]); return true; })()`);
        await navigate(`${origin}/${url}`, "!!document.querySelector('.ib .list')");
      }
    }
    await settle();
  };
  const click = async (sel, until) => {
    await evaluate(`document.querySelector(${JSON.stringify(sel)}).click()`);
    await waitFor(until, { soft: true });
    await settle();
  };
  const seg = (name) => `${root}.hfrow .hft[aria-label^="${name},"]`;
  const secHead = (k) => (phone ? `#m-sec-${k} .m-sec` : `#sec-${k} .sec`);
  const STATE = `(() => {
    const box = (e) => e?.getBoundingClientRect();
    const rows = [...document.querySelectorAll(${JSON.stringify(phone ? ".m-list .m-mv" : ".ib .list [data-id]")})];
    return {
      hosts: [...document.querySelectorAll('${root}.hfrow .hft')].map((b) => ({ name: b.querySelector('.nm')?.textContent ?? '', on: b.getAttribute('aria-pressed') === 'true', last: b.getAttribute('aria-disabled') === 'true', n: b.querySelector('.hn')?.textContent ?? '', struck: getComputedStyle(b.querySelector('.nm')).textDecorationLine, dot: (() => { const m = b.querySelector('.hmark'); return m ? m.className + ':' + m.dataset.glyph : null; })(), h: Math.round(box(b).height) })),
      projects: [...document.querySelectorAll('${root}.pfrow .pfc')].map((b) => ({ name: b.querySelector('.ell')?.textContent ?? '', n: Number((/: (\\d+) thread/.exec(b.title) ?? [])[1] ?? -1), h: Math.round(box(b).height) })),
      order: (() => { const a = box(document.querySelector('${root}.hfrow')), b = box(document.querySelector('${root}.pfrow')); return a && b ? b.top >= a.bottom : null; })(),
      fits: [...document.querySelectorAll('${root}.hfrow, ${root}.pfrow')].every((e) => box(e).right <= innerWidth + 0.5),
      // The phone rows carry their host's mark (V0.170); the desktop rows too, but there the ids say it.
      rowHosts: [...new Set(rows.map((r) => (${JSON.stringify(phone)} ? r.querySelector('.rhm .hmark')?.dataset.host : r.dataset.id) ?? ''))],
      rows: rows.length,
      empty: document.querySelector(${JSON.stringify(phone ? ".m-fempty" : ".fempty")})?.innerText ?? null,
      stored: localStorage.getItem('fyr.hostsOff'),
      old: localStorage.getItem('fyr.hostFilter'),
    };
  })()`;
  // 1. One host: no host row; folded sections are not considered.
  await go("?mock=rail");
  const one = await evaluate(STATE);
  check(one.hosts.length === 0 && !(await evaluate(`!!document.querySelector('${root}.hfrow')`)), `${ctx}: one host shows a host row (${JSON.stringify(one.hosts)})`);
  check(one.projects.length > 1 && one.projects.every((c) => c.n >= 1), `${ctx}: a project chip without a thread in view (${JSON.stringify(one.projects)})`);
  // Fold Needs, Working and Seen (Snoozed and Settled are folded already): only the pinned threads' projects keep a
  // chip. (Not by opening Settled: its auto-close clock keeps the DOM moving.)
  for (const k of ["needs", "working", "seen"]) await click(secHead(k), "true");
  const foldedAll = await evaluate(`({ s: ${STATE}, pinned: document.querySelectorAll(${JSON.stringify(phone ? "#m-sec-pinned .m-mv" : "#sec-pinned [data-id]")}).length })`);
  check(foldedAll.s.projects.length < one.projects.length && foldedAll.s.projects.reduce((a, c) => a + c.n, 0) <= foldedAll.pinned, `${ctx}: folding the sections does not take their projects' chips away (${JSON.stringify({ before: one.projects.map((c) => c.name), after: foldedAll.s.projects, pinned: foldedAll.pinned })})`);
  await evaluate(`localStorage.removeItem('fyr.sectionOpen')`); // V0.186: the folds outlive a reload; the rest starts unfolded
  // 2. Two hosts: the switch, all on, in the owner's order, above the projects. 5: the old key is dropped.
  await go("?mock=rail&hosts=2&hostOrder=host-b,desk", { "fyr.hostFilter": "desk" });
  const two = await evaluate(STATE);
  check(JSON.stringify(two.hosts.map((c) => c.name)) === JSON.stringify(["host-b", "desk"]) && two.hosts.every((c) => c.on && !c.last && Number(c.n) >= 1), `${ctx}: the host switch (${JSON.stringify(two.hosts)})`);
  // V0.170: the segments carry the host's mark (an offline host's with its ring).
  check(two.hosts[0]?.dot === "hmark off:server" && two.hosts[1]?.dot === "hmark:laptop", `${ctx}: the hosts' marks (${JSON.stringify(two.hosts.map((c) => c.dot))})`);
  check(two.order === true && two.fits, `${ctx}: the host row is not above the project row, or a row runs past the edge (${JSON.stringify({ order: two.order, fits: two.fits })})`);
  check(two.old === null && two.stored === null && two.rows > 2, `${ctx}: V0.134's one-host filter still applies (${JSON.stringify({ old: two.old, stored: two.stored, rows: two.rows })})`);
  if (phone) check(two.hosts.every((c) => c.h >= 36) && two.projects.every((c) => c.h >= 32), `${ctx}: a host segment under 36 px or a chip under 32 px on the phone`);
  await shot(`host-rows-${phone ? "phone" : "desktop"}.png`);
  // 3. host-b off: its threads go, its projects fall out of row 2, remembered; desk is the last one on.
  await click(seg("host-b"), `localStorage.getItem('fyr.hostsOff') === '["host-b"]'`);
  const off = await evaluate(STATE);
  check(off.hosts[0]?.on === false && off.hosts[0]?.n === "hidden" && off.hosts[0]?.struck === "line-through" && off.hosts[1]?.on && off.hosts[1]?.last, `${ctx}: host-b off (${JSON.stringify(off.hosts)})`);
  check(off.rows >= 1 && off.rows < two.rows && off.rowHosts.every((x) => (phone ? x === "desk" : !["t_lasso", "t_triage"].includes(x))), `${ctx}: a host-b thread with host-b off (${JSON.stringify({ rowHosts: off.rowHosts, rows: off.rows, before: two.rows })})`);
  const gone = two.projects.map((c) => c.name).filter((n) => !off.projects.some((c) => c.name === n));
  check(gone.includes("acme/ops") && off.projects.every((c) => c.n >= 1 && two.projects.some((d) => d.name === c.name)), `${ctx}: host-b's projects do not fall out of row 2 (${JSON.stringify({ before: two.projects.map((c) => c.name), after: off.projects })})`);
  await shot(`host-rows-off-${phone ? "phone" : "desktop"}.png`);
  await click(seg("desk"), "true");
  check((await evaluate(STATE)).stored === '["host-b"]', `${ctx}: the last host on was turned off`);
  await go("?mock=rail&hosts=2&hostOrder=host-b,desk");
  const again = await evaluate(STATE);
  check(again.hosts[0]?.on === false && again.rows === off.rows, `${ctx}: the hosts turned off are not remembered on this device (${JSON.stringify(again.hosts)})`);
  await click(seg("host-b"), `!localStorage.getItem('fyr.hostsOff')`);
  check((await evaluate(STATE)).hosts.every((c) => c.on), `${ctx}: host-b back on`);
  // 4. desk off, then fold Needs and Unread (host-b's threads are there, V0.188): desk stays off; the empty state; Clear filter.
  await click(seg("desk"), `localStorage.getItem('fyr.hostsOff') === '["desk"]'`);
  await click(secHead("needs"), "true");
  await click(secHead("unread"), `!!document.querySelector(${JSON.stringify(phone ? ".m-fempty" : ".fempty")})`);
  const folded = await evaluate(STATE);
  check(folded.hosts.some((c) => c.name === "desk" && !c.on), `${ctx}: the host turned off after the other host's threads were folded away (${JSON.stringify(folded.hosts)})`);
  check(/^No threads in view with desk hidden\.\s*\d+ more in folded sections?\.\s*Clear filter$/.test(folded.empty ?? ""), `${ctx}: the empty state "${folded.empty}"`);
  await shot(`host-rows-empty-${phone ? "phone" : "desktop"}.png`);
  await click(`${phone ? ".m-fempty" : ".fempty"} .fe3`, `!localStorage.getItem('fyr.hostsOff')`);
  const cleared = await evaluate(STATE);
  check(cleared.empty === null && cleared.stored === null, `${ctx}: Clear filter (${JSON.stringify(cleared)})`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// SPEC 0.5 V0.170: each host's mark (a circle with its machine's glyph in its colour), the same everywhere, on the
// desktop and the phone. 1 one host: no mark on rows. 2 two hosts: every row has its host's mark before the project's
// icon, named by its tooltip, without overlap, and the title keeps its two lines; the phone rows no longer say the host
// in words (an offline one still does); the host switch's segments use the mark. 3 the thread header, Settings → Hosts
// (a laptop, a server, a desktop, each its own colour), the load meters and the Host field.
for (const [w, hh, phone] of [[1280, 800, false], [390, 844, true]]) section(`V0.170 ${phone ? "phone" : "desktop"}`, async (h) => {
  const { send, evaluate, waitFor, settle, navigate, openScreen, shot } = h;
  const ctx = `V0.170 ${phone ? "phone" : "desktop"}`;
try {
  const go = async (url, ready) => {
    if (phone) await openScreen({ url, ready }, w, hh);
    else {
      await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: false });
      await navigate(`${origin}/${url}`, ready);
    }
    await settle();
  };
  const listReady = phone ? "!!document.querySelector('.m-list .hd')" : "!!document.querySelector('.ib .list .hd')";
  const ROWS = `[...document.querySelectorAll(${JSON.stringify(phone ? ".m-list .hd" : ".ib .list .hd")})].map((hd) => {
    const m = hd.querySelector('.rhm .hmark'), p = hd.querySelector('.rpi'), t = hd.querySelector('.t');
    const mb = m?.getBoundingClientRect(), pb = p?.getBoundingClientRect(), lh = parseFloat(getComputedStyle(t).lineHeight);
    return { host: m?.dataset.host ?? null, title: m?.getAttribute('title') ?? null, label: m?.getAttribute('aria-label') ?? null, apart: !!mb && !!pb && mb.right <= pb.left + 0.5, lines: Math.round(t.getBoundingClientRect().height / lh), words: !!hd.closest('.m-mv')?.querySelector('.m-hostn') };
  })`;
  // 1. One host: no marks on rows.
  await go("?mock=rail", listReady);
  const one = await evaluate(ROWS);
  check(one.length > 3 && one.every((r) => r.host === null), `${ctx}: one host shows host marks on rows (${JSON.stringify(one.slice(0, 2))})`);
  // 2. Two hosts.
  await go("?mock=rail&hosts=2", listReady);
  const rows = await evaluate(ROWS);
  check(rows.length > 3 && rows.every((r) => r.host && r.title?.startsWith(r.host) && r.label?.startsWith("Host ") && r.apart && r.lines === 2), `${ctx}: a row without its host's mark, or with the mark over the project icon, or a title not two lines (${JSON.stringify(rows.filter((r) => !(r.host && r.apart && r.lines === 2)).slice(0, 3))})`);
  check(rows.some((r) => r.host === "host-b" && r.title === "host-b · offline") && rows.some((r) => r.host === "desk"), `${ctx}: the rows' hosts (${JSON.stringify([...new Set(rows.map((r) => r.title))])})`);
  if (phone) {
    check(rows.every((r) => !r.words), `${ctx}: a phone row still names its host in words`);
    check(await evaluate(`[...document.querySelectorAll('.m-list .m-off')].some((e) => e.textContent === 'host-b offline')`), `${ctx}: an offline host's row no longer says so`);
  }
  const segs = await evaluate(`[...document.querySelectorAll('${phone ? ".m-screen" : ".ib"} .hfrow .hft')].map((b) => ({ mark: b.querySelector('.hmark')?.dataset.host ?? null, dot: !!b.querySelector('.hdot') }))`);
  check(segs.length === 2 && segs.every((s) => s.mark && !s.dot), `${ctx}: the host switch's segments (${JSON.stringify(segs)})`);
  await shot(`host-marks-${phone ? "phone" : "desktop"}.png`);
  // 3. The thread header.
  await go("?mock=rail&hosts=2&open=t_mapperf", phone ? "!!document.querySelector('.m-hostc')" : "!!document.querySelector('.th .hostc')");
  const hc = await evaluate(`(() => { const c = document.querySelector(${JSON.stringify(phone ? ".m-hostc" : ".th .hostc")}); return { mark: c?.querySelector('.hmark')?.dataset.host ?? null, text: c?.textContent ?? '' }; })()`);
  check(hc.mark === "desk" && hc.text === "desk", `${ctx}: the header's host chip (${JSON.stringify(hc)})`);
  // Settings → Hosts: a laptop, a server, a desktop, each its own colour.
  await go(phone ? "?mock=rail&hosts=2&settings=hosts" : "?mock=rail&hosts=2&dialog=settings&page=hosts", `document.querySelectorAll('.sr.host .hmark').length === 4`);
  const sh = await evaluate(`[...document.querySelectorAll('.sr.host .hmark')].map((m) => ({ host: m.dataset.host, glyph: m.dataset.glyph, color: getComputedStyle(m).color }))`);
  const g = Object.fromEntries(sh.map((x) => [x.host, x.glyph]));
  check(g.desk === "laptop" && g["host-b"] === "server" && g.lab === "monitor" && g["old-nuc"] === "server", `${ctx}: Settings → Hosts glyphs (${JSON.stringify(g)})`);
  check(new Set(sh.map((x) => x.color)).size === sh.length, `${ctx}: two hosts share a colour (${JSON.stringify(sh)})`);
  await shot(`host-marks-settings-${phone ? "phone" : "desktop"}.png`);
  // The load meters (the desktop rail; the phone's strip in the inbox header) name each host with its mark.
  await go("?mock=rail&hosts=2", listReady);
  const lm = await evaluate(`[...document.querySelectorAll('.hmn')].map((n) => n.querySelector('.hmark')?.dataset.host ?? null)`);
  check(lm.length >= 2 && lm.every(Boolean), `${ctx}: a load meter's host without its mark (${JSON.stringify(lm)})`);
  // The Host field (New project): the picked host's mark beside the list.
  const root = phone ? ".m-app" : ".dlg";
  await go(phone ? "?mock=rail&hosts=2&settings=projects" : "?mock=rail&hosts=2&dialog=settings&page=projects", `document.querySelectorAll('${root} .pli').length > 3`);
  await evaluate(`[...document.querySelectorAll('${root} button')].find((x) => x.textContent.trim().startsWith('New project'))?.click()`);
  await waitFor(`!!document.querySelector('${root} .hostf .hostsel .hmark')`, { soft: true });
  const hf = await evaluate(`document.querySelector('${root} .hostf .hostsel .hmark')?.dataset.host ?? null`);
  check(hf === "desk", `${ctx}: the Host field's mark (${hf})`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// SPEC 0.5 V0.171: the owner sets a host's icon and colour on its Settings page, by its install id, the same on the
// desktop and the phone. 1 the Mark group shows the automatic mark (a server for host-b). 2 a glyph, a colour, an emoji
// and Automatic each send front.hostMark with host-b's id, and the mark follows. 3 the owner's mark shows on the rows
// and in the host switch; no other host takes its colour. 4 a host without an install id cannot be set.
for (const [w, hh, phone] of [[1280, 800, false], [390, 844, true]]) section(`V0.171 ${phone ? "phone" : "desktop"}`, async (h) => {
  const { send, evaluate, waitFor, settle, navigate, openScreen, shot } = h;
  const ctx = `V0.171 ${phone ? "phone" : "desktop"}`;
  const ID = "M3RS-QX7P-5LKA-2WNE";
try {
  const go = async (url, ready) => {
    if (phone) await openScreen({ url, ready }, w, hh);
    else {
      await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: false });
      await navigate(`${origin}/${url}`, ready);
    }
    await settle();
  };
  const page = (sub) => (phone ? `?mock=rail&hosts=2&settings=hosts/${sub}` : `?mock=rail&hosts=2&dialog=settings&page=hosts/${sub}`);
  const PREV = `(() => { const m = document.querySelector('.hmprev .hmark'); return m ? { glyph: m.dataset.glyph, text: m.textContent, color: getComputedStyle(m).color, desc: m.closest('.sr')?.querySelector('.sr-d')?.textContent ?? '' } : null; })()`;
  const sent = () => evaluate(`${COMMANDS("front.hostMark")}.map((c) => c.args)`);
  // 1. Automatic.
  await go(page("host-b"), "!!document.querySelector('.hmprev .hmark')");
  const a = await evaluate(PREV);
  check(a?.glyph === "server" && /^Automatic: a server/.test(a.desc) && (await evaluate(`document.querySelector('.hmicon .ig.auto')?.getAttribute('aria-checked')`)) === "true", `${ctx}: the automatic mark (${JSON.stringify(a)})`);
  if (phone) check(await evaluate(`[...document.querySelectorAll('.hmicon .ig, .swatches .sw')].every((b) => b.getBoundingClientRect().height >= 44)`), `${ctx}: a mark control under 44 px on the phone`);
  // 2. A glyph, a colour, an emoji, Automatic.
  await evaluate(`document.querySelector('.hmicon .ig[aria-label="house"]').click()`);
  await waitFor(`${PREV}?.glyph === 'house'`, { soft: true });
  await evaluate(`document.querySelector('.swatches .sw[aria-label="teal"]').click()`);
  await waitFor(`${COMMANDS("front.hostMark")}.length === 2`, { soft: true });
  await settle();
  const teal = await evaluate(`getComputedStyle(document.querySelector('.swatches .sw[aria-label="teal"]')).backgroundColor`);
  const b = await evaluate(PREV);
  check(b?.glyph === "house" && b.color === teal && b.desc === "Set by you.", `${ctx}: the mark after house and teal (${JSON.stringify({ b, teal })})`);
  await shot(`host-mark-set-${phone ? "phone" : "desktop"}.png`);
  await evaluate(`(() => { const i = document.querySelector('.pemoji input'); i.value = '🍓'; i.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  await settle();
  await evaluate(`document.querySelector('.pemoji .btn').click()`);
  await waitFor(`${PREV}?.glyph === 'emoji'`, { soft: true });
  const c = await evaluate(PREV);
  check(c?.glyph === "emoji" && c.text === "🍓", `${ctx}: the emoji (${JSON.stringify(c)})`);
  await evaluate(`document.querySelector('.hmicon .ig.auto').click()`);
  await waitFor(`${PREV}?.glyph === 'server'`, { soft: true });
  const args = await sent();
  check(JSON.stringify(args) === JSON.stringify([
    { id: ID, icon: "house", color: "" }, { id: ID, icon: "house", color: "teal" }, { id: ID, icon: "🍓", color: "teal" }, { id: ID, icon: "", color: "teal" },
  ]), `${ctx}: the commands (${JSON.stringify(args)})`);
  // 3. The owner's mark on the rows and in the switch; nobody else teal.
  const seeded = encodeURIComponent(JSON.stringify({ [ID]: { icon: "🍓", color: "teal" } }));
  await go(`?mock=rail&hosts=2&hostMarks=${seeded}`, phone ? "!!document.querySelector('.m-list .rhm')" : "!!document.querySelector('.ib .list .rhm')");
  const rows = await evaluate(`[...document.querySelectorAll('.rhm .hmark, .hfrow .hmark')].map((m) => ({ host: m.dataset.host, glyph: m.dataset.glyph, text: m.textContent, color: getComputedStyle(m).color }))`);
  const hb = rows.filter((r) => r.host === "host-b"), others = rows.filter((r) => r.host !== "host-b");
  check(hb.length >= 2 && hb.every((r) => r.glyph === "emoji" && r.text === "🍓" && r.color === teal) && others.length && others.every((r) => r.color !== teal), `${ctx}: the owner's mark on rows and in the switch (${JSON.stringify({ hb: hb.slice(0, 2), others: others.slice(0, 2) })})`);
  await shot(`host-mark-rows-${phone ? "phone" : "desktop"}.png`);
  // 4. old-nuc has no install id.
  await go(page("old-nuc"), "!!document.querySelector('.hmprev, .sr .hmark')");
  const n = await evaluate(`({ grid: !!document.querySelector('.hmicon'), text: [...document.querySelectorAll('.sr-d')].map((e) => e.textContent).find((t) => t.includes('no install id')) ?? '' })`);
  check(!n.grid && /cannot be set until its next update/.test(n.text), `${ctx}: a host without an id (${JSON.stringify(n)})`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// V0.186: a section folds and unfolds only by the owner's hand. Opening a thread in a folded section, a thread moving
// into one, j/k and a reload leave the folds alone; the folded header carries the mark, on both fronts.
for (const phone of [false, true]) section(`V0.186 ${phone ? "phone" : "desktop"}`, async (h) => {
  const { send, evaluate, waitFor, settle, navigate, openScreen, key, shot } = h;
  const ctx = `V0.186 ${phone ? "phone" : "desktop"}`;
try {
  const head = (k) => (phone ? `#m-sec-${k} .m-sec` : `#sec-${k} .sec`);
  const rowsOf = (k) => (phone ? `#m-sec-${k} .m-mv` : `#sec-${k} [data-id]`);
  const list = phone ? ".m-list" : ".ib .list";
  const HELD = (k) => `(() => {
    const h = document.querySelector(${JSON.stringify(head(k))}), t = h?.querySelector('.held-t'), n = h?.querySelector('.n'), l = document.querySelector(${JSON.stringify(list)}).getBoundingClientRect();
    const r = h?.getBoundingClientRect(), c = n?.getBoundingClientRect(), b = t?.getBoundingClientRect();
    return { held: !!h?.classList.contains('held'), id: h?.dataset.holds ?? null, current: h?.getAttribute('aria-current') ?? null, label: h?.getAttribute('aria-label') ?? '', expanded: h?.getAttribute('aria-expanded') ?? null,
      title: t?.textContent ?? null, rows: document.querySelectorAll(${JSON.stringify(rowsOf(k))}).length, bg: h ? getComputedStyle(h).backgroundColor : '',
      inView: !!r && r.top >= l.top - 0.5 && r.bottom <= l.bottom + 0.5, fits: !!r && !!b && !!c && b.right <= r.right + 0.5 && c.right <= b.left + 0.5 && b.width >= 40 && h.scrollWidth <= h.clientWidth + 1 };
  })()`;
  const fine = (m, id) => m.held && m.id === id && m.current === "true" && m.expanded === "false" && m.rows === 0 && m.inView && m.fits && !!m.title && m.label.includes("holds the current thread") && m.label.includes(m.title);
  if (!phone) {
    await send("Emulation.setTouchEmulationEnabled", { enabled: false });
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 400, deviceScaleFactor: 1, mobile: false });
    // 1. A deep link into folded Settled: it stays folded, its header is marked and scrolled to, no row is.
    await navigate(`${origin}/?mock=rail&open=t_set7`, "!!document.querySelector('.th .tt')");
    await waitFor(`${HELD("settled")}.held`, { soft: true });
    await settle();
    const a = await evaluate(HELD("settled"));
    const pane = await evaluate(`document.querySelector('.th .tt').getAttribute('title')`);
    const select = await evaluate(`(() => { const e = document.createElement('div'); e.className = 'one sel'; document.querySelector('.ib .list').append(e); const c = getComputedStyle(e).backgroundColor; e.remove(); return c; })()`);
    check(fine(a, "t_set7") && a.title === pane && a.bg === select, `${ctx}: the folded header is not the mark (${JSON.stringify({ a, pane, select })})`);
    check(await evaluate(`document.querySelectorAll('.list [aria-selected="true"]').length === 0`), `${ctx}: a row is marked as well as the header`);
    // V0.184's narrowest list: the name, the count and some of the title still fit.
    await evaluate(`document.querySelector('.app').style.setProperty('--list-w', '280px')`);
    await settle();
    const narrow = await evaluate(HELD("settled"));
    check(fine(narrow, "t_set7"), `${ctx}: the marked header at the list's minimum width (${JSON.stringify(narrow)})`);
    await evaluate(`document.querySelector('.app').style.removeProperty('--list-w')`);
    await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
    await settle();
    await shot("folded-group-desktop.png");
    // 2. Clicking the marked header only toggles: the row takes the mark; again, the header has it back.
    await evaluate(`document.querySelector(${JSON.stringify(head("settled"))}).click()`);
    await waitFor(`${HELD("settled")}.rows > 0`, { soft: true });
    const b = await evaluate(`({ h: ${HELD("settled")}, row: document.querySelector('#sec-settled [aria-selected="true"]')?.dataset.id ?? null })`);
    check(!b.h.held && b.h.current === null && b.row === "t_set7", `${ctx}: unfolded, the row alone is the mark (${JSON.stringify(b)})`);
    await evaluate(`document.querySelector(${JSON.stringify(head("settled"))}).click()`);
    await waitFor(`${HELD("settled")}.held`, { soft: true });
    check(fine(await evaluate(HELD("settled")), "t_set7"), `${ctx}: folding it again did not mark the header`);
    // 3. k leaves for a drawn row, and unfolds nothing.
    await evaluate(`document.querySelector('.list').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))`);
    await key("k", "KeyK");
    await waitFor(`!${HELD("settled")}.held`, { soft: true });
    const c = await evaluate(`({ h: ${HELD("settled")}, snoozed: ${HELD("snoozed")}, marked: document.querySelectorAll('.list [aria-selected="true"]').length })`);
    check(!c.h.held && c.h.rows === 0 && c.snoozed.rows === 0 && c.marked === 1, `${ctx}: k from a folded section (${JSON.stringify(c)})`);
    // 4. The open thread moves into a folded section: the mark follows, the section stays folded.
    await navigate(`${origin}/?mock=rail`, "!!document.querySelector('#sec-seen [data-id]')");
    const seen = await evaluate(`(() => { const r = document.querySelector('#sec-seen [data-id]'); r.click(); return r.dataset.id; })()`);
    await waitFor(`!!document.querySelector('.th .tt')`, { soft: true });
    await settle();
    await key("e", "KeyE");
    await waitFor(`${HELD("settled")}.id === ${JSON.stringify(seen)}`, { soft: true });
    await settle();
    const d = await evaluate(HELD("settled"));
    check(d.id === seen && d.rows === 0 && d.held, `${ctx}: a thread moving into folded Settled (${JSON.stringify({ seen, d })})`);
  } else {
    // 1. Back from a thread in folded Settled: it stayed folded and its header is marked. The phone's 390 px is its minimum.
    await openScreen({ url: "?mock=rail&open=t_set7", ready: "!!document.querySelector('.m-thread')" }, 390, 844);
    await evaluate(`history.back()`);
    await waitFor(`!!document.querySelector('.m-list') && ${HELD("settled")}.held`, { soft: true });
    await evaluate(`document.querySelector(${JSON.stringify(head("settled"))})?.scrollIntoView({ block: 'center' })`);
    await settle();
    check(fine(await evaluate(HELD("settled")), "t_set7"), `${ctx}: the folded header is not the mark (${JSON.stringify(await evaluate(HELD("settled")))})`);
    await shot("folded-group-phone.png");
    // 2. A tap only toggles.
    await evaluate(`document.querySelector(${JSON.stringify(head("settled"))}).click()`);
    await waitFor(`${HELD("settled")}.rows > 0`, { soft: true });
    const b = await evaluate(HELD("settled"));
    check(!b.held && b.current === null && b.rows > 0, `${ctx}: unfolded, the header is still marked (${JSON.stringify(b)})`);
  }
  // Last on both: the owner's fold survives a reload (a new build reloads by itself), and so does an unfold.
  const url = `${origin}/?mock=rail`;
  if (phone) await navigate(url, "!!document.querySelector('#m-sec-working .m-mv')");
  await evaluate(`document.querySelector(${JSON.stringify(head("working"))}).click()`);
  await waitFor(`${HELD("working")}.rows === 0`, { soft: true });
  await settle();
  await navigate(url, `!!document.querySelector(${JSON.stringify(head("working"))})`);
  await settle();
  const e = await evaluate(`({ rows: ${HELD("working")}.rows, stored: localStorage.getItem('fyr.sectionOpen') })`);
  check(e.rows === 0 && JSON.parse(e.stored ?? "{}").working === false, `${ctx}: a folded section came back open after a reload (${JSON.stringify(e)})`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// SPEC 0.5 V0.138: a thread that opens clears the filters it fails, so its row is in the list and marked (V0.109).
// V0.169: per row. 1 with host-b off and acme/web picked, a link to t_lasso (host-b, acme/web) turns host-b back on,
// keeps the project and marks its row. 2 with both filters failing, both clear. 3 V0.114: a link to a quiet thread shows the gate and
// clears nothing.
for (const phone of [false, true]) section(`V0.138 ${phone ? "phone" : "desktop"}`, async (h) => {
  const { send, evaluate, waitFor, settle, navigate, openScreen } = h;
  const ctx = `V0.138 ${phone ? "phone" : "desktop"}`;
try {
  const open = async (url, storage, ready) => {
    if (phone) await openScreen({ url, storage: Object.fromEntries(Object.entries(storage).filter(([, v]) => v !== null)), ready }, 390, 844);
    else {
      await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
      await navigate(`${origin}/${url}`, "!!document.querySelector('.ib .list')");
      await evaluate(`(() => { const s = ${JSON.stringify(storage)}; for (const k in s) s[k] === null ? localStorage.removeItem(k) : localStorage.setItem(k, s[k]); return true; })()`);
      await navigate(`${origin}/${url}`, ready);
    }
    await settle();
  };
  const F = `({ off: localStorage.getItem('fyr.hostsOff'), project: localStorage.getItem('fyr.projectFilter') })`;
  const MARK = `({ ids: [...document.querySelectorAll('.list [aria-selected="true"]')].map((x) => x.dataset.id), pane: document.querySelector('.th .tt')?.getAttribute('title') ?? null })`;
  const threadReady = phone ? "!!document.querySelector('.m-thread, .qgate')" : "!!document.querySelector('.th .tt, .qgate')";
  // 1. Only the host filter fails.
  await open("?mock=rail&hosts=2&open=t_lasso", { "fyr.hostsOff": '["host-b"]', "fyr.projectFilter": "F7QK-2M9X-AB34-CD56/p_web" }, threadReady);
  await waitFor(`${F}.off === null`, { soft: true });
  const a = await evaluate(F);
  check(a.off === null && a.project === "F7QK-2M9X-AB34-CD56/p_web", `${ctx}: a link to a host-b thread with host-b off (${JSON.stringify(a)})`);
  if (!phone) {
    await waitFor(`${MARK}.ids[0] === 'M3RS-QX7P-5LKA-2WNE/t_lasso'`, { soft: true });
    const m = await evaluate(MARK);
    check(m.ids.length === 1 && m.ids[0] === "M3RS-QX7P-5LKA-2WNE/t_lasso" && m.pane === "Fix map lasso on mobile", `${ctx}: the opened thread's row is not marked (${JSON.stringify(m)})`);
    // Turning a host off while the thread is open is not undone.
    await evaluate(`document.querySelector('.ib .hfrow .hft[aria-label^="host-b,"]')?.click()`);
    await waitFor(`${F}.off === '["host-b"]'`, { soft: true });
    await settle();
    check((await evaluate(F)).off === '["host-b"]', `${ctx}: a host turned off while the thread is open is turned back on`);
  } else {
    // Back to the list: the thread's row is there.
    await evaluate(`history.back()`);
    await waitFor(`[...document.querySelectorAll('.m-list .m-mv')].some((r) => r.textContent.includes('Fix map lasso'))`, { soft: true });
    check(await evaluate(`[...document.querySelectorAll('.m-list .m-mv')].some((r) => r.textContent.includes('Fix map lasso'))`), `${ctx}: the opened thread's row is not in the list after Back`);
  }
  // 2. Both fail.
  await open("?mock=rail&hosts=2&open=t_lasso", { "fyr.hostsOff": '["host-b"]', "fyr.projectFilter": "F7QK-2M9X-AB34-CD56/p_home" }, threadReady);
  await waitFor(`${F}.off === null && ${F}.project === null`, { soft: true });
  const b = await evaluate(F);
  check(b.off === null && b.project === null, `${ctx}: both filters failing (${JSON.stringify(b)})`);
  if (!phone) {
    // 2b. A thread in the folded Settled section: the filter clears; V0.186: the section stays folded, its header is marked.
    await open("?mock=rail&hosts=2&open=t_set7", { "fyr.hostsOff": '["desk"]', "fyr.projectFilter": null }, "!!document.querySelector('.th .tt')");
    const HOLDS = `({ id: document.querySelector('#sec-settled .sec.held')?.dataset.holds ?? null, rows: document.querySelectorAll('#sec-settled [data-id]').length })`;
    await waitFor(`${HOLDS}.id === 'F7QK-2M9X-AB34-CD56/t_set7'`, { soft: true });
    const m = await evaluate(HOLDS);
    check(m.id === "F7QK-2M9X-AB34-CD56/t_set7" && m.rows === 0 && (await evaluate(F)).off === null, `${ctx}: a Settled thread opened with its host off (${JSON.stringify(m)})`);
  }
  // 3. A quiet thread: the gate, and the filter stays.
  await open("?mock=rail&hosts=2&quiet=p_home&open=t_dish", { "fyr.hostsOff": '["desk"]', "fyr.projectFilter": null, "fyr.quietShowUntil": null }, "true");
  await settle();
  const c = await evaluate(F);
  check(c.off === '["desk"]', `${ctx}: a link to a quiet thread turned its host on before the hold (${JSON.stringify(c)})`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// SPEC 0.5 V0.165: previews are usable at any size, the app around them is static. On desktop and phone: every
// preview opens full screen (the Fullscreen API where the browser has it, else an in-app layer) and leaves it by
// Escape, the Close button and Back (the phone's back gesture is a Back); an image zooms by pinch, wheel and double
// tap while the page's own scale stays 1; an HTML page and the live view fill the screen in place (the same frame,
// so the live view's bridge keeps working) with zoom steps; the chrome around them does not zoom, and an input's
// focus does not zoom it on a phone.
// The page's real scale, from the browser: the harness's window.visualViewport is scripted and always says 1.
const pageScale = async (send) => {
  const m = (await send("Page.getLayoutMetrics")).result;
  return Math.round((m.cssVisualViewport ?? m.visualViewport).scale * 1000) / 1000;
};
for (const [front, w, hh] of [["desktop", 1280, 800], ["phone", 390, 844]]) section(`V0.165 fullscreen ${front}`, async ({ send, evaluate, waitFor, settle, navigate, openScreen, key, shot }) => {
  const ctx = `V0.165 fullscreen ${front}`;
  const phone = front === "phone";
  // A real press (a user gesture, so the Fullscreen API may run): a tap on the phone, a mouse click on the desktop.
  // The point is taken once it holds still for two frames: entries under content-visibility: auto take their real
  // height as they draw, so the card can move just after a layer closes.
  const press = async (sel) => {
    const c = await evaluate(`new Promise((done) => { let last = '', n = 0, f = 0; const step = () => { const el = document.querySelector(${JSON.stringify(sel)}); if (!el) return done(null); el.scrollIntoView({ block: 'center' }); const r = el.getBoundingClientRect(); const p = [r.left + r.width / 2, r.top + r.height / 2]; const k = p.join(','); if ((k === last && ++n >= 2) || ++f > 120) return done(p); if (k !== last) n = 0; last = k; requestAnimationFrame(step); }; step(); })`);
    if (!c) throw new Error(`no element ${sel}`);
    if (phone) {
      await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: c[0], y: c[1] }] });
      await sleep(40);
      await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    } else {
      for (const type of ["mousePressed", "mouseReleased"]) await send("Input.dispatchMouseEvent", { type, x: c[0], y: c[1], button: "left", clickCount: 1 });
    }
  };
  const COVERS = (sel) => `(() => { const el = document.querySelector(${JSON.stringify(sel)}); if (!el) return false; const r = el.getBoundingClientRect(); return r.left <= 0.5 && r.top <= 0.5 && Math.abs(r.width - innerWidth) <= 1 && Math.abs(r.height - innerHeight) <= 1; })()`;
  const STATE = `({ url: location.search, fs: !!document.fullscreenElement, viewer: !!document.querySelector('.pv-full'), card: !!document.querySelector('.pv.isfs'), lv: !!document.querySelector('.lv.isfs') })`;
  const leave = async (how, closeSel) => {
    if (how === "Escape") await key("Escape");
    else if (how === "Close") await press(closeSel);
    else await evaluate(`history.back()`);
  };
try {
  const url = "?mock=1&open=t_w3";
  const ready = phone ? "!!document.querySelector('.m-app .pv.image')" : "!!document.querySelector('.th .pv.image')";
  if (phone) await openScreen({ url, ready }, w, hh);
  else {
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/${url}`, ready);
  }
  await shot(`v0165-thread-${front}.png`);
  const cards = await evaluate(`[...document.querySelectorAll('.pv')].map((c) => ({ kind: c.classList.contains('html') ? 'html' : 'image', fs: !!c.querySelector('.pv-fs') }))`);
  check(cards.length > 0 && cards.every((c) => c.fs), `${ctx}: a preview card has no Full screen button (${JSON.stringify(cards)})`);
  // 1. An image: the viewer covers the screen; each way out leaves the thread where it was.
  for (const how of ["Escape", "Close", "Back"]) {
    await press(".pv.image .pv-fs");
    await waitFor(COVERS(".pv-full"), { soft: true });
    const inn = await evaluate(`({ covers: ${COVERS(".pv-full")}, ...${STATE} })`);
    check(inn.covers, `${ctx}: the image viewer does not cover the screen (${JSON.stringify(inn)})`);
    if (!phone) check(inn.fs, `${ctx}: the image viewer did not use the Fullscreen API on the desktop (${JSON.stringify(inn)})`);
    if (how === "Escape") await shot(`v0165-image-full-${front}.png`);
    await leave(how, ".pv-full .pv-x");
    await waitFor(`!document.querySelector('.pv-full') && !document.fullscreenElement`, { soft: true });
    await settle();
    const out = await evaluate(STATE);
    check(!out.viewer && !out.fs && /open=t_w3/.test(out.url), `${ctx}: ${how} did not leave the image viewer for the thread (${JSON.stringify(out)})`);
  }
  // 2. An HTML preview fills the screen in place, with zoom steps that scale its page, and leaves by each way.
  for (const how of ["Escape", "Close", "Back"]) {
    await press(".pv.html .pv-fs");
    await waitFor(COVERS(".pv.isfs"), { soft: true });
    const inn = await evaluate(`({ covers: ${COVERS(".pv.isfs")}, frame: Math.round(document.querySelector('.pv.isfs iframe')?.getBoundingClientRect().height ?? 0), ...${STATE} })`);
    check(inn.covers && inn.frame > hh * 0.6, `${ctx}: the HTML preview does not fill the screen (${JSON.stringify(inn)})`);
    if (how === "Escape") {
      await press(".pv.isfs .fz-in");
      await waitFor(`/scale\\(1\\.25\\)/.test(document.querySelector('.pv.isfs iframe')?.style.transform ?? '')`, { soft: true });
      const z = { ...(await evaluate(`({ t: document.querySelector('.pv.isfs iframe')?.style.transform ?? '', label: document.querySelector('.pv.isfs .fz-reset')?.textContent ?? '' })`)), vv: await pageScale(send) };
      check(/scale\(1\.25\)/.test(z.t) && /125/.test(z.label) && z.vv === 1, `${ctx}: zoom in does not scale the HTML page alone (${JSON.stringify(z)})`);
      await shot(`v0165-html-full-${front}.png`);
    }
    await leave(how, ".pv.isfs .fz-x");
    await waitFor(`!document.querySelector('.pv.isfs') && !document.fullscreenElement`, { soft: true });
    await settle();
    const out = await evaluate(STATE);
    check(!out.card && !out.fs && /open=t_w3/.test(out.url), `${ctx}: ${how} did not leave the HTML preview's full screen (${JSON.stringify(out)})`);
  }
  // 3. Without the Fullscreen API (iPhone Safari, the iOS PWA): the in-app layer still covers the screen.
  await evaluate(`Object.defineProperty(Document.prototype, 'fullscreenEnabled', { configurable: true, get: () => false }); true`);
  await press(".pv.html .pv-fs");
  await waitFor(COVERS(".pv.isfs"), { soft: true });
  const noApi = await evaluate(`({ covers: ${COVERS(".pv.isfs")}, ...${STATE} })`);
  check(noApi.covers && !noApi.fs, `${ctx}: without the Fullscreen API the HTML preview does not cover the screen (${JSON.stringify(noApi)})`);
  await evaluate(`history.back()`);
  await waitFor(`!document.querySelector('.pv.isfs')`, { soft: true });
  await press(".pv.image .pv-fs");
  await waitFor(COVERS(".pv-full"), { soft: true });
  const noApiImg = await evaluate(`({ covers: ${COVERS(".pv-full")}, r: (() => { const r = document.querySelector('.pv-full')?.getBoundingClientRect(); return r ? [r.left, r.top, r.width, r.height] : null; })(), ...${STATE} })`);
  check(noApiImg.covers, `${ctx}: without the Fullscreen API the image viewer does not cover the screen (${JSON.stringify(noApiImg)})`);
  await key("Escape");
  await waitFor(`!document.querySelector('.pv-full')`, { soft: true });
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

for (const [front, w, hh] of [["desktop", 1280, 800], ["phone", 390, 844]]) section(`V0.165 image zoom ${front}`, async ({ send, evaluate, waitFor, settle, navigate, openScreen, key, tapCenter, shot }) => {
  const ctx = `V0.165 image zoom ${front}`;
  const phone = front === "phone";
  const SCALE = `Number(document.querySelector('.pv-stage')?.dataset.scale ?? 0)`;
  const IMG = `(() => { const s = document.querySelector('.pv-stage')?.getBoundingClientRect(); const i = document.querySelector('.pv-stage img')?.getBoundingClientRect(); return s && i ? { s: [s.left, s.top, s.right, s.bottom].map(Math.round), i: [i.left, i.top, i.right, i.bottom].map(Math.round), title: document.querySelector('.pv-ft')?.textContent ?? '', n: document.querySelector('.pv-fn')?.textContent ?? '' } : null; })()`;
  const center = async () => evaluate(`(() => { const r = document.querySelector('.pv-stage').getBoundingClientRect(); return [Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2)]; })()`);
try {
  const url = "?mock=1&open=t_w3";
  const ready = phone ? "!!document.querySelector('.m-app .pv.image')" : "!!document.querySelector('.th .pv.image')";
  if (phone) await openScreen({ url, ready }, w, hh);
  else {
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/${url}`, ready);
  }
  // The tall full-page image (390 × 2600) opens fitted: all of it inside the stage.
  await evaluate(`[...document.querySelectorAll('.pv.image')].find((c) => (c.querySelector('img')?.getAttribute('height') ?? '') === '2600')?.querySelector('.pv-img')?.click()`);
  await waitFor(`${SCALE} === 1 && (document.querySelector('.pv-stage img')?.complete ?? false)`, { soft: true });
  await settle();
  const fit = await evaluate(IMG);
  check(!!fit && fit.i[0] >= fit.s[0] && fit.i[1] >= fit.s[1] && fit.i[2] <= fit.s[2] && fit.i[3] <= fit.s[3], `${ctx}: the tall image is cut off at fit (${JSON.stringify(fit)})`);
  // Arrow keys and swipes move between the thread's images.
  await key("ArrowLeft");
  await waitFor(`${IMG}?.title !== ${JSON.stringify(fit?.title ?? "")}`, { soft: true });
  const prev = await evaluate(IMG);
  check(!!prev && prev.title !== fit?.title, `${ctx}: ArrowLeft did not move to the previous image (${JSON.stringify([fit?.n, prev?.n])})`);
  const [cx, cy] = await center();
  if (phone) {
    await send("Input.synthesizePinchGesture", { x: cx, y: cy, scaleFactor: 2.2, relativeSpeed: 600, gestureSourceType: "touch" });
    await waitFor(`${SCALE} > 1.3`, { soft: true });
    const r = { scale: await evaluate(SCALE), page: await pageScale(send) };
    check(r.scale > 1.3 && r.page === 1, `${ctx}: a pinch on the image did not zoom it alone (${JSON.stringify(r)})`);
    // A double tap: two taps within 300 ms of the page's clock. On a busy runner the CDP calls can land further apart
    // than that, so the gesture is tried again while the page saw its taps too far apart (it records each gap).
    await evaluate(`(() => { window.__taps = []; document.querySelector('.pv-stage').addEventListener('pointerup', (e) => __taps.push(e.timeStamp)); return true; })()`);
    const doubleTapUntil = async (until) => {
      let gap = 0;
      for (let attempt = 0; attempt < 5; attempt++) {
        await evaluate(`(__taps.length = 0, true)`);
        for (let i = 0; i < 2; i++) {
          await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: cx, y: cy }] });
          await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
        }
        const ok = await waitFor(until, { soft: true, timeout: 1500 });
        if (ok) return { ok: true };
        gap = await evaluate(`__taps.length === 2 ? Math.round(__taps[1] - __taps[0]) : -1`);
        if (gap >= 0 && gap < 300) return { ok: false, gap }; // the page saw a real double tap and did nothing
        await sleep(400); // let the lone tap expire before the next try
      }
      return { ok: false, gap };
    };
    const back = await doubleTapUntil(`${SCALE} === 1`);
    check(back.ok, `${ctx}: a double tap on a zoomed image does not go back to fit (${JSON.stringify(back)})`);
    // ...and a double tap at fit zooms in.
    const zin = await doubleTapUntil(`${SCALE} > 1`);
    const dt = { ...zin, scale: await evaluate(SCALE), page: await pageScale(send) };
    check(dt.ok && dt.scale > 1 && dt.page === 1, `${ctx}: a double tap at fit does not zoom the image (${JSON.stringify(dt)})`);
    await shot(`v0165-image-zoomed-${front}.png`);
  } else {
    await send("Input.dispatchMouseEvent", { type: "mouseWheel", x: cx, y: cy, deltaX: 0, deltaY: -400 });
    await waitFor(`${SCALE} > 1.3`, { soft: true });
    const a = { scale: await evaluate(SCALE), page: await pageScale(send), t: await evaluate(`document.querySelector('.pv-stage img')?.style.transform`) };
    check(a.scale > 1.3 && a.page === 1, `${ctx}: the wheel did not zoom the image (${JSON.stringify(a)})`);
    // A drag pans the zoomed image.
    const before = await evaluate(IMG);
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x: cx, y: cy, button: "left", buttons: 1, clickCount: 1 });
    for (let i = 1; i <= 5; i++) await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: cx + i * 20, y: cy, button: "left", buttons: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: cx + 100, y: cy, button: "left", buttons: 0, clickCount: 1 });
    await settle();
    const after = await evaluate(IMG);
    check(after.i[0] > before.i[0], `${ctx}: a drag does not pan the zoomed image (${JSON.stringify([before.i, after.i])})`);
    // A double click goes back to fit, and again to 100 %.
    for (const n of [1, 2]) await send("Input.dispatchMouseEvent", { type: "mousePressed", x: cx, y: cy, button: "left", clickCount: n }), await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: cx, y: cy, button: "left", clickCount: n });
    await waitFor(`${SCALE} === 1`, { soft: true });
    check((await evaluate(SCALE)) === 1, `${ctx}: a double click on a zoomed image does not go back to fit`);
    await send("Input.dispatchMouseEvent", { type: "mouseWheel", x: cx, y: cy, deltaX: 0, deltaY: -200, modifiers: 2 });
    await waitFor(`${SCALE} > 1`, { soft: true });
    check((await evaluate(SCALE)) > 1, `${ctx}: a trackpad pinch (ctrl+wheel) does not zoom the image`);
    await shot(`v0165-image-zoomed-${front}.png`);
  }
  await key("Escape");
  await waitFor(`!document.querySelector('.pv-full')`, { soft: true });
  // An attachment the owner sent opens in the same viewer (fx_img in t_ask).
  // On the phone the thread opens on its question; Read all shows the conversation under it.
  if (phone) {
    await openScreen({ url: "?mock=1&open=t_ask", ready: "!!document.querySelector('.m-app')" }, w, hh);
    await evaluate(`document.querySelector('.m-ah [aria-label="Show the conversation"]')?.click()`);
    await evaluate(`document.querySelector('.att-sent')?.scrollIntoView({ block: 'center' })`);
    await waitFor(`!!document.querySelector('.att-img')`, { what: "the sent image on the phone" });
  } else await navigate(`${origin}/?mock=1&open=t_ask`, "!!document.querySelector('.att-img')");
  await evaluate(`document.querySelector('.att-img')?.click()`);
  await waitFor(`!!document.querySelector('.pv-full .pv-stage img')`, { soft: true });
  check(await evaluate(`!!document.querySelector('.pv-full .pv-stage img')`), `${ctx}: a sent image does not open in the zooming viewer`);
  const pdf = await evaluate(`[...document.querySelectorAll('.att-file')].map((a) => ({ n: a.textContent, href: a.getAttribute('href') ?? '', t: a.getAttribute('target') }))`);
  check(pdf.some((p) => /spec\.pdf/.test(p.n) && p.href && p.t === "_blank"), `${ctx}: a sent PDF has no Open link (${JSON.stringify(pdf)})`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

for (const [front, w, hh] of [["desktop", 1280, 800], ["phone", 390, 844]]) section(`V0.165 live view ${front}`, async ({ send, evaluate, waitFor, settle, navigate, openScreen, key, shot }) => {
  const ctx = `V0.165 live view ${front}`;
  const phone = front === "phone";
  const COVERS = `(() => { const el = document.querySelector('.lv.isfs'); if (!el) return false; const r = el.getBoundingClientRect(); return r.left <= 0.5 && r.top <= 0.5 && Math.abs(r.width - innerWidth) <= 1 && Math.abs(r.height - innerHeight) <= 1; })()`;
try {
  const url = "?mock=1&open=t_view";
  if (phone) await openScreen({ url, ready: "!!document.querySelector('.lv-f')" }, w, hh);
  else {
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: hh, deviceScaleFactor: 1, mobile: false });
    await navigate(`${origin}/${url}`, "!!document.querySelector('.lv-f')");
  }
  await evaluate(`window.__lvf = document.querySelector('.lv-f'); true`);
  await evaluate(`document.querySelector('.lv .pv-fs')?.click()`);
  await waitFor(COVERS, { soft: true });
  const r = await evaluate(`({ covers: ${COVERS}, same: document.querySelector('.lv.isfs .lv-f') === window.__lvf, h: Math.round(document.querySelector('.lv.isfs .lv-f')?.getBoundingClientRect().height ?? 0) })`);
  check(r.covers && r.same && r.h > hh * 0.6, `${ctx}: the live view does not fill the screen with its own frame (${JSON.stringify(r)})`);
  await shot(`v0165-live-full-${front}.png`);
  await key("Escape");
  await waitFor(`!document.querySelector('.lv.isfs')`, { soft: true });
  const out = await evaluate(`({ fs: !!document.querySelector('.lv.isfs'), same: document.querySelector('.lv-f') === window.__lvf, url: location.search })`);
  check(!out.fs && out.same && /open=t_view/.test(out.url), `${ctx}: Escape did not bring the live view back in place (${JSON.stringify(out)})`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// The app's chrome is static on a phone: a pinch or a double tap on it does not zoom the page, and focusing an
// input does not either (every field is at least 16 px, iOS's threshold).
section(`V0.165 static chrome phone`, async ({ send, evaluate, waitFor, settle, openScreen }) => {
  const ctx = `V0.165 static chrome phone`;
try {
  await openScreen({ url: "?mock=1&open=t_w3", ready: "!!document.querySelector('.m-app textarea')" }, 390, 844);
  const meta = await evaluate(`document.querySelector('meta[name=viewport]')?.content ?? ''`);
  check(/maximum-scale=1/.test(meta) && /user-scalable=no/.test(meta), `${ctx}: the viewport lets the page zoom (${meta})`);
  // A touch pinch over the chrome does not zoom headless Chrome's page even without the fix; a mouse-source pinch
  // (the browser's own pinch-zoom path) does, so both are tried.
  for (const src of ["touch", "mouse"]) {
    await send("Input.synthesizePinchGesture", { x: 195, y: 300, scaleFactor: 2, relativeSpeed: 600, gestureSourceType: src });
    await settle();
    check((await pageScale(send)) === 1, `${ctx}: a ${src} pinch on the conversation zoomed the page`);
  }
  for (let i = 0; i < 2; i++) {
    await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: 195, y: 300 }] });
    await sleep(30);
    await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    await sleep(60);
  }
  await settle();
  check((await pageScale(send)) === 1, `${ctx}: a double tap on the conversation zoomed the page`);
  const small = (sel) => evaluate(`[...document.querySelectorAll(${JSON.stringify(sel)})].filter((e) => e.getClientRects().length && !['checkbox', 'radio', 'range', 'file', 'hidden', 'color'].includes(e.type)).map((e) => ({ c: e.className || e.tagName, px: parseFloat(getComputedStyle(e).fontSize) })).filter((e) => e.px < 16)`);
  const s1 = await small("input, textarea, select");
  check(s1.length === 0, `${ctx}: fields under 16 px in a thread (iOS zooms on their focus) ${JSON.stringify(s1)}`);
  await evaluate(`document.querySelector('.m-app textarea')?.focus()`);
  await settle();
  check((await pageScale(send)) === 1, `${ctx}: focusing the composer zoomed the page`);
  // The confirmation sheet's reason field was 15 px.
  await openScreen({ url: "?mock=rail&open=t_triage", ready: "!!document.querySelector('.m-app')" }, 390, 844);
  const s2 = await small("input, textarea, select");
  check(s2.length === 0, `${ctx}: fields under 16 px ${JSON.stringify(s2)}`);
  const css = await evaluate(`(() => { for (const sh of document.styleSheets) { try { for (const r of sh.cssRules) if (r.media && /coarse/.test(r.media.mediaText) && /font-size/.test(r.cssText) && /input/.test(r.cssText)) return true; } catch {} } return false; })()`);
  check(css, `${ctx}: no rule keeps every field at 16 px or more on a touch screen`);
} catch (e) {
  failures.push(`${ctx}: ${e.message}`);
}
});

// SPEC V0.184: the thread list's width. The handle on its edge drags (mouse and touch), takes the keyboard, resets on
// a double press; the width is this browser's, and the window limits what is applied.
const LRZ = ".ib .lrz";
const lrzState = `(() => { const h = document.querySelector(${JSON.stringify(LRZ)}), ib = document.querySelector('.ib').getBoundingClientRect(), th = document.querySelector('.th').getBoundingClientRect();
  return { w: Math.round(ib.width), th: Math.round(th.width), now: Number(h?.getAttribute('aria-valuenow')), min: Number(h?.getAttribute('aria-valuemin')), max: Number(h?.getAttribute('aria-valuemax')), stored: localStorage.getItem('fyr.listWidth') }; })()`;
const lrzCenter = (evaluate, y) => evaluate(`(() => { const r = document.querySelector(${JSON.stringify(LRZ)}).getBoundingClientRect(); return [r.left + r.width / 2, ${y ?? "r.top + r.height / 2"}]; })()`);
section("V0.184 desktop", async ({ send, evaluate, waitFor, settle, navigate, key, shot }) => {
  const ctx = "V0.184 desktop";
  const size = async (width, height = 800) => {
    await send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false });
    await waitFor(`innerWidth === ${width} && innerHeight === ${height}`, { what: `a ${width} x ${height} window` });
    await settle();
  };
  const mouse = (type, x, y, extra = {}) => send("Input.dispatchMouseEvent", { type, x, y, ...extra });
  const press = (x, y, clickCount = 1) => mouse("mousePressed", x, y, { button: "left", buttons: 1, clickCount });
  const release = (x, y, clickCount = 1) => mouse("mouseReleased", x, y, { button: "left", buttons: 0, clickCount });
  const drag = (x, y) => mouse("mouseMoved", x, y, { button: "left", buttons: 1 });
  const click = async (clickCount = 1) => {
    const [x, y] = await lrzCenter(evaluate);
    await mouse("mouseMoved", x, y);
    await press(x, y, clickCount);
    await release(x, y, clickCount);
  };
  // The handle counts two presses within 400 ms: the events go out together, so a busy machine cannot stretch the gap.
  const dblclick = async () => {
    const [x, y] = await lrzCenter(evaluate);
    await mouse("mouseMoved", x, y);
    await Promise.all([press(x, y, 1), release(x, y, 1), press(x, y, 2), release(x, y, 2)]);
  };
  const widthIs = (w) => waitFor(`Math.round(document.querySelector('.ib').getBoundingClientRect().width) === ${w}`, { soft: true, timeout: 2000 });
  const focusHandle = () => evaluate(`document.querySelector(${JSON.stringify(LRZ)}).focus()`);
  try {
    // 1. The default, and what the handle says it is.
    await navigate(`${origin}/?mock=1&open=t_w3`, "!!document.querySelector('.th .pv.html iframe') && !!document.querySelector('.ib')");
    const a = await evaluate(`(() => { const h = document.querySelector(${JSON.stringify(LRZ)}); return h && { role: h.getAttribute('role'), o: h.getAttribute('aria-orientation'), tab: h.tabIndex, label: h.getAttribute('aria-label') }; })()`);
    check(a?.role === "separator" && a.o === "vertical" && a.tab === 0 && !!a.label, `${ctx}: the handle is not a focusable vertical separator (${JSON.stringify(a)})`);
    const s0 = await evaluate(lrzState);
    check(s0.w === 372 && s0.now === 372 && s0.min === 280 && s0.max === 640 && s0.stored === null, `${ctx}: the default is wrong (${JSON.stringify(s0)})`);

    // 2. A drag is live, past the iframe that previews are, with nothing selected; the width is kept at the release.
    await evaluate(`document.querySelector('.th .pv.html iframe').scrollIntoView({ block: 'center' })`);
    await settle();
    const fy = await evaluate(`(() => { const r = document.querySelector('.th .pv.html iframe').getBoundingClientRect(); return Math.round(r.top + r.height / 2); })()`);
    const [hx, hy] = await lrzCenter(evaluate, fy);
    await mouse("mouseMoved", hx, hy);
    await press(hx, hy);
    await drag(hx + 100, hy);
    check(await widthIs(472), `${ctx}: the list does not follow the drag (${JSON.stringify(await evaluate(lrzState))})`);
    check((await evaluate(lrzState)).now === 472, `${ctx}: aria-valuenow does not follow the drag`);
    await drag(1100, hy); // past the maximum: the pointer is now over the preview
    check(await widthIs(640), `${ctx}: a drag past the maximum is not stopped there (${JSON.stringify(await evaluate(lrzState))})`);
    const over = await evaluate(`(() => { const f = document.querySelector('.th .pv.html iframe'), r = f.getBoundingClientRect(), cs = getComputedStyle(document.body);
      return { inside: r.left <= 1100 && r.right >= 1100 && r.top <= ${hy} && r.bottom >= ${hy}, hit: document.elementFromPoint(1100, ${hy})?.tagName, pe: getComputedStyle(f).pointerEvents, select: cs.userSelect, cursor: cs.cursor, text: String(getSelection()) }; })()`);
    check(over.inside && over.hit !== "IFRAME" && over.pe === "none", `${ctx}: an iframe can take the pointer during a drag (${JSON.stringify(over)})`);
    check(over.select === "none" && over.text === "" && over.cursor === "col-resize", `${ctx}: a drag can select text or loses its cursor (${JSON.stringify(over)})`);
    await drag(hx + 100, hy);
    check(await widthIs(472), `${ctx}: the drag was lost over the iframe (${JSON.stringify(await evaluate(lrzState))})`);
    await release(hx + 100, hy);
    await settle();
    const s1 = await evaluate(lrzState);
    check(s1.w === 472 && s1.stored === "472" && s1.th === 1280 - 76 - 472, `${ctx}: the released width is not kept (${JSON.stringify(s1)})`);
    const after = await evaluate(`({ pe: getComputedStyle(document.querySelector('.th .pv.html iframe')).pointerEvents, select: getComputedStyle(document.body).userSelect, cls: document.documentElement.classList.contains('list-resizing') })`);
    check(after.pe !== "none" && after.select !== "none" && !after.cls, `${ctx}: the page stays in drag mode after the release (${JSON.stringify(after)})`);

    // 3. A reload keeps it.
    await navigate(`${origin}/?mock=1&open=t_w3`, "!!document.querySelector('.ib .lrz')");
    check((await evaluate(lrzState)).w === 472, `${ctx}: a reload loses the width (${JSON.stringify(await evaluate(lrzState))})`);

    // 4. The window limits what is applied; the stored width is not changed by it. The list is asked for (fyr.list):
    // a window under 1100 px starts without it (V0.40).
    await evaluate(`localStorage.setItem('fyr.listWidth', '600'); localStorage.setItem('fyr.list', '1')`);
    await navigate(`${origin}/?mock=1`, "!!document.querySelector('.ib .lrz') && !!document.querySelector('.th')");
    check((await evaluate(lrzState)).w === 600, `${ctx}: the stored 600 is not applied`);
    await size(1000);
    const s2 = await evaluate(lrzState);
    check(s2.w === 444 && s2.now === 444 && s2.max === 444 && s2.th === 480 && s2.stored === "600", `${ctx}: a 1000 px window does not clamp the width (${JSON.stringify(s2)})`);
    await click(); // a press that does not move stores nothing
    await settle();
    check((await evaluate(lrzState)).stored === "600", `${ctx}: a press without a drag changed the stored width`);
    await size(800);
    const s3 = await evaluate(lrzState);
    check(s3.w === 280 && s3.max === 280 && s3.th === 800 - 76 - 280, `${ctx}: at 800 px the list is not at its minimum (${JSON.stringify(s3)})`);
    await size(1280);
    check(await widthIs(600), `${ctx}: a wider window does not bring the stored width back`);
    await evaluate(`localStorage.setItem('fyr.listWidth', 'wide')`);
    await navigate(`${origin}/?mock=1`, "!!document.querySelector('.ib .lrz')");
    check((await evaluate(lrzState)).w === 372, `${ctx}: a broken stored width is applied`);

    // 5. Keys. They stay the handle's: the app's own shortcuts do not see them.
    await focusHandle();
    await key("ArrowRight", "ArrowRight");
    check((await evaluate(lrzState)).w === 388, `${ctx}: ArrowRight does not widen by 16`);
    await key("ArrowLeft", "ArrowLeft", 8);
    check((await evaluate(lrzState)).w === 324, `${ctx}: Shift+ArrowLeft does not narrow by 64`);
    await key("End", "End");
    check((await evaluate(lrzState)).w === 640, `${ctx}: End does not go to the maximum`);
    await key("Home", "Home");
    const s4 = await evaluate(lrzState);
    check(s4.w === 280 && s4.now === 280 && s4.stored === "280", `${ctx}: Home does not go to the minimum and stay (${JSON.stringify(s4)})`);
    check(await evaluate(`document.activeElement === document.querySelector(${JSON.stringify(LRZ)})`), `${ctx}: the handle lost the focus`);
    await key("Enter", "Enter");
    const s5 = await evaluate(lrzState);
    check(s5.w === 372 && s5.stored === null, `${ctx}: Enter does not reset (${JSON.stringify(s5)})`);

    // 6. A double click resets, and leaves nothing stored.
    await evaluate(`localStorage.setItem('fyr.listWidth', '520')`);
    await navigate(`${origin}/?mock=1`, "!!document.querySelector('.ib .lrz')");
    await dblclick();
    check(await widthIs(372), `${ctx}: a double click does not reset (${JSON.stringify(await evaluate(lrzState))})`);
    check((await evaluate(lrzState)).stored === null, `${ctx}: a reset leaves a stored width`);

    // 7. The list and the rail hold at both limits, in a tall and a short window, with two hosts (the most they carry).
    await navigate(`${origin}/?mock=rail&hosts=2`, "!!document.querySelector('.ib .lrz') && !!document.querySelector('.ib .hfrow') && !!document.querySelector('.ib .list .hd')");
    await shot("list-resize-default.png");
    for (const [k, want] of [["Home", 280], ["End", 640]]) for (const h of [800, 600]) {
      const at = `${ctx} at ${want} px, ${h} px high`;
      await size(1280, h);
      await focusHandle();
      await key(k, k);
      const m = await evaluate(`(() => { const box = (e) => e.getBoundingClientRect(), ib = box(document.querySelector('.ib')), de = document.documentElement, app = document.querySelector('.app'), rail = document.querySelector('.rail'), r = box(rail);
        const inside = (e) => { const b = box(e); return b.left >= ib.left - 0.5 && b.right <= ib.right + 0.5; };
        const list = document.querySelector('.ib .list'), hf = document.querySelector('.ib .hfrow'), pf = document.querySelector('.ib .pfrow');
        const rows = [...document.querySelectorAll('.ib .list [data-id]')];
        return { w: Math.round(ib.width), list: list.scrollWidth - list.clientWidth, hf: hf.scrollWidth - hf.clientWidth, pf: pf.scrollWidth - pf.clientWidth,
          top: [...document.querySelectorAll('.ib .top > *')].filter((e) => e.getClientRects().length && !inside(e)).length, chips: [...document.querySelectorAll('.ib .pfc, .ib .hft')].filter((e) => !inside(e)).length,
          rows: rows.length, wideRows: rows.filter((e) => !inside(e)).length, titles: [...document.querySelectorAll('.ib .list .hd .t')].filter((e) => box(e).right > box(e.closest('[data-id]')).right + 0.5 || getComputedStyle(e).overflow !== 'hidden').length,
          ages: rows.filter((e) => { const a = e.querySelector('.age'); return a && box(a).right > box(e).right + 0.5; }).length,
          pageX: de.scrollWidth - innerWidth, pageY: de.scrollHeight - innerHeight, sx: scrollX, sy: scrollY, appX: app.scrollWidth - app.clientWidth, appY: app.scrollHeight - app.clientHeight,
          railW: Math.round(r.width), railTop: Math.round(r.top), railBottom: Math.round(r.bottom), railX: rail.scrollWidth - rail.clientWidth, th: Math.round(box(document.querySelector('.th')).width) }; })()`);
      check(m.w === want, `${at}: the list is ${m.w} px`);
      check(m.list <= 0 && m.top === 0 && m.rows > 0 && m.wideRows === 0 && m.titles === 0 && m.ages === 0, `${at}: the list's rows do not fit (${JSON.stringify(m)})`);
      check(m.hf <= 0 && m.pf <= 0 && m.chips === 0, `${at}: a filter is cut or must be scrolled sideways (${JSON.stringify(m)})`);
      // The left-menu glitch of V0.141: the rail ran past the window and the whole page scrolled.
      check(m.pageX <= 0 && m.pageY <= 0 && m.sx === 0 && m.sy === 0 && m.appX <= 0 && m.appY <= 0, `${at}: the page overflows (${JSON.stringify(m)})`);
      check(m.railW === 76 && m.railTop === 0 && m.railBottom === h && m.railX <= 0, `${at}: the rail does not fit (${JSON.stringify(m)})`);
      check(m.th === 1280 - 76 - want, `${at}: the conversation is ${m.th} px`);
      const out = await evaluate(OUTSIDE);
      check(out.length === 0, `${at}: text outside its box ${JSON.stringify(out)}`);
      if (want === 280 && h === 800) await shot("list-resize-min.png");
    }

    // 8. A hidden list has no handle to reach.
    await size(1280);
    await evaluate(`localStorage.setItem('fyr.list', '0')`);
    await navigate(`${origin}/?mock=rail&open=t_triage`, "!!document.querySelector('.app.nolist')");
    check(await evaluate(`(() => { const h = document.querySelector(${JSON.stringify(LRZ)}); return !h || !h.getClientRects().length; })()`), `${ctx}: a hidden list leaves its handle on screen`);
    await evaluate(`localStorage.removeItem('fyr.list'); localStorage.removeItem('fyr.listWidth')`);

    // 9. A touch screen at desktop width: the same handle, by finger, with a larger target.
    await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
    await navigate(`${origin}/?mock=1`, "!!document.querySelector('.ib .lrz')");
    const [tx, ty] = await lrzCenter(evaluate);
    await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: tx, y: ty }] });
    await send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x: tx + 40, y: ty }] });
    await send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x: tx + 60, y: ty }] });
    check(await widthIs(432), `${ctx}: a finger does not drag the handle (${JSON.stringify(await evaluate(lrzState))})`);
    await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    await waitFor(`localStorage.getItem('fyr.listWidth') === '432'`, { soft: true, timeout: 2000 });
    check((await evaluate(lrzState)).stored === "432", `${ctx}: a finger's drag is not kept`);
    const [t2x, t2y] = await lrzCenter(evaluate);
    const tap = [{ type: "touchStart", touchPoints: [{ x: t2x, y: t2y }] }, { type: "touchEnd", touchPoints: [] }];
    await Promise.all([...tap, ...tap].map((ev) => send("Input.dispatchTouchEvent", ev))); // together, as the double click
    check(await widthIs(372), `${ctx}: a double tap does not reset (${JSON.stringify(await evaluate(lrzState))})`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
});
// The phone shows the list full screen, beside nothing: no handle, and a stored width changes nothing there.
section("V0.184 phone", async ({ evaluate, openScreen }) => {
  const ctx = "V0.184 phone";
  try {
    await openScreen({ url: "?mock=rail", ready: "!!document.querySelector('.m-list .hd')", storage: { "fyr.listWidth": "600" } }, 390, 844);
    const r = await evaluate(`({ handle: document.querySelectorAll('.lrz, [role=separator]').length, wide: document.documentElement.scrollWidth - innerWidth, list: Math.round(document.querySelector('.m-list').getBoundingClientRect().width) })`);
    check(r.handle === 0 && r.wide <= 0 && r.list === 390, `${ctx}: the phone's list changed (${JSON.stringify(r)})`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
});

// The phone screens after the sections: the sections are the longest jobs, so they start first and the short
// screens fill the tabs at the end.
for (const s of SCREENS) for (const [w, hh] of SIZES) jobs.push({ name: `${s.name} ${w}x${hh}`, run: (t) => phonePass(t, w, hh, [s]) });
// FYR_SECTIONS=<regex> runs only the jobs whose name matches (a quick local run of one section; CI runs them all).
if (process.env.FYR_SECTIONS) { const re = new RegExp(process.env.FYR_SECTIONS); for (const l of [jobs, timedJobs]) l.splice(0, l.length, ...l.filter((j) => re.test(j.name))); }
const started = Date.now();
await runJobs(jobs, TABS);
const poolDone = Date.now();
await runJobs(timedJobs, 1);
clearTimeout(watchdog);
cleanup();
console.log(`time: ${jobs.length} jobs in ${TABS} tabs ${((poolDone - started) / 1000).toFixed(1)} s, then ${timedJobs.length} timed sections alone ${((Date.now() - poolDone) / 1000).toFixed(1)} s`);
if (process.env.FYR_LAYOUT_PROFILE) for (const [ms, name] of jobTimes.sort((a, b) => b[0] - a[0]).slice(0, 15)) console.log(`  ${(ms / 1000).toFixed(1)} s  ${name}`);
if (pageErrors.length) failures.push(...pageErrors.map((e) => `page error: ${e}`));
if (failures.length) {
  console.error(`layout test: ${failures.length} of ${checks} checks failed`);
  for (const f of failures.slice(0, 60)) console.error("  ✖ " + f);
  if (failures.length > 60) console.error(`  … ${failures.length - 60} more`);
  process.exit(1);
}
console.log(`layout test: ${checks} checks passed (${SCREENS.length} screens × ${SIZES.length} sizes)${timingSkipped ? `; ${timingSkipped} timing budgets skipped (FYR_LAYOUT_TIMING=0)` : ""}`);
process.exit(0);

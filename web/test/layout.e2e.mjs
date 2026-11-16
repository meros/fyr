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
  let file = join(dist, path === "/" ? "index.html" : path);
  if (!file.startsWith(dist) || !existsSync(file) || statSync(file).isDirectory()) file = join(dist, "index.html");
  res.writeHead(200, { "Content-Type": TYPES[extname(file)] ?? "application/octet-stream", "Cache-Control": "no-store" });
  res.end(readFileSync(file));
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = `http://127.0.0.1:${server.address().port}`;

const chromeBin = process.env.FYR_CHROME || "google-chrome";
const profile = mkdtempSync(join(tmpdir(), "fyr-layout-"));
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
let version;
for (let i = 0; i < 100; i++) {
  try {
    version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
    if (version.webSocketDebuggerUrl) break;
  } catch {}
  await sleep(100);
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
  { name: "quiet project through the filter", url: "?mock=rail&quiet=p_home", storage: { "fyr.projectFilter": "p_home" } },
  { name: "project screen with quiet hours", url: "?mock=rail&project=p_home&quiet=p_home", storage: { "fyr.projectFilter": "" } },
  // SPEC 0.5 V0.45: the handover sheet from the thread's ⋯; its buttons stay above the bottom inset.
  { name: "handover sheet", url: "?mock=rail&open=t_triage", sheet: "handover" },
  // SPEC 0.5 V0.50: Settings → Providers from the phone menu: the providers, Found on this host with Import, Add
  // provider; every target 44 px and inside the safe areas.
  { name: "settings providers", url: "?mock=rail&settings" },
  { name: "board of changes", url: "?mock=1&changes" },
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
    otherProjectRows: [...document.querySelectorAll('.m-list .ptag .ptn')].filter(e => e.textContent !== 'acme/web').length,
    saveButton: rect('.pedit .pformb .btn.p'),
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
      const el = document.querySelector('.m-settings .set'); if (!el) return null;
      const targets = [...el.querySelectorAll('button, select, input, a.sbtn')].filter(e => e.getBoundingClientRect().height > 0);
      const small = targets.map(e => ({ t: (e.textContent || e.getAttribute('aria-label') || e.tagName).trim().slice(0, 30), h: e.getBoundingClientRect().height })).filter(x => x.h < 43.5);
      const lg = el.querySelector('.login');
      const login = lg ? { link: lg.querySelector('a.loginlink')?.getAttribute('href') ?? '', code: !!lg.querySelector('input'), cancel: [...lg.querySelectorAll('button')].some(b => b.textContent.trim() === 'Cancel') } : null;
      const add = el.querySelector('.addp'); add?.scrollIntoView({ block: 'center' }); // V0.58: Projects follow Providers, so Add provider is no longer at the end
      const ar = add ? add.getBoundingClientRect() : null;
      return { rows: el.querySelectorAll('.prov:not(.found)').length, found: el.querySelectorAll('.prov.found').length,
        imports: [...el.querySelectorAll('.prov.found button')].filter(b => b.textContent.trim() === 'Import').length,
        needs: [...el.querySelectorAll('.pst')].map(e => e.textContent), small, login,
        right: Math.max(0, ...[...el.querySelectorAll('*')].map(e => e.getBoundingClientRect().right)),
        add: ar ? { top: ar.top, bottom: ar.bottom } : null };
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
      const others = [...el.querySelectorAll('.qrm, .qtog, .qadd button')].map(box);
      const all = [...days, ...times, ...others];
      return { parts, right: Math.max(r.right, ...all.map(d => d.right)), rows: el.querySelectorAll('.qwin').length,
        add: [...el.querySelectorAll('.qadd button')].map(b => b.textContent.replace(/\\s+/g, ' ').trim()),
        line: el.querySelector('.lede')?.textContent ?? '', days: days.length, times: times.length, small: all.filter(d => d.height < 43.5).length };
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
  if (ctx.startsWith("settings providers")) {
    const st = m.settings;
    check(!!st && st.rows === 3 && st.found >= 1 && st.imports === st.found && st.needs.includes("Needs a login"), `${ctx}: the Providers section is wrong ${JSON.stringify(st)}`);
    if (st) {
      check(st.small.length === 0, `${ctx}: targets under 44 px ${JSON.stringify(st.small)}`);
      // SPEC 0.5 V0.53: a running login shows its sign-in link, the code field and Cancel, all 44 px.
      check(!!st.login && /^https:\/\/claude\.com\//.test(st.login.link) && st.login.code && st.login.cancel, `${ctx}: the login panel is wrong ${JSON.stringify(st.login)}`);
      check(st.right <= m.innerWidth + 0.5, `${ctx}: Settings runs ${(st.right - m.innerWidth).toFixed(1)} px past the right edge`);
      if (st.add) check(st.add.top >= m.top + insetTop - 0.5 && st.add.bottom <= m.bottom - insetBottom + 0.5, `${ctx}: Add provider (${st.add.top.toFixed(1)}–${st.add.bottom.toFixed(1)}) is outside the safe areas`);
    }
  }
  if (ctx.startsWith("project picker")) check(m.pickerRows[0] === "New project from this thread" && m.pickerRows[1] === "No project", `${ctx}: the picker's first rows are ${JSON.stringify(m.pickerRows)}`);
  if (ctx.startsWith("new project step")) check(m.stepName === "lasso-app" && m.stepSources === 1 && m.stepFolder === "~/src/lasso-app", `${ctx}: the step is not prefilled from the thread (name ${m.stepName}, ${m.stepSources} folders, folder "${m.stepFolder}")`);
  if (ctx.startsWith("new thread, project without")) check(m.newProject === "Garden" && m.folderLine === "A fresh scratch folder", `${ctx}: expected Garden in a fresh scratch folder (${m.newProject}, ${m.folderLine})`);
  if (ctx.startsWith("folder picker sheet")) check(m.folderSections.join("|") === "Recent|Repos found|Browse" && m.browseRows >= 4, `${ctx}: sections ${JSON.stringify(m.folderSections)}, ${m.browseRows} browse rows`);
  if (ctx.startsWith("project screen with its folder")) check(m.projFolder === "~/src/acme/web" && m.projFolderLine === "Threads started in this folder or below land here." && m.projFolderBtn === "Change…" && m.stars === 0 && m.noOldWords, `${ctx}: folder "${m.projFolder}", line "${m.projFolderLine}", button "${m.projFolderBtn}", ${m.stars} stars, old words gone ${m.noOldWords}`);
  if (ctx.startsWith("details without a project")) check(!m.makeProject, `${ctx}: a Make project button still shows`);
  if (ctx.startsWith("inbox with a quiet project")) check(!m.dishwasher && !m.quietNote, `${ctx}: the quiet project's thread shows in the inbox (${m.dishwasher}, note ${m.quietNote})`);
  if (ctx.startsWith("quiet project through the filter")) check(m.dishwasher && /^Quiet until \d\d:\d\d$/.test(m.quietNote ?? ""), `${ctx}: the filter does not show the quiet project's thread (${m.dishwasher}) or its note ("${m.quietNote}")`);
  if (ctx.startsWith("project screen with quiet hours")) {
    const q = m.quietRow;
    // Decision 0014 addendum: two windows (the mock's daily one and a weekend one), each with its 24-hour times and
    // seven days, "+ Add quiet hours" and both presets, and one line for all of them.
    check(!!q && q.rows === 2 && q.days === 14 && q.times === 8 && q.add.join("|") === "+ Add quiet hours|Weeknights 17:00–06:00|All weekend Sat–Sun"
      && /^Threads hide and send no notifications daily \d\d:\d\d–\d\d:\d\d and weekends \d\d:\d\d–\d\d:\d\d\. Quiet until \d\d:\d\d\.$/.test(q.line), `${ctx}: the Quiet hours rows are wrong ${JSON.stringify(q)}`);
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
  const key = async (k, code = k) => {
    const vk = { Enter: 13, Escape: 27, ArrowDown: 40, ArrowUp: 38, Tab: 9 }[k] ?? 0;
    await send("Input.dispatchKeyEvent", k === "Enter" ? { type: "keyDown", key: k, code, text: "\r", windowsVirtualKeyCode: vk } : { type: "rawKeyDown", key: k, code, windowsVirtualKeyCode: vk });
    await send("Input.dispatchKeyEvent", { type: "keyUp", key: k, code });
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
      writeFileSync(join(profile, "shot.png"), Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64"));
      writeFileSync(join(profile, "build-output-with-a-very-long-name-that-does-not-fit.log"), "log line\n".repeat(200));
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

  return { send, evaluate, poll, waitIn, waitFor, frames, settle, navigate, tapCenter, key, viewportApplied, openScreen, attached: t.attached };
}

// The phone screens at one size.
async function phonePass(t, w, h) {
  const { send, evaluate, waitFor, frames, settle, openScreen, viewportApplied } = harness(t);
  for (const s of SCREENS) {
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
      if (process.env.FYR_SHOTS && s.name.startsWith("board of changes") && w === 390) writeFileSync(join(process.env.FYR_SHOTS, "board-phone.png"), Buffer.from((await send("Page.captureScreenshot", { format: "png" })).result.data, "base64"));
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
// The three sizes run at the same time, each in its own tab; the desktop and timing checks below run after
// them in the last tab, alone, so the CPU they measure is not shared with the phone passes.
const started = Date.now();
const tabs = await Promise.all(SIZES.map(() => newTab()));
await Promise.all(SIZES.map(([w, h], i) => phonePass(tabs[i], w, h)));
const phoneDone = Date.now();
const { send, evaluate, poll, waitIn, waitFor, settle, navigate, tapCenter, key, openScreen, attached } = harness(tabs[tabs.length - 1]);
for (const t of tabs.slice(0, -1)) await sendRaw("Target.closeTarget", { targetId: t.targetId });

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
const calibrate = (rate) =>
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

// V0.29: a draft survives a reload (a deploy reloads the page); clearing it removes it.
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

// V0.79: on the phone, `/` at the start of the composer lists the commands above it, inside the screen, rows 44 px
// tall; a tap fills `/name ` and the argument hint shows above the field.
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

// V0.33: on the phone, a message the agent has not read waits after "Agent is here", dimmed, with
// Waiting and a Cancel that fits the screen; Cancel strikes it through with Send again.
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

// V0.24: a message that fails to render shows an error card; the thread, its composer and the
// rest of the conversation stay. (Found live: one bad field blanked the whole client.)
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

// SPEC 0.5 V0.26: on the desktop, a long tool group shows the last 3 calls behind "+N more tools",
// and the thread ⋯ menu moves the thread to a project from the keyboard.
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

// SPEC 0.5 V0.79: on the desktop, `/` in the composer opens the list; ArrowDown moves, Enter picks and does not
// send; Escape closes the list and leaves the text.
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

// SPEC 0.5 V0.42: on the desktop, the new thread's Change opens the folder picker from the keyboard; a typed path
// completes with Tab, Enter opens the folder, Enter again picks it; Esc closes only the picker.
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
  await send("Input.insertText", { text: "~/src/no" });
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

// V0.43: on the desktop, "Ask the agent to fix this check" posts the owner's message to the thread.
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

// SPEC 0.5 V0.45: the thread ⋯ menu offers Hand over…; the dialog says how the context goes over, and a
// confirmed handover leaves a divider that names both profiles.
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

// SPEC 0.5 V0.50: the gear at the bottom of the desktop rail opens Settings with the Providers section.
try {
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .rgear')");
  const g = await evaluate(`(() => { const b = document.querySelector('.rail .rgear'); if (!b) return null; const r = b.getBoundingClientRect(), f = document.querySelector('.rail .foot').getBoundingClientRect(); b.click(); return { below: r.top > window.innerHeight / 2, aboveFoot: r.bottom <= f.top + 0.5 }; })()`);
  check(!!g && g.below && g.aboveFoot, `V0.50 desktop: no gear at the bottom of the rail (${JSON.stringify(g)})`);
  await waitFor(`document.querySelectorAll('.dlg .prov:not(.found)').length === 3`, { soft: true });
  await settle();
  const d = await evaluate(`({ title: document.querySelector('.dlg .dh b')?.textContent ?? '', sections: [...document.querySelectorAll('.dlg .sets h3')].map(e => e.textContent), rows: document.querySelectorAll('.dlg .prov:not(.found)').length })`);
  check(d.title === "Settings" && d.sections.join("|") === "Providers|Projects|Auto-merge|Hosts|Notifications|Appearance" && d.rows === 3, `V0.50 desktop: Settings is wrong (${JSON.stringify(d)})`);
  await evaluate(`document.querySelector('.dlg .prov .provh')?.click()`);
  await waitFor(`!!document.querySelector('.dlg .prov.open textarea')`, { soft: true });
  const o = await evaluate(`({ ta: !!document.querySelector('.dlg .prov.open textarea'), key: document.querySelector('.dlg .prov.open input[type=password]')?.value ?? null })`);
  check(o.ta && o.key === "", `V0.50 desktop: an opened provider has no models field or a key in the page (${JSON.stringify(o)})`);
} catch (e) {
  failures.push(`V0.50 desktop: ${e.message}`);
}

// SPEC 0.5 V0.80: the rail's load line sits above the quota inside the rail and covers no other rail item; a hot
// working thread's row shows its CPU, coloured, inside its row. V0.99: the figure sits in a ring, the ring carries the
// level, and the mock's 2 held slots and 1 waiter are 2 filled pips and "+1".
try {
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .rld .rldr') && !!document.querySelector('.one .tcpu')");
  const r = await evaluate(`(() => {
    const rail = document.querySelector('.rail').getBoundingClientRect();
    const ld = document.querySelector('.rail .rld'), b = ld.getBoundingClientRect();
    const fig = ld.querySelector('.rldr .rring b'), row = ld.querySelector('.rldr'), pips = ld.querySelector('.rpips');
    const others = [...document.querySelectorAll('.rail > *')].filter((e) => e !== ld).map((e) => e.getBoundingClientRect());
    const hit = others.filter((o) => o.width && o.height && o.left < b.right - 0.5 && b.left < o.right - 0.5 && o.top < b.bottom - 0.5 && b.top < o.bottom - 0.5).length;
    const q = document.querySelector('.rail .rq')?.getBoundingClientRect();
    const tc = [...document.querySelectorAll('.one')].find((x) => x.dataset.id === 't_mapperf');
    const c = tc?.querySelector('.tcpu'), cr = c?.getBoundingClientRect(), rr = tc?.getBoundingClientRect();
    return { text: fig.textContent, hot: row.classList.contains('hot'), tip: row.title,
      pips: pips ? pips.querySelectorAll('i').length : 0, held: pips ? pips.querySelectorAll('i.on').length : 0, wait: pips?.querySelector('em')?.textContent ?? '',
      inside: b.left >= rail.left - 0.5 && b.right <= rail.right + 0.5 && b.top >= rail.top && b.bottom <= rail.bottom, hit,
      aboveQuota: !q || b.bottom <= q.top + 0.5,
      cpu: c?.textContent ?? '', cpuHot: !!c?.classList.contains('hot'), inRow: !!cr && cr.top >= rr.top - 0.5 && cr.bottom <= rr.bottom + 0.5 };
  })()`);
  check(r.text === "10" && r.hot && /load 10\.4/.test(r.tip) && /CPU pressure 46 %/.test(r.tip), `V0.80 desktop: the rail's load is wrong (${JSON.stringify(r)})`);
  check(r.pips === 2 && r.held === 2 && r.wait === "+1", `V0.99 desktop: the rail's slot pips are wrong (${JSON.stringify(r)})`);
  check(r.inside && r.hit === 0 && r.aboveQuota, `V0.80 desktop: the load line is outside the rail, covers an item or sits below the quota (${JSON.stringify(r)})`);
  check(r.cpu === "2.6 cores" && r.cpuHot && r.inRow, `V0.80 desktop: the hot thread's CPU is missing or outside its row (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.80 desktop: ${e.message}`);
}

// V0.39: the update banner is below the top safe area and inside the screen, and names the threads it waits for.
try {
  await openScreen({ url: "?mock=rail&deploy=drain" }, 390, 844);
  const b = await evaluate(`(() => { const e = document.querySelector('.dbn'); if (!e) return null; const r = e.getBoundingClientRect(); return { top: r.top, left: r.left, right: r.right, text: e.textContent }; })()`);
  check(!!b, "V0.39 banner: no update banner on the phone");
  if (b) {
    check(b.top >= INSETS.top && b.left >= 0 && b.right <= 390, `V0.39 banner: outside the safe area (${JSON.stringify(b)})`);
    check(/waiting for 2 threads to finish: Map perf/.test(b.text), `V0.39 banner: text "${b.text}"`);
  }
} catch (e) {
  failures.push(`V0.39 banner: ${e.message}`);
}

// V0.54: with two hosts the phone rows name each thread's host and an offline host's rows say so; the offline
// thread's header carries "<host> offline since HH:MM · read-only" inside the screen; one host shows no host at all.
try {
  await openScreen({ url: "?mock=rail&hosts=2" }, 390, 844);
  const rows = await evaluate(`[...document.querySelectorAll('.m-mv')].map(r => ({ label: r.getAttribute('aria-label'), off: r.querySelector('.m-off')?.textContent ?? '', host: r.querySelector('.m-hostn')?.textContent ?? '', right: r.getBoundingClientRect().right }))`);
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
  const none = await evaluate(`!document.querySelector('.m-hostc, .m-hostn, .m-offl')`);
  check(none, "V0.54: one host still shows a host on the phone");
} catch (e) {
  failures.push(`V0.54 hosts: ${e.message}`);
}

// V0.35: a table in a message reads as cards on the phone: no cell is squeezed, nothing runs past the edge.
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

// V0.32: native feel on the phone: no page bounce, controls not selectable, message text is,
// and no browser menu on a long press outside text fields.
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

// V0.31: a right-click on a thread row opens Fyr's menu; elsewhere nothing opens.
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

// SPEC 0.5 V0.30, V0.55: from the keyboard, the project chip opens the picker; ArrowDown reaches New project from this
// thread, Enter opens the one step prefilled with the thread's folder, Enter makes the project and files the thread; p
// and the arrows then unfile it with No project.
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

// SPEC 0.5 V0.37: on the desktop, a press in the live view posts a message from the view, shown as a chip.
// The view's sandboxed frame runs out of process, so the click goes through its own CDP session.
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

// SPEC 0.5 V0.47: previews run real pages. A WebGL canvas draws inside the sandboxed frame, and a folder preview
// loads its own script, module, JSON and image by relative URL while the rest of the host stays closed to it.
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

// V0.38: never block the main thread. While a burst of 300 thread updates and 300 messages streams in
// (3 s), no task on the page's main thread takes over 50 ms, and the last message shows. The long tasks are
// observed inside the page from the burst's start until its last message has drawn, so the test's own
// navigation and reads are not in the measurement.
// Budget on a quiet machine: no task over 100 ms, under 250 ms of long tasks in all (before V0.38: 10 tasks, 523 ms),
// scaled by the calibration factor (#50).
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

// V0.40: in a narrow desktop window the thread list starts hidden; \\ shows it, and the thread keeps its room.
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

// V0.32: a screen change on the phone is quick, transition included (found live: open and Back
// took 4 s each, because the transition's update waited for a frame that a transition never draws).
// Budget on a quiet machine: 800 ms each, scaled by the calibration factor (#50).
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

// U15: the desktop question card always shows a button that says how to finish.
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

// V0.48: Settled opened from its header shows a clock and closes by itself (30 s; the test
// fast-forwards with a short wait on a patched clock is not possible, so it checks the clock and
// that the rail's g5 keeps it open without one).
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

// #56: a Needs you row whose state word and change word are longer than the rail. No cell passes the row's content
// box or covers another cell, the clipped texts end in an ellipsis, and the list's scrollbar does not cover the row.
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

// SPEC 0.5 V0.91: every inbox row starts with its project tag, then the title, on one line; every chip (PR, change,
// subagents) sits below the title, inside the row; no row says "0 sub".
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.list .rw .hd .ptag') && !!document.querySelector('.list .one .hd .ptag')");
  const r = await evaluate(`(() => {
    const bad = [];
    const rows = [...document.querySelectorAll('.list .rw, .list .one')];
    for (const row of rows) {
      const id = row.dataset.id, box = row.getBoundingClientRect();
      const tag = row.querySelector('.hd .ptag'), title = row.querySelector('.hd .t');
      if (!tag || !title) { bad.push(id + ': no project or title'); continue; }
      const a = tag.getBoundingClientRect(), b = title.getBoundingClientRect();
      if (a.right > b.left + 0.5) bad.push(id + ': project not before the title');
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

// SPEC 0.5 V0.92, V0.93: the rail's top is the rendered logo (loaded, 32 px, inside the rail), not the text "Fyr".
try {
  await navigate(`${origin}/?mock=rail`, "!!document.querySelector('.rail .brand img') && document.querySelector('.rail .brand img').complete");
  const r = await evaluate(`(() => {
    const img = document.querySelector('.rail .brand img'), b = img.getBoundingClientRect(), rail = document.querySelector('.rail').getBoundingClientRect();
    return { w: b.width, h: b.height, loaded: img.naturalWidth > 0, alt: img.alt, src: img.getAttribute("src"), text: document.querySelector(".rail .brand").textContent.trim(), inside: b.left >= rail.left && b.right <= rail.right };
  })()`);
  check(r.src === "/logo.png" && r.w === 32 && r.h === 32 && r.loaded && r.alt === "Fyr" && r.text === "" && r.inside, `V0.92 desktop: the rail's mark (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.92 desktop: ${e.message}`);
}

// #64: the thread header's blocks (title, chips, offers, tabs) sit 12 px apart, and the conversation starts where
// the header ends: a thread without previews has no band between the tabs and the conversation.
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

// #72: without git the Changes tab has no Refresh and no empty diff section; the change cards keep the tab's height.
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

// #81: a busy update banner folds to a chip inside the screen and opens again on a press, on the desktop and the phone.
for (const [w, h, mobile] of [[1280, 800, false], [390, 844, true]]) {
  const ctx = `#81 ${mobile ? "phone" : "desktop"}`;
  try {
    await send("Emulation.setTouchEmulationEnabled", { enabled: mobile });
    await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: mobile ? 3 : 1, mobile });
    await navigate(`${origin}/?mock=rail&deploy=drain`, "!!document.querySelector('.dbn [aria-label=Fold]')");
    await evaluate(`document.querySelector('.dbn [aria-label=Fold]').click()`);
    await waitFor(`!!document.querySelector('.dbn.dbn-chip')`, { soft: true });
    const c = await evaluate(`(() => { const e = document.querySelector('.dbn.dbn-chip'); if (!e) return null; const r = e.getBoundingClientRect(); return { w: r.width, h: r.height, in: r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight, text: e.textContent.trim() }; })()`);
    check(!!c && c.in && c.w <= 160 && c.text === "Updating", `${ctx}: the folded banner is not a small chip inside the screen (${JSON.stringify(c)})`);
    await evaluate(`document.querySelector('.dbn.dbn-chip')?.click()`);
    await waitFor(`!!document.querySelector('.dbn [aria-label=Fold]')`, { soft: true });
    check(await evaluate(`!document.querySelector('.dbn.dbn-chip') && /waiting for/.test(document.querySelector('.dbn')?.textContent ?? '')`), `${ctx}: a press on the chip does not open the banner`);
  } catch (e) {
    failures.push(`${ctx}: ${e.message}`);
  }
}

// V0.83: while the update waits for running threads the banner has Restart now inside the screen (44 px on the
// phone); a press opens Fyr's confirmation with a red Restart now, and Cancel sends nothing.
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

// #85: a dismissed "Fyr is updated" stays dismissed after a reload (the mock stamps the report with a new time on
// every load, as a restarted host may).
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

// V0.73 (2): on the desktop, the Changes tab of six cards over an empty diff, in a tall and a short window.
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

// SPEC 0.5 V0.68: on the desktop the confirmation is a dialog that fits the window, with focus on its primary
// button; Escape closes it without a press, Enter presses. A click outside does not close it.
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
} catch (e) {
  failures.push(`V0.68 desktop: ${e.message}`);
}

clearTimeout(watchdog);
cleanup();
console.log(`time: phone screens ${((phoneDone - started) / 1000).toFixed(1)} s (${SIZES.length} sizes at once), desktop and timing checks ${((Date.now() - phoneDone) / 1000).toFixed(1)} s`);
if (pageErrors.length) failures.push(...pageErrors.map((e) => `page error: ${e}`));
if (failures.length) {
  console.error(`layout test: ${failures.length} of ${checks} checks failed`);
  for (const f of failures.slice(0, 60)) console.error("  ✖ " + f);
  if (failures.length > 60) console.error(`  … ${failures.length - 60} more`);
  process.exit(1);
}
console.log(`layout test: ${checks} checks passed (${SCREENS.length} screens × ${SIZES.length} sizes)${timingSkipped ? `; ${timingSkipped} timing budgets skipped (FYR_LAYOUT_TIMING=0)` : ""}`);
process.exit(0);

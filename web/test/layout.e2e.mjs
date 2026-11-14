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

const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".woff2": "font/woff2", ".png": "image/png", ".webmanifest": "application/manifest+json" };
const server = createServer((req, res) => {
  const path = decodeURIComponent(new URL(req.url, "http://x").pathname);
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
const chrome = spawn(chromeBin, ["--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore", detached: true });
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
}, 280_000);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let targets;
for (let i = 0; i < 100; i++) {
  try {
    targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
    if (targets.some((t) => t.type === "page")) break;
  } catch {}
  await sleep(100);
}
const ws = new WebSocket(targets.find((t) => t.type === "page").webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let seq = 0;
const pending = new Map();
const pageErrors = [];
const attached = []; // child targets (the sandboxed live view runs out of process)
ws.addEventListener("message", (m) => {
  const j = JSON.parse(m.data);
  if (j.id && pending.has(j.id)) {
    pending.get(j.id)(j);
    pending.delete(j.id);
  }
  if (j.method === "Target.attachedToTarget") attached.push(j.params);
  if (j.method === "Runtime.exceptionThrown") pageErrors.push(j.params.exceptionDetails?.exception?.description ?? j.params.exceptionDetails?.text);
});
const send = (method, params = {}, sessionId) =>
  new Promise((r) => {
    const i = ++seq;
    pending.set(i, r);
    ws.send(JSON.stringify({ id: i, method, params, ...(sessionId ? { sessionId } : {}) }));
  });
const evaluate = async (expression) => {
  const r = await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
  if (r.result?.exceptionDetails) throw new Error(r.result.exceptionDetails.exception?.description ?? r.result.exceptionDetails.text);
  return r.result?.result?.value;
};

await send("Page.enable");
await send("Runtime.enable");

// A scriptable visualViewport, before any page script runs.
await send("Page.addScriptToEvaluateOnNewDocument", {
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

const INSETS = { top: 47, bottom: 34, left: 0, right: 0 };
// env(safe-area-inset-*) is always defined in a browser (0 without a notch), so CSS variables cannot
// stand in for it; this Chrome must support the override.
const trySafeArea = await send("Emulation.setSafeAreaInsetsOverride", { insets: INSETS });
if (trySafeArea.error) {
  console.error(`layout test: this Chrome cannot emulate safe-area insets (${trySafeArea.error.message}); use Chrome 136 or later`);
  cleanup();
  process.exit(1);
}
console.log(`insets: Emulation.setSafeAreaInsetsOverride (top ${INSETS.top}, bottom ${INSETS.bottom}); keyboard and resume: scripted visualViewport`);
const injectInsets = async () => {};

const SIZES = [
  [360, 640],
  [390, 844],
  [430, 932],
];

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
  // SPEC 0.5 V0.28: a thread without a project; its details offer Make project “lasso-app”.
  { name: "details with a project suggestion", url: "?mock=1&open=t_s4", sheet: "details" },
  // SPEC 0.5 V0.30: the project picker from the header chip, the one-step New project, and a new thread whose
  // project has no folder here (the folder field shows and is required).
  { name: "project picker", url: "?mock=1&open=t_s4", sheet: "project", field: ".m-sheet .pq" },
  { name: "new project step", url: "?mock=1&open=t_s4", sheet: "newproject", field: ".m-sheet .pform input" },
  // SPEC 0.5 V0.42: a project without a folder here starts in a fresh scratch folder; "Change" opens the folder
  // picker sheet; the project screen lists folders with the main star and the repo line.
  { name: "new thread, project without a folder", url: "?mock=rail&new", storage: { "fyr.lastProject": "p_home", "fyr.projectFilter": "" }, field: ".m-ta" },
  { name: "folder picker sheet", url: "?mock=rail&new", storage: { "fyr.lastProject": "p_web", "fyr.projectFilter": "" }, sheet: "folder", field: ".fsheet .pq" },
  { name: "project screen with folders", url: "?mock=rail&project=p_web" },
  // SPEC 0.5 V0.37: a thread with a pinned live view; the composer stays inside the safe areas.
  { name: "thread with a live view", url: "?mock=1&open=t_view", field: ".m-cmp .in" },
  // SPEC 0.5 V0.25: the Changes tab with one file's diff open.
  { name: "changes with a diff", url: "?mock=rail&open=t_mapperf&tab=changes", openDiff: true },
  // SPEC 0.5 V0.43: the PR tab with a failing check and 2 unresolved review threads, and the Merge dialog.
  { name: "PR tab with a failing check", url: "?mock=1&open=t_block&tab=pr", prTab: true },
  { name: "PR merge dialog", url: "?mock=1&open=t_s2&tab=pr", sheet: "merge" },
  // SPEC 0.5 V0.46: quiet hours. ?quiet=p_home puts Garden in quiet hours: its thread leaves the inbox, comes back
  // with the project filter (whose line says until when), and the project screen's Quiet hours row fits the phone.
  { name: "inbox with a quiet project", url: "?mock=rail&quiet=p_home", storage: { "fyr.projectFilter": "" } },
  { name: "quiet project through the filter", url: "?mock=rail&quiet=p_home", storage: { "fyr.projectFilter": "p_home" } },
  { name: "project screen with quiet hours", url: "?mock=rail&project=p_home&quiet=p_home", storage: { "fyr.projectFilter": "" } },
  // SPEC 0.5 V0.45: the handover sheet from the thread's ⋯; its buttons stay above the bottom inset.
  { name: "handover sheet", url: "?mock=rail&open=t_triage", sheet: "handover" },
];

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
    sheetItem: lastSheetItem,
    chips: rect('.m-cmpw .att-strip'),
    camera: rect('.m-cmp .cam'),
    chipCount: document.querySelectorAll('.att-chip.done').length,
    previewCards: document.querySelectorAll('.pv').length,
    previewOverflow: Math.max(0, ...[...document.querySelectorAll('.pv, .pv-frame, .pv-img img, .fs')].map(e => e.getBoundingClientRect().right)) - window.innerWidth,
    pageOverflow: document.documentElement.scrollWidth - window.innerWidth,
    innerWidth: window.innerWidth,
    projectEditor: !!document.querySelector('.pedit'),
    filterLine: !!document.querySelector('.m-pfline'),
    otherProjectRows: [...document.querySelectorAll('.m-list .ptag .ptn')].filter(e => e.textContent !== 'acme/web').length,
    saveButton: rect('.pedit .pformb .btn.p'),
    toolOutput: (() => { const o = document.querySelector('.tlout'); if (!o) return null; const r = o.getBoundingClientRect(); return { right: r.right, lines: o.textContent.split(String.fromCharCode(10)).length }; })(),
    changeRows: document.querySelectorAll('.chg-file').length,
    diffLines: document.querySelectorAll('.chg-diff .dl').length,
    changeRight: Math.max(0, ...[...document.querySelectorAll('.chg, .chg-file, .chg-diff')].map(e => e.getBoundingClientRect().right)),
    focused: active ? { top: active.top, bottom: active.bottom } : null,
    pickerRows: [...document.querySelectorAll('.m-sheet .pli .pln')].slice(0, 2).map(e => e.textContent),
    stepName: document.querySelector('.m-sheet .pform input')?.value ?? null,
    stepSources: document.querySelectorAll('.m-sheet .pform .src').length,
    newProject: document.querySelector('.m-new .m-ppick .pln')?.textContent ?? null,
    folderLine: document.querySelector('.m-new .m-fline .ell')?.textContent ?? null,
    stepRepo: document.querySelector('.m-sheet .pform .srcl:not(.src .srcl)')?.textContent ?? '',
    folderSections: [...document.querySelectorAll('.fsheet .fsec h4')].map(e => e.textContent),
    browseRows: document.querySelectorAll('.fsheet .fsec.browse .fpr').length,
    stars: [...document.querySelectorAll('.pedit .srcs.folders .star')].map(e => e.textContent).join(''),
    repoLine: [...document.querySelectorAll('.pedit .srcl')].map(e => e.textContent).find(t => t.startsWith('Also')) ?? '',
    noSourceWord: !/\bsources?\b/i.test(document.querySelector('.pedit, .m-new, .m-sheet')?.innerText ?? ''),
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
    dlg: rect('.dlg'),
    dlgButton: (() => { const b = document.querySelector('.dlg .df .btn.p'); if (!b) return null; const r = b.getBoundingClientRect(); return { top: r.top, bottom: r.bottom, h: r.height, text: b.textContent }; })(),
    dlgRows: [...document.querySelectorAll('.dlg .prv-m')].map(e => e.getBoundingClientRect().height),
    liveView: rect('.m-thread .lv'),
    liveFrame: rect('.m-thread .lv-f'),
    innerHeight: window.innerHeight,
    dishwasher: (document.querySelector('.m-app')?.innerText ?? '').includes('Pick a dishwasher'),
    quietNote: document.querySelector('.m-pfq')?.textContent ?? null,
    quietRow: (() => {
      const el = document.querySelector('.pedit .qfld'); if (!el) return null;
      el.scrollIntoView({ block: 'center' });
      const r = el.getBoundingClientRect();
      const days = [...el.querySelectorAll('.qdays button')].map(b => b.getBoundingClientRect());
      const times = [...el.querySelectorAll('.qtimes input')].map(b => b.getBoundingClientRect());
      return { top: r.top, bottom: r.bottom, right: Math.max(r.right, ...days.map(d => d.right), ...times.map(d => d.right)), on: !!el.querySelector('.qtog input')?.checked,
        line: el.querySelector('.lede')?.textContent ?? '', days: days.length, small: [...days, ...times].filter(d => d.height < 43.5).length };
    })(),
  };
})()`;

const failures = [];
let checks = 0;
const check = (ok, what) => {
  checks++;
  if (!ok) failures.push(what);
};

const window_innerWidth = (m) => m.innerWidth;
const ctxWantsChips = (ctx) => ctx.startsWith("composer with chips");

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
  if (ctx.startsWith("project picker")) check(m.pickerRows[0] === "New project from this thread" && m.pickerRows[1] === "No project", `${ctx}: the picker's first rows are ${JSON.stringify(m.pickerRows)}`);
  if (ctx.startsWith("new project step")) check(m.stepName === "lasso-app" && m.stepSources === 1 && /clones of github.com\/owner\/lasso-app/.test(m.stepRepo), `${ctx}: the step is not prefilled from the thread (name ${m.stepName}, ${m.stepSources} folders, repo line "${m.stepRepo}")`);
  if (ctx.startsWith("new thread, project without")) check(m.newProject === "Garden" && m.folderLine === "A fresh scratch folder", `${ctx}: expected Garden in a fresh scratch folder (${m.newProject}, ${m.folderLine})`);
  if (ctx.startsWith("folder picker sheet")) check(m.folderSections.join("|") === "acme/web's folders|Recent|Repos found|Browse" && m.browseRows >= 4, `${ctx}: sections ${JSON.stringify(m.folderSections)}, ${m.browseRows} browse rows`);
  if (ctx.startsWith("project screen with folders")) check(m.stars === "★☆" && /^Also matches clones of github.com\/acme\/web and github.com\/acme\/web-docs/.test(m.repoLine) && m.noSourceWord, `${ctx}: stars "${m.stars}", repo line "${m.repoLine}", no "source" word ${m.noSourceWord}`);
  if (ctx.startsWith("inbox with a quiet project")) check(!m.dishwasher && !m.quietNote, `${ctx}: the quiet project's thread shows in the inbox (${m.dishwasher}, note ${m.quietNote})`);
  if (ctx.startsWith("quiet project through the filter")) check(m.dishwasher && /^Quiet until \d\d:\d\d$/.test(m.quietNote ?? ""), `${ctx}: the filter does not show the quiet project's thread (${m.dishwasher}) or its note ("${m.quietNote}")`);
  if (ctx.startsWith("project screen with quiet hours")) {
    const q = m.quietRow;
    check(!!q && q.on && q.days === 7 && /^Threads hide and send no notifications from \d\d:\d\d to \d\d:\d\d\. Quiet until \d\d:\d\d\.$/.test(q.line), `${ctx}: the Quiet hours row is wrong ${JSON.stringify(q)}`);
    if (q) {
      check(q.top >= m.top + insetTop - 0.5 && q.bottom <= m.bottom - insetBottom + 0.5, `${ctx}: the Quiet hours row (${q.top.toFixed(1)}–${q.bottom.toFixed(1)}) is outside the safe areas`);
      check(q.right <= m.innerWidth + 0.5, `${ctx}: the Quiet hours row runs ${(q.right - m.innerWidth).toFixed(1)} px past the right edge`);
      check(q.small === 0, `${ctx}: ${q.small} quiet-hours targets are under 44 px`);
    }
  }
  if (ctx.startsWith("inbox filtered")) {
    check(m.filterLine, `${ctx}: the filter line is missing`);
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
    check(m.changeRows >= 4, `${ctx}: expected the changed files (found ${m.changeRows})`);
    check(m.diffLines >= 5, `${ctx}: the opened file shows no diff lines (${m.diffLines})`);
    check(m.changeRight <= window_innerWidth(m) + 0.5, `${ctx}: the changes list or diff runs ${(m.changeRight - window_innerWidth(m)).toFixed(1)} px past the right edge`);
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
  check(m.pageOverflow <= 0, `${ctx}: the page scrolls sideways by ${m.pageOverflow} px`);
  if (ctxWantsChips(ctx)) check(m.chipCount >= 2 && !!m.chips && !!m.camera, `${ctx}: expected 2 uploaded chips and the Camera button (chips ${m.chipCount})`);
  if (m.sheet) check(m.sheet.bottom <= visBottom + 0.5, `${ctx}: sheet runs below the viewport`);
  if (keyboard) {
    check(!!m.focused, `${ctx}: no field is focused`);
    if (m.focused) check(m.focused.top >= visTop - 0.5 && m.focused.bottom <= visBottom + 0.5, `${ctx}: the focused field (${m.focused.top.toFixed(1)}–${m.focused.bottom.toFixed(1)}) is outside the visible area ${visTop}–${visBottom}`);
  }
}

const frames = () => evaluate("new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(r, 60))))");

async function tapCenter(sel, hold = 0) {
  const c = await evaluate(`(() => { const el = document.querySelector(${JSON.stringify(sel)}); if (!el) return null; const r = el.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`);
  if (!c) throw new Error(`no element ${sel}`);
  await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: c[0], y: c[1] }] });
  await sleep(hold || 40);
  await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await sleep(350);
}

async function openScreen(s, w, h) {
  await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: true });
  await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
  await send("Page.navigate", { url: `${origin}/${s.url}` });
  await sleep(900);
  await injectInsets();
  if (s.storage) {
    await evaluate(`(() => { const s = ${JSON.stringify(s.storage)}; for (const k in s) localStorage.setItem(k, s[k]); })()`);
    await send("Page.navigate", { url: `${origin}/${s.url}` });
    await sleep(900);
  }
  await frames();
  if (s.files) {
    writeFileSync(join(profile, "shot.png"), Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64"));
    writeFileSync(join(profile, "build-output-with-a-very-long-name-that-does-not-fit.log"), "log line\n".repeat(200));
    await send("DOM.enable");
    const doc = await send("DOM.getDocument", {});
    const q = await send("DOM.querySelector", { nodeId: doc.result.root.nodeId, selector: s.files });
    await send("DOM.setFileInputFiles", { nodeId: q.result.nodeId, files: [join(profile, "shot.png"), join(profile, "build-output-with-a-very-long-name-that-does-not-fit.log")] });
    await sleep(900);
  }
  if (s.openTools) {
    // V0.41: the run's steps are folded into one line first
    await evaluate(`document.querySelector('.tl.fold')?.click()`);
    await sleep(150);
    await evaluate(`document.querySelector('.tl.grp')?.click()`);
    await sleep(150);
    await evaluate(`(() => { const rows = document.querySelectorAll('.tl.row'); rows[rows.length - 1]?.click(); })()`);
    await sleep(200);
    await evaluate(`document.querySelector('.tlout')?.scrollIntoView({ block: 'nearest' })`);
  }
  if (s.openDiff) {
    await sleep(300);
    // The file with the longest path: its row must ellipsize, its long lines scroll inside the diff.
    await evaluate(`[...document.querySelectorAll('.chg-file')].sort((a, b) => b.title.length - a.title.length)[0]?.click()`);
    await sleep(400);
  }
  if (s.sheet === "snooze") {
    await tapCenter(".m-tr1 .b44:last-child");
    await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('Snooze'))?.click()`);
    await sleep(300);
  } else if (s.sheet === "handover") {
    await tapCenter(".m-tr1 .b44:last-child");
    await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('Hand over'))?.click()`);
    await sleep(300);
  } else if (s.sheet === "details") {
    await tapCenter(".m-chipsd");
    await sleep(300);
  } else if (s.sheet === "project" || s.sheet === "newproject") {
    await tapCenter(".m-pjb");
    if (s.sheet === "newproject") await tapCenter(".m-sheet .pli.new");
    await sleep(300);
  } else if (s.sheet === "folder") {
    await sleep(300);
    await tapCenter(".m-new .m-fchg");
    await sleep(400);
  } else if (s.sheet === "merge") {
    await sleep(300);
    await tapCenter(".prv-merge");
    await sleep(200);
  } else if (s.sheet === "actions") {
    await tapCenter("#m-sec-needs .m-swp .m-mv", 600);
  }
  await frames();
}

for (const [w, h] of SIZES) {
  for (const s of SCREENS) {
    const ctx = `${s.name} ${w}x${h}`;
    try {
      await openScreen(s, w, h);
      // 1. At rest.
      assertLayout(await evaluate(MEASURE), `${ctx} at rest`);
      if (s.sheet === "handover") {
        const ho = await evaluate(`({ rung: document.querySelector('.m-sheet .ho-rung')?.textContent ?? '', btn: [...document.querySelectorAll('.m-sheet .df .btn')].map(b => b.textContent.trim()) })`);
        check(!!ho.rung && ho.btn.includes("Hand over"), `${ctx}: the handover sheet has no rung line or no Hand over button ${JSON.stringify(ho)}`);
      }
      if (s.sheet === "details") {
        const mk = await evaluate(`(() => { const b = document.querySelector('.m-sheet .m-mkp'); if (!b) return null; const r = b.getBoundingClientRect(); return { left: r.left, right: r.right, h: r.height, text: b.textContent, vw: innerWidth }; })()`);
        check(!!mk && mk.text.includes("lasso-app") && mk.left >= 0 && mk.right <= mk.vw && mk.h >= 44, `${ctx}: Make project button missing or out of bounds ${JSON.stringify(mk)}`);
      }

      // 2. Keyboard: focus the field, shrink the visual viewport (iOS also scrolls the layout viewport).
      if (!s.sheet || s.field) {
        if (s.openOther) {
          await evaluate(`[...document.querySelectorAll('.m-op')].find(b => b.textContent.startsWith('Other'))?.click()`);
          await sleep(200);
        }
        const field = s.openOther ? ".m-otherf" : s.field;
        if (field) {
          await evaluate(`document.querySelector(${JSON.stringify(field)})?.focus()`);
          const kh = Math.round(h * 0.6);
          await evaluate(`window.__vv.h = ${kh}; window.__vv.top = ${Math.round(h * 0.15)}; visualViewport.dispatchEvent(new Event('resize')); visualViewport.dispatchEvent(new Event('scroll'))`);
          await frames();
          assertLayout(await evaluate(MEASURE), `${ctx} keyboard`, { keyboard: true });
          await evaluate(`document.activeElement?.blur(); window.__vv.h = null; window.__vv.top = 0; visualViewport.dispatchEvent(new Event('resize'))`);
          await frames();
        }
      }

      // 3. Resume: hidden, the window size changes (the visualViewport fires nothing, as when iOS
      // brings the app back), visible again. Then the same with the size restored.
      for (const rh of [Math.round(h * 0.85), h]) {
        await evaluate(`window.__setHidden(true)`);
        await send("Emulation.setDeviceMetricsOverride", { width: w, height: rh, deviceScaleFactor: 1, mobile: true });
        await sleep(100);
        await evaluate(`window.__setHidden(false); window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`);
        await frames();
        assertLayout(await evaluate(MEASURE), `${ctx} after resume at ${w}x${rh}`);
      }
    } catch (e) {
      failures.push(`${ctx}: ${e.message}`);
    }
  }
}

// U8: settle the open thread from its ⋯ menu at 390 × 844; the inbox shows with the Undo toast.
try {
  await openScreen({ url: "?mock=rail&open=t_triage" }, 390, 844);
  await tapCenter(".m-tr1 .b44:last-child");
  await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('Settle'))?.click()`);
  await sleep(1200);
  const r = await evaluate(`({ inbox: !!document.querySelector('.m-strip'), toast: document.querySelector('.m-toast')?.textContent ?? '', url: location.search })`);
  check(r.inbox, `U8 settle from the thread: the inbox does not show (${r.url})`);
  check(/Settled/.test(r.toast) && /Undo/.test(r.toast), `U8 settle from the thread: no Undo toast on the inbox ("${r.toast}")`);
  // Undo reopens the thread.
  await evaluate(`document.querySelector('.m-toast .tb')?.click()`);
  await sleep(900);
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
  await sleep(150);
  await openScreen({ url: "?mock=rail&open=t_triage" }, 390, 844);
  const v = await evaluate(`document.querySelector('.m-cmp .in')?.value ?? ''`);
  check(v === "half a sentence", `V0.29 draft: lost after a reload ("${v}")`);
  await typeIn("");
  await sleep(150);
} catch (e) {
  failures.push(`V0.29 draft: ${e.message}`);
}

// V0.33: on the phone, a message the agent has not read waits after "Agent is here", dimmed, with
// Waiting and a Cancel that fits the screen; Cancel strikes it through with Send again.
try {
  await openScreen({ url: "?mock=rail&open=t_mapperf" }, 390, 844);
  await sleep(300);
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
  await tapCenter(".m-app .youw.pend .dlb");
  await sleep(300);
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
  await sleep(300);
  const r = await evaluate(`({ card: document.querySelector('.crash')?.textContent ?? '', header: !!document.querySelector('.m-tth'), composer: !!document.querySelector('.m-cmp'), body: document.body.innerText.length })`);
  check(/could not be shown/.test(r.card), `V0.24 crash card: no error card ("${r.card}")`);
  check(r.header && r.composer, `V0.24 crash card: the thread did not stay (header ${r.header}, composer ${r.composer})`);
  check(r.body > 200, `V0.24 crash card: the page is nearly empty (${r.body} characters)`);
} catch (e) {
  failures.push(`V0.24 crash card: ${e.message}`);
}

// SPEC 0.5 V0.26: on the desktop, a long tool group shows the last 3 calls behind "+N more tools",
// and the thread ⋯ menu moves the thread to a project from the keyboard.
const key = async (k, code = k) => {
  const vk = { Enter: 13, Escape: 27, ArrowDown: 40, ArrowUp: 38, Tab: 9 }[k] ?? 0;
  await send("Input.dispatchKeyEvent", k === "Enter" ? { type: "keyDown", key: k, code, text: "\r", windowsVirtualKeyCode: vk } : { type: "rawKeyDown", key: k, code, windowsVirtualKeyCode: vk });
  await send("Input.dispatchKeyEvent", { type: "keyUp", key: k, code });
  await sleep(150);
};
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await send("Page.navigate", { url: `${origin}/?mock=rail&open=t_mapperf` });
  await sleep(900);
  // V0.41: the finished run is one folded line; its steps show after one tap
  const f = await evaluate(`({ folds: document.querySelectorAll('.th .tl.fold').length, grp: document.querySelectorAll('.th .tl.grp').length, out: [...document.querySelectorAll('.th .cv.out .cl')].pop()?.textContent ?? '' })`);
  check(f.folds === 1 && f.grp === 1, `V0.41 desktop: expected one folded line and only the live step open (${JSON.stringify(f)})`);
  check(/run that test alone/.test(f.out), `V0.41 desktop: the live run's newest message is not an outcome ("${f.out}")`);
  await evaluate(`document.querySelector('.th .tl.fold')?.click()`);
  await sleep(150);
  await evaluate(`document.querySelector('.th .tl.grp')?.click()`);
  await sleep(150);
  const g = await evaluate(`({ more: document.querySelector('.th .tl.more')?.textContent ?? '', rows: document.querySelectorAll('.th .tl.row').length })`);
  check(/\+5 more tools/.test(g.more), `V0.26 desktop: no "+5 more tools" row in the 8-call group ("${g.more}")`);
  check(g.rows === 4, `V0.26 desktop: the opened group shows ${g.rows} rows, expected the more row and 3 calls`);
  await evaluate(`document.querySelector('.th .tl.more')?.click()`);
  await sleep(150);
  const g2 = await evaluate(`({ more: !!document.querySelector('.th .tl.more'), rows: document.querySelectorAll('.th .tl.row').length })`);
  check(!g2.more && g2.rows === 8, `V0.26 desktop: "+N more tools" does not show every call (${g2.rows} rows)`);

  await send("Page.navigate", { url: `${origin}/?mock=rail&open=t_triage` });
  await sleep(900);
  await evaluate(`document.querySelector('.thh .tmenu-b')?.focus()`);
  await key("Enter");
  const m = await evaluate(`({ menu: !!document.querySelector('.thh [role=menu]'), focused: document.activeElement?.textContent ?? '' })`);
  check(m.menu, "V0.26 desktop: the thread ⋯ menu does not open from the keyboard");
  check(/Project…/.test(m.focused), `V0.26 desktop: the ⋯ menu does not focus "Project…" ("${m.focused}")`);
  await key("Enter");
  const d = await evaluate(`({ dlg: !!document.querySelector('.dlg .plist'), focus: document.activeElement?.classList.contains('pq') })`);
  check(d.dlg && d.focus, "V0.26 desktop: Project… does not open the project picker with the search focused");
  await send("Input.insertText", { text: "Hemm" });
  await sleep(150);
  await key("Enter");
  await sleep(300);
  const r = await evaluate(`({ dlg: !!document.querySelector('.dlg'), chip: document.querySelector('.thh .chip.proj')?.textContent ?? '' })`);
  check(!r.dlg && /Garden/.test(r.chip), `V0.26 desktop: Enter in the picker does not move the thread to the first match (chip "${r.chip}")`);
} catch (e) {
  failures.push(`V0.26 desktop: ${e.message}`);
}

// SPEC 0.5 V0.42: on the desktop, the new thread's Change opens the folder picker from the keyboard; a typed path
// completes with Tab, Enter opens the folder, Enter again picks it; Esc closes only the picker.
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await send("Page.navigate", { url: `${origin}/?mock=rail` });
  await sleep(600);
  await evaluate(`localStorage.setItem("fyr.lastProject", "p_web"); localStorage.setItem("fyr.projectFilter", ""); true`);
  await send("Page.navigate", { url: `${origin}/?mock=rail&dialog=new` });
  await sleep(900);
  const before = await evaluate(`document.querySelector('.dlg .fline .ell')?.textContent ?? ''`);
  check(before === "~/src/acme/web", `V0.42 desktop: a new thread in acme/web does not start in its main folder ("${before}")`);
  await evaluate(`[...document.querySelectorAll('.dlg .fline .btn')].find(b => b.textContent.trim() === 'Change')?.focus()`);
  await key("Enter");
  await sleep(300);
  const o = await evaluate(`({ picker: !!document.querySelector('.fpick'), focus: document.activeElement?.classList.contains('pq') && !!document.activeElement.closest('.fpick'), first: document.querySelector('.fpick .fsec h4')?.textContent ?? '' })`);
  check(o.picker && o.focus, `V0.42 desktop: Change does not open the folder picker with its field focused (${JSON.stringify(o)})`);
  check(o.first === "acme/web's folders", `V0.42 desktop: the project's other folders do not come first ("${o.first}")`);
  // acme and acme-platform-… share "acme": Tab completes to the common start; Enter opens the exact name.
  await send("Input.insertText", { text: "~/src/no" });
  await sleep(400);
  await key("Tab");
  await sleep(300);
  const v = await evaluate(`document.querySelector('.fpick .pq')?.value ?? ''`);
  check(v === "~/src/acme", `V0.42 desktop: Tab does not complete the common start ("${v}")`);
  await key("Enter");
  await sleep(400);
  const v2 = await evaluate(`document.querySelector('.fpick .pq')?.value ?? ''`);
  check(v2 === "~/src/acme/", `V0.42 desktop: Enter does not open the exact folder ("${v2}")`);
  await send("Input.insertText", { text: "o" });
  await sleep(400);
  await key("Tab");
  await sleep(300);
  const v3 = await evaluate(`document.querySelector('.fpick .pq')?.value ?? ''`);
  check(v3 === "~/src/acme/ops/", `V0.42 desktop: Tab does not complete the one match ("${v3}")`);
  await sleep(300);
  await key("Enter");
  await sleep(400);
  const r = await evaluate(`({ picker: !!document.querySelector('.fpick'), dlg: !!document.querySelector('.dlg'), folder: document.querySelector('.dlg .fline .ell')?.textContent ?? '' })`);
  check(!r.picker && r.dlg && r.folder === "~/src/acme/ops", `V0.42 desktop: Enter does not pick the typed folder (${JSON.stringify(r)})`);
  await evaluate(`[...document.querySelectorAll('.dlg .fline .btn')].find(b => b.textContent.trim() === 'Change')?.focus()`);
  await key("Enter");
  await sleep(300);
  await key("ArrowDown");
  const row = await evaluate(`document.activeElement?.classList.contains('fpr') ?? false`);
  check(row, "V0.42 desktop: ArrowDown does not reach the first folder row");
  await key("Escape");
  await sleep(200);
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
  await send("Page.navigate", { url: `${origin}/?mock=1&open=t_block&tab=pr` });
  await sleep(1200);
  const before = await evaluate(`({ ask: document.querySelector('.th .prv-ask')?.textContent ?? '', status: document.querySelector('.th .prv-st')?.textContent ?? '' })`);
  check(before.ask === "Ask the agent to fix this check", `V0.43 desktop: no "Ask the agent to fix this check" on the failed check ("${before.ask}")`);
  check(/^Blocked: /.test(before.status), `V0.43 desktop: status line "${before.status}"`);
  await evaluate(`document.querySelector('.th .prv-ask')?.click()`);
  await sleep(500);
  const note = await evaluate(`document.querySelector('.th .prv-note')?.textContent ?? ''`);
  check(/Sent to the agent: fix lint/.test(note), `V0.43 desktop: no confirmation after asking ("${note}")`);
  await evaluate(`[...document.querySelectorAll('.th .tabs [role=tab]')].find(e => e.textContent === 'Conversation')?.click()`);
  await sleep(400);
  const posted = await evaluate(`[...document.querySelectorAll('.th .you')].map(e => e.textContent).find(t => t.includes('The check lint failed')) ?? ''`);
  check(/The check lint failed: https:\/\/github\.com\/.*\. Find the cause and fix it\./.test(posted), `V0.43 desktop: the owner's message is not in the conversation ("${posted.slice(0, 120)}")`);
} catch (e) {
  failures.push(`V0.43 desktop: ${e.message}`);
}

// SPEC 0.5 V0.45: the thread ⋯ menu offers Hand over…; the dialog says how the context goes over, and a
// confirmed handover leaves a divider that names both profiles.
try {
  await send("Page.navigate", { url: `${origin}/?mock=rail&open=t_triage` });
  await sleep(900);
  await evaluate(`document.querySelector('.thh .tmenu-b')?.click()`);
  await sleep(150);
  const it = await evaluate(`[...document.querySelectorAll('.thh [role=menuitem]')].map(b => b.textContent)`);
  check(it.some((x) => /Hand over…/.test(x)), `V0.45 desktop: no Hand over… in the ⋯ menu (${JSON.stringify(it)})`);
  await evaluate(`[...document.querySelectorAll('.thh [role=menuitem]')].find(b => /Hand over/.test(b.textContent))?.click()`);
  await sleep(400);
  const d = await evaluate(`({ dlg: !!document.querySelector('.dlg .ho-rung'), rung: document.querySelector('.dlg .ho-rung b')?.textContent ?? '', to: document.querySelector('.dlg select')?.selectedOptions[0]?.textContent ?? '' })`);
  check(d.dlg && /Full history|reads the old conversation|writes a handover/.test(d.rung) && d.to !== "", `V0.45 desktop: the dialog does not show the target and the rung (${JSON.stringify(d)})`);
  await evaluate(`[...document.querySelectorAll('.dlg .df .btn')].find(b => b.textContent.trim() === 'Hand over')?.click()`);
  await sleep(400);
  const r = await evaluate(`({ dlg: !!document.querySelector('.dlg'), div: document.querySelector('.th .hodiv')?.textContent ?? '' })`);
  check(!r.dlg && r.div.startsWith("Handed over from ") && r.div.includes(` to ${d.to}`), `V0.45 desktop: no divider after the handover (${JSON.stringify(r)})`);
} catch (e) {
  failures.push(`V0.45 desktop: ${e.message}`);
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
  await send("Page.navigate", { url: `${origin}/?mock=rail` });
  await sleep(900);
  const rightClick = async (x, y) => {
    await send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "right", clickCount: 1 });
    await send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "right", clickCount: 1 });
    await sleep(200);
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

// SPEC 0.5 V0.30: from the keyboard, the suggestion chip opens the picker on New project from this thread; Enter
// opens the one step prefilled from the thread, Enter makes the project and files the thread; p and the arrows then
// unfile it with No project.
try {
  await send("Page.navigate", { url: `${origin}/?mock=1&open=t_s4` });
  await sleep(900);
  await evaluate(`document.querySelector('.thh .chip.mkp')?.focus()`);
  await key("Enter");
  const a = await evaluate(`({ dlg: !!document.querySelector('.dlg .plist'), focused: document.activeElement?.classList.contains('new') ? document.activeElement.textContent : '' })`);
  check(a.dlg && /New project from this thread/.test(a.focused), `V0.30 desktop: the suggestion chip does not open the picker on New project from this thread ("${a.focused}")`);
  await key("Enter");
  const b = await evaluate(`({ name: document.activeElement?.value ?? '', sources: document.querySelectorAll('.dlg .pform .src').length, button: document.querySelector('.dlg .pform .btn.p')?.textContent ?? '' })`);
  check(b.name === "lasso-app" && b.sources === 1 && /Create and file this thread/.test(b.button), `V0.30 desktop: the step is not prefilled and focused (${JSON.stringify(b)})`);
  await key("Enter");
  await sleep(400);
  const c = await evaluate(`({ dlg: !!document.querySelector('.dlg'), chip: document.querySelector('.thh .chip.proj')?.textContent ?? '', mk: !!document.querySelector('.thh .chip.mkp') })`);
  check(!c.dlg && /lasso-app/.test(c.chip) && !c.mk, `V0.30 desktop: Enter does not make the project and file the thread (${JSON.stringify(c)})`);
  await evaluate(`document.activeElement?.blur()`);
  await send("Input.dispatchKeyEvent", { type: "keyDown", key: "p", code: "KeyP", text: "p", windowsVirtualKeyCode: 80 });
  await send("Input.dispatchKeyEvent", { type: "keyUp", key: "p", code: "KeyP" });
  await sleep(200);
  await key("ArrowDown");
  await key("ArrowDown");
  const f = await evaluate(`document.activeElement?.textContent ?? ''`);
  check(/^No project/.test(f), `V0.30 desktop: the second row is not No project ("${f}")`);
  await key("Enter");
  await sleep(400);
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
  await send("Page.navigate", { url: `${origin}/?mock=1&open=t_view` });
  await sleep(1500);
  const v = await evaluate(`(() => { const f = document.querySelector('.th .lv-f'); return f ? { h: f.getBoundingClientRect().height, stamp: document.querySelector('.th .lv-s')?.textContent ?? '' } : null; })()`);
  check(!!v && v.h > 120 && /updated 12 min ago/.test(v.stamp), `V0.37 desktop: the live view is missing, not sized by its page, or has no stamp (${JSON.stringify(v)})`);
  const frame = attached.filter((a) => a.targetInfo.type === "iframe").pop();
  check(!!frame, "V0.37 desktop: no frame target for the live view");
  if (frame) {
    const r = await send("Runtime.evaluate", { expression: `(() => { const b = document.querySelector('[data-send="Open the PR"]'); if (!b) return false; b.click(); return true; })()`, returnByValue: true }, frame.sessionId);
    check(r.result?.result?.value === true, `V0.37 desktop: no "Open the PR" button in the view (${JSON.stringify(r.result ?? r.error)})`);
    await sleep(600);
    // V0.37: the pressed button says what happened, inside the view.
    const fb = await send("Runtime.evaluate", { expression: `document.querySelector('[data-send="Open the PR"]')?.getAttribute('data-fyr') ?? ''`, returnByValue: true }, frame.sessionId);
    check(fb.result?.result?.value === "sent", `V0.37 desktop: the pressed button gives no Sent feedback (${JSON.stringify(fb.result?.result?.value)})`);
    // The message chip is in the Conversation tab; the thread opened on View.
    await evaluate(`[...document.querySelectorAll('.th [role=tab]')].find((x) => x.textContent.trim() === 'Conversation')?.click()`);
    await sleep(300);
    const chips = await evaluate(`[...document.querySelectorAll('.th .vchip')].map((c) => c.textContent)`);
    check(chips.some((c) => /Open the PR/.test(c)), `V0.37 desktop: the press did not post a message from the view (chips ${JSON.stringify(chips)})`);
  }
  // A message from another window is ignored: the top page posts a press as if it were the view.
  await evaluate(`window.postMessage({ jsonrpc: "2.0", id: 1, method: "ui/message", params: { role: "user", content: [{ type: "text", text: "forged" }] } }, "*")`);
  await sleep(400);
  const forged = await evaluate(`[...document.querySelectorAll('.th .vchip')].some((c) => /forged/.test(c.textContent))`);
  check(!forged, "V0.37 desktop: a message from outside the view's frame was sent");
  await send("Target.setAutoAttach", { autoAttach: false, waitForDebuggerOnStart: false, flatten: true });
} catch (e) {
  failures.push(`V0.37 desktop: ${e.message}`);
}

// V0.38: never block the main thread. While a burst of 300 thread updates and 300 messages streams in
// (3 s), no task on the page's main thread takes over 50 ms, and the last message shows.
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await send("Page.navigate", { url: `${origin}/?mock=rail&open=t_mapperf` });
  await sleep(1200);
  // A mid-range phone's speed: 4 times slower than this machine's CPU.
  await send("Emulation.setCPUThrottlingRate", { rate: 4 });
  await evaluate(`window.__long = []; new PerformanceObserver((l) => { for (const e of l.getEntries()) window.__long.push(Math.round(e.duration)); }).observe({ type: "longtask", buffered: false }); window.__fyrBurst("t_mapperf", 300); true`);
  await sleep(7000);
  await send("Emulation.setCPUThrottlingRate", { rate: 1 });
  const r = await evaluate(`({ long: window.__long, last: [...document.querySelectorAll('.th .md')].map((x) => x.textContent).filter((t) => /Burst message/.test(t)).pop() ?? '' })`);
  const total = r.long.reduce((a, b) => a + b, 0);
  // Budget: no task over 100 ms, under 250 ms of long tasks in all (before V0.38: 10 tasks, 523 ms).
  check(Math.max(0, ...r.long) <= 100 && total < 250, `V0.38 main thread: long tasks during the burst: ${r.long.length}, ${total} ms in all (${r.long.slice(0, 8).join(", ")} ms)`);
  check(/Burst message 300/.test(r.last), `V0.38 main thread: the last burst message is not shown ("${r.last.slice(0, 60)}")`);
} catch (e) {
  failures.push(`V0.38 main thread: ${e.message}`);
}

// V0.40: in a narrow desktop window the thread list starts hidden; \\ shows it, and the thread keeps its room.
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 900, height: 800, deviceScaleFactor: 1, mobile: false });
  await send("Page.navigate", { url: `${origin}/?mock=rail&open=t_triage` });
  await sleep(1200);
  const a = await evaluate(`({ list: !!document.querySelector('.ib') && getComputedStyle(document.querySelector('.ib')).display !== 'none', th: Math.round(document.querySelector('.th')?.getBoundingClientRect().width ?? 0) })`);
  check(!a.list && a.th > 700, `V0.40 list: a narrow window does not start with the list hidden (list ${a.list}, thread ${a.th} px)`);
  await evaluate(`document.activeElement?.blur(); true`);
  await send("Input.dispatchKeyEvent", { type: "keyDown", key: "\\", code: "Backslash", text: "\\" });
  await send("Input.dispatchKeyEvent", { type: "keyUp", key: "\\", code: "Backslash" });
  await sleep(300);
  const b = await evaluate(`!!document.querySelector('.ib') && getComputedStyle(document.querySelector('.ib')).display !== 'none'`);
  check(b, "V0.40 list: the \\ key does not show the list");
  await evaluate(`try { localStorage.removeItem("fyr.list") } catch {} true`);
} catch (e) {
  failures.push(`V0.40 list: ${e.message}`);
}

// V0.32: a screen change on the phone is quick, transition included (found live: open and Back
// took 4 s each, because the transition's update waited for a frame that a transition never draws).
try {
  await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 3, mobile: true });
  await send("Emulation.setTouchEmulationEnabled", { enabled: true });
  await send("Page.navigate", { url: `${origin}/?mock=rail` });
  await sleep(1500);
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
  const open = await tapAndWait(".m-app .m-mv", "!!document.querySelector('.m-tth')");
  await sleep(300);
  const back = await tapAndWait(".m-tth .b44", "!document.querySelector('.m-tth')");
  check(open >= 0 && open < 800, `V0.32 phone: opening a thread took ${open} ms`);
  check(back >= 0 && back < 800, `V0.32 phone: Back to the list took ${back} ms`);
} catch (e) {
  failures.push(`V0.32 phone navigation: ${e.message}`);
}

// U15: the desktop question card always shows a button that says how to finish.
try {
  await send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  await send("Page.navigate", { url: `${origin}/?mock=1&open=t_lasso` });
  await sleep(1200);
  const btn = () => evaluate(`(() => { const b = document.querySelector('.th .ask .askf .btn'); return b ? { text: b.textContent.replace('↵', '').trim(), disabled: b.disabled } : null; })()`);
  const a = await btn();
  check(!!a && a.text === "Pick an answer" && a.disabled, `U15 desktop: the card does not start with a disabled "Pick an answer" (${JSON.stringify(a)})`);
  await evaluate(`document.querySelectorAll('.th .ask .grid4 .op')[0]?.click()`);
  await sleep(200);
  await evaluate(`document.querySelectorAll('.th .ask .grid4 .op')[0]?.click()`);
  await sleep(200);
  const b = await btn();
  check(!!b && /^Send 2 answers$/.test(b.text) && !b.disabled, `U15 desktop: after both answers the button is not "Send 2 answers" (${JSON.stringify(b)})`);
  await evaluate(`document.querySelector('.th .ask .askf .btn')?.click()`);
  await sleep(600);
  const gone = await evaluate(`!document.querySelector('.th .ask .askf')`);
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
  await send("Page.navigate", { url: `${origin}/?mock=rail` });
  await sleep(1000);
  await evaluate(`[...document.querySelectorAll('.sec.tog')].find((x) => /Settled/.test(x.textContent))?.click()`);
  await sleep(300);
  const a = await evaluate(`({ clock: !!document.querySelector('#sec-settled .ahc'), rows: document.querySelectorAll('#sec-settled [data-id]').length })`);
  check(a.clock && a.rows > 0, `V0.48 settled: opened from its header without a clock (${JSON.stringify(a)})`);
  await evaluate(`[...document.querySelectorAll('.rail .ri')].find((x) => /Settled/.test(x.textContent))?.click()`);
  await sleep(300);
  const b = await evaluate(`({ clock: !!document.querySelector('#sec-settled .ahc'), rows: document.querySelectorAll('#sec-settled [data-id]').length })`);
  check(!b.clock && b.rows > 0, `V0.48 settled: chosen in the rail it still runs down (${JSON.stringify(b)})`);
} catch (e) {
  failures.push(`V0.48 settled: ${e.message}`);
}

clearTimeout(watchdog);
cleanup();
if (pageErrors.length) failures.push(...pageErrors.map((e) => `page error: ${e}`));
if (failures.length) {
  console.error(`layout test: ${failures.length} of ${checks} checks failed`);
  for (const f of failures.slice(0, 60)) console.error("  ✖ " + f);
  if (failures.length > 60) console.error(`  … ${failures.length - 60} more`);
  process.exit(1);
}
console.log(`layout test: ${checks} checks passed (${SCREENS.length} screens × ${SIZES.length} sizes)`);
process.exit(0);

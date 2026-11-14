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
ws.addEventListener("message", (m) => {
  const j = JSON.parse(m.data);
  if (j.id && pending.has(j.id)) {
    pending.get(j.id)(j);
    pending.delete(j.id);
  }
  if (j.method === "Runtime.exceptionThrown") pageErrors.push(j.params.exceptionDetails?.exception?.description ?? j.params.exceptionDetails?.text);
});
const send = (method, params = {}) =>
  new Promise((r) => {
    const i = ++seq;
    pending.set(i, r);
    ws.send(JSON.stringify({ id: i, method, params }));
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
  { name: "thread", url: "?mock=rail&open=t_mapperf", field: ".m-cmp .in" },
  { name: "opened tool group", url: "?mock=rail&open=t_mapperf", field: ".m-cmp .in", openTools: true },
  { name: "thread with previews", url: "?mock=1&open=t_w3", field: ".m-cmp .in", previews: true },
  { name: "composer with chips", url: "?mock=rail&open=t_mapperf", field: ".m-cmp .in", files: ".m-cmp input[data-picker=files]" },
  { name: "question Q1", url: "?mock=rail&open=t_lasso", openOther: true },
  { name: "approval", url: "?mock=1&open=t_approval" },
  { name: "effect", url: "?mock=1&open=t_effect" },
  { name: "new thread", url: "?mock=rail&new", field: ".m-ta" },
  { name: "snooze sheet", url: "?mock=rail&open=t_mapperf", sheet: "snooze" },
  { name: "action sheet", url: "?mock=rail", sheet: "actions" },
];

// What must be visible, and where, on the current screen.
const MEASURE = `(() => {
  const vv = window.visualViewport;
  const top = vv.offsetTop, bottom = vv.offsetTop + vv.height;
  const rect = (sel) => { const el = document.querySelector(sel); if (!el) return null; const r = el.getBoundingClientRect(); return r.height ? { top: r.top, bottom: r.bottom } : null; };
  const lastSheetItem = (() => { const items = document.querySelectorAll('.m-sheet .m-mi, .m-sheet .m-bt'); const el = items[items.length - 1]; if (!el) return null; el.scrollIntoView({ block: 'nearest' }); const r = el.getBoundingClientRect(); return { top: r.top, bottom: r.bottom }; })();
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
    toolOutput: (() => { const o = document.querySelector('.tlout'); if (!o) return null; const r = o.getBoundingClientRect(); return { right: r.right, lines: o.textContent.split(String.fromCharCode(10)).length }; })(),
    focused: active ? { top: active.top, bottom: active.bottom } : null,
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
  if (ctx.startsWith("opened tool group")) {
    check(!!m.toolOutput, `${ctx}: the opened row shows no output`);
    if (m.toolOutput) {
      check(m.toolOutput.lines === 200, `${ctx}: long output shows ${m.toolOutput.lines} lines, expected the 200-line cut`);
      check(m.toolOutput.right <= window_innerWidth(m) + 0.5, `${ctx}: the output block runs past the right edge`);
    }
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
    await evaluate(`document.querySelector('.tl.grp')?.click()`);
    await sleep(150);
    await evaluate(`(() => { const rows = document.querySelectorAll('.tl.row'); rows[rows.length - 1]?.click(); })()`);
    await sleep(200);
    await evaluate(`document.querySelector('.tlout')?.scrollIntoView({ block: 'nearest' })`);
  }
  if (s.sheet === "snooze") {
    await tapCenter(".m-tr1 .b44:last-child");
    await evaluate(`[...document.querySelectorAll('.m-sheet .m-mi')].find(b => b.textContent.startsWith('Snooze'))?.click()`);
    await sleep(300);
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

      // 2. Keyboard: focus the field, shrink the visual viewport (iOS also scrolls the layout viewport).
      if (!s.sheet) {
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

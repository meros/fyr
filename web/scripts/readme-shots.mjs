// Makes the README's images (docs/images/*.png) from the mock client, in light and dark:
//   hero:    the desktop with a question card open, beside the phone's inbox (?mock=rail)
//   approve: a merge ask and a Run card, cut out of their threads (?mock=1, ?mock=rail&runs=1)
// Run after `npm run build`: node scripts/readme-shots.mjs [outdir]. Needs google-chrome (or FYR_CHROME).
// The screens come from the same fixtures the layout test uses, so a UI change shows up here on the next run.
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dist = join(here, "..", "dist");
const out = resolve(process.argv[2] ?? join(here, "..", "..", "docs", "images"));
const raw = mkdtempSync(join(tmpdir(), "fyr-shots-"));
mkdirSync(out, { recursive: true });
if (!existsSync(join(dist, "index.html"))) throw new Error("build the client first: npm run build");

const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".svg": "image/svg+xml", ".woff2": "font/woff2", ".webmanifest": "application/manifest+json", ".json": "application/json" };
// /raw/* are the screenshots taken so far (the composites load them); everything else is the client.
const server = createServer((req, res) => {
  const p = decodeURIComponent(new URL(req.url, "http://x").pathname);
  const root = p.startsWith("/raw/") ? raw : dist;
  let f = join(root, p.startsWith("/raw/") ? p.slice(5) : p);
  if (!f.startsWith(root) || !existsSync(f) || statSync(f).isDirectory()) f = join(dist, "index.html");
  res.writeHead(200, { "Content-Type": TYPES[extname(f)] ?? "application/octet-stream", "Cache-Control": "no-store" });
  res.end(readFileSync(f));
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = `http://127.0.0.1:${server.address().port}`;

const port = 9400 + Math.floor(Math.random() * 400);
const profile = mkdtempSync(join(tmpdir(), "fyr-shots-chrome-"));
const chrome = spawn(process.env.FYR_CHROME || "google-chrome", ["--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore", detached: true });
const done = () => {
  try {
    process.kill(-chrome.pid, "SIGKILL");
  } catch {}
  server.close();
  rmSync(profile, { recursive: true, force: true });
  rmSync(raw, { recursive: true, force: true });
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let version;
for (const t0 = Date.now(); Date.now() - t0 < 60_000 && !version?.webSocketDebuggerUrl; ) {
  try {
    version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
  } catch {
    await sleep(100);
  }
}
if (!version?.webSocketDebuggerUrl) {
  done();
  throw new Error("Chrome did not open its debug port");
}
const ws = new WebSocket(version.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let seq = 0;
const pending = new Map();
ws.addEventListener("message", (m) => {
  const j = JSON.parse(m.data);
  if (j.id && pending.has(j.id)) pending.get(j.id)(j), pending.delete(j.id);
});
const send = (method, params = {}, sessionId) =>
  new Promise((r) => {
    const i = ++seq;
    pending.set(i, r);
    ws.send(JSON.stringify({ id: i, method, params, ...(sessionId ? { sessionId } : {}) }));
  });

// The mock merge ask's reason is half Swedish (a stress fixture); the README shows it in English.
const english = `(() => { const w = document.createTreeWalker(document.body, 4); let n; while ((n = w.nextNode())) n.nodeValue = n.nodeValue.replace("the översättningar are verified", "the translations are checked"); })()`;

/** One screenshot in its own browser context: path is a URL path on the server, file a name under dir. */
async function shot({ path, file, dir = raw, w, h, dark = false, mobile = false, scale = 2, js }) {
  const ctx = (await send("Target.createBrowserContext")).result.browserContextId;
  const target = (await send("Target.createTarget", { url: "about:blank", browserContextId: ctx })).result.targetId;
  const sid = (await send("Target.attachToTarget", { targetId: target, flatten: true })).result.sessionId;
  const t = (m, p) => send(m, p, sid);
  await t("Page.enable");
  await t("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: scale, mobile });
  if (mobile) await t("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
  await t("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: dark ? "dark" : "light" }, { name: "prefers-reduced-motion", value: "reduce" }] });
  await t("Page.navigate", { url: origin + path });
  await sleep(2500); // the mock's first frame and its fonts
  if (js) await t("Runtime.evaluate", { expression: js }), await sleep(300);
  const r = await t("Page.captureScreenshot", { format: "png" });
  writeFileSync(join(dir, file), Buffer.from(r.result.data, "base64"));
  await send("Target.disposeBrowserContext", { browserContextId: ctx });
}

const THEMES = {
  light: { bg: "linear-gradient(135deg,#f6f3ec 0%,#e8edf4 100%)", line: "rgba(15,23,42,.12)", shadow: "0 30px 60px -20px rgba(15,23,42,.35),0 10px 24px -12px rgba(15,23,42,.2)", bezel: "#1f2430" },
  dark: { bg: "linear-gradient(135deg,#0e1218 0%,#18202b 100%)", line: "rgba(255,255,255,.12)", shadow: "0 30px 60px -20px rgba(0,0,0,.8),0 10px 24px -12px rgba(0,0,0,.6)", bezel: "#3a4150" },
};

try {
  for (const [name, th] of Object.entries(THEMES)) {
    const dark = name === "dark";
    await shot({ path: "/?mock=rail&open=t_lasso", file: `desk-${name}.png`, w: 1440, h: 900, dark });
    await shot({ path: "/?mock=rail", file: `phone-${name}.png`, w: 390, h: 844, dark, mobile: true, scale: 3 });
    await shot({ path: "/?mock=1&open=t_effect", file: `merge-${name}.png`, w: 1440, h: 900, dark, js: english });
    await shot({ path: "/?mock=rail&runs=1&open=t_run", file: `run-${name}.png`, w: 1440, h: 900, dark });

    // The composites are pages too: the desktop at 90 % with the phone in front of its right edge.
    writeFileSync(join(raw, `hero-${name}.html`), `<!doctype html><meta charset=utf-8><style>
html,body{margin:0;width:1602px;height:890px;background:${th.bg};overflow:hidden}
.d{position:absolute;left:40px;top:40px;width:1296px;height:810px;border-radius:14px;overflow:hidden;border:1px solid ${th.line};box-shadow:${th.shadow}}
.d img{width:1296px;height:810px;display:block}
.p{position:absolute;left:1250px;top:170px;width:312px;height:675px;border-radius:40px;overflow:hidden;border:9px solid ${th.bezel};box-shadow:${th.shadow};background:${th.bezel}}
.p img{width:312px;height:675px;display:block;border-radius:31px}
</style><div class=d><img src="desk-${name}.png"></div><div class=p><img src="phone-${name}.png"></div>`);
    // The two cards cut out at their own edges (CSS pixels of the 1440 × 900 screens).
    writeFileSync(join(raw, `approve-${name}.html`), `<!doctype html><meta charset=utf-8><style>
html,body{margin:0;width:1045px;height:560px;background:${th.bg};overflow:hidden}
.c{position:absolute;left:50px;width:945px;border-radius:13px;overflow:hidden;box-shadow:${th.shadow}}
.c img{position:absolute;width:1440px;height:900px}
</style><div class=c style="top:40px;height:249px"><img style="left:-472px;top:-580px" src="merge-${name}.png"></div>
<div class=c style="top:329px;height:191px"><img style="left:-472px;top:-638px" src="run-${name}.png"></div>`);
    await shot({ path: `/raw/hero-${name}.html`, file: `hero-${name}.png`, dir: out, w: 1602, h: 890, scale: 1 });
    await shot({ path: `/raw/approve-${name}.html`, file: `approve-${name}.png`, dir: out, w: 1045, h: 560, scale: 1.5 });
    console.log(`readme-shots: ${name} done`);
  }
} finally {
  done();
}
console.log(`readme-shots: wrote ${out}`);

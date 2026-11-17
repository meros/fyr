// Authentication in a real browser (decision 0030 Part 4.5, slice 0): headless Chrome against scratch fyrds and a
// fake tailscaled, from `internal/authtest/cmd/authstack`. Slice 0 pins what works today: a phone that reaches the
// install through Tailscale Serve signs in by its tailnet identity, at the desktop and the phone size; a browser
// that holds the fixture's session cookie is let in and not given another. Later slices add the QR link, the
// approval code, a revoked device, Settings → Devices and Hosts.
//
// Run `npm run build` first (the web client is embedded in the fyrd that authstack builds); `npm run test:auth`.
// Every wait polls a condition up to a deadline; nothing sleeps a fixed time to let the page catch up.

import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const failures = [];
const check = (ok, what) => {
  if (!ok) failures.push(what);
  console.log(`${ok ? "ok  " : "FAIL"} ${what}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The stack: build once, run, read its one line. Every FYR_* variable out of its environment (#167, V0.131).
const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith("FYR_CHROME") && !k.startsWith("FYR_")));
const bin = join(mkdtempSync(join(tmpdir(), "fyr-authstack-")), "authstack");
const build = spawnSync("go", ["build", "-o", bin, "./internal/authtest/cmd/authstack"], { cwd: root, env: { ...env, CGO_ENABLED: "0" }, encoding: "utf8" });
if (build.status !== 0) {
  console.error(`auth e2e: go build authstack failed\n${build.stderr}`);
  process.exit(1);
}
const stack = spawn(bin, [], { env, stdio: ["pipe", "pipe", "inherit"] });
const stackInfo = await new Promise((resolve, reject) => {
  let buf = "";
  stack.stdout.on("data", (d) => {
    buf += d;
    if (buf.includes("\n")) resolve(JSON.parse(buf.split("\n")[0]));
  });
  stack.on("exit", (c) => reject(new Error(`authstack exited with ${c}`)));
  setTimeout(() => reject(new Error("authstack not ready within 120 s")), 120_000);
});

const chromeBin = process.env.FYR_CHROME || "google-chrome";
const profile = mkdtempSync(join(tmpdir(), "fyr-auth-e2e-"));
const port = 9800 + Math.floor(Math.random() * 400);
const chrome = spawn(chromeBin, ["--headless=new", "--disable-gpu", "--enable-unsafe-swiftshader", "--no-sandbox", "--hide-scrollbars", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore", detached: true });
const cleanup = () => {
  try {
    process.kill(-chrome.pid, "SIGKILL");
  } catch {}
  stack.stdin.end();
  setTimeout(() => stack.kill("SIGKILL"), 5000).unref();
  rmSync(profile, { recursive: true, force: true });
};
const watchdog = setTimeout(() => {
  console.error("auth e2e: timed out");
  cleanup();
  process.exit(2);
}, 240_000);

let version;
for (const t0 = Date.now(); Date.now() - t0 < 60_000; ) {
  try {
    version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
    if (version.webSocketDebuggerUrl) break;
  } catch {}
  await sleep(100);
}
if (!version?.webSocketDebuggerUrl) {
  console.error(`auth e2e: Chrome (${chromeBin}) did not open its debug port ${port}`);
  cleanup();
  process.exit(1);
}
const ws = new WebSocket(version.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let seq = 0;
const pending = new Map();
const waiters = [];
ws.addEventListener("message", (m) => {
  const j = JSON.parse(m.data);
  if (j.id && pending.has(j.id)) {
    pending.get(j.id)(j);
    pending.delete(j.id);
  }
  for (const w of waiters.filter((x) => x.method === j.method && x.sessionId === j.sessionId)) {
    waiters.splice(waiters.indexOf(w), 1);
    w.ok(j.params);
  }
});
const sendRaw = (method, params = {}, sessionId) =>
  new Promise((r) => {
    const i = ++seq;
    pending.set(i, r);
    ws.send(JSON.stringify({ id: i, method, params, ...(sessionId ? { sessionId } : {}) }));
  });

// A tab in its own browser context: its own cookie jar, so a signed-in tab does not sign in another.
async function newTab(size) {
  const ctx = await sendRaw("Target.createBrowserContext", { disposeOnDetach: true });
  const target = await sendRaw("Target.createTarget", { url: "about:blank", browserContextId: ctx.result.browserContextId });
  const att = await sendRaw("Target.attachToTarget", { targetId: target.result.targetId, flatten: true });
  const sessionId = att.result.sessionId;
  const send = (m, p) => sendRaw(m, p, sessionId);
  await send("Page.enable");
  await send("Runtime.enable");
  await send("Network.enable");
  await send("Emulation.setDeviceMetricsOverride", { width: size.w, height: size.h, deviceScaleFactor: 1, mobile: size.mobile });
  const evaluate = async (expression) => {
    const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
    return r.result?.result?.value;
  };
  const waitFor = async (expr, what, ms = 20_000) => {
    for (const t0 = Date.now(); Date.now() - t0 < ms; await sleep(50)) if (await evaluate(`!!(${expr})`)) return true;
    failures.push(`timed out waiting for ${what}`);
    console.log(`FAIL timed out waiting for ${what}`);
    return false;
  };
  const navigate = async (url) => {
    const loaded = new Promise((ok) => waiters.push({ method: "Page.loadEventFired", sessionId, ok }));
    await send("Page.navigate", { url });
    await Promise.race([loaded, sleep(20_000)]);
  };
  const cookies = async (url) => (await send("Network.getCookies", { urls: [url] })).result?.cookies ?? [];
  const setCookie = (url, value) => send("Network.setCookie", { url, name: "fyr_session", value, httpOnly: true, sameSite: "Strict" });
  return { evaluate, waitFor, navigate, cookies, setCookie };
}

const sizes = [
  { name: "desktop", w: 1280, h: 800, mobile: false },
  { name: "phone", w: 390, h: 844, mobile: true },
];
try {
  for (const size of sizes) {
    // The owner's phone, through Tailscale Serve, with no cookie: the tailnet identity signs it in.
    const t = await newTab(size);
    await t.navigate(stackInfo.serve + "/");
    await t.waitFor(`document.querySelector('.m-app, .shell')`, `${size.name}: the app to draw after a tailnet sign-in`);
    const jar = await t.cookies(stackInfo.serve);
    const session = jar.find((c) => c.name === "fyr_session");
    check(!!session, `${size.name}: the tailnet identity minted a session cookie`);
    check(session?.httpOnly === true && session?.sameSite === "Strict", `${size.name}: the session cookie is HttpOnly and SameSite=Strict`);
    const hosts = await t.evaluate(`fetch('/api/state').then(r => r.json()).then(j => j.hosts.map(h => h.name).sort().join(','))`);
    check(hosts === "laptop,server", `${size.name}: the signed-in page reads the API and its front follows its host (${hosts})`);

    // A browser that holds the fixture's tailnet session (the phone as V0.130 left it) is let in and gets no new one.
    const k = await newTab(size);
    await k.setCookie(stackInfo.serve, stackInfo.tailnet);
    await k.navigate(stackInfo.serve + "/");
    await k.waitFor(`document.querySelector('.m-app, .shell')`, `${size.name}: the app to draw with the fixture's cookie`);
    const jar2 = await k.cookies(stackInfo.serve);
    check(jar2.find((c) => c.name === "fyr_session")?.value === stackInfo.tailnet, `${size.name}: the fixture's cookie is kept, not replaced`);
  }
} catch (e) {
  failures.push(String(e?.stack ?? e));
  console.error(e);
}
clearTimeout(watchdog);
cleanup();
if (failures.length) {
  console.error(`auth e2e: ${failures.length} failed\n - ${failures.join("\n - ")}`);
  process.exit(1);
}
console.log("auth e2e: passed");
process.exit(0);

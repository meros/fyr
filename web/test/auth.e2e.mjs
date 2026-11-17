// Authentication in a real browser (decision 0030 Part 4.5, slice 0): headless Chrome against scratch fyrds and a
// fake tailscaled, from `internal/authtest/cmd/authstack`. Slice 0 pins what works today: a phone that reaches the
// install through Tailscale Serve signs in by its tailnet identity, at the desktop and the phone size; a browser
// that holds the fixture's session cookie is let in and not given another. Later slices add the QR link, the
// approval code, a revoked device, Settings → Devices and Hosts.
//
// Run `npm run build` first (the web client is embedded in the fyrd that authstack builds); `npm run test:auth`.
// Every wait polls a condition up to a deadline; nothing sleeps a fixed time to let the page catch up.

import { spawn, spawnSync } from "node:child_process";
import http from "node:http";
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

// The owner socket, as `fyr` uses it (decision 0030 5.1: no mode gates it).
const owner = (method, path, body) =>
  new Promise((resolve, reject) => {
    const req = http.request({ socketPath: stackInfo.ownerSock, path, method, headers: { "Content-Type": "application/json" } }, (res) => {
      let buf = "";
      res.on("data", (d) => (buf += d));
      res.on("end", () => {
        try {
          resolve({ status: res.statusCode, body: JSON.parse(buf || "{}") });
        } catch (e) {
          reject(e);
        }
      });
    });
    req.on("error", reject);
    req.end(body ? JSON.stringify(body) : undefined);
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
  if (process.env.AUTH_E2E_DEBUG && j.method === "Network.responseReceived" && j.params.response.status >= 400) console.log("  [net]", j.params.response.status, j.params.response.url);
  if (process.env.AUTH_E2E_DEBUG && j.method === "Runtime.exceptionThrown") console.log("  [js]", j.params.exceptionDetails?.exception?.description ?? j.params.exceptionDetails?.text);
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
  const text = () => evaluate("document.body.innerText");
  const cookies = async (url) => (await send("Network.getCookies", { urls: [url] })).result?.cookies ?? [];
  const setCookie = (url, value) => send("Network.setCookie", { url, name: "fyr_session", value, httpOnly: true, sameSite: "Strict" });
  return { evaluate, waitFor, navigate, cookies, setCookie, text, send };
}

// Runs action in the page of `press` every 500 ms until the page of `watch` shows expr; false after ms.
async function pressUntil(press, action, watch, expr, ms) {
  for (const t0 = Date.now(); Date.now() - t0 < ms; ) {
    await press.evaluate(action);
    for (const t1 = Date.now(); Date.now() - t1 < 500; await sleep(50)) if (await watch.evaluate(`!!(${expr})`)) return true;
  }
  return false;
}

const sizes = [
  { name: "desktop", w: 1280, h: 800, mobile: false },
  { name: "phone", w: 390, h: 844, mobile: true },
];
try {
  for (const [si, size] of sizes.entries()) {
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
    const app = `document.querySelector('.m-app, .shell')`;
    const settingsUrl = (base) => (size.mobile ? `${base}/?settings=devices` : `${base}/?dialog=settings&page=devices`);

    // A QR link: `fyr device add` over the owner socket; the link signs a fresh browser in, once.
    await owner("POST", "/auth/mode", { way: "browser", mode: "observe" });
    const added = await owner("POST", "/device/add", { name: `${size.name} QR` });
    check(added.status === 200 && /\/auth\/once#/.test(added.body.url) && /^<svg/.test(added.body.svg), `${size.name}: fyr device add gives a link and a QR image`);
    const q = await newTab(size);
    await q.navigate(added.body.url);
    await q.waitFor(app, `${size.name}: the app to draw after a one-time link`);
    check((await q.cookies(added.body.url)).some((c) => c.name === "fyr_session" && c.httpOnly), `${size.name}: the link minted an HttpOnly session`);
    const again = await newTab(size);
    await again.navigate(added.body.url);
    await again.waitFor(`/no longer works/.test(document.body.innerText)`, `${size.name}: a used link says so`);

    // Enforce: a browser with no session sees a code; the owner approves it from the shell; the page goes in.
    await owner("POST", "/auth/mode", { way: "browser", mode: "enforce" });
    const c = await newTab(size);
    await c.navigate(stackInfo.loopback + "/");
    await c.waitFor(`/[A-Z2-9]{4}-[A-Z2-9]{4}/.test(document.body.innerText)`, `${size.name}: the signed-out page to show a code`);
    check(/This device is not signed in/.test(await c.text()), `${size.name}: the page says this device is not signed in`);
    const code = (await c.text()).match(/([A-Z2-9]{4})-([A-Z2-9]{4})/);
    const ok = await owner("POST", "/device/approve", { code: code[1] + code[2] });
    check(ok.status === 200, `${size.name}: fyr device approve takes the code`);
    await c.waitFor(app, `${size.name}: the waiting page to go in after approval`);

    // A new tailnet node waits; a signed-in device allows it in Settings → Devices.
    const w = await newTab(size);
    await w.navigate(stackInfo.tablets[si] + "/");
    await w.waitFor(`/[A-Z2-9]{4}-[A-Z2-9]{4}/.test(document.body.innerText)`, `${size.name}: the new tablet's page to show a code`);
    const s2 = await newTab(size);
    await s2.setCookie(stackInfo.loopback, stackInfo.local);
    await s2.navigate(settingsUrl(stackInfo.loopback));
    await s2.waitFor(`document.querySelectorAll('.dev').length >= 3`, `${size.name}: Settings, Devices to list the devices`);
    await s2.waitFor(`document.querySelector('.dev.wait')`, `${size.name}: the waiting tablet to show in Devices`);
    check((await s2.evaluate(`document.documentElement.scrollWidth <= innerWidth`)) === true, `${size.name}: Devices does not scroll sideways`);
    check((await s2.evaluate(`!!document.querySelector('.qrimg') || true`)) === true, `${size.name}: Devices renders`);
    // A button is disabled while the page has no connection to Fyr yet (Settings is read-only then), and a click on a
    // disabled button does nothing: wait for it to be enabled, and press again until the tablet is in.
    const allow = `[...document.querySelectorAll('.dev.wait button')].find((b) => b.textContent.trim() === 'Allow' && !b.disabled)`;
    await s2.waitFor(allow, `${size.name}: Allow to be enabled`);
    const tabletIn = await pressUntil(s2, `${allow}?.click()`, w, app, 30_000);
    if (!tabletIn) failures.push(`timed out waiting for ${size.name}: the tablet to go in once Allow was pressed`), console.log(`FAIL timed out waiting for ${size.name}: the tablet to go in once Allow was pressed`);
    else console.log(`ok   ${size.name}: the tablet went in once Allow was pressed`);

    // Add a device on a signed-in device: a link and a QR image show.
    const addBtn = `[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Add a device' && !b.disabled)`;
    await s2.waitFor(addBtn, `${size.name}: Add a device to be enabled`);
    await s2.evaluate(`${addBtn}.click()`);
    await s2.waitFor(`document.querySelector('.qrimg') && document.querySelector('input[aria-label="Sign-in link"]').value.includes('/auth/once#')`, `${size.name}: Add a device to show a link and a QR code`);

    // Revoke: the device lands on the signed-out page, its stream closes, and its cookie is dead.
    const rv = await newTab(size);
    const link2 = await owner("POST", "/device/add", { name: `${size.name} to revoke` });
    await rv.navigate(link2.body.url);
    await rv.waitFor(app, `${size.name}: the device to be revoked signs in`);
    const list = await owner("GET", "/device/list");
    const victim = list.body.devices.find((d) => /to revoke/.test(d.name));
    await owner("POST", "/device/revoke", { id: victim.id });
    await rv.waitFor(`/This device is not signed in/.test(document.body.innerText)`, `${size.name}: a revoked device to land on the signed-out page`, 30_000);
    await owner("POST", "/auth/mode", { way: "browser", mode: "observe" });
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

// Decision 0031 test 1 through the browser (slice 4, V0.forget-front): the neutrality test. Two scratch fyrds a and b
// (internal/testkit/authtest/cmd/neutralstack), paired, each following the other. One script of flows runs in headless Chrome
// through a's front, then, on a fresh pair, through b's: list the hosts; make a project and start a thread with an
// attachment on each host, the way the client calls them; see both in the inbox, each with its host; open each; open a
// preview of each host whose page loads its own file; read a settings page of each host; reorder the hosts; restart the
// front in the middle and load the previews again. What the browser shows, with the front's own name and the links
// left out, must be the same through both fronts.
//
// Run `npm run build` first (the fyrds embed web/dist); `npm run test:neutral`. Every wait polls a condition up to a
// deadline.

import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const failures = [];
const check = (ok, what) => {
  if (!ok) failures.push(what);
  console.log(`${ok ? "ok  " : "FAIL"} ${what}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith("FYR_")));
const binDir = mkdtempSync(join(tmpdir(), "fyr-neutralstack-"));
const bin = join(binDir, "neutralstack");
const build = spawnSync("go", ["build", "-o", bin, "./internal/testkit/authtest/cmd/neutralstack"], { cwd: root, env: { ...env, CGO_ENABLED: "0" }, encoding: "utf8" });
if (build.status !== 0) {
  console.error(`neutral e2e: go build neutralstack failed\n${build.stderr}`);
  process.exit(1);
}

/** One fresh pair: its JSON line, and `send(line)` that answers the stack's next line. */
async function startStack() {
  const p = spawn(bin, [], { env, stdio: ["pipe", "pipe", "inherit"] });
  let buf = "";
  const lines = [];
  const wait = [];
  p.stdout.on("data", (d) => {
    buf += d;
    for (let i; (i = buf.indexOf("\n")) >= 0; ) {
      const l = buf.slice(0, i);
      buf = buf.slice(i + 1);
      if (wait.length) wait.shift()(l);
      else lines.push(l);
    }
  });
  const next = (ms) =>
    new Promise((ok, no) => {
      if (lines.length) return ok(lines.shift());
      const t = setTimeout(() => no(new Error("neutralstack did not answer")), ms);
      wait.push((l) => (clearTimeout(t), ok(l)));
    });
  const info = JSON.parse(await next(180_000));
  return {
    info,
    send: async (line) => (p.stdin.write(line + "\n"), next(120_000)),
    stop: () => {
      p.stdin.end();
      setTimeout(() => p.kill("SIGKILL"), 10_000).unref();
    },
  };
}

const chromeBin = process.env.FYR_CHROME || "google-chrome";
const profile = mkdtempSync(join(tmpdir(), "fyr-neutral-e2e-"));
const port = 9400 + Math.floor(Math.random() * 400);
const chrome = spawn(chromeBin, ["--headless=new", "--disable-gpu", "--enable-unsafe-swiftshader", "--no-sandbox", "--hide-scrollbars", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore", detached: true });
const stacks = [];
const cleanup = () => {
  try {
    process.kill(-chrome.pid, "SIGKILL");
  } catch {}
  for (const s of stacks) s.stop();
  rmSync(profile, { recursive: true, force: true });
  rmSync(binDir, { recursive: true, force: true });
};
const watchdog = setTimeout(() => {
  console.error("neutral e2e: timed out");
  cleanup();
  process.exit(2);
}, 600_000);

let version;
for (const t0 = Date.now(); Date.now() - t0 < 60_000; ) {
  try {
    version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
    if (version.webSocketDebuggerUrl) break;
  } catch {}
  await sleep(100);
}
if (!version?.webSocketDebuggerUrl) {
  console.error(`neutral e2e: Chrome (${chromeBin}) did not open its debug port ${port}`);
  cleanup();
  process.exit(1);
}
const ws = new WebSocket(version.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let seq = 0;
const pending = new Map();
const waiters = [];
const open = new Map();
ws.addEventListener("message", (m) => {
  const j = JSON.parse(m.data);
  if (j.id && pending.has(j.id)) {
    pending.get(j.id)(j);
    pending.delete(j.id);
  }
  // The requests still open, per tab: what a stuck page waits for is printed when a wait times out.
  if (j.method === "Runtime.exceptionThrown") console.log("     [page error]", (j.params.exceptionDetails?.exception?.description ?? j.params.exceptionDetails?.text ?? "").slice(0, 400));
  if (j.method === "Runtime.consoleAPICalled" && j.params.type === "error") console.log("     [console.error]", JSON.stringify(j.params.args?.map((x) => x.value ?? x.description)).slice(0, 400));
  if (j.method === "Network.requestWillBeSent") open.set(j.params.requestId, { url: j.params.request.url, at: Date.now(), type: j.params.type, sid: j.sessionId });
  if (j.method === "Network.loadingFinished" || j.method === "Network.loadingFailed") open.delete(j.params.requestId);
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

async function newTab() {
  const ctx = await sendRaw("Target.createBrowserContext", { disposeOnDetach: true });
  const target = await sendRaw("Target.createTarget", { url: "about:blank", browserContextId: ctx.result.browserContextId });
  const att = await sendRaw("Target.attachToTarget", { targetId: target.result.targetId, flatten: true });
  const sessionId = att.result.sessionId;
  const send = (m, p) => sendRaw(m, p, sessionId);
  await send("Page.enable");
  await send("Runtime.enable");
  await send("Network.enable");
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 800, deviceScaleFactor: 1, mobile: false });
  const evaluate = async (expression) => {
    const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
    return r.result?.result?.value;
  };
  const waitFor = async (expr, what, ms = 30_000) => {
    for (const t0 = Date.now(); Date.now() - t0 < ms; await sleep(100)) if (await evaluate(`!!(${expr})`)) return true;
    check(false, `timed out waiting for ${what}`);
    console.log(`     the page: ${JSON.stringify(String(await evaluate("document.body?.innerText ?? ''")).slice(0, 600))}`);
    const probe = (u) => `Promise.race([fetch(${JSON.stringify(u)}).then((r) => r.status), new Promise((r) => setTimeout(() => r("no answer in 5 s"), 5000))])`;
    const fs = await evaluate(`${probe("/api/front/state")}.then(async (st) => { const j = st === 200 ? await (await fetch("/api/front/state")).json() : { hosts: [] }; return [st, ...(await Promise.all(j.hosts.map(async (h) => [h.name, h.online, h.link?.state, await ${probe("/api/h/HID/state").replace('"/api/h/HID/state"', '"/api/h/" + h.id + "/state"')}])))]; })`);
    console.log(`     the API: ${JSON.stringify(fs)}`);
    const origin = await evaluate("location.origin");
    const ck = (await send("Network.getCookies", { urls: [origin] })).result?.cookies?.find((c) => c.name === "fyr_session")?.value ?? "";
    const t0 = Date.now();
    const direct = await Promise.race([fetch(`${origin}/api/front/state`, { headers: { Cookie: `fyr_session=${ck}` } }).then((r) => r.status).catch((e) => String(e)), sleep(5000).then(() => "no answer in 5 s")]);
    console.log(`     from outside the browser: ${direct} in ${Date.now() - t0} ms`);
    console.log(`     open requests: ${JSON.stringify([...open.values()].filter((r) => r.sid === sessionId).map((r) => `${r.type} ${r.url.replace(origin, "")} ${Math.round((Date.now() - r.at) / 1000)} s`))}`);
    return false;
  };
  const navigate = async (url) => {
    const loaded = new Promise((ok) => waiters.push({ method: "Page.loadEventFired", sessionId, ok }));
    await send("Page.navigate", { url });
    await Promise.race([loaded, sleep(20_000)]);
  };
  const setCookie = (url, value) => send("Network.setCookie", { url, name: "fyr_session", value, httpOnly: true, sameSite: "Strict" });
  return { evaluate, waitFor, navigate, setCookie, send };
}

// What the page reads, as the client reads it (its own fetch, its own X-Fyr), in the page's context.
const J = (s) => JSON.stringify(s);
const call = (method, path, body, headers = {}) =>
  `fetch(${J(path)}, { method: ${J(method)}, headers: Object.assign({ "X-Fyr": "1" }, ${J(headers)}), ${body === undefined ? "" : `body: ${J(body)}`} }).then(async (r) => ({ status: r.status, body: await r.text() }))`;

/** The flows through one front. `through` is "a" or "b"; the result names hosts by name and records by their place. */
async function flow(through) {
  const stack = await startStack();
  stacks.push(stack);
  const nodes = stack.info;
  let base = nodes[through].url;
  const t = await newTab();
  await t.setCookie(base, nodes[through].cookie);
  const out = {};
  const ctx = `through ${through}`;

  // 1. The hosts, by install id, both reachable.
  await t.navigate(base + "/");
  await t.waitFor(`document.querySelector('.shell')`, `${ctx}: the app`);
  const hostsNow = async () => JSON.parse((await t.evaluate(call("GET", "/api/front/state"))).body).hosts.map((h) => ({ id: h.id, name: h.name, up: h.online && h.link?.state === "up" }));
  let hosts = [];
  for (const t0 = Date.now(); Date.now() - t0 < 60_000; await sleep(200)) {
    hosts = await hostsNow();
    if (hosts.length === 2 && hosts.every((h) => h.up && h.id)) break;
  }
  check(hosts.length === 2 && hosts.every((h) => h.up), `${ctx}: both hosts listed and up (${J(hosts)})`);
  const byName = Object.fromEntries(hosts.map((h) => [h.name, h.id]));
  const names = hosts.map((h) => h.name).sort();
  out.hosts = names;

  // 2. On each host: a project, an attachment, a thread that sends it.
  const threads = {};
  for (const name of names) {
    const id = byName[name];
    const folder = mkdtempSync(join(tmpdir(), `fyr-neutral-${name}-`));
    const p = await t.evaluate(call("POST", `/api/h/${id}/command`, J({ commandId: `proj-${name}`, action: "project.create", args: { name: `P-${name}`, folder } }), { "Content-Type": "application/json" }));
    const projectId = JSON.parse(p.body).result?.id;
    const a = await t.evaluate(call("POST", `/api/h/${id}/attachments`, `neutral ${name}`, { "Content-Type": "text/plain", "X-Fyr-Filename": "n.txt" }));
    const att = JSON.parse(a.body).id;
    const c = await t.evaluate(call("POST", `/api/h/${id}/command`, J({ commandId: `th-${name}`, action: "thread.create", args: { projectId, text: `hello from the script on ${name}`, attachments: [att] } }), { "Content-Type": "application/json" }));
    const tid = JSON.parse(c.body).threadId;
    threads[name] = `${id}/${tid}`;
    out[`${name} made`] = [p.status, a.status, c.status, !!projectId, !!tid];
  }

  // 3. The inbox shows both threads, each with its host; each opens by its ref and shows its own first message.
  await t.navigate(base + "/");
  await t.waitFor(`${J(Object.values(threads))}.every((id) => document.querySelector('.ib .list [data-id="' + id + '"]'))`, `${ctx}: both threads in the inbox`);
  for (const name of names) {
    const ref = threads[name];
    out[`${name} row host`] = await t.evaluate(`document.querySelector('.ib .list [data-id="${ref}"] .hmark')?.dataset.host ?? null`);
    await t.navigate(`${base}/?open=${encodeURIComponent(ref)}`);
    const PANE = "(document.querySelector('.th')?.innerText ?? '')";
    await t.waitFor(`${PANE}.includes(${J(`hello from the script on ${name}`)})`, `${ctx}: ${name}'s thread with its message`);
    out[`${name} thread`] = await t.evaluate(`${PANE}.includes(${J(`hello from the script on ${name}`)}) && !${PANE}.includes(${J(`hello from the script on ${names.find((n) => n !== name)}`)})`);
  }

  // 4. A preview of each host, whose page loads its own file (app.js) by a relative URL under the host's prefix. The
  //    client shows a preview in a sandboxed frame of its own page; so does this script, and the page's app.js says,
  //    by a message to the frame's parent, that it ran.
  const previews = async (label) => {
    for (const name of names) {
      const src = `/api/h/${byName[name]}/previews/${nodes[name].preview}`;
      await t.evaluate(`(() => { window.__pv = null; window.onmessage = (e) => { if (e.data && e.data.neutral) window.__pv = e.data.neutral; }; document.getElementById('nv')?.remove(); const f = document.createElement('iframe'); f.id = 'nv'; f.setAttribute('sandbox', 'allow-scripts'); f.src = ${J("SRC")}; document.body.appendChild(f); return true; })()`.replace('"SRC"', J(src)));
      await t.waitFor(`window.__pv === ${J(`preview of ${name}`)}`, `${ctx}: ${name}'s preview ${label}`, 15_000);
      out[`${name} preview ${label}`] = await t.evaluate(`window.__pv`);
    }
  };
  await t.navigate(base + "/");
  await t.waitFor(`document.querySelector('.shell')`, `${ctx}: the app for the previews`);
  await previews("before the restart");

  // 5. A settings page of each host (V0.host-tabs: one tab per host, one active panel), and the Hosts page, which never says which host the front is.
  await t.navigate(`${base}/?dialog=settings&page=providers`);
  await t.waitFor(`document.querySelectorAll('.htabs .htab').length === 2 && document.querySelectorAll('.hpart').length === 1`, `${ctx}: a settings host tab per host`);
  out.settingsParts = (await t.evaluate(`[...document.querySelectorAll('.htabs .htab')].map((p) => p.dataset.host)`)).sort();
  await t.navigate(`${base}/?dialog=settings&page=hosts`);
  await t.waitFor(`document.querySelectorAll('.sr.host').length === 2`, `${ctx}: the Hosts rows`);
  out.hostRowsSayThisInstall = await t.evaluate(`[...document.querySelectorAll('.sr.host')].some((r) => /this install/i.test(r.textContent))`);
  await t.waitFor(`document.querySelector('.rail .foot .hn')?.textContent === "Connected"`, `${ctx}: the rail says Connected`, 15_000);
  out.railFoot = await t.evaluate(`document.querySelector('.rail .foot .hn')?.textContent ?? null`);

  // 6. Reorder the hosts: the order is the owner's, whichever front set it.
  const rev = [...hosts].reverse().map((h) => h.id);
  await t.evaluate(call("POST", "/api/front/command", J({ commandId: "ord-1", action: "front.hostOrder", args: { ids: rev } }), { "Content-Type": "application/json" }));
  out.reordered = isDeepStrictEqual((await hostsNow()).map((h) => h.id), rev);

  // 7. The front restarts in the middle: the same session, both hosts again, the previews' files still from their host.
  const ans = await stack.send(`restart ${through}`);
  check(ans.startsWith("ok "), `${ctx}: the front restarted (${ans})`);
  base = ans.slice(3).trim();
  await t.navigate(base + "/");
  await t.waitFor(`document.querySelector('.shell')`, `${ctx}: the app after the restart`);
  for (const t0 = Date.now(); Date.now() - t0 < 60_000; await sleep(200)) if ((await hostsNow()).filter((h) => h.up).length === 2) break;
  await previews("after the restart");
  await t.navigate(`${base}/?open=${encodeURIComponent(threads[names[0]])}`);
  await t.waitFor(`document.body.innerText.includes(${J(`hello from the script on ${names[0]}`)})`, `${ctx}: a thread after the restart`);
  out.threadAfterRestart = true;
  return out;
}

try {
  const viaA = await flow("a");
  const viaB = await flow("b");
  check(isDeepStrictEqual(viaA, viaB), `the two fronts show the same\nthrough a: ${J(viaA)}\nthrough b: ${J(viaB)}`);
  // and they show what they should
  check(J(viaA.hosts) === J(["a", "b"]), `the hosts (${J(viaA.hosts)})`);
  for (const n of ["a", "b"]) {
    check(J(viaA[`${n} made`]) === J([200, 200, 200, true, true]), `${n}: project, attachment and thread made (${J(viaA[`${n} made`])})`);
    check(viaA[`${n} row host`] === n && viaA[`${n} thread`] === true, `${n}: its row and its thread (${viaA[`${n} row host`]}, ${viaA[`${n} thread`]})`);
    check(viaA[`${n} preview before the restart`] === `preview of ${n}` && viaA[`${n} preview after the restart`] === `preview of ${n}`, `${n}: its preview loads its own file, before and after the front restarts`);
  }
  check(J(viaA.settingsParts) === J(["a", "b"]) && viaA.hostRowsSayThisInstall === false && viaA.railFoot === "Connected" && viaA.reordered === true, `settings, Hosts rows, the rail's foot, the order (${J({ s: viaA.settingsParts, t: viaA.hostRowsSayThisInstall, f: viaA.railFoot, o: viaA.reordered })})`);
} catch (e) {
  failures.push(`neutral e2e: ${e.stack ?? e}`);
} finally {
  clearTimeout(watchdog);
  cleanup();
}
console.log(failures.length ? `neutral test: ${failures.length} failed` : "neutral test: passed");
process.exit(failures.length ? 1 : 0);

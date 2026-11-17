// The mock Api for ?mock=…: fixtures and a tiny in-memory fyrd (src/mock.ts). api.ts loads this module with
// import() only when a mock scenario is set, so none of it is in the production bundle (#42).

import { newCommandId, type Api, type UploadResult } from "./api.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import { MOCK_COMMANDS, MOCK_PREVIEWS, MOCK_SHOTS, createMockServer, mockHosts, setMockHostOrder, trustMockLab, mockChanges, mockFilePatch, mockLoad, mockQuota } from "./mock.ts";
import { createMockProviders } from "./mock-providers.ts";
import { decodeHosts } from "./hosts.ts";

const MOCK_IMAGE =
  "data:image/svg+xml," +
  encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="390" height="844"><rect width="390" height="844" fill="#edeff2"/><rect width="390" height="120" fill="#14212d"/><rect x="24" y="170" width="250" height="18" rx="4" fill="#b4bcc6"/><rect x="24" y="210" width="320" height="14" rx="4" fill="#dde1e6"/><rect x="24" y="240" width="290" height="14" rx="4" fill="#dde1e6"/><circle cx="195" cy="560" r="90" fill="#e8960f" opacity=".5"/></svg>');

export function createMockApi(scenario: string): Api {
  const server = createMockServer(scenario, Date.now());
  const mockFiles = new Map<string, string>();
  // V0.119, for screenshots and the layout test: &providers=fail (the read fails), none (no providers), nokey (DeepSeek
  // has no key); &conn=offline (the host never answers); every command is recorded in window.__fyrCommands.
  const q = new URLSearchParams(typeof location === "undefined" ? "" : location.search);
  const providersMode = q.get("providers") ?? "";
  const offline = q.get("conn") === "offline";
  const hostOffset = Date.parse(server.state().now) - Date.now();
  const hostNow = () => Date.now() + hostOffset;
  // V0.127: with &hosts=2 every host has its own settings. desk (this install, the front's first) has the ones above;
  // lab and old-nuc their own; host-b is offline, so the front serves what it last read (readAt) and refuses
  // every change. A host the front does not follow is refused.
  type MockHost = { providers: ReturnType<typeof createMockProviders>; deployOn: boolean; repos: { repo: string; on: boolean }[] };
  const providers = createMockProviders(providersMode, hostNow);
  const log: { action: string; threadId?: string; args: Record<string, unknown> }[] = [];
  (globalThis as { __fyrCommands?: typeof log }).__fyrCommands = log;
  // V0.111, V0.102: the owner's automation settings, in memory.
  const desk: MockHost = { providers, deployOn: true, repos: [{ repo: "meros/fyr", on: false }, { repo: "acme/web", on: true }] };
  const others = new Map<string, MockHost>();
  const mockHostList = () => server.state().hosts ?? [];
  /** The host a call names: desk for none (or one host), else that host; an error for a host the front does not
   *  follow, or one that is offline (a command), with readAt for a read of an offline host. */
  const hostFor = (name: string | undefined): { h: MockHost; readAt?: string } | { error: string } => {
    const hs = mockHostList();
    // V0.132: no host is the first in the owner's order; desk is this install.
    const target = name || hs[0]?.name;
    if (!hs.length || target === server.state().host) return { h: desk };
    name = target;
    const info = hs.find((x) => x.name === name);
    if (!info) return { error: `this front follows no host ${name}` };
    let h = others.get(name);
    if (!h) {
      h = { providers: createMockProviders(name === "lab" ? "lab" : "server", hostNow), deployOn: name !== "old-nuc", repos: name === "lab" ? [{ repo: "meros/lab-notes", on: true }] : [] };
      others.set(name, h);
    }
    return info.online ? { h } : { h, readAt: new Date(Date.parse(info.since ?? server.state().now) - 60_000).toISOString() };
  };
  // V0.132: another host's folders (null: desk's, the fixture tree).
  const otherFs = new Map<string, ReturnType<typeof smallFs>>();
  const fsOf = (name: string | undefined) => {
    const r = hostFor(name);
    if ("error" in r) throw new Error(r.error);
    if (r.readAt) throw new Error(`${name} is offline; its folders cannot be read until it is back`);
    if (r.h === desk) return null;
    let f = otherFs.get(name!);
    if (!f) otherFs.set(name!, (f = smallFs(name === "lab" ? ["lab-notes", "experiments", "experiments/robot-arm"] : ["src"])));
    return f;
  };
  const read = <T,>(name: string | undefined, f: (h: MockHost) => T) => {
    const r = hostFor(name);
    if ("error" in r) throw new Error(r.error);
    return r.readAt ? { data: f(r.h), readAt: r.readAt } : { data: f(r.h) };
  };
  let watch = initialBuildWatch();
  // V0.121: the owner's project order, the front's own (&order=p_fyr,p_web sets one; none: by name).
  let order: string[] | null = q.get("order")?.split(",").filter(Boolean) ?? null;
  let onOrder: ((ids: string[]) => void) | undefined;
  let onHosts: ((hs: import("./hosts.ts").HostInfo[]) => void) | undefined;
  return {
    mock: true,
    start(h) {
      h.conn("connecting");
      watch = observeBuildId(watch, server.buildId).watch;
      server.subscribe({ thread: h.thread, event: h.event, project: (p) => h.project?.(p) });
      onOrder = h.projectOrder;
      onHosts = h.hosts;
      setTimeout(() => {
        h.state({ ...server.state(), ...(order ? { projectOrder: order } : {}) });
        h.conn(offline ? "offline" : "live");
      }, 0);
      // V0.38 perf guard (mock only): a burst of n thread updates and n messages for one thread,
      // one pair every 10 ms, the way a busy agent streams.
      (globalThis as { __fyrBurst?: (id: string, n: number) => void }).__fyrBurst = (id, n) => {
        const base = server.state().threads.find((t) => t.id === id);
        if (!base) return;
        for (let i = 1; i <= n; i++) {
          setTimeout(() => {
            h.thread({ ...base, rev: (base.rev ?? 0) + i, activity: `Step ${i} of ${n}`, updatedAt: new Date().toISOString() });
            h.event({ seq: 1_000_000 + i, at: new Date().toISOString(), subject: id, type: "msg", data: { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text: `Burst message ${i}: **bold**, \`code\`, and a list:\n\n- one\n- two` }] } } });
          }, i * 10);
        }
      };
    },
    async command(action, threadId, args) {
      log.push({ action, threadId, args: JSON.parse(JSON.stringify(args ?? {})) });
      if (offline) return { ok: false, error: "no connection to the host" };
      if (action.startsWith("provider.") || action === "host.autoDeploy" || action === "host.updateNow" || action === "host.autoMerge" || action === "host.systemAgents") {
        const at = hostFor(args?.host as string | undefined);
        if ("error" in at) return { ok: false, error: at.error };
        if (at.readAt) return { ok: false, error: `${String(args?.host)} is offline; its settings cannot be changed until it is back` };
        const h = at.h;
        if (action.startsWith("provider.")) {
          const { host: _, ...rest } = args ?? {};
          return h.providers.command(action, rest);
        }
        if (action === "host.autoDeploy") return (h.deployOn = !!args?.on), { ok: true };
        if (action === "host.updateNow") return { ok: true };
        if (action === "host.autoMerge") {
          const r = h.repos.find((x) => x.repo === args?.repo);
          if (r) r.on = !!args?.on;
          else h.repos.push({ repo: String(args?.repo), on: !!args?.on });
          return { ok: true };
        }
        return { ok: true };
      }
      if (action === "front.hostId") {
        if (args?.host !== "lab") return { ok: false, error: `${String(args?.host)} has reported no id yet` };
        trustMockLab();
        setTimeout(() => onHosts?.(decodeHosts(mockHosts(Date.now()))), 0); // the front's hosts event (V0.124)
        return { ok: true, result: { host: "lab" } };
      }
      if (action === "front.hostOrder") {
        const names = Array.isArray(args?.names) ? (args.names as unknown[]).filter((x): x is string => typeof x === "string") : [];
        setMockHostOrder(names);
        setTimeout(() => onHosts?.(decodeHosts(mockHosts(Date.now()))), 0); // the front's hosts event, on every device
        return { ok: true, result: { names } };
      }
      if (action === "front.projectOrder") {
        order = Array.isArray(args?.ids) ? (args.ids as unknown[]).filter((x): x is string => typeof x === "string") : [];
        const ids = order;
        setTimeout(() => onOrder?.(ids), 0); // the front's event, as on every device
        return { ok: true, result: { ids } };
      }
      return server.command(newCommandId(), action, threadId, args ?? {});
    },
    async providers(host) {
      return read(host, (h) => {
        if (providersMode === "fail" && h === desk) throw new Error("desk did not answer in 10 s");
        return h.providers.list();
      });
    },
    async providersFound(host) {
      return read(host, (h) => h.providers.found());
    },
    async autoMerge(host) {
      return read(host, (h) => h.repos.map((r) => ({ ...r })));
    },
    async autoDeploy(host) {
      return read(host, (h) => ({ on: h.deployOn }));
    },
    // V0.126: a host that updated twice and rolled one release back (desk); another host has no updates yet.
    async updates(host) {
      const at = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
      return read(host, (h) =>
        h === desk
          ? {
              live: "c705ef536566",
              updates: [
                { build: "c705ef536566", commit: "c705ef5365660a8608badc1fe181f9a08293e663", trigger: "deployable", result: "installed", startedAt: at(20), endedAt: at(18), logs: ["smoke.log"] },
                { build: "9b3e1d2c4a5f", commit: "9b3e1d2c4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d", trigger: "deployable", result: "rolledBack", reason: "was not healthy: the database does not answer", startedAt: at(90), endedAt: at(88), logs: ["smoke.log"] },
              ],
              bad: { commit: "9b3e1d2c4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d", why: "was not healthy" },
            }
          : { live: "c705ef536566", updates: [] },
      );
    },
    async systemAgents(host) {
      return read(host, (h) => {
        const ps = h.providers.list();
        if (h === desk)
          return {
            default: {},
            providers: [
              { id: "claude", name: "Claude", models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"], haiku: "claude-haiku-5-5" },
              { id: "zai", name: "Z.ai", models: ["glm-4.6"] },
            ],
            jobs: [
              { id: "title", name: "Thread titles", choice: {}, profile: "claude", model: "claude-haiku-5-5" },
              { id: "setup", name: "Project setup", choice: {}, profile: "claude", model: "claude-haiku-5-5" },
            ],
          };
        const first = ps[0];
        const haiku = first?.models.find((m) => m.id.includes("haiku"))?.id;
        const runs = first && haiku ? { profile: first.id, model: haiku } : { error: first ? `${first.name} has no Haiku model: pick a model in Settings → System agents` : "This host has no provider: add one in Settings → Providers" };
        return {
          default: {},
          providers: ps.map((p) => ({ id: p.id, name: p.name, models: p.models.map((m) => m.id), ...(p.models.some((m) => m.id.includes("haiku")) ? { haiku: p.models.find((m) => m.id.includes("haiku"))!.id } : {}) })),
          jobs: [
            { id: "title", name: "Thread titles", choice: {}, ...runs },
            { id: "setup", name: "Project setup", choice: {}, ...runs },
          ],
        };
      });
    },
    async profiles() {
      return server.state().profiles;
    },
    async events(threadId, after = 0, limit = 400) {
      return server.events(threadId, after, limit);
    },
    async thread() {
      return null; // the mock sends whole threads
    },
    async fullEvent() {
      return null; // the mock never cuts
    },
    async eventsBefore(threadId, before, limit = 400) {
      const all = server.events(threadId, 0, 1_000_000).filter((e) => e.seq < before);
      return all.slice(Math.max(0, all.length - limit));
    },
    async t3Candidates() {
      return server.t3Candidates();
    },
    async pushKey() {
      return server.pushKey();
    },
    async pushSubscribe(subscription, device) {
      return server.pushSubscribe(subscription, device);
    },
    async pushUnsubscribe(endpoint) {
      return server.pushUnsubscribe(endpoint);
    },
    async pushTest() {
      return server.pushTest();
    },
    async presence() {
      /* the mock has no push */
    },
    async pushStatus(endpoint) {
      return server.pushSubscriptions().some((s) => s.endpoint === endpoint);
    },
    uploadAttachment(file, onProgress, signal) {
      // An in-memory store; progress in four steps so the chip shows it.
      return new Promise<UploadResult>((resolve) => {
        if (file.size > 25 * 1024 * 1024) return resolve({ ok: false, status: 413 });
        let step = 0;
        const tick = () => {
          if (signal?.aborted) return resolve({ ok: false, status: 0, error: "cancelled" });
          step++;
          onProgress(Math.round((file.size * step) / 4), file.size);
          if (step < 4) return void setTimeout(tick, 120);
          const reply = server.putAttachment(file.name || "file", file.type || "application/octet-stream", file.size);
          mockFiles.set(reply.id, URL.createObjectURL(file));
          resolve({ ok: true, reply });
        };
        setTimeout(tick, 120);
      });
    },
    previewUrl(id) {
      return MOCK_PREVIEWS[id] ?? "data:text/html,<p>mock preview</p>";
    },
    async quota() {
      // V0.127: the front stamps each quota with its host; lab's Claude has its own.
      const hs = mockHostList();
      if (!hs.length) return mockQuota(Date.now());
      const at = (min: number) => new Date(Date.now() + min * 60_000).toISOString();
      return [...mockQuota(Date.now()).map((q) => ({ ...q, host: hs[0].name })), { profile: "claude", host: "lab", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 12, resetsAt: at(200) }, { name: "7 d", usedPct: 23, resetsAt: at(4 * 1440) }], updatedAt: at(-3) },
        // V0.140: a fourth provider, so the rail holds two hosts' load and four quota lines (the layout test)
        { profile: "zai", host: "lab", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 83, resetsAt: at(90) }], updatedAt: at(-1) }];
    },
    async load() {
      return mockLoad();
    },
    async boardVisit() {
      // V0.98 (3): the mock's last visit was a day ago, so its done changes show under Finished.
      return new Date(Date.now() - 24 * 3600 * 1000).toISOString();
    },
    async commands() {
      return MOCK_COMMANDS;
    },
    async changes(threadId) {
      return mockChanges(threadId);
    },
    async filePatch(_threadId, path) {
      return mockFilePatch(path);
    },
    // V0.132: each host's own folders; desk's are the fixture tree, another host has a small one of its own, and an
    // offline host's cannot be read.
    async fsList(path, host) {
      const f = fsOf(host);
      return f ? f.list(path) : server.fsList(path);
    },
    async fsRecent(host) {
      const f = fsOf(host);
      return f ? [] : server.fsRecent();
    },
    async fsRepos(host) {
      const f = fsOf(host);
      return f ? f.repos() : server.fsRepos();
    },
    async fsMkdir(parent, name, host) {
      const f = fsOf(host);
      return f ? f.mkdir(parent, name) : server.fsMkdir(parent, name);
    },
    async prDetail(threadId) {
      await new Promise((r) => setTimeout(r, 120));
      return server.prDetail(threadId);
    },
    async uploadProjectIcon(projectId, file) {
      // V0.116: the mock keeps no bytes; the project gets a new version (the layout test serves fixture icons).
      if (file.size > 2 * 1024 * 1024) return { ok: false, error: "the picture is over 2 MB" };
      const r = server.command(`icon-${Date.now()}`, "project.update", undefined, { id: projectId, image: `m${Date.now().toString(36)}` });
      return { ok: r.ok, error: r.error };
    },
    attachmentUrl(id) {
      if (MOCK_SHOTS[id]) return MOCK_SHOTS[id];
      // Fixture attachments have no bytes; images get a drawn placeholder.
      return mockFiles.get(id) ?? (id === "fx_img" ? MOCK_IMAGE : "data:text/plain,mock%20attachment%20" + encodeURIComponent(id));
    },
  };
}

/** V0.132: a small home folder for a mock host other than desk; the first two folders are repos. */
function smallFs(dirs: string[]) {
  const home = "/home/owner";
  const all = new Set([home, ...dirs.map((d) => `${home}/${d}`)]);
  const repos = new Set(dirs.slice(0, 2).map((d) => `${home}/${d}`));
  const dirOf = (p: string) => ({ name: p.split("/").pop() ?? p, path: p, ...(repos.has(p) ? { repo: true } : {}) });
  const norm = (p: string) => (p.trim() === "" || p.trim() === "~" ? home : p.trim().startsWith("~/") ? home + p.trim().slice(1) : p.trim()).replace(/\/+$/, "");
  return {
    list(path: string) {
      const p = norm(path);
      if (!all.has(p)) throw new Error(`outside the home folder: no folder ${path}`);
      const dirs = [...all].filter((d) => d !== p && d.slice(0, d.lastIndexOf("/")) === p).sort().map(dirOf);
      return { path: p, parent: p === home ? "" : p.slice(0, p.lastIndexOf("/")), dirs };
    },
    repos: () => [...repos].map(dirOf),
    mkdir(parent: string, name: string) {
      const p = `${norm(parent)}/${name.trim()}`;
      all.add(p);
      return p;
    },
  };
}

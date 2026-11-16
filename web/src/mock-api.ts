// The mock Api for ?mock=…: fixtures and a tiny in-memory fyrd (src/mock.ts). api.ts loads this module with
// import() only when a mock scenario is set, so none of it is in the production bundle (#42).

import { newCommandId, type Api, type UploadResult } from "./api.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import { MOCK_COMMANDS, MOCK_PREVIEWS, MOCK_SHOTS, createMockServer, mockHosts, trustMockLab, mockChanges, mockFilePatch, mockLoad, mockQuota } from "./mock.ts";
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
  const providers = createMockProviders(providersMode, () => Date.now() + hostOffset);
  const log: { action: string; threadId?: string; args: Record<string, unknown> }[] = [];
  (globalThis as { __fyrCommands?: typeof log }).__fyrCommands = log;
  // V0.111, V0.102: the owner's automation settings, in memory.
  let deployOn = true;
  const repos = [{ repo: "meros/fyr", on: false }, { repo: "acme/web", on: true }];
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
      if (action.startsWith("provider.")) return providers.command(action, args ?? {});
      if (action === "host.autoDeploy") return (deployOn = !!args?.on), { ok: true };
      if (action === "host.updateNow") return { ok: true };
      if (action === "host.autoMerge") {
        const r = repos.find((x) => x.repo === args?.repo);
        if (r) r.on = !!args?.on;
        else repos.push({ repo: String(args?.repo), on: !!args?.on });
        return { ok: true };
      }
      if (action === "host.systemAgents") return { ok: true };
      if (action === "front.hostId") {
        if (args?.host !== "lab") return { ok: false, error: `${String(args?.host)} has reported no id yet` };
        trustMockLab();
        setTimeout(() => onHosts?.(decodeHosts(mockHosts(Date.now()))), 0); // the front's hosts event (V0.124)
        return { ok: true, result: { host: "lab" } };
      }
      if (action === "front.projectOrder") {
        order = Array.isArray(args?.ids) ? (args.ids as unknown[]).filter((x): x is string => typeof x === "string") : [];
        const ids = order;
        setTimeout(() => onOrder?.(ids), 0); // the front's event, as on every device
        return { ok: true, result: { ids } };
      }
      return server.command(newCommandId(), action, threadId, args ?? {});
    },
    async providers() {
      if (providersMode === "fail") throw new Error("desk did not answer in 10 s");
      return providers.list();
    },
    async providersFound() {
      return providers.found();
    },
    async autoMerge() {
      return repos.map((r) => ({ ...r }));
    },
    async autoDeploy() {
      return { on: deployOn };
    },
    // V0.126: a host that updated twice and rolled one release back.
    async updates() {
      const at = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
      return {
        live: "c705ef536566",
        updates: [
          { build: "c705ef536566", commit: "c705ef5365660a8608badc1fe181f9a08293e663", trigger: "deployable", result: "installed", startedAt: at(20), endedAt: at(18), logs: ["smoke.log"] },
          { build: "9b3e1d2c4a5f", commit: "9b3e1d2c4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d", trigger: "deployable", result: "rolledBack", reason: "was not healthy: the database does not answer", startedAt: at(90), endedAt: at(88), logs: ["smoke.log"] },
        ],
        bad: { commit: "9b3e1d2c4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d", why: "was not healthy" },
      };
    },
    async systemAgents() {
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
      return mockQuota(Date.now());
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
    async fsList(path) {
      return server.fsList(path);
    },
    async fsRecent() {
      return server.fsRecent();
    },
    async fsRepos() {
      return server.fsRepos();
    },
    async fsMkdir(parent, name) {
      return server.fsMkdir(parent, name);
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

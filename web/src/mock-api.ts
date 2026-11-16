// The mock Api for ?mock=…: fixtures and a tiny in-memory fyrd (src/mock.ts). api.ts loads this module with
// import() only when a mock scenario is set, so none of it is in the production bundle (#42).

import { newCommandId, type Api, type UploadResult } from "./api.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import { MOCK_COMMANDS, MOCK_PREVIEWS, MOCK_SHOTS, createMockServer, mockChanges, mockFilePatch, mockLoad, mockQuota } from "./mock.ts";
import { createMockProviders } from "./mock-providers.ts";

const MOCK_IMAGE =
  "data:image/svg+xml," +
  encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="390" height="844"><rect width="390" height="844" fill="#edeff2"/><rect width="390" height="120" fill="#14212d"/><rect x="24" y="170" width="250" height="18" rx="4" fill="#b4bcc6"/><rect x="24" y="210" width="320" height="14" rx="4" fill="#dde1e6"/><rect x="24" y="240" width="290" height="14" rx="4" fill="#dde1e6"/><circle cx="195" cy="560" r="90" fill="#e8960f" opacity=".5"/></svg>');

export function createMockApi(scenario: string): Api {
  const server = createMockServer(scenario, Date.now());
  const mockFiles = new Map<string, string>();
  const providers = createMockProviders();
  let watch = initialBuildWatch();
  return {
    mock: true,
    start(h) {
      h.conn("connecting");
      watch = observeBuildId(watch, server.buildId).watch;
      server.subscribe({ thread: h.thread, event: h.event, project: (p) => h.project?.(p) });
      setTimeout(() => {
        h.state(server.state());
        h.conn("live");
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
      if (action.startsWith("provider.")) return providers.command(action, args ?? {});
      return server.command(newCommandId(), action, threadId, args ?? {});
    },
    async providers() {
      return providers.list();
    },
    async providersFound() {
      return providers.found();
    },
    async autoMerge() {
      return [{ repo: "meros/fyr", on: false }];
    },
    async autoDeploy() {
      return { on: true };
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
    attachmentUrl(id) {
      if (MOCK_SHOTS[id]) return MOCK_SHOTS[id];
      // Fixture attachments have no bytes; images get a drawn placeholder.
      return mockFiles.get(id) ?? (id === "fx_img" ? MOCK_IMAGE : "data:text/plain,mock%20attachment%20" + encodeURIComponent(id));
    },
  };
}

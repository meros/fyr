// The mock Api for ?mock=…: fixtures and a tiny in-memory fyrd (src/mock.ts). api.ts loads this module with
// import() only when a mock scenario is set, so none of it is in the production bundle (#42).

import { newCommandId, type Api, type UploadResult } from "./api.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import { MOCK_COMMANDS, MOCK_PREVIEWS, MOCK_SHOTS, createMockServer, mockHosts, setMockHostOrder, trustMockLab, mockChanges, mockFilePatch, mockLoad, mockQueueLoad, mockQuota } from "./mock.ts";
import { mockLeftoverCommand, mockRerunCommand, mockUndeclaredCommand } from "./mock.ts";
import { createMockProviders } from "./mock-providers.ts";
import { withLongValues } from "./mock-long.ts";
import { decodeHosts } from "./hosts.ts";
import type { AuthState } from "./devices.ts";
import type { PeerState } from "./peers.ts";
import type { HostLoad } from "./load.ts";
import type { ThreadDTO, Project, CommandReply } from "./types.ts";
import type { HostInfo } from "./hosts.ts";
import { bare, ref, stampEvent, stampLoad, stampProject, stampSlots, stampThread, unrefDeep } from "./hostfeed.ts";

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
  // V0.137: the load, kept so that a reorder (host.slotOrder) shows on the next read.
  const load: HostLoad[] = q.get("slots") ? mockQueueLoad(q.get("slots")!) : mockLoad();
  // The layout test's hook: a queued thread gets its slot, or its run ends (it leaves the queue); the page sees it on
  // its next read of the load.
  (globalThis as { __fyrSlot?: (owner: string, grant: boolean) => void }).__fyrSlot = (owner, grant) => {
    for (const h of load) {
      const w = h.slots?.waiting.find((x) => x.owner === owner);
      if (!w || !h.slots) continue;
      h.slots = { ...h.slots, waiting: h.slots.waiting.filter((x) => x !== w), held: grant ? [...h.slots.held.slice(1), w] : h.slots.held };
    }
  };
  const hostOffset = Date.parse(server.state().now) - Date.now();
  const hostNow = () => Date.now() + hostOffset;
  // V0.127: with &hosts=2 every host has its own settings. desk (this install, the front's first) has the ones above;
  // lab and old-nuc their own; host-b is offline, so the front serves what it last read (readAt) and refuses
  // every change. A host the front does not follow is refused.
  type MockHost = { providers: ReturnType<typeof createMockProviders>; deployOn: boolean; repos: { repo: string; on: boolean }[] };
  const providers = createMockProviders(providersMode, hostNow);
  const log: { host?: string; action: string; threadId?: string; args: Record<string, unknown> }[] = [];
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
  // Decision 0031: a call names its host by install id. The fixtures name hosts; these two map between them. A front
  // without a host list is one host (desk), reached by any id; a host with no id (old-nuc) is stamped "".
  // The ids do not change (the order does), so they are read once: stamping runs for every thread of every frame.
  const startHosts = mockHostList();
  const idOfName = (name: string | undefined): string => {
    if (!startHosts.length) return ""; // one host and no list: its ids are not prefixed, as a record of a host that reports no id
    return startHosts.find((h) => h.name === (name || startHosts[0].name))?.id ?? "";
  };
  const nameOf = (id: string): string => {
    const hs = mockHostList();
    if (!hs.length) return "";
    // A host with no install id (older than V0.124) cannot be addressed (decision 0031): there is no name to fall back to.
    return hs.find((h) => h.id === id)?.name ?? "\u0000" + id;
  };
  // The host's row for a fixture's host name; a front without a host list is one host, desk, always online.
  const infoOf = (name: string | undefined): HostInfo => startHosts.find((h) => h.name === (name || startHosts[0]?.name)) ?? { id: "", name: name ?? "", online: true }; // read once: stamping runs per thread
  const oneHostId = server.state().hostId ?? ""; // read once: stamping runs per thread
  // A front with one host and no list keeps the fixtures' ids as they are (the layout test names them in its selectors);
  // with several hosts the records are refs, as the real front's are.
  // Decision 0031 2.4: a record's host is the host whose state it came in, so the one host's name is stamped too.
  const oneHost = server.state().host ?? "";
  const stampT = (t: ThreadDTO): ThreadDTO => (startHosts.length ? stampThread(t, infoOf(t.host)) : { ...t, host: t.host || oneHost, hostId: oneHostId });
  const stampP = (p: Project): Project => (startHosts.length ? stampProject(p, infoOf(p.host)) : { ...p, host: p.host || oneHost, hostId: oneHostId });
  // Decision 0031 2.6: a front lists every host it follows, its own too; a front with one host lists that one, reached
  // over the pipe.
  const oneHostRow = (st: ReturnType<typeof server.state>): HostInfo => ({ name: st.host ?? "", online: true, since: st.now, ...(st.hostId ? { id: st.hostId } : {}), ...(st.machine ? { machine: st.machine } : {}), link: { state: "up", via: "pipe" } });
  // Decision 0031 test 8: &sameid=1 (with &hosts=2) gives desk a thread with host-b's thread's id (t_lasso): two records.
  const sameId = (ts: ThreadDTO[]): ThreadDTO[] => {
    if (q.get("sameid") !== "1" || !startHosts.length) return ts;
    const src = ts.find((t) => t.rid === "t_lasso");
    return src ? [...ts, stampThread({ ...src, id: "t_lasso", rid: undefined, host: "desk", hostOffline: undefined, title: "Same id on desk", projectId: undefined, parent: undefined } as ThreadDTO, infoOf("desk"))] : ts;
  };
  const stampState = (st: ReturnType<typeof server.state>) => ({ ...st, ...(st.hosts?.length ? {} : { hosts: [oneHostRow(st)] }), threads: sameId(st.threads.map(stampT)), projects: st.projects.map(stampP) });
  let watch = initialBuildWatch();
  // V0.121: the owner's project order, the front's own (&order=p_fyr,p_web sets one; none: by name).
  let order: string[] | null = q.get("order")?.split(",").filter(Boolean) ?? null;
  let onOrder: ((ids: string[]) => void) | undefined;
  let onHosts: ((hs: import("./hosts.ts").HostInfo[]) => void) | undefined;
  // V0.171: the owner's host marks by install id, as the front keeps them (&hostMarks=<json> sets some at load).
  let marks: import("./host-mark.ts").OwnerMarks = (() => {
    try {
      return JSON.parse(new URLSearchParams(typeof location !== "undefined" ? location.search : "").get("hostMarks") ?? "{}");
    } catch {
      return {};
    }
  })();
  let onMarks: ((m: Record<string, { icon?: string; color?: string }>) => void) | undefined;
  const mockCommand = async (host: string, action: string, threadId: string | undefined, args: Record<string, unknown> | undefined): Promise<CommandReply> => {
      log.push({ host, action, threadId, args: JSON.parse(JSON.stringify(args ?? {})) });
      if (offline) return { ok: false, error: "no connection to the host" };
      const hn = nameOf(host);
      if (action.startsWith("provider.") || action === "host.autoDeploy" || action === "host.updateNow" || action === "host.autoMerge" || action === "host.systemAgents") {
        const at = hostFor(hn);
        if ("error" in at) return { ok: false, error: at.error };
        if (at.readAt) return { ok: false, error: `${hn} is offline; its settings cannot be changed until it is back` };
        const h = at.h;
        if (action.startsWith("provider.")) {
          return h.providers.command(action, args ?? {});
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
        if (args?.host !== mockHosts(Date.now()).find((x) => x.name === "lab")?.id) return { ok: false, error: `${String(args?.host)} has reported no id yet` };
        trustMockLab();
        setTimeout(() => onHosts?.(decodeHosts(mockHosts(Date.now()))), 0); // the front's hosts event (V0.124)
        return { ok: true, result: { host: "lab" } };
      }
      if (action === "host.setupRun") {
        // V0.208: Set up automatically on a host: a thread there; the mock agent sets nothing.
        const id = mockHosts(Date.now()).find((x) => x.name === hn)?.id;
        if (id) marks = { ...marks, [id]: { ...marks[id], setup: { at: new Date().toISOString(), thread: "t_setup" } } };
        setTimeout(() => onMarks?.(marks), 0);
        return { ok: true, result: { thread: "t_setup" } };
      }
      if (action === "front.hostMark") {
        // V0.171: as the front: one host's mark by its id; V0.208: a changed field is the owner's, an emptied one too.
        const id = String(args?.id ?? "");
        const icon = String(args?.icon ?? "").trim();
        const color = String(args?.color ?? "").trim();
        if (!id || (color && !/^[a-z]{1,16}$/.test(color))) return { ok: false, error: "a host mark needs the host's id; an icon is a glyph name or an emoji, a colour a palette name" };
        const cur = marks[id] ?? {};
        const by = { ...cur.by, ...(icon !== (cur.icon ?? "") ? { icon: "owner" } : {}), ...(color !== (cur.color ?? "") ? { color: "owner" } : {}) };
        const next = { ...marks };
        if (!icon && !color && !by.icon && !by.color && !cur.setup) delete next[id];
        else next[id] = { ...(icon ? { icon } : {}), ...(color ? { color } : {}), ...(by.icon || by.color ? { by } : {}), ...(cur.setup ? { setup: cur.setup } : {}) };
        marks = next;
        setTimeout(() => onMarks?.(marks), 0); // the front's hostMarks event
        return { ok: true, result: { id } };
      }
      if (action === "front.hostOrder") {
        const names = (Array.isArray(args?.ids) ? (args.ids as unknown[]).filter((x): x is string => typeof x === "string") : []).map(nameOf);
        setMockHostOrder(names);
        setTimeout(() => onHosts?.(decodeHosts(mockHosts(Date.now()))), 0); // the front's hosts event, on every device
        return { ok: true, result: { names } };
      }
      if (action === "host.slotOrder") {
        // V0.137: as the host's pool does it: priority waits first, then the listed ones, then the rest.
        const h = load.find((x) => x.host === (hn || load[0]?.host));
        if (!h?.slots) return { ok: false, error: "This host has no slots for heavy jobs" };
        const owners = Array.isArray(args?.owners) ? (args.owners as unknown[]).filter((x): x is string => typeof x === "string") : [];
        const pri = h.slots.waiting.filter((w) => w.priority);
        let rest = h.slots.waiting.filter((w) => !w.priority);
        const next = [...pri];
        for (const o of owners) {
          const w = rest.find((x) => x.owner === o);
          if (w) (next.push(w), (rest = rest.filter((x) => x !== w)));
        }
        h.slots = { ...h.slots, waiting: [...next, ...rest] };
        return { ok: true, result: JSON.parse(JSON.stringify(h.slots)) };
      }
      if (action === "host.jobReset") {
        // Decision 0029 S3: as the host does it: the class's learned cost is forgotten.
        const h = load.find((x) => x.host === (hn || load[0]?.host));
        const j = h?.jobs?.find((x) => x.class === args?.class && x.learned);
        if (!h || !j) return { ok: false, error: `No learned job class ${String(args?.class ?? "")}` };
        h.jobs = h.jobs!.map((x) => (x === j ? { ...x, learned: undefined } : x));
        return { ok: true, result: { class: j.class } };
      }
      // V0.164: the owner's commands on undeclared heavy work, as the host answers them (and updates its load).
      if (action === "host.heavyRemove" || action === "host.undeclaredSettings" || action === "host.servicePatterns") {
        const h = load.find((x) => x.host === (hn || load[0]?.host));
        if (!h) return { ok: false, error: `this front follows no host ${hn}` };
        return mockUndeclaredCommand(h, action, { ...(args ?? {}), host: h.host });
      }
      if (action === "host.rerunClasses") {
        // V0.rerun-classes: the owner's rerunnable classes, as the host answers them.
        const h = load.find((x) => x.host === (hn || load[0]?.host));
        if (!h) return { ok: false, error: `this front follows no host ${hn}` };
        return mockRerunCommand(h, args ?? {});
      }
      // V0.176: the owner's End, Keep and Remove of leftovers, as the host answers them.
      if (action === "host.leftoverEnd" || action === "host.leftoverKeep" || action === "host.tempRemove") {
        const h = load.find((x) => x.host === (hn || load[0]?.host));
        if (!h) return { ok: false, error: `this front follows no host ${hn}` };
        return mockLeftoverCommand(h, action, { ...(args ?? {}), host: h.host }, Date.now());
      }
      if (action === "front.projectOrder") {
        order = Array.isArray(args?.ids) ? (args.ids as unknown[]).filter((x): x is string => typeof x === "string") : [];
        const ids = order;
        setTimeout(() => onOrder?.(ids), 0); // the front's event, as on every device
        return { ok: true, result: { ids } };
      }
      // Decision 0034: the front writes each host's own record.
      if (action === "front.projectLink" || action === "front.projectUnlink") {
        const refs = (action === "front.projectLink" ? (args?.refs as string[]) : [args?.ref as string]) ?? [];
        const link = action === "front.projectLink" ? `l_${"0123456789abcdef"}` : "";
        for (const r of refs) server.command(newCommandId(), "project.link", undefined, { id: r.slice(r.indexOf("/") + 1), link });
        return { ok: true, result: { link, stored: refs, failed: [] } };
      }
      if (action === "front.projectAdd") {
        const from = String(args?.ref ?? "");
        const source = server.state().projects.find((p) => p.id === from.slice(from.indexOf("/") + 1));
        const link = source?.link ?? "l_0123456789abcdef";
        server.command(newCommandId(), "project.link", undefined, { id: source?.id, link });
        const made = server.command(newCommandId(), "project.create", undefined, { name: source?.name, folder: args?.folder, host: nameOf(String(args?.host ?? "")) });
        const id = (made as { result?: { id?: string } }).result?.id ?? "";
        server.command(newCommandId(), "project.link", undefined, { id, link });
        return { ok: true, result: { link, id: `${args?.host}/${id}` } };
      }
      if (action === "front.projectShared") {
        const { link, ...vals } = args ?? {};
        const ids = server.state().projects.filter((p) => p.link === link).map((p) => p.id!);
        for (const id of ids) server.command(newCommandId(), "project.update", undefined, { id, ...vals });
        return { ok: true, result: { link, stored: ids, failed: [] } };
      }
      if (hn.startsWith("\u0000")) return { ok: false, error: `this front follows no host ${host}` };
      // The fixture server places a new thread or project by name (a front without hosts: desk).
      return server.command(newCommandId(), action, threadId, mockHostList().length && hn && !threadId ? { ...(args ?? {}), host: hn } : (args ?? {}));
    };

  const long = q.get("long");
  const api: Api = {
    mock: true,
    start(h) {
      h.conn("connecting");
      watch = observeBuildId(watch, server.buildId).watch;
      server.subscribe({ thread: (t) => h.thread(stampT(t)), event: (e) => h.event(stampEvent(e, idOfName(server.state().threads.find((t) => t.id === e.subject)?.host))), project: (p) => h.project?.(stampP(p)) });
      onOrder = h.projectOrder;
      onHosts = h.hosts;
      onMarks = h.hostMarks;
      setTimeout(() => {
        h.state({ ...stampState(server.state()), ...(order ? { projectOrder: order } : {}), ...(Object.keys(marks).length ? { hostMarks: marks } : {}) });
        h.conn(offline ? "offline" : "live");
      }, 0);
      // V0.207, the layout test's hook: a host update of one thread, as the stream brings it.
      const patched = new Map<string, ThreadDTO>();
      let patchRev = 1_000_000;
      (globalThis as { __fyrThread?: (id: string, patch: Partial<ThreadDTO>) => void }).__fyrThread = (id, patch) => {
        const base = patched.get(id) ?? server.state().threads.find((t) => t.id === id);
        if (!base) return;
        // The scenario's threads carry fixed clock times, so "now" can be older than them, and an older update is dropped.
        const updatedAt = new Date(Math.max(Date.now(), Date.parse(base.updatedAt) + 1000)).toISOString();
        const next = { ...base, ...patch, rev: ++patchRev, updatedAt };
        patched.set(id, next);
        h.thread(next);
      };
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
    frontCommand(action, args) {
      return this.command("", action, undefined, args);
    },
    async command(host, action, tid, a) {
      // The host knows its own ids: a ref goes in bare and the ids in the reply come back as refs, as api.ts does.
      const r = await mockCommand(host, action, tid ? bare(host, tid) : tid, unrefDeep(host, a));
      if (!r.ok) return r;
      return {
        ...r,
        ...(r.threadId ? { threadId: ref(host, r.threadId) } : {}),
        ...(r.result && typeof r.result.id === "string" && action.startsWith("project.") ? { result: { ...r.result, id: ref(host, r.result.id) } } : {}),
        ...(r.result && action === "host.slotOrder" && Array.isArray((r.result as { waiting?: unknown }).waiting) ? { result: stampSlots(r.result as never, host) as never } : {}),
      };
    },
    async providers(host) {
      return read(nameOf(host), (h) => {
        if (providersMode === "fail" && h === desk) throw new Error("desk did not answer in 10 s");
        return h.providers.list();
      });
    },
    async providersFound(host) {
      return read(nameOf(host), (h) => h.providers.found());
    },
    async autoMerge(host) {
      return read(nameOf(host), (h) => h.repos.map((r) => ({ ...r })));
    },
    async autoDeploy(host) {
      return read(nameOf(host), (h) => ({ on: h.deployOn }));
    },
    // V0.157: Settings → Devices: this device, a phone that got in through Tailscale, a tablet added by a link and one
    // that waits with a code.
    async authState() {
      return JSON.parse(JSON.stringify(mockAuth)) as AuthState;
    },
    // V0.159: Settings → Hosts: a paired Mac, a box vouched for by it, a tagged node that needs an invite.
    async peerState() {
      const s = JSON.parse(JSON.stringify(mockPeers)) as PeerState;
      if (q.get("config") === "changed") s.configChanged = "config changed; restart fyrd to apply"; // V0.180
      return s;
    },
    async peerAct(action, body) {
      if (action === "remove") {
        mockPeers.members = mockPeers.members.filter((m) => m.id !== body.id);
        return { ok: true };
      }
      if (action === "join") {
        if (!/^fyr1-/i.test(String(body.code))) return { ok: false, error: "that is not an invite" };
        return { ok: true, name: "box", addr: "box.tail1234.ts.net:7779" };
      }
      return { ok: true, code: "fyr1-F7QK-2M9X-AB34-CD56-K7QM2XRA3B", until: new Date(Date.now() + 10 * 60_000).toISOString(), link: "https://desk.tail1234.ts.net:7443/?settings=hosts&pair=fyr1-F7QK-2M9X-AB34-CD56-K7QM2XRA3B", svg: MOCK_QR };
    },
    async authAct(action, body) {
      if (action === "revoke") mockAuth.devices = mockAuth.devices.filter((d) => d.id !== body.id && d.by !== body.id);
      else if (action === "approve" || action === "deny") {
        const d = mockAuth.devices.find((x) => x.pending && (x.id === body.id || x.code === body.code));
        if (!d) return { ok: false, error: "That code does not work (it may have ended)" };
        if (action === "deny") mockAuth.devices = mockAuth.devices.filter((x) => x !== d);
        else (d.pending = false), (d.how = "code"), delete d.code;
      } else if (action === "mode") {
        const m = mockAuth.modes.find((x) => x.way === body.way);
        if (m) (m.mode = body.mode as "observe" | "enforce"), (m.hand = body.mode === "observe");
      } else if (action === "add") return { ok: true, url: "https://desk.tail1234.ts.net:7443/auth/once#k3Jd8sLq0pXn2VwTzYbHrA", svg: MOCK_QR };
      return { ok: true };
    },
    // V0.126: a host that updated twice and rolled one release back (desk); another host never updated itself and sends
    // null for the list, as an older fyrd on a NixOS host did (the owner's Settings could not close, 17 Nov).
    async updates(host) {
      const at = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
      return read(nameOf(host), (h) =>
        h === desk
          ? {
              live: "c705ef536566",
              updates: [
                {
                  build: "c705ef536566",
                  commit: "c705ef5365660a8608badc1fe181f9a08293e663",
                  trigger: "deployable",
                  result: "installed",
                  startedAt: at(20),
                  endedAt: at(18),
                  logs: ["smoke.log"],
                  // V0.update-changelog: what it brought over what ran before
                  from: "3277af6a1b2c",
                  changesAll: 3,
                  changes: [
                    { commit: "3be8b2f47894a1b2c3d4e5f60718293a4b5c6d7e", title: "An opened subagent shows its prompt and everything it did, in order (#424)" },
                    { commit: "64b8fd3112a3b4c5d6e7f8091a2b3c4d5e6f7081", title: "The guard keeps a frozen job's hold (#414)" },
                    { commit: "d93e1ba4c5d6e7f8091a2b3c4d5e6f708192a3b4", title: "A link that loses a race closes only when both sides are done with it" },
                  ],
                },
                { build: "9b3e1d2c4a5f", commit: "9b3e1d2c4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d", trigger: "deployable", result: "rolledBack", reason: "was not healthy: the database does not answer", startedAt: at(90), endedAt: at(88), logs: ["smoke.log"] },
              ],
              bad: { commit: "9b3e1d2c4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d", why: "was not healthy" },
              repo: "meros/fyr",
              key: { fingerprint: "SHA256:RPhCNjz0mCuohR8sdDkJ0bw4JXHzmNJAAZ/BR5C9uWM", builtIn: true }, // V0.release-without-gh
              version: "v1.0.0", // V0.versioned-releases
              channel: "stable",
            }
          : { live: "nix-d8b9560", updates: null as unknown as [] },
      );
    },
    async systemAgents(host) {
      return read(nameOf(host), (h) => {
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
              { id: "question", name: "Questions in text", choice: {}, profile: "claude", model: "claude-haiku-5-5" },
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
            { id: "question", name: "Questions in text", choice: {}, ...runs },
          ],
        };
      });
    },
    async profiles() {
      return server.state().profiles;
    },
    async events(host, threadId, after = 0, limit = 400) {
      return server.events(bare(host, threadId), after, limit).map((e) => stampEvent(e, host));
    },
    async thread() {
      return null; // the mock sends whole threads
    },
    async fullEvent() {
      return null; // the mock never cuts
    },
    async eventsBefore(host, threadId, before, limit = 400) {
      const all = server.events(bare(host, threadId), 0, 1_000_000).filter((e) => e.seq < before);
      return all.slice(Math.max(0, all.length - limit)).map((e) => stampEvent(e, host));
    },
    async t3Candidates(host) {
      const r = hostFor(nameOf(host)); // V0.139: only desk has T3 in the mock
      if ("error" in r) throw new Error(r.error);
      return r.h === desk ? server.t3Candidates() : [];
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
    async pushHidden() {
      return { ok: true };
    },
    async pushPrefs() {
      return { ok: true };
    },
    async pushTest() {
      return server.pushTest();
    },
    async presence(_host, _threadId, _device) {
      /* the mock has no push */
    },
    async pushStatus(endpoint) {
      return server.pushSubscriptions().some((s) => s.endpoint === endpoint);
    },
    uploadAttachment(_host, file, onProgress, signal) {
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
    previewUrl(_host, id) {
      return MOCK_PREVIEWS[id] ?? "data:text/html,<p>mock preview</p>";
    },
    async quota() {
      // V0.127: the front stamps each quota with its host; lab's Claude has its own.
      const hs = mockHostList();
      if (!hs.length) return mockQuota(Date.now());
      const at = (min: number) => new Date(Date.now() + min * 60_000).toISOString();
      return [...mockQuota(Date.now()).map((q) => ({ ...q, host: hs[0].name })), { profile: "claude", host: "lab", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 12, resetsAt: at(200) }, { name: "7 d", usedPct: 23, resetsAt: at(4 * 1440) }], updatedAt: at(-3) },
        // V0.141: a fourth provider, for the layout tests with two hosts and four quotas (the phone's load sheet lists them)
        { profile: "zai", host: "lab", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 83, resetsAt: at(90) }], updatedAt: at(-1) }];
    },
    async load() {
      // V0.140: with &hosts=2 the rail has a ring per host (desk's, and a calm lab).
      const l = JSON.parse(JSON.stringify(load)) as HostLoad[];
      if (!mockHostList().length || !l.length) return l.map((x) => stampLoad(x, idOfName(x.host)));
      return [{ ...l[0], host: "desk" }, { ...l[0], host: "lab", cores: 16, load: [1.4, 1.1, 0.9], threads: [], slots: undefined }].map((x) => stampLoad(x as HostLoad, idOfName(x.host)));
    },
    async boardVisit() {
      // V0.98 (3): the mock's last visit was a day ago, so its done changes show under Finished.
      return new Date(Date.now() - 24 * 3600 * 1000).toISOString();
    },
    async commands() {
      return MOCK_COMMANDS;
    },
    async changes(host, threadId) {
      return mockChanges(bare(host, threadId));
    },
    async filePatch(_host, _threadId, path) {
      return mockFilePatch(path);
    },
    // V0.132: each host's own folders; desk's are the fixture tree, another host has a small one of its own, and an
    // offline host's cannot be read.
    async fsList(host, path) {
      const f = fsOf(nameOf(host));
      return f ? f.list(path) : server.fsList(path);
    },
    async fsRecent(host) {
      const f = fsOf(nameOf(host));
      return f ? [] : server.fsRecent();
    },
    async fsRepos(host) {
      const f = fsOf(nameOf(host));
      return f ? f.repos() : server.fsRepos();
    },
    async fsMkdir(host, parent, name) {
      const f = fsOf(nameOf(host));
      return f ? f.mkdir(parent, name) : server.fsMkdir(parent, name);
    },
    async searchThreads(host, query, ids) {
      // V0.ai-search: the mock's "model" takes the first threads, or none for a query with "nothing" in it.
      await new Promise((r) => setTimeout(r, 300));
      if (/nothing/i.test(query)) return [];
      if (/fail/i.test(query)) throw new Error("the model gave no usable answer");
      return ids.slice(0, 2).map((id) => ({ id: bare(host, id), why: `Talks about ${query.trim()}` }));
    },
    async prDetail(host, threadId) {
      await new Promise((r) => setTimeout(r, 120));
      return server.prDetail(bare(host, threadId));
    },
    async uploadProjectIcon(host, projectId0, file) {
      const projectId = bare(host, projectId0);
      // V0.116: the mock keeps no bytes; the project gets a new version (the layout test serves fixture icons).
      if (file.size > 2 * 1024 * 1024) return { ok: false, error: "the picture is over 2 MB" };
      const r = server.command(`icon-${Date.now()}`, "project.update", undefined, { id: projectId, image: `m${Date.now().toString(36)}` });
      return { ok: r.ok, error: r.error };
    },
    attachmentUrl(_host, id) {
      if (MOCK_SHOTS[id]) return MOCK_SHOTS[id];
      // Fixture attachments have no bytes; images get a drawn placeholder.
      return mockFiles.get(id) ?? (id === "fx_img" ? MOCK_IMAGE : "data:text/plain,mock%20attachment%20" + encodeURIComponent(id));
    },
  };
  return long === "word" || long === "words" ? withLongValues(api, long) : api;
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

const iso = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
const mockAuth: AuthState = {
  you: "dev_you",
  devices: [
    { id: "dev_you", name: "Chrome on Linux · on desk", how: "local", created: iso(60 * 24 * 9), lastSeen: iso(0) },
    { id: "dev_phone", name: "Chrome on Android", how: "tailnet", created: iso(60 * 24 * 6), lastSeen: iso(12), node: "n100.64.0.2" },
    { id: "dev_tab", name: "Safari on iPad", how: "link", created: iso(60 * 5), lastSeen: iso(60 * 3), by: "dev_you" },
    { id: "dev_wait", name: "Firefox on Windows", how: "code", created: iso(2), lastSeen: iso(0), pending: true, code: "K7QM2XRA", codeUntil: new Date(Date.now() + 3 * 60_000).toISOString() },
  ],
  modes: [
    { way: "browser", mode: "observe", hand: false, since: iso(60 * 24 * 2), refused: 0, success: { local: 4, tailnet: 2 }, ready: false, why: "1 day of clean days to go" },
    { way: "peer", mode: "observe", hand: false, since: iso(60 * 24 * 2), refused: 0, success: null, ready: false, why: "this way in cannot enforce yet" },
  ],
  audit: [],
};
// A QR-looking image for the layout test: the real one comes from the host.
const MOCK_QR = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 29 29"><rect width="29" height="29" fill="#fff"/><path fill="#000" d="M4 4h7v7H4zM18 4h7v7h-7zM4 18h7v7H4zM13 13h3v3h-3zM18 18h3v7h-3z"/></svg>`;

const mockPeers: PeerState = {
  signedIn: true,
  members: [
    { id: "F7QK-2M9X-AB34-CD56", name: "desk", self: true, drive: true, via: "paired" },
    { id: "QX2C-7LD4-9PWE-1ABN", name: "mac", port: 7779, drive: true, via: "paired", link: { state: "up", via: "peer", opened: "them", since: new Date(Date.now() - 40 * 60_000).toISOString() } },
    { id: "H3TT-8KQM-2ZRD-4VYC", name: "box", port: 7779, drive: false, via: "vouched", by: "mac", link: { state: "down", via: "peer", error: { kind: "timeout", text: "" } } },
  ],
  candidates: [
    { name: "server.tail1234.ts.net", online: true, kind: "tagged" },
    { name: "guest-phone.tail1234.ts.net", online: false, kind: "other-user" },
  ],
};

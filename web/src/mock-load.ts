// The mock host's quota and load: the busy host, the memory guard's history, leftovers and undeclared work, and the
// commands that act on them. Loaded with the mock Api only.
import type { CommandReply } from "./types.ts";
import type { Quota } from "./quota.ts";
import type { HostLoad, Slots, UndeclaredUse } from "./host-load.ts";
import type { Guard, GuardEvent, Invariants } from "./guard.ts";
import type { Leftover, Temp } from "./leftovers.ts";
import { MOCK_HOSTS, mockParam, railNow } from "./mock-util.ts";

/** Decision 0029 S5: ?paused=1 has the memory guard freeze map perf's job. */
const MOCK_PAUSED = mockParam("paused") === "1";

/** V0.164: ?undeclared=1 (Map perf runs heavy work it did not ask for), =paused (the memory guard froze it), =2 (Map perf
 *  and Babysit PR #4170, and Nix flake update dismissed: the rail's menu and Settings' three states). Read once at load. */
export const MOCK_UNDECLARED = mockParam("undeclared") ?? "";
export const UNDECLARED_NOTICE = {
  undeclared:
    "This thread runs heavy work Fyr did not admit: about 2.8 GB memory (20 % of laptop) and 2.1 cores (52 %) for 45 s, from `node test/layout.e2e.mjs`. Before a heavy job call mcp__fyr__slot_acquire {\"kind\":\"e2e\",\"command\":\"node test/layout.e2e.mjs\"}, run it once admitted, then call mcp__fyr__slot_release; a subagent's or a background run counts too. If it still runs, call it now. If it has ended, there is nothing to do for it, but ask before you run it or anything like it again. Fyr counts this use against the host either way, so other jobs wait; nothing is stopped, but if memory runs short Fyr may pause it.",
  mem: 2.8e9,
  cpu: 2.1,
};

/** SPEC 0.5 V0.44: provider quota for the mock profiles; "work" is busy and stale. */
export function mockQuota(now: number): Quota[] {
  const at = (min: number) => new Date(now + min * 60_000).toISOString();
  return [
    { profile: "claude", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 34, resetsAt: at(140) }, { name: "7 d", usedPct: 61, resetsAt: at(3 * 1440) }], updatedAt: at(-2) },
    { profile: "work", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 86, resetsAt: at(45) }, { name: "7 d", usedPct: 40, resetsAt: at(5 * 1440) }], updatedAt: at(-14), stale: true, error: "Rate limited by the provider; trying again later", retryAt: at(6) },
  ];
}

/** SPEC 0.5 V0.80: a busy host: load past the cores, the agents at their cap; one thread in each fixture is hot
 * (t_w2, t_mapperf), one calm, one without a second read yet. */
export function mockLoad(): HostLoad[] {
  return withInvariants(withGuard(withLeftovers(withUndeclared(mockLoadBase(), MOCK_UNDECLARED), MOCK_LEFTOVERS), MOCK_GUARD), MOCK_INVARIANTS);
}

/** V0.load-invariants, V0.thread-invariants: ?invariants=1 has a broken load invariant (12 min ago) and a broken thread
 *  invariant (5 min ago, so its card is first), each a card in Needs you, and a load invariant that never broke. */
const MOCK_INVARIANTS = mockParam("invariants") ?? "";
export function withInvariants(list: HostLoad[], mode: string, now = railNow(Date.now())): HostLoad[] {
  if (!mode) return list;
  const [h, ...rest] = list;
  const ago = (min: number) => new Date(now - min * 60_000).toISOString();
  const thaw = "The guard thawed work while the agents' memory stalled critically.";
  const phantom = "A thread is marked Running, but the agent manager has no live turn for it.";
  const invariants: Invariants = {
    cards: [
      { id: "thaw-into-critical", kind: "load", text: thaw, at: ago(12), evidence: "thread=t_mapperf work=undeclared reason=ended agents_stall=critical", lines: ["14:19:50 guard: froze undeclared work thread=t_mapperf", "14:20:00 guard: thawed undeclared work thread=t_mapperf"] },
      { id: "thread-phantom-running", kind: "thread", text: phantom, at: ago(5), evidence: "thread=t_w1 engine.running=true agent.live=false", lines: ["14:26:00 run.started (observer:process) seen→working running=true"] },
    ],
    stats: [
      { id: "thaw-into-critical", kind: "load", text: thaw, count: 2, last: ago(12) },
      { id: "probe-silent", kind: "load", text: "The load probe reported within the last minute.", count: 0 },
      { id: "thread-phantom-running", kind: "thread", text: phantom, count: 1, last: ago(5) },
    ],
    fixDir: "/work/fyr",
  };
  return [{ ...h, invariants }, ...rest];
}

/** The owner's Fix it and Dismiss on an invariant's card, as the host answers them: either closes the card. */
export function mockInvariantCommand(h: HostLoad, args: Record<string, unknown>): CommandReply {
  const cards = h.invariants?.cards ?? [];
  if (!cards.some((c) => c.id === args.id)) return { ok: false, error: `no open card ${String(args.id ?? "")}` };
  h.invariants = { ...h.invariants!, cards: cards.filter((c) => c.id !== args.id) };
  return { ok: true };
}

/** Decision 0032: ?guard=1 has the memory guard's history: memory short twice in 15 minutes, Map perf's undeclared work
 *  frozen three times (the third still on), Babysit frozen once. ?guard=capped shows a thread let run after 8 pauses. */
const MOCK_GUARD = mockParam("guard") ?? "";
export function withGuard(list: HostLoad[], mode: string, now = railNow(Date.now())): HostLoad[] {
  if (!mode) return list;
  const [h, ...rest] = list;
  const ago = (sec: number) => new Date(now - sec * 1000).toISOString();
  const ev = (secAgo: number, kind: GuardEvent["kind"], extra: Partial<GuardEvent> = {}): GuardEvent => ({ at: ago(secAgo), kind, ...extra });
  const events: GuardEvent[] = [
    ev(14 * 60, "short", { headroom: 0.04, stall: "critical" }),
    ev(13.5 * 60, "freeze", { work: "undeclared", thread: "t_mapperf", n: 1, headroom: 0.04 }),
    ev(12 * 60, "recovered", { headroom: 0.31 }),
    ev(11.9 * 60, "thaw", { work: "undeclared", thread: "t_mapperf", reason: "memory" }),
    ev(8 * 60, "short", { headroom: 0.05, stall: "critical" }),
    ev(7.5 * 60, "freeze", { work: "undeclared", thread: "t_mapperf", n: 2, headroom: 0.05 }),
    ev(7 * 60, "freeze", { work: "undeclared", thread: "t_babysit", n: 1, headroom: 0.05 }),
    ev(5 * 60, "thaw", { work: "undeclared", thread: "t_babysit", reason: "gone" }),
    ev(4 * 60, "thaw", { work: "undeclared", thread: "t_mapperf", reason: "memory" }),
    ev(150, "freeze", { work: "undeclared", thread: "t_mapperf", n: mode === "capped" ? 8 : 3, headroom: 0.04 }),
  ];
  const guard: Guard = {
    short: true,
    shortSince: ago(8 * 60),
    max: 8,
    refreezeSec: 60,
    events,
    threads: [{ thread: "t_mapperf", n: mode === "capped" ? 8 : 3, frozen: true, frozenAt: ago(150), capped: mode === "capped" }, { thread: "t_babysit", n: 1, frozen: false, nextAt: new Date(now + 24_000).toISOString() }],
  };
  return [{ ...h, slots: { ...h.slots!, guard } }, ...rest];
}

/** V0.176: ?leftovers=1: Map perf left a Chrome and a mockup server, Nix flake update a port-forward under timeout, a
 *  dev server Babysit PR #4170's agent kept, a test binary in a cgroup of no thread, and two settled threads' worktrees. */
const MOCK_LEFTOVERS = mockParam("leftovers") ?? "";
export function withLeftovers(list: HostLoad[], mode: string, now = railNow(Date.now())): HostLoad[] {
  if (!mode) return list;
  const [h, ...rest] = list;
  const ago = (min: number) => new Date(now - min * 60_000).toISOString();
  const leftovers: Leftover[] = [
    { id: "41022@100", thread: "t_mapperf", pid: 41022, name: "chromium", command: "/nix/store/x-chromium/bin/chromium --headless --user-data-dir=/tmp/fyr-layout-a1", procs: 9, rss: 1.9e9, started: ago(65), seen: ago(60), endsAt: ago(-25) },
    { id: "41090@101", thread: "t_mapperf", pid: 41090, name: "http.server", command: "python3 -m http.server 8000", procs: 1, rss: 21e6, started: ago(60), seen: ago(58), endsAt: ago(-27) },
    { id: "38810@102", thread: "t_nix", pid: 38810, name: "kubectl port-forward", command: "timeout 12h kubectl port-forward svc/db 5432", procs: 2, rss: 62e6, started: ago(90), seen: ago(88), until: ago(-630) },
    { id: "39001@103", thread: "t_babysit", pid: 39001, name: "vite", command: "node /w/node_modules/.bin/vite --port 5173", procs: 3, rss: 180e6, started: ago(40), seen: ago(38), kept: { thread: "t_babysit", pid: 39001, until: ago(-180), why: "the dev server the owner asked for", by: "agent" } },
    { id: "37777@104", thread: "t_0ld", orphan: true, pid: 37777, name: "engine.test", command: "/tmp/go-build1/b001/engine.test -test.run=TestX", procs: 1, rss: 8e6, started: ago(360), seen: ago(1), endsAt: ago(-1) },
  ];
  const temps: Temp[] = [
    { path: "/home/me/fyr/.claude/worktrees/map-perf", kind: "worktree", thread: "t_mapperf", bytes: 412e6, safe: true, removeAt: ago(-5 * 60) }, // V0.fix-flakes: on the mock's clock, as every time above; Date.now() read "in 1 min" before 09:32
    { path: "/home/me/fyr/.claude/worktrees/nix-bump", kind: "worktree", thread: "t_nix", bytes: 3e6, safe: false, why: "2 commits not pushed" },
  ];
  return [{ ...h, leftovers, temps }, ...rest];
}

/** V0.176: host.leftoverEnd, host.leftoverKeep and host.tempRemove on the mock host's load, as the host answers them. */
export function mockLeftoverCommand(h: HostLoad, action: string, args: Record<string, unknown>, now = Date.now()): CommandReply {
  if (action === "host.tempRemove") {
    const t = h.temps?.find((x) => x.path === args.path);
    if (!t) return { ok: false, error: "not a listed folder" };
    if (!t.safe) return { ok: false, error: `kept: ${t.why ?? "not safe"}` };
    h.temps = h.temps!.filter((x) => x !== t);
    return { ok: true, result: { path: t.path } };
  }
  const l = h.leftovers?.find((x) => x.id === args.id);
  if (!l) return { ok: false, error: "no such leftover process" };
  if (action === "host.leftoverEnd") {
    h.leftovers = h.leftovers!.filter((x) => x !== l);
    return { ok: true, result: { id: l.id } };
  }
  const hours = typeof args.hours === "number" ? args.hours : 24;
  l.kept = { thread: l.thread, pid: l.pid, until: new Date(now + hours * 3600_000).toISOString(), why: "kept by the owner", by: "owner" };
  delete l.endsAt;
  return { ok: true, result: { id: l.id, hours } };
}

/** V0.undeclared-mock-age: the mock's load is one scene, made when the page loads, and each read shows it as of that
 *  read: the undeclared work's start moves on by the time since (`ms`), so Babysit has run 50 s however long the page
 *  is open (a full layout run under load reached its card more than ten seconds in, and it read "1 min"). */
export function ageUndeclared(list: HostLoad[], ms: number): HostLoad[] {
  const later = (iso: string) => new Date(Date.parse(iso) + ms).toISOString();
  return list.map((h) => (h.slots?.undeclared?.length ? { ...h, slots: { ...h.slots, undeclared: h.slots.undeclared.map((u) => ({ ...u, since: later(u.since) })) } } : h));
}

/** V0.164: the mock host's view of undeclared work (mode "" is none): the threads, the shares in the pool, the wait
 *  reason that names it, the thresholds and one command Fyr learned. */
export function withUndeclared(list: HostLoad[], mode: string, now = railNow(Date.now())): HostLoad[] {
  if (!mode) return list;
  const [h, ...rest] = list;
  const s = h.slots!;
  const usage = (thread: string, mem: number, cpu: number, memPct: number, cpuPct: number, secs: number, command: string, extra: Partial<UndeclaredUse> = {}): UndeclaredUse => ({
    thread, mem, cpu, memPct, cpuPct, since: new Date(now - secs * 1000).toISOString(), command, paused: false, queued: false, overCpu: true, overMem: true, ...extra,
  });
  const und: UndeclaredUse[] = [usage("t_mapperf", 2.8e9, 2.1, 20, 52, 180, "node test/layout.e2e.mjs", { paused: mode === "paused" })];
  if (mode === "2") und.push(usage("t_babysit", 1.6e9, 1.2, 11, 30, 50, "yarn pre-push:check"));
  const admitted = { cpu: 13, mem: 16 };
  const uCpu = und.reduce((n, u) => n + u.cpuPct, 0);
  const uMem = und.reduce((n, u) => n + u.memPct, 0);
  const cpuPct = admitted.cpu + uCpu;
  const memPct = admitted.mem + uMem;
  const need = 45;
  const free = Math.max(0, 100 - cpuPct);
  // the host names the largest and "+N" for the others
  const names = `Map perf uses ${und[0].cpuPct} % undeclared${und.length > 1 ? ` +${und.length - 1}` : ""}`;
  const slots: Slots = {
    ...s,
    held: [{ owner: "t_w2", kind: "test", cpuPct: admitted.cpu, memPct: admitted.mem, pct: admitted.cpu }],
    waiting: [{ owner: "t_search", kind: "test", cpuPct: need, memPct: 10, pct: need }],
    why: `needs ≈ ${need} % CPU, ${free} % free (${names})`,
    needPct: need,
    cpuPct,
    memPct,
    cpuFreePct: free,
    memFreePct: 100 - memPct,
    usedPct: Math.max(cpuPct, memPct),
    tight: cpuPct >= memPct ? "cpu" : "memory",
    binding: "cpu",
    undeclared: und,
    undeclaredCpuPct: uCpu,
    undeclaredMemPct: uMem,
  };
  const threads = h.threads.map((t) => (t.id === "t_mapperf" ? { ...t, job: undefined, paused: undefined } : t));
  return [
    {
      ...h,
      slots,
      threads,
      headroom: mode === "paused" ? 0.06 : h.headroom,
      memoryStall: mode === "paused" ? "critical" : h.memoryStall,
      undeclaredSettings: { on: true, cpu: 0.25, memory: 0.08, after: 45 },
      heavyAdded: [{ pattern: "python3? build_admin_ny\\.py", kind: "build", at: new Date(now - 24 * 3600_000).toISOString(), refused: 0, learned: true }],
      servicePatterns: [
        { pattern: "(^|[\\s/])docker\\s+compose\\b.*\\sup\\b", builtIn: true },
        { pattern: "(^|[\\s/])(npm|pnpm|bun)\\s+run\\s+dev\\b", builtIn: true },
      ],
      rerun: { classes: ["make test"] }, // V0.rerun-classes
    },
    ...rest,
  ];
}
/** V0.rerun-classes: host.rerunClasses on the mock host's load, with the host's refusals. */
export function mockRerunCommand(h: HostLoad, args: Record<string, unknown>): { ok: boolean; error?: string; result?: Record<string, unknown> } {
  const list = h.rerun?.classes ?? [];
  const add = typeof args.add === "string" ? args.add.trim().split(/\s+/).join(" ") : "";
  const remove = typeof args.remove === "string" ? args.remove : "";
  if (add && !remove) {
    if (add.length > 200) return { ok: false, error: "a class is a command of 1 to 200 characters, e.g. make test" };
    h.rerun = { classes: list.includes(add) ? list : [...list, add] };
    return { ok: true, result: { add } };
  }
  if (remove && !add) {
    if (!list.includes(remove)) return { ok: false, error: "no such rerunnable class" };
    h.rerun = { classes: list.filter((c) => c !== remove) };
    return { ok: true, result: { remove } };
  }
  return { ok: false, error: "give add or remove: one class of 1 to 200 characters" };
}

/** V0.164: host.heavyRemove and host.undeclaredSettings on the mock host's load, with the host's refusals (an unknown
 *  pattern, a threshold out of range). */
export function mockUndeclaredCommand(h: HostLoad, action: string, args: Record<string, unknown>): { ok: boolean; error?: string; result?: Record<string, unknown> } {
  if (action === "host.servicePatterns") {
    // V0.services-not-jobs: as the host does it (a removed built-in pattern can be added back).
    const list = h.servicePatterns ?? [];
    if (typeof args.remove === "string" && args.remove) {
      if (!list.some((x) => x.pattern === args.remove)) return { ok: false, error: "no such service pattern" };
      h.servicePatterns = list.filter((x) => x.pattern !== args.remove);
    }
    if (typeof args.add === "string" && args.add.trim()) {
      try {
        new RegExp(args.add);
      } catch {
        return { ok: false, error: "not a valid pattern" };
      }
      if (!(h.servicePatterns ?? []).some((x) => x.pattern === args.add)) h.servicePatterns = [...(h.servicePatterns ?? []), { pattern: args.add.trim() }];
    }
    return { ok: true, result: args };
  }
  if (action === "host.heavyRemove") {
    if (!h.heavyAdded?.some((x) => x.pattern === args.pattern)) return { ok: false, error: "That pattern was not added" };
    h.heavyAdded = h.heavyAdded.filter((x) => x.pattern !== args.pattern);
    return { ok: true };
  }
  const s = h.undeclaredSettings;
  if (!s) return { ok: false, error: "This host does not detect undeclared work" };
  const share = (v: unknown) => typeof v === "number" && v >= 0.01 && v <= 1;
  if (args.cpu !== undefined && !share(args.cpu)) return { ok: false, error: "cpu is a share from 1 % to 100 %" };
  if (args.memory !== undefined && !share(args.memory)) return { ok: false, error: "memory is a share from 1 % to 100 %" };
  if (args.after !== undefined && !(typeof args.after === "number" && args.after >= 30 && args.after <= 120)) return { ok: false, error: "after is 30 to 120 seconds" };
  h.undeclaredSettings = { on: typeof args.on === "boolean" ? args.on : s.on, cpu: (args.cpu as number) ?? s.cpu, memory: (args.memory as number) ?? s.memory, after: (args.after as number) ?? s.after };
  return { ok: true, result: { ...h.undeclaredSettings } };
}

function mockLoadBase(): HostLoad[] {
  // V0.141: with ?hosts=2, lab's load follows the laptop's (calm, two free slots), as a front with two hosts shows.
  const lab: HostLoad[] = MOCK_HOSTS ? [{ host: "lab", cores: 16, load: [0.3, 0.5, 0.6], cpuPressure: 0.4, cgroups: true, agents: { cpu: 0.1, cpuMax: 8, memory: 1e9, memoryMax: 28 * 2 ** 30 }, threads: [], slots: { n: 2, held: [], waiting: [] } }] : [];
  return [...laptopLoad(), ...lab];
}
function laptopLoad(): HostLoad[] {
  return [
    {
      host: "laptop",
      cores: 8,
      load: [10.4, 7.9, 5.2],
      cpuPressure: 46.3,
      cgroups: true,
      agents: { cpu: 3.96, cpuMax: 4, memory: 9.1e9, memoryMax: 14 * 2 ** 30 },
      threads: [
        { id: "t_w1", cpu: 0.4, memory: 6e8 },
        { id: "t_w2", cpu: 2.7, memory: 3.2e9 },
        { id: "t_w3", memory: 2e8 },
        { id: "t_babysit", cpu: 0.3, memory: 5e8 },
        { id: "t_mapperf", cpu: 2.6, memory: 3e9, job: "yarn build", paused: MOCK_PAUSED || undefined },
        { id: "t_search", cpu: 1.0, memory: 9e8 },
      ],
      // V0.86: two jobs run, one thread waits. V0.163: heavy jobs in percent of the host, CPU and memory apart; the
      // head waits for CPU.
      slots: {
        n: 2,
        why: "needs ≈ 30 % CPU, 5 % free",
        held: [
          { owner: "t_w2", kind: "test", cpuPct: 50, memPct: 16, pct: 50 },
          { owner: "t_mapperf", kind: "build", cpuPct: 45, memPct: 25, pct: 45 },
        ],
        waiting: [{ owner: "t_search", kind: "test", cpuPct: 30, memPct: 10, pct: 30 }],
        cpuPct: 95,
        memPct: 41,
        cpuBudgetPct: 100,
        memBudgetPct: 100,
        cpuFreePct: 5,
        memFreePct: 59,
        usedPct: 95,
        tight: "cpu",
        binding: "cpu",
        needPct: 30,
      },
      // Decision 0029 S1: the host's signals and the measured job classes.
      provider: "cgroups",
      capacity: { cpu: 4, memory: 13 * 2 ** 30 },
      slowdown: 3.4,
      headroom: 0.31,
      memoryPressure: 1.2,
      memoryStall: "ok",
      agentsCpuPressure: 88,
      // V0.load-cleanup: oomd's rule on the agents' slice, and the stall it compares (the card's "oomd" row).
      oomdLimit: 85,
      unitMemoryStall: 3.2,
      jobs: [
        { class: "fyr: make test", runs: 14, cores: 2.3, demand: 3.4, peak: 2.1e9, wall: 95, at: "2026-11-17T09:12:00Z", learned: 14, cpuPct: 58, memPct: 15 },
        { class: "Acme: yarn screenshot:diff", runs: 6, cores: 1.1, demand: 1.9, peak: 6.4e9, wall: 437, at: "2026-11-17T08:40:00Z", learned: 6, cpuPct: 28, memPct: 46 },
        { class: "fyr: e2e", runs: 3, cores: 0.9, demand: 1.2, peak: 1.8e9, wall: 254, at: "2026-11-17T08:02:00Z", learned: 2, cpuPct: 50, memPct: 50 },
      ],
    },
  ];
}

/** SPEC V0.137, for screenshots and the layout test: &slots=queue puts the deploy's tests and three threads in the
 *  queue; &slots=two adds a second host (lab) with a queue of its own; &slots=none
 *  has no queue. */
export function mockQueueLoad(mode: string): HostLoad[] {
  const [h] = mockLoad();
  // V0.163: the same pool in percent; Fyr's own tests wait first, the head's reason is theirs.
  h.slots = {
    ...h.slots!,
    why: "needs ≈ 50 % CPU, 5 % free",
    needPct: 50,
    waiting: [
      { owner: "self-deploy", kind: "test", priority: true, cpuPct: 50, memPct: 25, pct: 50 },
      { owner: "t_search", kind: "test", cpuPct: 30, memPct: 10, pct: 30 },
      { owner: "t_pw", kind: "e2e", cpuPct: 20, memPct: 35, pct: 35 },
      { owner: "t_nix", kind: "build", cpuPct: 40, memPct: 20, pct: 40 },
    ],
  };
  if (mode === "none") h.slots.waiting = [];
  if (mode !== "two") return [h];
  return [h, { host: "lab", cores: 4, load: [1.2, 1, 0.8], cgroups: false, threads: [], slots: { n: 1, held: [{ owner: "t_snap", kind: "test" }], waiting: [{ owner: "t_poly", kind: "test" }] } }];
}

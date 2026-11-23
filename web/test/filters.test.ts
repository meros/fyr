// SPEC 0.5 V0.134, V0.169: the inbox filters. Row 1 toggles each host on or off (all on is no filter); row 2,
// the projects, follows from it. Only active values show; a selected value (a project picked, a host off) stays.
import { test } from "node:test";
import assert from "node:assert/strict";
import { activeFilters, applyFilters, clickHost, filterEmpty, inView, threadHost, toggleHost, type FilterSel } from "../src/filters.ts";
import { parseJson, strings } from "../src/decode.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Project, Thread } from "../src/types.ts";
import { thread, hid } from "./helpers.ts";

const P = (id: string, name: string, extra: Partial<Project> = {}): Project => ({ id, name, threads: 1, lastUsed: "", ...extra }) as Project;
const projects = [P("p_web", "web"), P("p_fyr", "fyr"), P("p_home", "home")];
// Slice 2b: a host is its install id (here its name, for short), and a thread is in the state of its host.
const H = (name: string, online = true): HostInfo => ({ name, online, id: hid(name) });
const hosts = [H("lab"), H("desk"), H("server", false)]; // the owner's order (V0.132): lab first
const NOW = Date.parse("2026-11-17T12:00:00Z");
const byId = (...ts: Thread[]) => Object.fromEntries(ts.map((t) => [t.id, t]));
const T = (id: string, section: "needs" | "working" | "seen" | "snoozed" | "settled", projectId: string, host: string, extra: Record<string, unknown> = {}) =>
  thread(id, "done", section, { projectId, project: projects.find((p) => p.id === projectId)?.name ?? "", host, hostId: hid(host), ...extra });
const base = { projects, hosts, now: NOW, showUntil: 0, open: {} };
const S = (hostsOff: string[] = [], project: string | null = null): FilterSel => ({ hostsOff, project });
const names = (f: ReturnType<typeof activeFilters>) => ({ hosts: f.hosts.map((h) => h.name), projects: f.projects.map((p) => p.project.name) });

test("a thread's host: its own; decision 0031 2.6: never the front's own as a fallback", () => {
  assert.equal(threadHost(T("a", "needs", "p_web", "lab")), "lab");
  assert.equal(threadHost(thread("b", "done", "needs")), "");
});

test("in view: an unfolded section or pinned; Snoozed and Settled are folded by default; archived never", () => {
  assert.equal(inView(T("a", "needs", "p_web", "lab"), {}), true);
  assert.equal(inView(T("a", "settled", "p_web", "lab"), {}), false);
  assert.equal(inView(T("a", "snoozed", "p_web", "lab"), {}), false);
  assert.equal(inView(T("a", "settled", "p_web", "lab"), { settled: true }), true);
  assert.equal(inView(T("a", "needs", "p_web", "lab"), { needs: false }), false, "a section the owner folded");
  assert.equal(inView(T("a", "needs", "p_web", "lab", { pinnedAt: "2026-11-17T10:00:00Z" }), { needs: false }), true, "pinned threads are always shown");
  assert.equal(inView(thread("x", "done", "archived" as never), {}), false);
});

test("V0.169: row 1 has a toggle per host with threads in view, in the owner's order, all on by default, with short names", () => {
  const hs = [H("home-laptop"), H("home-server", false)];
  const threads = byId(T("a", "needs", "p_fyr", "home-laptop"), T("b", "working", "p_web", "home-server"), T("c", "seen", "p_web", "home-laptop"));
  const f = activeFilters({ ...base, hosts: hs, threads, sel: S() });
  assert.deepEqual(f.hosts.map((h) => [h.name, h.short, h.n, h.on, h.last]), [["home-laptop", "laptop", 2, true, false], ["home-server", "server", 1, true, false]]);
  assert.equal(f.hosts[1].info?.online, false, "an offline host still has threads, so it counts");
});

test("V0.169: row 1 is hidden with one host; row 2 with one project in view", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "working", "p_web", "desk"));
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: S() })), { hosts: [], projects: ["web", "fyr"] });
  const one = byId(T("a", "needs", "p_fyr", "desk"), T("b", "working", "p_fyr", "lab"));
  assert.deepEqual(names(activeFilters({ ...base, threads: one, sel: S() })), { hosts: ["lab", "desk"], projects: [] });
});

test("folded sections are not considered, in either row", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "settled", "p_web", "lab"), T("c", "snoozed", "p_home", "server"));
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: S() })), { hosts: [], projects: [] });
  assert.deepEqual(names(activeFilters({ ...base, threads, open: { settled: true }, sel: S() })), { hosts: ["lab", "desk"], projects: ["web", "fyr"] });
});

test("V0.169: a host turned off hides its threads, and its projects fall out of row 2", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "lab"), T("c", "needs", "p_home", "lab"), T("d", "needs", "p_web", "desk"));
  const f = activeFilters({ ...base, threads, sel: S(["lab"]) });
  assert.deepEqual(names(f), { hosts: ["lab", "desk"], projects: ["web", "fyr"] }, "home is only on lab: it falls out; web is on desk too: it stays");
  assert.deepEqual(f.hosts.map((h) => [h.name, h.on, h.n, h.last]), [["lab", false, 2, false], ["desk", true, 2, true]], "row 1 keeps counting every host; desk is the last one on");
  assert.deepEqual(Object.keys(applyFilters(threads, S(["lab"]), projects)), ["a", "d"]);
});

test("V0.169: row 1 does not follow the project filter; row 2 follows row 1", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "lab"));
  // fyr is only on desk, yet row 1 still offers lab: the rows are separate, and row 1 comes first.
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: S([], "p_fyr") })).hosts, ["lab", "desk"]);
});

test("V0.169: a host that is off stays in row 1 when its last thread leaves, is folded away, or the front stops listing it", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "settled", "p_web", "lab"));
  const f = activeFilters({ ...base, threads, sel: S(["lab"]) });
  assert.deepEqual(f.hosts.map((h) => [h.name, h.on, h.n]), [["lab", false, 0], ["desk", true, 1]]);
  const g = activeFilters({ ...base, threads, sel: S(["gone"]) });
  assert.deepEqual(g.hosts.map((h) => [h.name, h.on, h.info]), [["desk", true, hosts[1]], ["gone", false, undefined]], "after the listed hosts, by name");
});

test("V0.134: a selected project stays, selected, after its last thread leaves; its row stays too", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "settled", "p_web", "desk"));
  const f = activeFilters({ ...base, threads, sel: S([], "p_web") });
  assert.deepEqual(f.projects.map((c) => [c.project.name, c.selected, c.n]), [["web", true, 0], ["fyr", false, 1]]);
  // A project picked whose threads are all on a host turned off stays, selected, too.
  const g = activeFilters({ ...base, threads: byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "lab")), sel: S(["lab"], "p_web") });
  assert.deepEqual(g.projects.map((c) => [c.project.name, c.selected, c.n]), [["web", true, 0], ["fyr", false, 1]], "fyr is on desk, which is on");
});

test("V0.114: a quiet project is not shown or counted while locked; unlocked, it is; selected, it stays", () => {
  const quietUntil = "2026-11-17T18:00:00Z";
  const qp = [P("p_web", "web"), P("p_fyr", "fyr"), P("p_home", "home", { quietUntil })];
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "desk"), T("c", "needs", "p_home", "lab", { quietUntil }));
  const locked = activeFilters({ ...base, projects: qp, threads, sel: S() });
  assert.deepEqual(names(locked), { hosts: [], projects: ["web", "fyr"] }, "lab has only quiet threads: not counted");
  const unlocked = activeFilters({ ...base, projects: qp, threads, showUntil: NOW + 60_000, sel: S() });
  assert.deepEqual(names(unlocked).projects, ["web", "fyr", "home"]);
  assert.deepEqual(names(unlocked).hosts, [], "unlocked quiet threads stay out of the inbox (V0.46) unless their project is picked");
  const picked = activeFilters({ ...base, projects: qp, threads, sel: S([], "p_home") });
  assert.deepEqual(names(picked).projects, ["web", "fyr", "home"]);
  assert.equal(picked.projects[2].selected, true);
});

test("toggleHost: off and on again; the list stays in no order but without repeats", () => {
  assert.deepEqual(toggleHost([], "lab"), ["lab"]);
  assert.deepEqual(toggleHost(["lab"], "lab"), []);
  assert.deepEqual(toggleHost(["lab"], "desk"), ["lab", "desk"]);
});

test("V0.204: a click on the only host on ends the host filter; any other click toggles", () => {
  const hosts = [{ key: "desk", last: true }, { key: "lab", last: false }, { key: "old", last: false }];
  assert.deepEqual(clickHost(["lab", "old"], hosts, "desk"), []);
  assert.deepEqual(clickHost(["lab", "old"], hosts, "lab"), ["old"]);
  assert.deepEqual(clickHost([], [{ key: "desk", last: false }, { key: "lab", last: false }], "lab"), ["lab"]);
});

test("V0.client-refs: a host is filtered by its install id; two hosts with one name stay two chips", () => {
  const hs: HostInfo[] = [{ name: "box", online: true, id: hid("AAAA-1111-AAAA-1111") }, { name: "box", online: true, id: hid("BBBB-2222-BBBB-2222") }];
  const threads = byId(
    T("AAAA-1111-AAAA-1111/t_1", "needs", "p_web", "box", { hostId: "AAAA-1111-AAAA-1111" }),
    T("BBBB-2222-BBBB-2222/t_1", "needs", "p_web", "box", { hostId: "BBBB-2222-BBBB-2222" }),
  );
  const f = activeFilters({ ...base, hosts: hs, threads, sel: S() });
  assert.deepEqual(f.hosts.map((h) => [h.key, h.name, h.n]), [["AAAA-1111-AAAA-1111", "box", 1], ["BBBB-2222-BBBB-2222", "box", 1]]);
  assert.deepEqual(Object.keys(applyFilters(threads, S(["AAAA-1111-AAAA-1111"]), projects)).sort(), ["BBBB-2222-BBBB-2222/t_1"]);
});

test("hosts off (strings): what the device stored, else none off", () => {
  assert.deepEqual(parseJson(null, strings, []), []);
  assert.deepEqual(parseJson('["lab","desk"]', strings, []), ["lab", "desk"]);
  assert.deepEqual(parseJson("not json", strings, []), []);
  assert.deepEqual(parseJson('[1,"lab",null]', strings, []), ["lab"]);
});

test("applyFilters: hosts on AND project; a project that no longer exists filters nothing", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "lab"), T("c", "needs", "p_web", "desk"), thread("n", "done", "needs"));
  assert.deepEqual(Object.keys(applyFilters(threads, S(["lab"], "p_web"), projects)), ["c"]);
  assert.deepEqual(Object.keys(applyFilters(threads, S(["desk"]), projects)), ["b", "n"], "a thread that names no host is on no host the owner turned off");
  assert.deepEqual(Object.keys(applyFilters(threads, S([], "p_gone"), projects)), ["a", "b", "c", "n"]);
});

test("the empty state: a filter is on and nothing it lets through is in view", () => {
  const threads = byId(T("a", "settled", "p_web", "lab"), T("b", "settled", "p_web", "lab"));
  assert.equal(filterEmpty(threads, S(), {}), null, "no filter: the plain list");
  assert.equal(filterEmpty(threads, S(["desk"]), {}), "2 more in folded sections.");
  assert.equal(filterEmpty({}, S([], "p_web"), {}), "");
  assert.equal(filterEmpty(threads, S(["desk"]), { settled: true }), null, "a thread in view: no empty state");
});

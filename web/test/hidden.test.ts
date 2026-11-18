// SPEC V0.207: the hosts and projects this device never shows.
import { test } from "node:test";
import assert from "node:assert/strict";
import { NOTHING_HIDDEN, applyHidden, hiddenCount, hostIsHidden, lastShownHost, parseHidden, pushHiddenOf, setHostShown, setProjectShown, type Hidden } from "../src/hidden.ts";
import type { HostInfo } from "../src/hosts.ts";
import { initialStore, type Store } from "../src/store.ts";
import type { Project, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const H = (name: string, id?: string): HostInfo => ({ name, online: true, ...(id ? { id } : {}) });
const P = (id: string, name: string, host: string, hostId: string): Project => ({ id, rid: id.split("/")[1], name, host, hostId, threads: 1, lastUsed: "" }) as Project;
const T = (id: string, projectId: string, host: string, hostId: string): Thread => thread(id, "done", "seen", { projectId, host, hostId });

const hosts = [H("desk", "D"), H("lab", "L")];
const projects = [P("D/p1", "fyr", "desk", "D"), P("D/p2", "wayout", "desk", "D"), P("L/p3", "ops", "lab", "L")];
const threads = [T("D/a", "D/p1", "desk", "D"), T("D/b", "D/p2", "desk", "D"), T("L/c", "L/p3", "lab", "L")];
const store: Store = { ...initialStore(), host: "desk", hosts, projects, threads: Object.fromEntries(threads.map((t) => [t.id, t])), stalls: [{ host: "lab", at: "", what: "x", file: "" }] as Store["stalls"] };
const H0 = (h: Partial<Hidden>): Hidden => ({ ...NOTHING_HIDDEN, ...h });

test("nothing hidden is the same store", () => {
  assert.equal(applyHidden(store, NOTHING_HIDDEN), store);
});

test("a hidden project leaves the projects and takes its threads along", () => {
  const s = applyHidden(store, H0({ projects: ["D/p2"] }));
  assert.deepEqual(s.projects.map((p) => p.name), ["fyr", "ops"]);
  assert.deepEqual(Object.keys(s.threads), ["D/a", "L/c"]);
  assert.equal(s.hosts.length, 2);
});

test("a hidden host takes its threads, projects and stall along", () => {
  const s = applyHidden(store, H0({ hosts: ["L"] }));
  assert.deepEqual(s.hosts.map((h) => h.name), ["desk"]);
  assert.deepEqual(s.projects.map((p) => p.name), ["fyr", "wayout"]);
  assert.deepEqual(Object.keys(s.threads), ["D/a", "D/b"]);
  assert.equal(s.stalls.length, 0);
});

test("every host hidden hides no host, so the front stays usable", () => {
  const s = applyHidden(store, H0({ hosts: ["D", "L"] }));
  assert.equal(s.hosts.length, 2);
  assert.equal(Object.keys(s.threads).length, 3);
  assert.deepEqual(pushHiddenOf(hosts, projects, H0({ hosts: ["D", "L"] })).hosts, []);
});

test("the last host shown cannot be hidden", () => {
  const h = H0({ hosts: ["L"] });
  assert.equal(lastShownHost(hosts[0], hosts, h), true);
  assert.equal(lastShownHost(hosts[1], hosts, h), false);
  assert.equal(lastShownHost(hosts[0], hosts, NOTHING_HIDDEN), false);
});

test("the switches: a host by its id, by its name without one; on again removes either", () => {
  assert.deepEqual(setHostShown(NOTHING_HIDDEN, hosts[1], false).hosts, ["L"]);
  assert.deepEqual(setHostShown(NOTHING_HIDDEN, H("old"), false).hosts, ["old"]);
  assert.deepEqual(setHostShown(H0({ hosts: ["lab"] }), hosts[1], true).hosts, []);
  assert.equal(hostIsHidden(hosts[1], H0({ hosts: ["lab"] })), true);
  const h = setProjectShown(NOTHING_HIDDEN, "D/p2", false);
  assert.equal(hiddenCount(h), 1);
  assert.deepEqual(setProjectShown(h, "D/p2", true).projects, []);
});

test("parseHidden: what the device stored, else nothing", () => {
  assert.deepEqual(parseHidden('{"hosts":["L","L",1],"projects":["D/p2"]}'), { hosts: ["L"], projects: ["D/p2"] });
  for (const raw of [null, "nope", "null", "[]"]) assert.deepEqual(parseHidden(raw), NOTHING_HIDDEN);
});

test("the front gets host names and each host's own project ids", () => {
  assert.deepEqual(pushHiddenOf(hosts, projects, H0({ hosts: ["L"], projects: ["D/p2"] })), { hosts: ["lab"], projects: [{ host: "desk", id: "p2" }] });
});

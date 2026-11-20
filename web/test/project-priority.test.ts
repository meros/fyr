import { test } from "node:test";
import assert from "node:assert/strict";
import { linkProjects, patchFor, priorityLine, priorityOf, splitSharedPatch } from "../src/projects.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Project } from "../src/types.ts";
import { hid } from "./helpers.ts";

test("V0.project-priority: the page shows normal for none, and saves normal as null", () => {
  assert.equal(priorityOf({}), "normal");
  assert.equal(priorityOf({ priority: "high" }), "high");
  assert.equal(priorityOf({ priority: "low" }), "low");
  assert.equal(priorityOf({ priority: "weird" }), "normal");
  assert.deepEqual(patchFor("priority", "high"), { priority: "high" });
  assert.deepEqual(patchFor("priority", "low"), { priority: "low" });
  assert.deepEqual(patchFor("priority", "normal"), { priority: null });
  for (const v of ["high", "normal", "low"] as const) assert.ok(priorityLine(v).length > 0);
});

test("V0.project-priority: a linked project shares its priority, from its newest member", () => {
  assert.deepEqual(splitSharedPatch({ priority: "high", model: "m" }), { shared: { priority: "high" }, own: { model: "m" } });
  const host = (id: string) => ({ id, name: id, online: true }) as HostInfo;
  const a: Project = { id: "A/p1", rid: "p1", hostId: hid("A"), name: "Fyr", link: "l_1", sharedAt: "2026-11-19T10:00:00Z" };
  const b: Project = { id: "B/p9", rid: "p9", hostId: hid("B"), name: "Fyr", link: "l_1", priority: "high", sharedAt: "2026-11-19T11:00:00Z" };
  const [p] = linkProjects([a, b], [host("A"), host("B")]);
  assert.equal(p.id, "A/p1");
  assert.equal(p.priority, "high");
});

test("V0.linked-agent-settle: a linked project shares what an agent's settle_request does, from its newest member", () => {
  assert.deepEqual(splitSharedPatch(patchFor("agentSettle", "auto")), { shared: { agentSettle: "auto" }, own: {} });
  assert.deepEqual(splitSharedPatch(patchFor("agentSettle", "ask")), { shared: { agentSettle: null }, own: {} });
  const host = (id: string) => ({ id, name: id, online: true }) as HostInfo;
  const a: Project = { id: "A/p1", rid: "p1", hostId: hid("A"), name: "Fyr", link: "l_1", sharedAt: "2026-11-19T10:00:00Z" };
  const b: Project = { id: "B/p9", rid: "p9", hostId: hid("B"), name: "Fyr", link: "l_1", agentSettle: "auto", sharedAt: "2026-11-19T11:00:00Z" };
  const [p] = linkProjects([a, b], [host("A"), host("B")]);
  assert.equal(p.id, "A/p1");
  assert.equal(p.agentSettle, "auto");
});

// SPEC 0.5 V0.45, V0.trans-host-handover, decision 0039: the handover dialog says which rung runs; the divider says which one ran.
import { test } from "node:test";
import assert from "node:assert/strict";
import { dividerLine, effectivePlan, gitLimitationNote, handoverTargets, handoverTargetsForHost, parsePlan, planGuess, rungDone, rungText, suggestTargetFolder } from "../src/handover.ts";
import { modelChoices, nearestModel } from "../src/model-choice.ts";
import { buildConversation } from "../src/conversation.ts";
import { threadMenu } from "../src/menu.ts";
import { hid, thread } from "./helpers.ts";
import type { Profile, Project } from "../src/types.ts";

const profiles: Profile[] = [
  { id: "claude", name: "Claude", color: "", models: ["opus", "haiku"], defaultModel: "opus" },
  { id: "acme", name: "Claude (acme)", color: "", models: ["sonnet"] },
  { id: "bare", name: "Bare", color: "", models: [] },
];

test("the dialog names each rung in the owner's words", () => {
  assert.equal(rungText("resume"), "Full history: this conversation continues as it is");
  assert.equal(rungText("adopt"), "The new agent reads the old conversation");
  assert.equal(rungText("brief"), "The current agent writes a handover first");
});

test("the targets are the other profiles, each with its own model", () => {
  const t = thread("t1", "seen", "seen");
  assert.deepEqual(handoverTargets(t, profiles).map((p) => p.id), ["acme", "bare"]);
  assert.equal(nearestModel("opus", profiles[1]), "sonnet");
  assert.equal(nearestModel("opus", profiles[2]), "");
});

test("the plan comes from the host; without one, a session means resume", () => {
  assert.deepEqual(parsePlan({ rung: "adopt", note: "No session transcript" }), { rung: "adopt", note: "No session transcript" });
  assert.equal(parsePlan({ rung: "other" }), null);
  assert.equal(planGuess(thread("t", "seen", "seen", { sessionId: "s1" })).rung, "resume");
  assert.equal(planGuess(thread("t", "seen", "seen")).rung, "adopt");
});

test("the divider names both profiles and the time", () => {
  assert.equal(dividerLine({ from: "claude", to: "acme" }, profiles, "17:20"), "Handed over from Claude to Claude (acme) · 17:20");
  assert.equal(rungDone("resume"), "Full history");
  assert.equal(rungDone("adopt"), "The new agent read the old conversation");
});

test("a handover event becomes a divider; a failed one a failed line", () => {
  const items = buildConversation([
    { seq: 1, at: "2026-11-14T16:20:00Z", subject: "t", type: "handover", data: { from: "claude", to: "acme", rung: "adopt", note: "Resume failed: x" } },
    { seq: 2, at: "2026-11-14T16:21:00Z", subject: "t", type: "effect.error", data: { kind: "handover", error: "Dead could not start" } },
  ]);
  assert.equal(items[0].kind, "handover");
  const h = items[0] as Extract<(typeof items)[number], { kind: "handover" }>;
  assert.equal(h.from, "claude");
  assert.equal(h.rung, "adopt");
  assert.equal(h.note, "Resume failed: x");
  assert.equal(items[1].kind, "effect");
  assert.match((items[1] as { line: string }).line, /^Handover failed: Dead could not start/);
});

test("the thread menu offers Hand over…, disabled with the host's reason", () => {
  const t = thread("t1", "working", "working", { unavailable: { "thread.handover": "Wait for the run to end" } });
  const it = threadMenu(t).find((x) => !("sep" in x) && x.id === "handover");
  assert.ok(it && !("sep" in it));
  assert.equal(it.label, "Hand over…");
  assert.equal(it.disabled, "Wait for the run to end");
});

// SPEC 0.5 V0.50 (found live), V0.model-choice: a provider that cannot run (no token yet) is shown in Hand over but
// cannot be picked; the dialog starts on the first that can run.
test("V0.50 an unavailable provider is a target that cannot be picked", () => {
  const t = thread("t1", "seen", "seen", { profile: "claude" });
  const ps: Profile[] = [{ id: "x", name: "X", color: "", models: [], unavailable: "Paste the token" }, ...profiles];
  assert.deepEqual(handoverTargets(t, ps).map((p) => p.id), ["x", "acme", "bare"]);
  const c = modelChoices(handoverTargets(t, ps));
  assert.equal(c[0].label, "X · needs setup");
  assert.equal(c[0].why, "Paste the token");
});

// SPEC V0.trans-host-handover, decision 0039: cross-host handover allows the same provider id on another host,
// resolves linked projects / matching repos, adapts rungs across CLIs, and renders cross-host dividers & notices.
test("V0.trans-host-handover targets, effective plan, folder suggestion and git limitations", () => {
  const hostA = hid("HOST-A");
  const hostB = hid("HOST-B");
  const t = thread("HOST-A/t1", "seen", "seen", { hostId: hostA, profile: "claude", projectId: "HOST-A/p1" });

  // Across hosts, all providers on target host (including "claude") are valid targets.
  assert.deepEqual(handoverTargetsForHost(t, hostA, profiles).map((p) => p.id), ["acme", "bare"]);
  assert.deepEqual(handoverTargetsForHost(t, hostB, profiles).map((p) => p.id), ["claude", "acme", "bare"]);

  // Plan parses cli & git facts and falls back to adopt when CLI families differ.
  const plan = parsePlan({ rung: "resume", cli: "claude", git: { repo: "github.com/acme/app", branch: "feat/x", default: "main", dirty: 2, unpushed: 1, worktree: true } });
  assert.ok(plan && plan.git?.branch === "feat/x" && plan.git.dirty === 2 && plan.git.unpushed === 1 && plan.git.worktree === true);
  assert.equal(effectivePlan(plan, hostA, hostB, { ...profiles[0], cli: "claude" }).rung, "resume");
  assert.equal(effectivePlan(plan, hostA, hostB, { ...profiles[0], cli: "other" }).rung, "adopt");

  // Linked project takes priority over matching repo; matching repo works when unlinked; otherwise scratch fallback.
  const projA: Project = { id: "HOST-A/p1", rid: "p1", hostId: hostA, name: "App", color: "blue", icon: { kind: "monogram" }, folder: "/a/app", link: "l_1111111111111111", repo: "github.com/acme/app" };
  const projBLinked: Project = { id: "HOST-B/p2", rid: "p2", hostId: hostB, name: "App", color: "blue", icon: { kind: "monogram" }, folder: "/b/app", link: "l_1111111111111111", repo: "github.com/acme/app" };
  const linkedGroup: Project = { ...projA, members: [projA, projBLinked] };
  assert.deepEqual(suggestTargetFolder(t, hostB, [linkedGroup], "github.com/acme/app"), { cwd: "/b/app", projectId: "p2", reason: "Linked project “App”" });

  const projBRepo: Project = { id: "HOST-B/p3", rid: "p3", hostId: hostB, name: "RepoCopy", color: "teal", icon: { kind: "monogram" }, folder: "/b/repo", repo: "github.com/acme/app" };
  assert.deepEqual(suggestTargetFolder(t, hostB, [projBRepo], "github.com/acme/app"), { cwd: "/b/repo", projectId: "p3", reason: "Matches repo github.com/acme/app" });

  assert.equal(suggestTargetFolder(t, hostB, [], "github.com/acme/missing").cwd, "");

  // Git limitation notice informs without mutating git.
  const dirtyMsg = gitLimitationNote(plan.git, "host-a", "host-b");
  assert.match(dirtyMsg, /2 uncommitted files and 1 unpushed commit on host-a/);
  assert.match(dirtyMsg, /will not commit or push/);
  const cleanMsg = gitLimitationNote({ branch: "feat/x" }, "host-a", "host-b");
  assert.match(cleanMsg, /clean and pushed/);
  const noGitMsg = gitLimitationNote(undefined, "host-a", "host-b");
  assert.match(noGitMsg, /host-a.*host-b/);

  // Cross-host divider names both hosts and carries counterpart link metadata.
  assert.equal(dividerLine({ from: "claude", to: "claude", fromHost: "host-a", toHost: "host-b" }, profiles, "17:20"), "Handed over from Claude on host-a to Claude on host-b · 17:20");
  const items = buildConversation([
    { seq: 1, at: "2026-11-21T08:00:00Z", subject: "t", type: "handover", data: { from: "claude", to: "claude", fromHost: "host-a", fromHostId: "HOST-A", fromThread: "t_src", toHost: "host-b", toHostId: "HOST-B", toThread: "t_dst", rung: "resume" } },
  ]);
  const h = items[0] as Extract<(typeof items)[number], { kind: "handover" }>;
  assert.equal(h.fromHost, "host-a");
  assert.equal(h.toHost, "host-b");
  assert.equal(h.toThread, "t_dst");
});

// SPEC 0.5 V0.24: the client survives what the host really sends. The Go test
// TestStateContractFixture writes test/fixtures/host-state.json from a real
// host; this test feeds it through the store and every pure function a screen
// uses, once as it is and once for each optional field removed (the host omits
// empty fields). Found live: a missing icon value blanked the whole client.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { applySnapshot, flatOrder, initialStore, sections, unavailableReason, upsertProject, upsertThread } from "../src/store.ts";
import { colorName, filterByProject, matchProjects, newProjectDraft, projectName, projectOf, projectSubtitle, resolveIcon } from "../src/projects.ts";
import { relTime, whenLabel } from "../src/format.ts";
import { decodeProject, decodeThread } from "../src/decode.ts";
import { blockedReason, needsGroup } from "../src/rows.ts";
import { hideQuiet, nextQuietEnd, quietLine, quietList, quietNote } from "../src/quiet.ts";
import { TH, snapshot, view } from "./helpers.ts";

const fixture = JSON.parse(readFileSync(new URL("./fixtures/host-state.json", import.meta.url), "utf8"));
const REQUIRED = new Set(["id"]); // a thread or project without an id is not data

/** Every way to remove one optional field, at any depth, from a thread or a project. */
function variants(): { label: string; state: any }[] {
  const out = [{ label: "as sent", state: structuredClone(fixture) }];
  const walk = (path: (string | number)[], v: any) => {
    if (v && typeof v === "object") {
      for (const k of Object.keys(v)) {
        const p = [...path, Array.isArray(v) ? Number(k) : k];
        if (!Array.isArray(v) && !REQUIRED.has(k)) {
          const s = structuredClone(fixture);
          let o = s;
          for (const x of p.slice(0, -1)) o = o[x];
          delete o[p[p.length - 1]];
          out.push({ label: p.join("."), state: s });
        }
        walk(p, v[k]);
      }
    }
  };
  fixture.threads.forEach((t: any, i: number) => walk(["threads", i], t));
  fixture.projects.forEach((p: any, i: number) => walk(["projects", i], p));
  for (const k of Object.keys(fixture)) {
    const s = structuredClone(fixture);
    delete s[k];
    out.push({ label: k, state: s });
  }
  return out;
}

function render(state: any) {
  const now = Date.parse("2026-01-01T00:00:00Z");
  // Slice 2b: the host's state is one host's in the snapshot; a frame names its host.
  const snap = snapshot({ now: state.now });
  let raw = applySnapshot(initialStore(), { ...snap, states: { [TH]: state } }, now);
  for (const t of state.threads ?? []) raw = upsertThread(raw, structuredClone({ ...t, hostId: TH, rev: (t.rev ?? 0) + 1 }));
  for (const p of state.projects ?? []) raw = upsertProject(raw, structuredClone({ ...p, hostId: TH }));
  const s = view(raw);
  const sec = sections(hideQuiet(s.threads, false, now));
  flatOrder(sec);
  nextQuietEnd(s.threads, s.projects, now);
  for (const t of Object.values(s.threads)) {
    const p = projectOf(t, s.projects);
    const name = projectName(t, s.projects);
    assert.equal(typeof name, "string");
    assert.equal(typeof newProjectDraft(t).folder, "string");
    resolveIcon(p?.icon, name);
    colorName(p?.color, name);
    relTime(t.activityAt, now);
    whenLabel(t.updatedAt, now);
    unavailableReason(t, "thread.send");
    assert.equal(typeof t.title, "string");
    assert.equal(typeof t.cwd, "string");
    assert.ok(t.attention && typeof t.attention.section === "string", "attention");
    if (t.attention.section === "needs") {
      assert.equal(typeof needsGroup(t), "string");
      blockedReason(t);
    }
  }
  for (const p of s.projects) {
    assert.equal(typeof p.name, "string");
    resolveIcon(p.icon, p.name);
    projectSubtitle(p);
    filterByProject(s.threads, p.id ?? null, s.projects);
    quietNote(p, now);
    quietLine(quietList(p.quiet));
  }
  matchProjects(s.projects, "a");
}

test("the host fixture has the edge cases (a thread with no project, an icon without value)", () => {
  assert.ok(fixture.threads.some((t: any) => !t.projectId), "a thread with no project");
  assert.ok(fixture.projects.some((p: any) => p.icon && !("value" in p.icon)), "an icon without value");
  // V0.55: one folder per project, no sources, workspaces or suggestions; a thread offers its folder.
  assert.ok(fixture.projects.every((p: any) => !("sources" in p) && !("repos" in p) && !("cwd" in p)), "no sources, repos or cwd");
  assert.ok(fixture.projects.some((p: any) => typeof p.folder === "string" && p.folder), "a project with a folder");
  assert.ok(fixture.threads.every((t: any) => !("workspaceId" in t) && !("suggest" in t)), "no workspace or suggestion");
  assert.ok(fixture.threads.some((t: any) => !t.projectId && t.newProject?.name === "lib" && t.newProject.folder.endsWith("/src/lib")), "a thread offers its folder");
  // V0.46: a project in its quiet hours and its thread carry quietUntil.
  assert.ok(fixture.projects.some((p: any) => Array.isArray(p.quiet) && quietList(p.quiet).length === p.quiet.length && p.quietUntil), "a quiet project, its quiet hours a list");
  assert.ok(fixture.threads.some((t: any) => t.quietUntil), "a thread in quiet hours");
  // V0.row-group-code: a thread in Needs you carries the host's group code and the two parts of its reason.
  const run = fixture.threads.find((t: any) => t.attention.section === "needs");
  assert.deepEqual(
    { group: run?.attention.group, word: run?.attention.word, subject: run?.attention.subject },
    { group: "approve", word: "Run", subject: "Mount the SD card" },
  );
  const row = decodeThread(run, TH);
  assert.equal(needsGroup(row), "approve");
  assert.deepEqual(blockedReason(row), { word: "Run", what: "Mount the SD card" });
});

test("SPEC 0.5 V0.55: decode keeps a new-project prefill with a name and drops one without", () => {
  const t = fixture.threads.find((x: any) => x.newProject?.name === "lib");
  assert.equal(decodeThread(t, TH).newProject?.name, "lib");
  assert.equal(decodeThread({ ...t, newProject: { folder: "/x" } }, TH).newProject, undefined);
  assert.deepEqual(decodeThread({ ...t, newProject: { name: "lib" } }, TH).newProject, { name: "lib", folder: "" });
  assert.equal(decodeProject({ name: "a" }).folder, "");
});

test("every screen function survives the host state with any optional field missing", () => {
  for (const v of variants()) {
    try {
      render(v.state);
    } catch (e) {
      assert.fail(`without ${v.label}: ${(e as Error).message}`);
    }
  }
});

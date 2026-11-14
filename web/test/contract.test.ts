// SPEC 0.5 V0.24: the client survives what the host really sends. The Go test
// TestStateContractFixture writes test/fixtures/host-state.json from a real
// host; this test feeds it through the store and every pure function a screen
// uses, once as it is and once for each optional field removed (the host omits
// empty fields). Found live: a missing icon value blanked the whole client.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { applySnapshot, flatOrder, initialStore, sections, unavailableReason, upsertProject, upsertThread } from "../src/store.ts";
import { colorName, filterByProject, makeProjectLabel, matchProjects, projectName, projectOf, projectSubtitle, resolveIcon, suggestionFor } from "../src/projects.ts";
import { relTime, whenLabel } from "../src/format.ts";
import { decodeThread } from "../src/decode.ts";
import { hideQuiet, nextQuietEnd, quietLine, quietList, quietNote } from "../src/quiet.ts";

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
  let s = applySnapshot(initialStore(), state, now);
  for (const t of state.threads ?? []) s = upsertThread(s, structuredClone({ ...t, rev: (t.rev ?? 0) + 1 }));
  for (const p of state.projects ?? []) s = upsertProject(s, structuredClone(p));
  const sec = sections(hideQuiet(s.threads, false, now));
  flatOrder(sec);
  nextQuietEnd(s.threads, s.projects, now);
  for (const t of Object.values(s.threads)) {
    const p = projectOf(t, s.projects);
    const name = projectName(t, s.projects);
    assert.equal(typeof name, "string");
    const sug = suggestionFor(t, s.projects);
    if (sug) {
      makeProjectLabel(sug);
      assert.ok(Array.isArray(sug.sources), "suggestion sources");
    }
    resolveIcon(p?.icon, name);
    colorName(p?.color, name);
    relTime(t.activityAt, now);
    whenLabel(t.updatedAt, now);
    unavailableReason(t, "thread.send");
    assert.equal(typeof t.title, "string");
    assert.equal(typeof t.cwd, "string");
    assert.ok(t.attention && typeof t.attention.section === "string", "attention");
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
  // V0.28: workspaces and a suggestion for a thread in a clone.
  assert.ok(fixture.threads.every((t: any) => typeof t.workspaceId === "string" && t.workspaceId), "a workspace per thread");
  assert.ok(fixture.threads.some((t: any) => !t.projectId && t.suggest?.name === "lib"), "a suggestion");
  // V0.46: a project in its quiet hours and its thread carry quietUntil.
  assert.ok(fixture.projects.some((p: any) => Array.isArray(p.quiet) && quietList(p.quiet).length === p.quiet.length && p.quietUntil), "a quiet project, its quiet hours a list");
  assert.ok(fixture.threads.some((t: any) => t.quietUntil), "a thread in quiet hours");
});

test("SPEC 0.5 V0.28: decode keeps a suggestion with a name and drops one without", () => {
  const t = fixture.threads.find((x: any) => x.suggest);
  assert.equal(decodeThread(t).suggest?.name, "lib");
  assert.equal(decodeThread({ ...t, suggest: { sources: [] } }).suggest, undefined);
  assert.deepEqual(decodeThread({ ...t, suggest: { name: "lib" } }).suggest, { name: "lib", sources: [] });
  assert.equal(decodeThread({ id: "t" }).workspaceId, "");
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

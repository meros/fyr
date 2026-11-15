// SPEC 0.5 V0.65: what changed since you looked: the look the client records, the Viewed marks that reset when the
// content changes, the since line, the range-diff rows after a rebase, and the agent's notes for this version.
import { test } from "node:test";
import assert from "node:assert/strict";
import { codeNotes, defaultMode, isViewed, lookOf, rangeDiffRows, sinceLabel, sinceLine, viewedLine, type ChangedFile, type ChangesDTO } from "../src/changes.ts";
import type { Change } from "../src/types.ts";

const A = "1".repeat(40);
const B = "2".repeat(40);
const H = "a".repeat(40);

const files: ChangedFile[] = [
  { path: "a.go", status: "modified", additions: 1, deletions: 0, binary: false, blob: A, viewed: true },
  { path: "b.go", status: "added", additions: 3, deletions: 0, binary: false, blob: B },
  { path: "gone.go", status: "deleted", additions: 0, deletions: 4, binary: false, blob: "" },
];

test("SPEC 0.5 V0.65: the look is the head and every listed file's blob; none outside git", () => {
  const c: ChangesDTO = { git: true, head: H, files };
  assert.deepEqual(lookOf(c), { head: H, files: { "a.go": A, "b.go": B, "gone.go": "" } });
  assert.equal(lookOf({ git: false }), null);
  assert.deepEqual(lookOf({ git: true, files: [] }), { head: "", files: {} });
});

test("SPEC 0.5 V0.65: viewed follows the host until the owner ticks; a mark holds only for its blob", () => {
  assert.equal(isViewed(files[0], {}), true);
  assert.equal(isViewed(files[1], {}), false);
  // Ticked here: viewed for this content.
  assert.equal(isViewed(files[1], { "b.go": B }), true);
  // Unticked here.
  assert.equal(isViewed(files[0], { "a.go": null }), false);
  // A local mark for older content does not count once the content changed.
  assert.equal(isViewed({ ...files[1], blob: "3".repeat(40) }, { "b.go": B }), false);
  assert.equal(viewedLine(files, {}), "1 of 3 viewed");
  assert.equal(viewedLine(files, { "b.go": B, "gone.go": "" }), "3 of 3 viewed");
  assert.equal(viewedLine([], {}), "");
});

test("SPEC 0.5 V0.65: the tab opens on New since you looked only when something is new", () => {
  const at = new Date(Date.parse("2026-11-15T10:00:00Z") - 3 * 3600_000).toISOString();
  const now = Date.parse("2026-11-15T10:00:00Z");
  const since = { head: H, at, mode: "ancestor" as const, commits: 2, files: files.slice(0, 2) };
  assert.equal(defaultMode({ git: true, files, since }), "since");
  assert.equal(defaultMode({ git: true, files, since: { ...since, files: [] } }), "whole");
  assert.equal(defaultMode({ git: true, files }), "whole");
  assert.equal(sinceLabel(since), "New since you looked · 2");
  assert.equal(sinceLine(since, now), "2 commits since you looked 3 h ago");
  assert.equal(sinceLine({ ...since, commits: 1 }, now), "1 commit since you looked 3 h ago");
  assert.equal(sinceLine({ ...since, commits: 0 }, now), "Uncommitted edits since you looked 3 h ago");
  assert.equal(sinceLine({ ...since, mode: "rebased" }, now), "Rebased since you looked 3 h ago");
  assert.equal(sinceLine({ ...since, mode: "missing" }, now), "The commit you saw is gone; new by content since 3 h ago");
});

test("SPEC 0.5 V0.65: range-diff rows: kept, changed with its interdiff, added, dropped", () => {
  const text = [
    "1:  1111111 = 1:  2222222 change two",
    "2:  3333333 ! 2:  4444444 add fresh",
    "    @@ fresh.txt (new)",
    "    +f",
    "    ++g",
    "-:  ------- > 3:  5555555 new tests",
    "3:  6666666 < -:  ------- old debug commit",
    "",
  ].join("\n");
  const rows = rangeDiffRows(text);
  assert.deepEqual(
    rows.map((r) => [r.kind, r.old ?? "", r.new ?? "", r.subject]),
    [
      ["same", "1111111", "2222222", "change two"],
      ["changed", "3333333", "4444444", "add fresh"],
      ["added", "", "5555555", "new tests"],
      ["dropped", "6666666", "", "old debug commit"],
    ],
  );
  assert.equal(rows[1].body, "@@ fresh.txt (new)\n+f\n++g");
  assert.equal(rows[0].body, "");
  assert.deepEqual(rangeDiffRows(""), []);
});

test("SPEC 0.5 V0.65: the agent's notes come from the code change of this branch, newest 3 with a summary", () => {
  const rev = (id: string, min: number, summary?: string) => ({ id, summary, by: "agent", at: new Date(Date.UTC(2026, 10, 15, 9, min)).toISOString() });
  const base = { phase: "working" as const, phaseBy: "agent" as const, createdAt: "", updatedAt: "" };
  const changes: Change[] = [
    { ...base, id: "c1", key: "doc", kind: "doc", title: "Doc", branch: "feat/x", revisions: [rev("d1", 1, "doc")] },
    { ...base, id: "c2", key: "other", kind: "code", title: "Other", branch: "main", revisions: [rev("o1", 1, "other")] },
    {
      ...base, id: "c3", key: "x", kind: "code", title: "Lasso fix", branch: "feat/x",
      revisions: [rev("r1", 1, "first"), rev("r2", 2), rev("r3", 3, "third"), rev("r4", 4, "fourth"), rev("r5", 5, "fifth")],
    },
  ];
  const n = codeNotes(changes, "feat/x");
  assert.equal(n?.title, "Lasso fix");
  assert.deepEqual(n?.revisions.map((r) => r.summary), ["fifth", "fourth", "third"]);
  assert.equal(codeNotes(changes, "nope"), null);
  assert.equal(codeNotes(undefined, "feat/x"), null);
  assert.equal(codeNotes([{ ...changes[2], revisions: [rev("r2", 2)] }], "feat/x"), null);
});

// SPEC 0.5 V0.66 (7), (8): CI annotations on the diff lines of the Changes tab.
import { test } from "node:test";
import assert from "node:assert/strict";
import { parsePatch } from "../src/changes.ts";
import { ciNotes, notesFor, noteText, placeNotes } from "../src/ci-notes.ts";
import type { Change, CiAnnotation } from "../src/types.ts";

const PATCH = [
  "diff --git a/a.ts b/a.ts",
  "--- a/a.ts",
  "+++ b/a.ts",
  "@@ -1,3 +1,4 @@",
  " one",
  "-two",
  "+two!",
  "+three",
  " four",
  "",
].join("\n");

const note = (o: Partial<CiAnnotation>): CiAnnotation => ({ path: "a.ts", line: 1, level: "failure", message: "m", check: "lint", ...o });

function change(o: Partial<Change>): Change {
  return { id: "c", key: "k", kind: "code", title: "t", phase: "review", phaseBy: "observed", createdAt: "", updatedAt: "", ...o };
}

test("SPEC 0.5 V0.66 (8): an annotation shows under the new-side line it names", () => {
  const lines = parsePatch(PATCH).filter((l) => l.kind !== "meta");
  const out = placeNotes(lines, [note({ line: 3, message: "three is wrong" })]);
  const i = out.findIndex((l) => l.kind === "add" && l.new === 3);
  assert.equal(out[i + 1].kind, "note");
  assert.match(out[i + 1].text, /lint: three is wrong/);
  assert.equal(out.length, lines.length + 1);
});

test("SPEC 0.5 V0.66 (8): a line outside the diff shows above the first hunk with its number", () => {
  const lines = parsePatch(PATCH).filter((l) => l.kind !== "meta");
  const out = placeNotes(lines, [note({ line: 90, message: "far away" })]);
  assert.equal(out[0].kind, "note");
  assert.match(out[0].text, /line 90/);
  assert.equal(out[1].kind, "hunk");
});

test("SPEC 0.5 V0.66 (8): failures come before warnings and notices on the same line; no notes, the same lines", () => {
  const lines = parsePatch(PATCH).filter((l) => l.kind !== "meta");
  const out = placeNotes(lines, [note({ line: 1, level: "notice", message: "n" }), note({ line: 1, level: "warning", message: "w" }), note({ line: 1, message: "f" })]);
  const texts = out.filter((l) => l.kind === "note").map((l) => l.text);
  assert.equal(texts.length, 3);
  assert.match(texts[0], /f$/);
  assert.match(texts[1], /w$/);
  assert.match(texts[2], /n$/);
  assert.equal(placeNotes(lines, []), lines);
});

test("SPEC 0.5 V0.66 (8): the text is check, title and the message's first line", () => {
  assert.equal(noteText(note({ check: "vet", title: "unused", message: "x is unused\nmore" })), "✖ vet · unused: x is unused");
  assert.equal(noteText(note({ level: "warning", message: "careful" })), "▲ lint: careful");
  assert.equal(noteText(note({ line: 7, message: "m" }), true), "✖ lint: m (line 7)");
});

test("SPEC 0.5 V0.66 (7): the open changes' annotations, by file", () => {
  const changes = [
    change({ annotations: [note({ path: "a.ts" }), note({ path: "b.ts" })] }),
    change({ id: "d", phase: "abandoned", annotations: [note({ path: "a.ts", message: "old" })] }),
    change({ id: "e", phase: "landed", annotations: [note({ path: "a.ts", message: "landed" })] }),
  ];
  const all = ciNotes(changes);
  assert.equal(all.length, 2);
  assert.deepEqual(notesFor(all, "a.ts").map((n) => n.message), ["m"]);
  assert.deepEqual(ciNotes(undefined), []);
});

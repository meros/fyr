// V0.diff-line-comments (#641): comments on diff lines, the message they are sent as, and their drafts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { parsePatch } from "../src/changes.ts";
import { commentStore, formatComments, lineKey, location, parseComments, pickRange, type DiffComment } from "../src/diff-comments.ts";
import type { KV } from "../src/drafts.ts";

const PATCH = ["@@ -40,4 +40,5 @@ fn", " keep", "-old one", "+new one", "+new two", " tail", "\\ No newline at end of file", "@@ -90,2 +91,2 @@", " a", "-b", "+c"].join("\n");

function fakeStorage(): KV & { m: Map<string, string> } {
  const m = new Map<string, string>();
  return { m, getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v), removeItem: (k) => void m.delete(k) };
}

test("a range leaves out hunk headers and notes, in either order", () => {
  const lines = parsePatch(PATCH);
  const r = pickRange(lines, 8, 1); // from the second hunk's context line back to the first hunk's
  assert.deepEqual(
    r.map((l) => l.kind),
    ["ctx", "del", "add", "add", "ctx", "ctx"],
  );
  assert.equal(lineKey(r[0]), "ctx:40:40");
});

test("the location names the file, the side and the lines", () => {
  const lines = parsePatch(PATCH);
  assert.equal(location({ path: "web/src/x.ts", lines: pickRange(lines, 3, 4) }), "web/src/x.ts:41-42 (new)");
  assert.equal(location({ path: "web/src/x.ts", lines: pickRange(lines, 2, 2) }), "web/src/x.ts:41 (old)");
  assert.equal(location({ path: "web/src/x.ts", lines: pickRange(lines, 2, 3) }), "web/src/x.ts:41 (new), 41 (old)");
  assert.equal(location({ path: "a.go", lines: pickRange(lines, 1, 5) }), "a.go:40-43 (new), 41 (old)");
});

const whole: DiffComment = {
  id: "1",
  path: "web/src/x.ts",
  mode: "whole",
  head: "def5678",
  lines: [
    { kind: "add", new: 42, text: "const a = `x`;" },
    { kind: "add", new: 43, text: "```" },
  ],
  text: "Why a fence here?\n\nSecond paragraph.",
};
const since: DiffComment = { id: "2", path: "go/b.go", mode: "since", head: "def5678", since: "abc1234", lines: [{ kind: "del", old: 7, text: "return nil" }], text: "Keep this." };

test("the message quotes each comment's file, side, lines, head and text", () => {
  const msg = formatComments([whole, since]);
  assert.equal(
    msg,
    [
      "Comments on the diff (2):",
      "",
      "web/src/x.ts:42-43 (new) · whole branch at def5678",
      "````diff",
      "+const a = `x`;",
      "+```",
      "````",
      "Why a fence here?",
      "",
      "Second paragraph.",
      "",
      "go/b.go:7 (old) · since abc1234, at def5678",
      "```diff",
      "-return nil",
      "```",
      "Keep this.",
    ].join("\n"),
  );
  assert.deepEqual(parseComments(msg), [
    { where: "web/src/x.ts:42-43 (new) · whole branch at def5678", quote: ["+const a = `x`;", "+```"], text: "Why a fence here?\n\nSecond paragraph." },
    { where: "go/b.go:7 (old) · since abc1234, at def5678", quote: ["-return nil"], text: "Keep this." },
  ]);
});

test("any other message is not read as comments", () => {
  assert.equal(parseComments("Comments on the diff are great"), null);
  assert.equal(parseComments("Comments on the diff (1):\n\nnot a location"), null);
});

test("drafts survive a reload, per thread; clearing removes them", () => {
  const st = fakeStorage();
  const a = commentStore(st);
  const seen: string[] = [];
  a.subscribe((id) => seen.push(id));
  a.set("h/t1", [whole]);
  a.set("h/t2", [since]);
  const reload = commentStore(st);
  assert.deepEqual(reload.get("h/t1"), [whole]);
  assert.deepEqual(reload.get("h/t2"), [since]);
  reload.set("h/t1", []);
  assert.deepEqual(commentStore(st).get("h/t1"), []);
  assert.deepEqual(seen, ["h/t1", "h/t2"]);
  st.m.set("fyr.diffComments.h/t3", "{broken");
  assert.deepEqual(commentStore(st).get("h/t3"), []);
});

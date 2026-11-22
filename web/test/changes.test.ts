// SPEC 0.5 V0.25: the Changes tab's pure logic: patch parsing, path shortening, sorting, the summary line.
import { test } from "node:test";
import assert from "node:assert/strict";
import { parsePatch, sortFiles, statusMark, summaryLine, type ChangedFile } from "../src/changes.ts";
import { shortenPath } from "../src/paths.ts";

const PATCH = [
  "diff --git a/web/src/map.ts b/web/src/map.ts",
  "index 1111111..2222222 100644",
  "--- a/web/src/map.ts",
  "+++ b/web/src/map.ts",
  "@@ -1,4 +1,5 @@ export function map()",
  " const a = 1;",
  "-const b = 2;",
  "+const b = 3;",
  "+const c = 4;",
  " const d = 5;",
  "@@ -20,2 +21,2 @@",
  " x",
  "-y",
  "\\ No newline at end of file",
  "+y",
  "",
].join("\n");

test("SPEC 0.5 V0.25: a unified diff parses into lines with old and new numbers", () => {
  const lines = parsePatch(PATCH);
  const kinds = lines.map((l) => l.kind).join(" ");
  assert.equal(kinds, "meta meta meta meta hunk ctx del add add ctx hunk ctx del note add");
  const h = lines[4];
  assert.equal(h.text, "@@ -1,4 +1,5 @@ export function map()");
  assert.deepEqual([lines[5].old, lines[5].new, lines[5].text], [1, 1, "const a = 1;"]);
  assert.deepEqual([lines[6].old, lines[6].new, lines[6].text], [2, undefined, "const b = 2;"]);
  assert.deepEqual([lines[7].old, lines[7].new], [undefined, 2]);
  assert.deepEqual([lines[8].old, lines[8].new], [undefined, 3]);
  assert.deepEqual([lines[9].old, lines[9].new], [3, 4]);
  assert.deepEqual([lines[11].old, lines[11].new], [20, 21]);
  assert.deepEqual([lines[12].old, lines[12].new], [21, undefined]);
  assert.equal(lines[13].text, "No newline at end of file");
  assert.deepEqual([lines[14].old, lines[14].new], [undefined, 22]);
});

test("SPEC 0.5 V0.25: an empty or binary patch has no lines to draw", () => {
  assert.deepEqual(parsePatch(""), []);
  const bin = parsePatch("diff --git a/x.png b/x.png\nnew file mode 100644\nBinary files /dev/null and b/x.png differ\n");
  assert.ok(bin.every((l) => l.kind === "meta"));
});

test("SPEC 0.5 V0.25: a long path shortens in the middle and keeps the file name", () => {
  assert.equal(shortenPath("web/src/app.css", 40), "web/src/app.css");
  const s = shortenPath("internal/very/deep/folder/structure/that/goes/on/handler_test.go", 32);
  assert.ok(s.length <= 32, s);
  assert.ok(s.endsWith("/handler_test.go"), s);
  assert.ok(s.startsWith("internal/"), s);
  assert.ok(s.includes("…"), s);
  const long = shortenPath("a/" + "x".repeat(60) + ".ts", 20);
  assert.ok(long.length <= 20 && long.startsWith("…") && long.endsWith(".ts"), long);
});

test("SPEC 0.5 V0.25: files sort by path; marks and the summary line", () => {
  const files: ChangedFile[] = [
    { path: "web/z.ts", status: "modified", additions: 3, deletions: 1, binary: false },
    { path: "docs/a.md", status: "added", additions: 10, deletions: 0, binary: false },
    { path: "img.png", status: "added", additions: 0, deletions: 0, binary: true },
    { path: "new/b.go", oldPath: "old/b.go", status: "renamed", additions: 1, deletions: 1, binary: false },
  ];
  assert.deepEqual(sortFiles(files).map((f) => f.path), ["docs/a.md", "img.png", "new/b.go", "web/z.ts"]);
  assert.deepEqual(files.map((f) => statusMark(f.status)), ["M", "A", "A", "R"]);
  assert.equal(statusMark("deleted"), "D");
  assert.equal(summaryLine({ git: true, branch: "fix/x", base: "origin/main", mergeBase: "abc", commits: 3, files, truncated: false }), "4 files · +14 −2 · against origin/main · 3 commits");
  assert.equal(summaryLine({ git: true, branch: "x", base: "", mergeBase: "", commits: 0, files: files.slice(0, 1), truncated: false }), "1 file · +3 −1 · uncommitted work");
  assert.equal(summaryLine({ git: true, branch: "x", base: "main", mergeBase: "a", commits: 1, files: files.slice(0, 1), truncated: true }), "1,000+ files · +3 −1 · against main · 1 commit");
});

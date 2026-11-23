// V0.web-helpers: one way to shorten text and one way to write the home folder as ~.
import { test } from "node:test";
import assert from "node:assert/strict";
import { clip, firstLine, middleTruncate, oneLine } from "../src/format.ts";
import { tildePath } from "../src/paths.ts";
import { relPath } from "../src/conversation.ts";

test("clip cuts at the end with …, leaves no space before it, and keeps short text whole", () => {
  assert.equal(clip("acme-platform-infrastructure", 14), "acme-platform…");
  assert.equal(clip("exactly-14-ch!", 14), "exactly-14-ch!");
  assert.equal(clip("a b c d e f", 5), "a b…");
  assert.equal(clip("", 3), "");
  assert.equal(clip("line\none", 40), "line\none", "clip does not touch white space");
});

test("clip and middleTruncate never split an emoji's surrogate pair", () => {
  assert.equal(clip("ab😀cd", 4), "ab…");
  assert.equal(clip("ab😀cd", 5), "ab😀…");
  assert.equal(middleTruncate("😀😀😀😀😀😀", 6), "😀…😀");
  for (const s of [clip("x😀😀😀😀", 4), middleTruncate("x😀😀😀😀y", 5)]) assert.ok(!/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/.test(s), s);
});

test("oneLine flattens white space before it clips", () => {
  assert.equal(oneLine("  git\n  push\t origin  "), "git push origin");
  assert.equal(oneLine("git\npush origin main", 10), "git push…");
  assert.equal(oneLine("short", 40), "short");
});

test("firstLine is the first line that is not blank, flattened and clipped", () => {
  assert.equal(firstLine("\n  \n  make   test\nmore"), "make test");
  assert.equal(firstLine("a long first line here\nsecond", 10), "a long fi…");
  assert.equal(firstLine(""), "");
  assert.equal(firstLine("\n\n"), "");
});

test("middleTruncate keeps the start and the more telling end", () => {
  assert.equal(middleTruncate("abcdefghij", 7), "ab…ghij");
  assert.equal(middleTruncate("short", 40), "short");
  assert.equal(middleTruncate("src/components/deep/FilterSheet.tsx", 20).length, 20);
  assert.ok(middleTruncate("src/components/deep/FilterSheet.tsx", 20).endsWith("Sheet.tsx"));
});

test("tildePath writes a Linux, macOS or root home folder as ~", () => {
  assert.equal(tildePath("/home/u/src/acme"), "~/src/acme");
  assert.equal(tildePath("/home/u"), "~");
  assert.equal(tildePath("/Users/u/src/acme"), "~/src/acme");
  assert.equal(tildePath("/Users/u"), "~");
  assert.equal(tildePath("/root/src/acme"), "~/src/acme");
  assert.equal(tildePath("/root"), "~");
  assert.equal(tildePath("/rootfs/x"), "/rootfs/x", "a folder that only starts like root is not home");
  assert.equal(tildePath("/home"), "/home");
  assert.equal(tildePath("/srv/home/u"), "/srv/home/u");
  assert.equal(tildePath("/data/u/x", "/data/u"), "~/x", "a known home folder wins");
});

test("relPath outside the thread's folder shortens every home folder the same way", () => {
  assert.equal(relPath("/Users/u/notes.md", "/Users/u/src/acme"), "~/notes.md");
  assert.equal(relPath("/root/.bashrc", "/srv/acme"), "~/.bashrc");
  assert.equal(relPath("/home/u/src/acme/a.ts", "/home/u/src/acme"), "a.ts");
});

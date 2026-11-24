// V0.message-links-files: which text of a message is a file path, what the host is asked, and the markup.
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { MISSING_TTL_MS, STAT_BATCH, fileLines, knownFile, learnFiles, pathRef, resetFileLinks, type FileStat } from "../src/filelinks.ts";
import { renderMarkdown } from "../src/markdown.ts";
import { hid } from "./helpers.ts";

const H = hid("F7QK-2M9X");

beforeEach(() => resetFileLinks());

test("V0.message-links-files: what reads as a path, with its line", () => {
  const yes: [string, string, number?][] = [
    ["/home/owner/src/acme/main.go", "/home/owner/src/acme/main.go"],
    ["~/notes.md", "~/notes.md"],
    ["./run.sh", "./run.sh"],
    ["../acme/Makefile", "../acme/Makefile"],
    ["web/src/map.ts", "web/src/map.ts"],
    ["web/src/map.ts:12", "web/src/map.ts", 12],
    ["web/src/map.ts:12:5", "web/src/map.ts", 12],
    ["web/src/map.ts:12-20", "web/src/map.ts", 12],
    ["web/src/map.ts#L7", "web/src/map.ts", 7],
    ["web/src/map.ts#L7-L9", "web/src/map.ts", 7],
    ["README.md", "README.md"],
    ["main.go:3", "main.go", 3],
    [".gitignore", ".gitignore"],
    ["cmd/fyrd", "cmd/fyrd"],
    [" docs/SPEC.md ", "docs/SPEC.md"],
  ];
  for (const [text, path, line] of yes) assert.deepEqual(pathRef(text), line ? { path, line } : { path }, text);
  const no = ["", "main", "v1.2", "1.5", "npm run build", "foo.bar()", "a = b/c", "https://example.com/a.js", "mailto:owner@example.com", "web/src/", "/", "~", "..", "x".repeat(401) + ".go", "git@example.com:acme/web.git", "C:\\src\\a.go", "$HOME/a.go", "*.go", "{a,b}.ts", "e.g.", "--flag=web/a.ts"];
  for (const text of no) assert.equal(pathRef(text), null, text);
});

test("V0.message-links-files: a text file's lines", () => {
  assert.deepEqual(fileLines("a\nb\n"), ["a", "b"]);
  assert.deepEqual(fileLines("a\r\nb"), ["a", "b"]);
  assert.deepEqual(fileLines("a\n\n"), ["a", ""]);
  assert.deepEqual(fileLines(""), [""]);
});

const stat = (path: string): FileStat => ({ path, abs: `/home/owner/src/acme/${path}`, size: 10, kind: "text" });

test("V0.message-links-files: the host is asked once about a path, and its answer is kept", async () => {
  const calls: string[][] = [];
  const lookup = async (paths: string[]) => {
    calls.push(paths);
    return { ok: true as const, data: paths.filter((p) => p !== "none.ts").map(stat) };
  };
  assert.equal(knownFile(H, "t1", "a.ts"), undefined, "not asked yet");
  // The same path from two messages at once: one call.
  await Promise.all([learnFiles(H, "t1", ["a.ts", "none.ts", "a.ts"], lookup), learnFiles(H, "t1", ["a.ts"], lookup)]);
  assert.deepEqual(calls, [["a.ts", "none.ts"]]);
  assert.equal(knownFile(H, "t1", "a.ts")?.abs, "/home/owner/src/acme/a.ts");
  assert.equal(knownFile(H, "t1", "none.ts"), null);
  await learnFiles(H, "t1", ["a.ts", "none.ts"], lookup);
  assert.equal(calls.length, 1, "known paths are not asked again");
  assert.equal(knownFile(H, "t2", "a.ts"), undefined, "another thread has its own folders");
  assert.equal(knownFile(hid("ZZZZ-0000"), "t1", "a.ts"), undefined, "and another host its own files");
});

test("V0.message-links-files: a missing file is asked again after a while, a failed call at once", async () => {
  let at = 1_000_000;
  let fail = true;
  let calls = 0;
  const lookup = async (paths: string[]) => {
    calls++;
    return fail ? { ok: false as const, error: "Fyr is not reachable" } : { ok: true as const, data: calls > 2 ? paths.map(stat) : [] };
  };
  await learnFiles(H, "t1", ["new.ts"], lookup, () => at);
  assert.equal(knownFile(H, "t1", "new.ts", at), undefined, "a failed call records nothing");
  fail = false;
  await learnFiles(H, "t1", ["new.ts"], lookup, () => at);
  assert.equal(knownFile(H, "t1", "new.ts", at), null);
  at += MISSING_TTL_MS - 1;
  await learnFiles(H, "t1", ["new.ts"], lookup, () => at);
  assert.equal(calls, 2, "'none' holds for a while");
  at += 2;
  assert.equal(knownFile(H, "t1", "new.ts", at), undefined, "then it is unknown again");
  await learnFiles(H, "t1", ["new.ts"], lookup, () => at); // the agent wrote the file meanwhile
  assert.equal(knownFile(H, "t1", "new.ts", at)?.path, "new.ts");
});

test("V0.message-links-files: many paths go in calls of the host's limit", async () => {
  const sizes: number[] = [];
  const paths = Array.from({ length: STAT_BATCH + 5 }, (_, i) => `f${i}.ts`);
  await learnFiles(H, "t1", paths, async (p) => {
    sizes.push(p.length);
    return { ok: true as const, data: [] };
  });
  assert.deepEqual(sizes, [STAT_BATCH, 5]);
});

test("V0.message-links-files: the renderer marks a path in a thread's message and links a URL written as code", () => {
  assert.equal(renderMarkdown("see `web/src/map.ts:12`", H).trim(), '<p>see <code class="md-path" data-path="web/src/map.ts" data-line="12">web/src/map.ts:12</code></p>');
  assert.equal(renderMarkdown("the [config](vitest.config.ts)", H).trim(), '<p>the <span class="md-path" data-path="vitest.config.ts">config</span></p>');
  assert.equal(renderMarkdown("a [note](docs/my%20note.md)", H).trim(), "<p>a note</p>", "a path with a space is no candidate");
  assert.equal(renderMarkdown("run `npm test` and `a < b`", H).trim(), "<p>run <code>npm test</code> and <code>a &lt; b</code></p>");
  assert.equal(
    renderMarkdown("open `https://example.com/a?b=1&c=2`", H).trim(),
    '<p>open <a href="https://example.com/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer"><code>https://example.com/a?b=1&amp;c=2</code></a></p>',
  );
  // Outside a thread (a PR's text) there is no host to ask: nothing is marked.
  assert.equal(renderMarkdown("see `web/src/map.ts:12`").trim(), "<p>see <code>web/src/map.ts:12</code></p>");
  assert.equal(renderMarkdown("the [config](vitest.config.ts)").trim(), "<p>the config</p>");
  // A path cannot carry markup into its attribute.
  assert.ok(!renderMarkdown('[x](a"b/c.ts)', H).includes('data-path="a"'));
});

test("V0.forwarded-ports: a URL of a server on the agent's machine, and its mark in a thread's message", async () => {
  const { localUrl } = await import("../src/filelinks.ts");
  const { forwardOpenPath } = await import("../src/urls.ts");
  assert.deepEqual(localUrl("http://localhost:5173/"), { port: 5173, rest: "/" });
  assert.deepEqual(localUrl("http://localhost:5173"), { port: 5173, rest: "/" });
  assert.deepEqual(localUrl("http://127.0.0.1:3000/docs/a?b=1#top"), { port: 3000, rest: "/docs/a?b=1#top" });
  assert.deepEqual(localUrl("http://[::1]:8080?x=1"), { port: 8080, rest: "/?x=1" });
  assert.deepEqual(localUrl("http://0.0.0.0:8000/x"), { port: 8000, rest: "/x" });
  assert.deepEqual(localUrl("http://LOCALHOST/x"), { port: 80, rest: "/x" });
  for (const no of ["https://localhost:5173/", "http://localhost.example.com:5173/", "http://localhost:99999/", "http://localhost:0/", "http://example.com/", "http://127.0.0.10:80/", "localhost:5173", "http://localhost:5173x/"])
    assert.equal(localUrl(no), null, no);

  const marked = ' class="md-local" data-port="5173" data-rest="/a?b=1&amp;c=2"';
  assert.equal(renderMarkdown("see http://localhost:5173/a?b=1&c=2 now", H).trim(), `<p>see <a href="http://localhost:5173/a?b=1&amp;c=2"${marked} target="_blank" rel="noopener noreferrer">http://localhost:5173/a?b=1&amp;c=2</a> now</p>`);
  assert.ok(renderMarkdown("[the app](http://localhost:5173/a?b=1&c=2)", H).includes(marked), "a markdown link");
  assert.ok(renderMarkdown("`http://localhost:5173/a?b=1&c=2`", H).includes(marked), "a URL written as code");
  assert.ok(!renderMarkdown("see http://localhost:5173/ now").includes("md-local"), "outside a thread there is no host to forward to");
  assert.ok(!renderMarkdown("see https://example.com/ now", H).includes("md-local"));
  assert.equal(forwardOpenPath(H, "t_1", 5173, "/a?b=1#c"), `/forward/open?host=${H}&thread=t_1&port=5173&path=%2Fa%3Fb%3D1%23c`);
});

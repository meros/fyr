// Decision 0031 (slice 2a): every path to the front is written by urls.ts, so no call can leave its host out.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { frontPath, hostPath, hostUrl } from "../src/urls.ts";
import { hid } from "./helpers.ts";

test("a host's path carries the install id and the host protocol's own path", () => {
  assert.equal(hostPath(hid("F7QK-2M9X-AB34-CD56"), "/threads/t_1/events?after=3"), "/api/h/F7QK-2M9X-AB34-CD56/threads/t_1/events?after=3");
  assert.equal(hostPath(hid("a b"), "/state"), "/api/h/a%20b/state", "the id is escaped");
  assert.equal(frontPath("/state"), "/api/front/state");
});

test("a call without a host throws instead of reaching another; a shown URL is empty", () => {
  assert.throws(() => hostPath(hid(""), "/state"), /names the host/);
  assert.equal(hostUrl(hid(""), "/attachments/a_1"), "");
  assert.equal(hostUrl(hid("H1"), "/attachments/a_1"), "/api/h/H1/attachments/a_1");
});

// Where a module may write "/api/" in a string: the one URL function, the service worker (it never caches /api/), and the
// mock (it plays the front's agent text and fixture paths). Comments and the agent's own path pattern are not strings.
const ALLOWED = new Set(["urls.ts", "sw.ts", "mock.ts", "mock-providers.ts"]);

test("no module but urls.ts writes an /api/ path in a string", () => {
  const dir = new URL("../src/", import.meta.url).pathname;
  const bad: string[] = [];
  for (const f of readdirSync(dir)) {
    if (!/\.tsx?$/.test(f) || ALLOWED.has(f)) continue;
    readFileSync(join(dir, f), "utf8")
      .split("\n")
      .forEach((line, i) => {
        const t = line.trim();
        if (t.startsWith("//") || t.startsWith("*") || t.startsWith("/*")) return;
        if (/["'`][^"'`\n]*\/api\//.test(line)) bad.push(`${f}:${i + 1}: ${t.slice(0, 100)}`);
      });
  }
  assert.deepEqual(bad, [], "write the path with hostPath or frontPath (src/urls.ts)");
});

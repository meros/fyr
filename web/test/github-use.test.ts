// SPEC V0.173: Settings → Automation shows the account's GitHub REST limit and this host's top callers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { githubPause, githubRows, githubState } from "../src/updates.ts";

const since = "2026-11-17T19:00:00";

test("V0.173 the line names the calls left and the reset; low under a tenth", () => {
  const s = githubState({ rate: { limit: 5000, remaining: 312, reset: "2026-11-17T19:50:57", graphqlRemaining: 4900, at: since }, since, uses: [], calls: 41 });
  assert.equal(s.title, "312 of 5000 REST calls left · resets 19:50");
  assert.equal(s.desc, "This host's Fyr made 41 counted REST calls since 19:00.");
  assert.equal(s.low, true);
});

test("V0.173 before the first read the line says so", () => {
  assert.equal(githubState({ rate: null, since, uses: null, calls: 1 }).title, "Account limit not read yet");
});

test("V0.173 the rows name the kind, the repo and the free 304s", () => {
  const r = githubRows({ since, calls: 3, uses: [{ kind: "rest deployments", repo: "o/r", calls: 3, free: 2 }, { kind: "graphql pr view", calls: 1 }] });
  assert.deepEqual(r, [
    { title: "rest deployments · o/r", desc: "3 calls (2 answered 304, free)" },
    { title: "graphql pr view", desc: "1 call" },
  ]);
});

test("V0.178 while the budget holds polls back, a row says until when and why; none while they run", () => {
  const v = { since, calls: 0, uses: [], paused: { resource: "core", until: "2026-11-17T20:51:14", remaining: 120, why: "the account has 120 calls left; Fyr keeps 500 for merges, the updater and agents", deferred: 4 } };
  assert.deepEqual(githubPause(v), { title: "Polls wait until 20:51", desc: "The account has 120 calls left; Fyr keeps 500 for merges, the updater and agents. Merges and updates still run." });
  assert.equal(githubPause({ since, calls: 0, uses: [] }), null);
});

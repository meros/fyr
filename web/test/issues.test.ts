// V0.thread-issues: the linked issues as chips in the thread header (desktop and phone share issues.ts).
import { test } from "node:test";
import assert from "node:assert/strict";
import { issueChip, issuePrefix, issuesNeedRepo, threadIssues } from "../src/issues.ts";
import type { Issue, PullRequest } from "../src/types.ts";
import { thread } from "./helpers.ts";

const is = (n: number, p: Partial<Issue> = {}): Issue => ({
  url: `https://github.com/${p.repo ?? "acme/app"}/issues/${n}`,
  repo: "acme/app",
  number: n,
  via: "owner",
  at: new Date(Date.UTC(2026, 10, 27, 10, n)).toISOString(),
  ...p,
});

test("open issues come first, each group oldest first", () => {
  const got = threadIssues({ issues: [is(3, { state: "CLOSED" }), is(2), is(1, { state: "OPEN" })] });
  assert.deepEqual(
    got.map((i) => i.number),
    [1, 2, 3],
  );
  assert.deepEqual(threadIssues({}), []);
});

test("an issue chip says issue, its number, closed, and why it is linked", () => {
  const open = issueChip(is(4, { state: "OPEN", title: "Crash", via: "pr" }));
  assert.equal(open.text, "issue #4");
  assert.equal(open.cls, "");
  assert.equal(open.title, "acme/app#4 · open · Crash · a PR of this thread closes it");
  const closed = issueChip(is(5, { state: "CLOSED" }), "lib");
  assert.equal(closed.text, "lib issue #5 closed");
  assert.equal(closed.cls, "merged");
});

test("chips name their repo only when the header spans repos", () => {
  const pr: PullRequest = { url: "https://github.com/acme/app/pull/9", repo: "acme/app", number: 9, state: "OPEN", checks: "pass" };
  assert.equal(issuesNeedRepo([is(1)], [pr]), false);
  const other = is(2, { repo: "acme/lib" });
  assert.equal(issuesNeedRepo([is(1), other], [pr]), true);
  assert.equal(issuePrefix(other, true), "lib");
  assert.equal(issuePrefix(other, false), "");
});

test("the decoder keeps drawable issues only", () => {
  const t = thread("t1", "seen", "seen", { issues: [is(1), { url: 7 } as unknown as Issue, { url: "https://github.com/acme/app/issues/2", number: 2 } as unknown as Issue] } as never);
  assert.deepEqual(
    (t.issues ?? []).map((i) => i.number),
    [1, 2],
  );
  assert.equal(t.issues?.[1].at, "");
});

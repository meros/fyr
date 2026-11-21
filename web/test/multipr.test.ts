// V0.multi-pr: tracking multiple PRs per thread in chips, row badges, PR tabs and effect cards.
import { test } from "node:test";
import assert from "node:assert/strict";
import { changeChip, headerPr, headerPrs, prForEffect, threadPrs } from "../src/offers.ts";
import { hasMultipleRepos, pickPr, prUrgency, rowPrChip, shortRepo } from "../src/pr.ts";
import { rowState, stateNamesPr } from "../src/rows.ts";
import { defaultTab, tabLabel, threadTabs } from "../src/view.ts";
import { threadActions } from "../src/actions.ts";
import type { Change, PullRequest } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (m: number) => new Date(Date.UTC(2026, 10, 20, 10, m)).toISOString();
const pr = (n: number, p: Partial<PullRequest> = {}): PullRequest => ({
  url: `https://github.com/${p.repo ?? "acme/app"}/pull/${n}`,
  repo: p.repo ?? "acme/app",
  number: n,
  state: "OPEN",
  checks: "pass",
  ...p,
});
const ch = (id: string, p: Partial<Change>): Change => ({
  id,
  key: id,
  kind: "code",
  title: id,
  phase: "review",
  phaseBy: "observed",
  createdAt: at(0),
  updatedAt: at(0),
  ...p,
});

test("V0.multi-pr: threadPrs keeps all non-dismissed PRs (open first), headerPrs keeps open PRs only", () => {
  const p1 = pr(101, { repo: "acme/api", checks: "fail", title: "API" });
  const p2 = pr(102, { repo: "acme/web", checks: "pass", title: "Web" });
  const p3 = pr(103, { repo: "acme/docs", state: "MERGED", title: "Docs" });
  const p4 = pr(104, { repo: "acme/old", state: "MERGED", title: "Dismissed" });
  const done = { at: at(5), by: "observed" as const, reason: "merged" };
  const t = {
    pr: p2,
    changes: [
      ch("c1", { pr: p1, createdAt: at(1) }),
      ch("c2", { pr: p2, createdAt: at(2) }),
      ch("c3", { pr: p3, phase: "landed", done, createdAt: at(3) }),
      ch("c4", { pr: p4, phase: "landed", done, dismissed: true, createdAt: at(4) }),
    ],
  };

  assert.deepEqual(
    threadPrs(t).map((x) => x.number),
    [102, 101, 103],
    "open PRs newest first, then non-dismissed merged PRs; dismissed excluded",
  );
  assert.deepEqual(
    headerPrs(t).map((x) => x.number),
    [102, 101],
    "header chips show open PRs only",
  );
  assert.equal(headerPr(t)?.number, 102);
  assert.equal(changeChip({ pr: p3, changes: [...t.changes, ch("c5", { kind: "other", title: "Other", phase: "working" })] }), null, "open PR in changes hides non-PR change chip even when main PR is merged");
});

test("V0.multi-pr: rowPrChip picks the most urgent PR + count and lists every PR in tooltip", () => {
  const ready = pr(10, { checks: "pass", title: "Ready one" });
  const running = pr(11, { checks: "pending", title: "Running one" });
  const conflict = pr(12, { checks: "pass", conflict: true, title: "Conflicting" });
  const failing = pr(13, { checks: "fail", title: "Broken CI" });

  assert.ok(prUrgency(failing) < prUrgency(conflict));
  assert.ok(prUrgency(conflict) < prUrgency(ready));
  assert.ok(prUrgency(ready) < prUrgency(running));

  const single = rowPrChip([ready]);
  assert.equal(single?.text, "#10 ready");

  const multi = rowPrChip([running, ready, failing, conflict]);
  assert.equal(multi?.text, "#13 failing +3");
  assert.equal(multi?.cls, "fail");
  assert.deepEqual(multi?.title.split("\n"), [
    "#13 · Checks failing · Broken CI",
    "#12 · Conflicts with the base branch · Conflicting",
    "#10 · Ready to merge · Ready one",
    "#11 · Checks running · Running one",
  ]);
});

test("V0.multi-pr: PR tab shows PRs (N), stays visible for non-dismissed PRs, and picks selected PR", () => {
  const p1 = pr(10, { repo: "acme/api" });
  const p2 = pr(20, { repo: "acme/web" });
  const tOne = { live: null, pr: p1, changes: [ch("c1", { pr: p1 })] };
  const tTwo = { live: null, pr: p1, changes: [ch("c1", { pr: p1, createdAt: at(1) }), ch("c2", { pr: p2, createdAt: at(2) })] };
  const tNoMain = { live: null, pr: null, changes: [ch("c1", { pr: p1 })] };

  assert.equal(tabLabel(tOne, "pr"), "PR");
  assert.equal(tabLabel(tTwo, "pr"), "PRs (2)");
  assert.deepEqual(threadTabs(tNoMain), ["conv", "changes", "pr"]);
  assert.equal(defaultTab(tNoMain, "pr"), "pr");

  assert.equal(hasMultipleRepos([p1, p2]), true);
  assert.equal(hasMultipleRepos([p1, pr(11, { repo: "acme/api" })]), false);
  assert.equal(shortRepo("acme/api"), "api");

  const list = threadPrs(tTwo);
  assert.equal(pickPr(list, p1.url, p2.url)?.number, 10, "explicit selection wins");
  assert.equal(pickPr(list, undefined, p1.url)?.number, 10, "preferred open PR wins when nothing selected");
  assert.equal(pickPr(list, "https://github.com/acme/gone/pull/99", undefined)?.number, 20, "falls back to newest open PR");
});

test("V0.multi-pr: stateNamesPr keeps multi-PR chip visible; effect cards and dismissPR target specific PRs", () => {
  const p1 = pr(10, { repo: "acme/api", title: "API" });
  const p2 = pr(20, { repo: "acme/web", title: "Web", state: "MERGED" });
  const p3 = pr(30, { repo: "acme/cli", title: "CLI" });
  const d = { id: "d1", kind: "effect" as const, effect: { action: "pr.merge", args: { pr: p3.url } } };
  const t = thread("t1", "asking", "needs", {
    pr: p1,
    decisions: [d],
    changes: [
      ch("c1", { pr: p1, createdAt: at(1) }),
      ch("c2", { pr: p2, phase: "landed", done: { at: at(2), by: "observed", reason: "merged" }, createdAt: at(2) }),
      ch("c3", { pr: p3, createdAt: at(3) }),
    ],
  });

  assert.equal(prForEffect(t, d)?.number, 30);
  assert.equal(rowState(t), "Merge #30");
  assert.equal(stateNamesPr(t), false, "multi-PR chip stays visible so +1 is not hidden");

  const dismiss = threadActions(t, "thread").find((a) => a.id === "dismissPR");
  assert.equal(dismiss?.kind, "command");
  assert.equal(dismiss?.label, "Dismiss PR #20");
  assert.deepEqual(dismiss.kind === "command" ? dismiss.args : undefined, { pr: p2.url });
});

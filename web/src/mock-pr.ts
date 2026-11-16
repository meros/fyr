// PR details for ?mock (SPEC 0.5 V0.43): #88 has a failing check, a running one and 2 unresolved review threads;
// #1234 passes everything; #1201 is merged; any other PR gets details made from the thread's snapshot.

import type { PrCheck, PrDetail, PrReply } from "./pr.ts";
import type { ThreadDTO } from "./types.ts";

const iso = (ms: number) => new Date(ms).toISOString();

function run(name: string, state: PrCheck["state"], now: number, startMin: number, secs: number, workflow = "CI"): PrCheck {
  const startedAt = iso(now - startMin * 60_000);
  const done = state !== "running";
  return {
    name,
    workflow,
    state,
    status: done ? "COMPLETED" : "IN_PROGRESS",
    conclusion: state === "fail" ? "FAILURE" : state === "skipped" ? "SKIPPED" : state === "pass" ? "SUCCESS" : "",
    startedAt,
    completedAt: done ? iso(now - startMin * 60_000 + secs * 1000) : undefined,
    url: `https://github.com/norban/web/actions/runs/4417/job/${Math.abs(hash(name))}`,
  };
}

function hash(s: string): number {
  let h = 7;
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) | 0;
  return h % 100000;
}

const PASSING = ["build", "typecheck", "unit (node 22)", "unit (node 24)", "storybook", "e2e (chromium)", "e2e (webkit)", "a11y", "bundle size", "license check", "docs"];

const BODY_88 = `## In short

The listing page shows **cards** instead of rows on every width. A card keeps the address, price and the first photo; the rest moves into the detail sheet.

## What changed

- \`CardGrid.tsx\` lays out 1 to 4 columns from the container width, not the viewport.
- \`ListingCard.tsx\` replaces \`ListingRow.tsx\`; the row is deleted.
- Photos load lazily with a blurred placeholder.
- The sort menu moved into the toolbar.

## How it was tested

1. \`yarn test\` (412 tests)
2. Playwright on chromium and webkit, 360 to 1440 px
3. By hand on an iPhone 15 and a Pixel 9

<!-- reviewers: check the empty state -->

## Not in this PR

- Saved searches keep the old rows; issue #91 covers them.
- The map view is unchanged.

Closes #77.`;

export function mockPrDetail(t: ThreadDTO | undefined, now: number): PrReply {
  const pr = t?.pr;
  if (!t || !pr) return { error: "This thread has no PR." };
  const common = { url: pr.url, repo: pr.repo ?? "norban/web", number: pr.number, fetchedAt: iso(now - 4000) };
  if (pr.number === 88) {
    const checks = [run("lint", "fail", now, 12, 74), run("e2e (mobile-safari)", "running", now, 6, 0), ...PASSING.map((n, i) => run(n, "pass", now, 12, 40 + i * 13)), run("deploy preview", "skipped", now, 12, 0, "Preview")];
    return {
      ...common,
      title: "Listing cards v2",
      body: BODY_88,
      state: pr.state ?? "OPEN",
      draft: false,
      author: "claude-agent",
      head: "feat/listing-cards-v2",
      base: "main",
      headSha: "9c1e0b7d4a",
      additions: 412,
      deletions: 187,
      changedFiles: 14,
      labels: [
        { name: "feature", color: "a2eeef" },
        { name: "web", color: "0e8a16" },
      ],
      mergeable: "MERGEABLE",
      mergeState: pr.state === "MERGED" ? "UNKNOWN" : "BLOCKED",
      reviewDecision: "CHANGES_REQUESTED",
      reviews: [
        { author: "anna-k", state: "CHANGES_REQUESTED", at: iso(now - 40 * 60_000) },
        { author: "johan", state: "COMMENTED", at: iso(now - 25 * 60_000) },
      ],
      checks,
      threads: [
        {
          path: "web/src/listing/CardGrid.tsx",
          line: 42,
          outdated: false,
          url: "https://github.com/norban/web/pull/88#discussion_r1001",
          comments: [
            { author: "anna-k", body: "This recomputes the column count on every render. Use the `useContainerWidth` hook we already have, and memo the result.", at: iso(now - 40 * 60_000), url: "https://github.com/norban/web/pull/88#discussion_r1001" },
            { author: "claude-agent", body: "Will do.", at: iso(now - 30 * 60_000) },
          ],
        },
        {
          path: "web/src/listing/ListingCard.tsx",
          line: 118,
          outdated: true,
          url: "https://github.com/norban/web/pull/88#discussion_r1002",
          comments: [{ author: "johan", body: "The price should use `formatPrice(locale)`; in Swedish it reads **4 950 000 kr**, not 4,950,000 SEK.", at: iso(now - 25 * 60_000), url: "https://github.com/norban/web/pull/88#discussion_r1002" }],
        },
      ],
    } satisfies PrDetail;
  }
  if (pr.number === 1234) {
    return {
      ...common,
      title: t.title,
      body: "Fixes the lasso on touch screens: it no longer closes the sheet when a drag starts inside it.\n\nTested on iOS Safari and Chrome on Android.",
      state: pr.state ?? "OPEN",
      author: "claude-agent",
      head: t.branch || "fix/map-lasso-mobile",
      base: "main",
      additions: 64,
      deletions: 12,
      changedFiles: 3,
      labels: [{ name: "bug", color: "d73a4a" }],
      mergeable: "MERGEABLE",
      mergeState: "CLEAN",
      reviewDecision: "APPROVED",
      reviews: [{ author: "anna-k", state: "APPROVED", at: iso(now - 15 * 60_000) }],
      checks: PASSING.slice(0, 8).map((n, i) => run(n, "pass", now, 20, 30 + i * 11)),
      threads: [],
    } satisfies PrDetail;
  }
  const merged = pr.state === "MERGED";
  const checks: PrCheck[] = pr.checks === "fail" ? [run("test", "fail", now, 30, 60), run("build", "pass", now, 30, 50)] : pr.checks === "pending" ? [run("test", "running", now, 3, 0), run("build", "pass", now, 3, 40)] : [run("test", "pass", now, 30, 60), run("build", "pass", now, 30, 50)];
  return {
    ...common,
    title: pr.title || t.title,
    body: merged ? "Simplifies the lasso polygon before hit-testing; 6× faster on 10,000 points." : "",
    state: pr.state ?? "OPEN",
    author: "claude-agent",
    head: t.branch || "work",
    base: "main",
    headSha: (Math.abs(hash(pr.url)).toString(16) + "0123456789abcdef").slice(0, 12), // V0.97: Merge names the head
    additions: 37,
    deletions: 9,
    changedFiles: 2,
    labels: [],
    mergeable: pr.conflict ? "CONFLICTING" : "MERGEABLE",
    mergeState: merged ? "UNKNOWN" : pr.conflict ? "DIRTY" : pr.checks === "fail" ? "BLOCKED" : "CLEAN",
    reviewDecision: pr.review ?? "",
    reviews: merged || pr.review === "APPROVED" ? [{ author: "anna-k", state: "APPROVED", at: iso(now - 3 * 3600_000) }] : [],
    checks,
    threads: [],
  } satisfies PrDetail;
}

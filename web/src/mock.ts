// Fixtures for ?mock=1 (full stress set) and ?mock=2 (sparse: empty sections). A tiny in-memory fyrd:
// commands change the fixtures and push thread upserts, so the UI can be clicked through without a host.

import type { Change, CommandReply, Decision, FsDir, FsListing, Project, QuietHours, TaskDTO, FyrEvent, Offer, StateDTO, T3Candidate, ThreadDTO } from "./types.ts";
import type { ChangesDTO, FilePatchDTO } from "./changes.ts";
import type { PrReply } from "./pr.ts";
import type { Quota } from "./quota.ts";
import type { HostLoad, Slots, UndeclaredUse } from "./load.ts";
import type { Guard, GuardEvent } from "./guard.ts";
import type { Leftover, Temp } from "./leftovers.ts";
import { mockPrDetail } from "./mock-pr.ts";
import { quietList } from "./quiet.ts";
import type { SlashCommand } from "./slash.ts";
import type { DeployProgress } from "./deploy.ts";

// V0.35: a table that squeezed on the phone (a commit column, long change text).
const TRIAGE_TABLE =
  "Filed 6 issues, closed 2 duplicates.\n\n| Commit | Change |\n|---|---|\n| `3d0a470` | Finished in 14 min in place of the lone check; a live working line on the desktop |\n| `a0e8242` | Drafts survive a new message, a screen change and a reload |\n| `20d4939` | Fyr's own right-click menu; text fields keep the browser menu |\n";

const LONG_TITLE =
  "Migrate the listing search from the legacy Elasticsearch cluster to OpenSearch without downtime and keep the old index readable for rollback";
const LONG_PROJECT = "acme-platform-infrastructure-terraform-modules-and-tooling"; // 59 chars
const LONG_BRANCH = "feature/very-long-branch-name-without-any-spaces-that-keeps-going-and-going-until-it-overflows";
const LONG_URL =
  "https://github.com/acme/web/actions/runs/12345678901/job/98765432101234567890?pr=1234&check_suite_focus=true&very_long_query_parameter=abcdefghijklmnopqrstuvwxyz";
const LONG_OPTION =
  "Keep both gestures, but only enable tap-outside on screens wider than 400 px so the lasso never fights with the backdrop on small phones";

const SIX_LINE_QUESTION = [
  "The filter sheet currently closes on pointerdown on the backdrop, and the lasso starts on the same event.",
  "On iPhone mini this means every lasso stroke that starts near the sheet edge closes the sheet instead.",
  "I can change the close gesture, add a guard zone, or keep the current behaviour and only fix the lasso.",
  "Each choice changes how the sheet feels on every phone, not only the small ones.",
  "The tests in src/map/lasso cover the lasso, but nothing covers the sheet gestures yet.",
  "Which gesture should close the filter sheet?",
].join(" ");

interface Fixture {
  threads: ThreadDTO[];
  events: Record<string, FyrEvent[]>;
  t3: T3Candidate[];
}

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

function base(id: string, title: string, now: number, ageMin: number, extra: Partial<ThreadDTO>): ThreadDTO {
  const at = iso(now - ageMin * 60_000);
  return {
    id,
    title,
    cwd: `/home/owner/src/${extra.project ?? "acme/web"}`,
    project: "acme/web",
    profile: "claude",
    model: "claude-opus-5-5",
    sessionId: `sess-${id}`,
    createdAt: iso(now - (ageMin + 30) * 60_000),
    updatedAt: at,
    running: false,
    runStartedAt: null,
    activity: "",
    subagents: 0,
    activityAt: at,
    summary: "",
    failure: null,
    decisions: [],
    pr: null,
    settledAt: null,
    archivedAt: null,
    settleWhenDone: false,
    settleWhenMerged: false,
    source: "fyr",
    attention: { state: "seen", section: "seen", reason: "" },
    view: { seenAt: null, snoozeUntil: null },
    unavailable: extra.running ? {} : { "thread.snoozeWhenDone": "Nothing is working", "thread.settleWhenDone": "Nothing to wait for" },
    ...extra,
  };
}

function task(id: string, description: string, status: TaskDTO["status"], now: number, startedMin: number, extra: Partial<TaskDTO> = {}): TaskDTO {
  return { id, description, type: "local_agent", status, activity: "", summary: "", toolUses: 0, tokens: 0, startedAt: iso(now - startedMin * 60_000), updatedAt: iso(now - Math.max(0, startedMin - 1) * 60_000), ...extra };
}

const NOT_RUNNING = { "thread.interrupt": "No run is going", "thread.retry": "Nothing failed", "thread.snoozeWhenDone": "Nothing is working", "thread.settleWhenDone": "Nothing to wait for" };
/** V0.101: the needs the mock host left out while the owner parked them (Undo brings them back). */
const parkedNeeds = new Map<string, NonNullable<Change["need"]>>();

function askDecision(now: number): Decision {
  return {
    id: "d_ask",
    kind: "ask",
    createdAt: iso(now - 2 * 60_000),
    agent: "explore gestures",
    questions: [
      {
        question: SIX_LINE_QUESTION,
        header: "Close gesture",
        multiSelect: false,
        options: [
          { label: "Swipe down", description: "Matches the iOS sheet the app already uses." },
          { label: "Tap outside", description: "Simpler; conflicts with the lasso on small screens." },
          { label: LONG_OPTION, description: "Most forgiving; two code paths to test, and a width breakpoint to keep in sync with the CSS." },
        ],
      },
      {
        question: "Which devices should the new test cover?",
        header: "Devices",
        multiSelect: true,
        options: [
          { label: "iPhone mini", description: "The device from the bug report." },
          { label: "Pixel 8", description: "The most common Android phone in analytics." },
          { label: "iPad", description: "Sheet is a popover there." },
          { label: "Desktop", description: "Sanity check only." },
        ],
      },
    ],
  };
}

function ev(seq: number, at: number, subject: string, type: string, data: unknown): FyrEvent {
  return { seq, at: iso(at), subject, type, data };
}

function assistantText(text: string, parent: string | null = null) {
  return { type: "assistant", parent_tool_use_id: parent, message: { content: [{ type: "text", text }] } };
}
function toolUse(id: string, name: string, input: unknown, parent: string | null = null) {
  return { type: "assistant", parent_tool_use_id: parent, message: { content: [{ type: "tool_use", id, name, input }] } };
}
function toolResult(id: string, isError = false, parent: string | null = null, content = "ok") {
  return { type: "user", parent_tool_use_id: parent, message: { content: [{ type: "tool_result", tool_use_id: id, content, is_error: isError }] } };
}

function askEvents(id: string, now: number): FyrEvent[] {
  let s = 0;
  let t = now - 20 * 60_000;
  const e = (type: string, data: unknown, step = 20_000) => ev(++s, (t += step), id, type, data);
  return [
    e("user", {
      text: "The lasso tool closes the filter sheet on iPhone mini. Fix it and open a PR. Screenshot and log attached.",
      images: 1,
      attachments: [
        { id: "fx_img", name: "IMG_4170 – filtret stängs.png", mediaType: "image/png", size: 845_312 },
        { id: "fx_log", name: "playwright-mobile-safari-" + LONG_BRANCH + ".log", mediaType: "text/plain", size: 23_100_000 },
        { id: "fx_pdf", name: "spec.pdf", mediaType: "application/pdf", size: 120_000 },
      ],
    }),
    e("run.started", {}),
    e("msg", assistantText("I traced it to `FilterSheet.tsx`: the sheet closes on `pointerdown` on the backdrop, and the lasso starts on the same event.")),
    e("msg", toolUse("tu1", "Read", { file_path: "src/map/FilterSheet.tsx" })),
    e("msg", toolResult("tu1"), 400),
    e("msg", toolUse("tu2", "Task", { description: "explore gestures", prompt: "Explore how the other sheets in src/ui close: which event closes each one (pointerdown, click, swipe), and whether any of them starts a gesture on the same event.\n\nRead the code, do not change it. Report one line per sheet, then a recommendation for FilterSheet." })),
    e("msg", assistantText("Looking at the other sheets.", "tu2")),
    e("msg", toolUse("tu2a", "Grep", { pattern: "onPointerDown" }, "tu2")),
    e("msg", toolResult("tu2a", false, "tu2")),
    e("msg", toolUse("tu2b", "Read", { file_path: "src/ui/Sheet.tsx" }, "tu2")),
    e("msg", toolResult("tu2b", false, "tu2")),
    e("msg", assistantText("All other sheets close on swipe down.", "tu2")),
    e("msg", toolResult("tu2"), 100_000),
    e("msg", toolUse("tu3", "Edit", { file_path: "src/map/FilterSheet.tsx", old_string: "x", new_string: "y" })),
    e("msg", toolResult("tu3"), 1000),
    e("msg", toolUse("tu4", "Bash", { command: "yarn test src/map/lasso --reporter=dot && yarn lint src/map" })),
    e("msg", toolResult("tu4", true), 12_000),
    e("msg", toolUse("tu5", "Bash", { command: "yarn test src/map/lasso" })),
    e("msg", toolResult("tu5"), 9000),
    e(
      "msg",
      assistantText(
        "The lasso no longer closes the sheet, and the tests pass.\n\n- CI run: " +
          LONG_URL +
          "\n- Branch: `" +
          LONG_BRANCH +
          "`\n\nRaw HTML is escaped: <script>alert('x')</script> <b>not bold</b>\n\n```ts\nsheet.on('pointerdown', close)\n```\n\nBefore I wire the close gesture, two choices for you.",
      ),
    ),
    e("task", { kind: "started", id: "k3", description: "Check iPad popover", status: "running" }),
    e("task", { kind: "ended", id: "k3", description: "Check iPad popover", status: "completed", summary: "The popover already closes on swipe down." }),
    e("task", { kind: "ended", id: "k4", description: "Run the visual diff", status: "failed", summary: "Playwright could not start: no display" }),
    e("decision.opened", { decision: askDecision(now) }),
  ];
}

function simpleEvents(id: string, now: number, prompt: string, reply: string, failure: ThreadDTO["failure"] = null): FyrEvent[] {
  let s = 0;
  let t = now - 30 * 60_000;
  const e = (type: string, data: unknown) => ev(++s, (t += 30_000), id, type, data);
  return [
    e("user", { text: prompt, images: 0 }),
    e("run.started", {}),
    e("msg", toolUse(`${id}-b1`, "Bash", { command: "gh pr view --json state" })),
    e("msg", toolResult(`${id}-b1`)),
    e("msg", assistantText(reply)),
    e("run.ended", { failure, interrupted: false, durationMs: 840_000 }),
  ];
}

function fullFixture(now: number): Fixture {
  const threads: ThreadDTO[] = [];
  const events: Record<string, FyrEvent[]> = {};
  const add = (t: ThreadDTO, evs?: FyrEvent[]) => {
    threads.push(t);
    events[t.id] = evs ?? simpleEvents(t.id, now, `Work on: ${t.title}`, t.summary || "Done.", t.failure);
  };

  add(
    base("t_ask", "Fix map lasso on mobile", now, 2, {
      tasks: [
        task("k1", LONG_TITLE, "running", now, 14, { activity: "Bash yarn playwright test --project=mobile-safari " + LONG_BRANCH, toolUses: 9999, tokens: 12_345_678 }),
        task("k2", "Explore sheet gestures", "running", now, 3, { activity: "Read src/ui/Sheet.tsx", toolUses: 14, tokens: 143_218, toolUseId: "tu2" }),
        task("k3", "Check iPad popover", "completed", now, 20, { summary: "The popover already closes on swipe down.", toolUses: 6, tokens: 41_000 }),
        task("k4", "Run the visual diff", "failed", now, 25, { summary: "Playwright could not start: no display", toolUses: 2, tokens: 3_100 }),
        task("k5", "Översätt felmeddelanden", "stopped", now, 40, { activity: "Edit src/i18n/sv.json", toolUses: 1, tokens: 900 }),
      ],
      attention: { state: "asking", section: "needs", reason: "2 questions", group: "answer" },
      decisions: [askDecision(now)],
      summary: "The lasso no longer closes the sheet.",
      pr: { url: "https://github.com/acme/web/pull/1234", repo: "acme/web", number: 1234, state: "OPEN", checks: "pass", review: "", conflict: false },
      unavailable: { ...NOT_RUNNING, "thread.settle": "A question is open" },
    }),
    askEvents("t_ask", now),
  );
  add(
    base("t_fail", "Speed up e2e suite", now, 9, {
      attention: { state: "failed", section: "needs", reason: "API error", group: "unblock" },
      failure: { class: "api", message: "API error 529: overloaded, stopped after 14 min", at: iso(now - 9 * 60_000) },
      unavailable: { "thread.interrupt": "No run is going" },
    }),
  );
  add(
    base("t_block", "Listing cards v2", now, 21, {
      attention: { state: "blocked", section: "needs", reason: "CI failed", group: "unblock" },
      summary: "lint: 2 errors in CardGrid.tsx",
      pr: { url: "https://github.com/acme/web/pull/88", repo: "acme/web", number: 88, state: "OPEN", checks: "fail", conflict: false },
      unavailable: NOT_RUNNING,
    }),
  );
  add(
    base("t_approval", LONG_TITLE, now, 4, {
      project: LONG_PROJECT,
      attention: { state: "asking", section: "needs", reason: "Approval", group: "approve" },
      decisions: [
        {
          id: "d_appr",
          kind: "approval",
          createdAt: iso(now - 4 * 60_000),
          agent: "",
          tool: "Bash",
          input: { command: "terraform apply -auto-approve -target=module.opensearch " + LONG_BRANCH, cwd: "/home/owner/src/" + LONG_PROJECT },
        },
      ],
      unavailable: { ...NOT_RUNNING, "thread.settle": "A question is open" },
    }),
  );
  add(
    base("t_update", "Ship the Changes tab and the hardening", now, 3, {
      project: "Fyr",
      attention: { state: "asking", section: "needs", reason: "Update Fyr", group: "approve" },
      decisions: [
        {
          id: "d_upd",
          kind: "effect",
          createdAt: iso(now - 3 * 60_000),
          agent: "",
          effect: { action: "fyr.deploy", args: { dir: "/home/owner/git/fyr-deploy" }, reason: "Hardening (no more blank screens, no backwards deploys), the Changes tab and the UI polish batch; every test passes." },
        },
      ],
      unavailable: { ...NOT_RUNNING, "thread.settle": "A question is open" },
    }),
  );
  add(
    base("t_effect", "Åtgärda översättningar för sökfältet på ändringssidan", now, 6, {
      project: "acme/web",
      attention: { state: "asking", section: "needs", reason: "Merge", group: "approve" },
      decisions: [
        {
          id: "d_eff",
          kind: "effect",
          createdAt: iso(now - 6 * 60_000),
          agent: "",
          effect: { action: "pr.merge", args: { pr: "https://github.com/acme/web/pull/4170", method: "squash" }, reason: "CI is green and the review is approved; the översättningar are verified." },
        },
      ],
      pr: { url: "https://github.com/acme/web/pull/4170", repo: "acme/web", number: 4170, state: "OPEN", checks: "pass", review: "APPROVED", conflict: false },
      unavailable: { ...NOT_RUNNING, "thread.settle": "A question is open" },
    }),
  );
  add(
    base("t_done", "Triage sprint issues", now, 60, {
      project: "acme/ops",
      attention: { state: "blocked", section: "needs", reason: "Placement rows load in one read: Merge", group: "approve", word: "Merge", subject: "Placement rows load in one read" }, // V0.101: the host computes it from the Ready change below
      summary: "Filed 6 issues, closed 2 duplicates",
      unavailable: NOT_RUNNING,
    }),
  );
  // V0.73 (2): the owner's phone screen: six changes, five of them ended, a missing main PR, 20 finished agents and a
  // git checkout without a diff. The diff section must flow after the cards (it was drawn over the last two).
  {
    const at = (min: number) => iso(now - min * 60_000);
    const deploy = (n: number, title: string, rev: string, summary: string): Change => ({
      id: `c_dep${n}`, key: `host:fyr deploy ${n}`, kind: "host", title, targets: ["host:fyr"], phase: "abandoned", phaseBy: "owner",
      revisions: [{ id: rev, summary, by: "agent", at: at(60 * n) }], createdAt: at(60 * n + 5), updatedAt: at(60 * n), done: { at: at(60 * n), by: "owner", reason: "abandoned" },
    });
    add(
      base("t_ended", "Fyr: review, plan and Solid core", now, 3, {
        project: "fyr",
        branch: "main",
        attention: { state: "done", section: "seen", reason: "" },
        summary: "Created a worktree for V0.73.",
        pr: { url: "https://github.com/acme/app/pull/88", repo: "acme/app", number: 88, state: "MISSING", checks: "none", conflict: false },
        tasks: Array.from({ length: 20 }, (_, i) => task(`te${i}`, `Solid core worker ${i + 1}`, "completed", now, 40 - i, { summary: "Done.", toolUses: 12, tokens: 40_000 })),
        changes: [
          deploy(1, "Deploy V0.72 on top of the running build (main 4302001)", "4302001", "V0.72 on top of the running build 98b15a2"),
          deploy(2, "Deploy Solid core part 2 (main ef103c7) with the layout test", "98b15a2", "Solid core complete (#48, #49) plus the layout test at about 42 s instead of 284 s; the pending request builds this"),
          deploy(3, "Deploy the review and cleanup merge with fifteen bug fixes", "0d4d81c", "15 bug fixes, safe cleanup, V0.67 one snooze list, plan docs"),
          deploy(4, "Deploy Solid core part 1 (main f8f97d8) plus a test fix", "a9a8ec0", "f8f97d8 plus a test fix: TestMergeOneGuard failed the deploy's make test under load"),
          {
            id: "c_app88", key: "pr:acme/app#88", kind: "code", title: "acme/app#88", targets: ["repo:github.com/acme/app"], phase: "abandoned", phaseBy: "observed", label: "the PR does not exist on GitHub",
            evidence: [{ kind: "pr", url: "https://github.com/acme/app/pull/88", title: "#88" }], createdAt: at(400), updatedAt: at(300), done: { at: at(300), by: "observed", reason: "abandoned" },
          },
          {
            id: "c_v073", key: "branch:v073-live-upkeep", kind: "code", title: "A live change needs no upkeep (V0.73)", branch: "v073-live-upkeep", phase: "working", phaseBy: "agent",
            revisions: [{ id: "262ec20", summary: "A live change needs no upkeep", by: "agent", at: at(4) }], createdAt: at(30), updatedAt: at(4),
          },
        ],
        unavailable: NOT_RUNNING,
      }),
    );
  }
  // V0.37: a thread with a live view; the log has an earlier press from the view.
  {
    const st = { headline: "E2e green on mobile; PR not opened yet", steps: [{ label: "Reproduce", state: "done" }, { label: "Fix the lasso", state: "done" }, { label: "E2e on mobile", state: "done" }, { label: "Open the PR", state: "now" }], next: "Open the PR when you say so" };
    const at = iso(now - 12 * 60_000);
    const evs = simpleEvents("t_view", now, "Fix the lasso on mobile and keep a live view", "Fixed; the mobile e2e run is green.");
    let s = evs.length;
    evs.push(
      ev(++s, now - 20 * 60_000, "t_view", "view.published", { pageId: "pv_view", state: st }),
      ev(++s, now - 14 * 60_000, "t_view", "user", { id: "m_v1", text: "Run the e2e suite again", from: "view" }),
      ev(++s, now - 14 * 60_000, "t_view", "message.state", { id: "m_v1", state: "read" }),
      ev(++s, now - 13 * 60_000, "t_view", "msg", assistantText("The mobile e2e run is green: 48 passed.")),
    );
    add(
      base("t_view", "Lasso fix with a live view", now, 12, {
        attention: { state: "done", section: "seen", reason: "" },
        summary: "The mobile e2e run is green: 48 passed.",
        liveView: true,
        live: { pageId: "pv_view", state: st, updatedAt: at, checkedAt: at, headline: st.headline },
        unavailable: NOT_RUNNING,
      }),
      evs,
    );
  }
  add(
    base("t_fix", "Fix", now, 75, {
      project: LONG_PROJECT,
      attention: { state: "done", section: "seen", reason: "" },
      summary: "Pushed " + LONG_BRANCH + " and opened " + LONG_URL,
      pr: { url: "https://github.com/acme/infra/pull/9999", repo: "acme/infra", number: 9999, state: "OPEN", checks: "pending", conflict: true },
      unavailable: NOT_RUNNING,
    }),
  );

  const working: [string, string, string, number, number][] = [
    ["t_w1", "Babysit PR #4170", "Bash git rebase origin/main", 0, 3],
    ["t_w2", "Map perf", "Bash yarn vitest src/map", 2, 12],
    ["t_w3", LONG_TITLE, "Read src/index/build.rs", 1, 22],
    ["t_w4", "Sök på svenska: å, ä och ö i rubriker", "Edit src/i18n/sv.json", 0, 30],
  ];
  for (const [id, title, act, subs, min] of working) {
    add(
      base(id, title, now, 0.2, {
        running: true,
        runStartedAt: iso(now - min * 60_000),
        activity: act,
        subagents: subs,
        attention: { state: "working", section: "working", reason: "" },
        unavailable: { "thread.retry": "Nothing failed", "thread.markUnseen": "A run is going" },
      }),
    );
  }
  add(
    base("t_wait", "Översätt appen till svenska och verifiera alla skärmar", now, 0.5, {
      running: false,
      runStartedAt: null,
      attention: { state: "waiting", section: "working", reason: "2 agents" },
      tasks: [
        task("w1", "Design mobile translation", "running", now, 18, { activity: "Running build", toolUses: 14, tokens: 143_218 }),
        task("w2", LONG_TITLE, "running", now, 7, { activity: "Read src/i18n/sv.json", toolUses: 9999, tokens: 2_400_000 }),
        task("w3", "Collect strings", "completed", now, 30, { summary: "412 strings in 31 files", toolUses: 22, tokens: 88_000 }),
      ],
      unavailable: { "thread.retry": "Nothing failed", "thread.interrupt": "No run is going" },
    }),
  );

  add(
    base("t_s1", "Lasso polygon perf", now, 40, {
      summary: "Merged; the polygon is simplified before hit-testing.",
      pr: { url: "https://github.com/acme/web/pull/1201", repo: "acme/web", number: 1201, state: "MERGED", checks: "pass", conflict: false },
      unavailable: NOT_RUNNING,
    }),
  );
  // V0.43: an open PR that is ready to merge (the PR tab's Merge dialog).
  add(
    base("t_s2", "Nix flake update", now, 115, {
      project: "dotfiles",
      summary: "Updated nixpkgs to 26.05.",
      branch: "chore/flake-update",
      pr: { url: "https://github.com/meros/dotfiles/pull/57", repo: "meros/dotfiles", number: 57, state: "OPEN", checks: "pass", review: "APPROVED", conflict: false },
      unavailable: NOT_RUNNING,
    }),
  );
  // V0.55: a thread with no project offers New project from its folder.
  add(
    base("t_s4", "Sketch the lasso tool", now, 320, {
      project: "",
      cwd: "/home/owner/src/lasso-app",
      summary: "Drew a first lasso over the map.",
      unavailable: NOT_RUNNING,
    }),
  );
  add(base("t_s3", "Rensa gamla grenar i förrådet", now, 300, { project: LONG_PROJECT, summary: "Tog bort 14 grenar; två är kvar för att de har öppna PR.", unavailable: NOT_RUNNING }));

  add(
    base("t_z1", "Follow up on Stripe webhook retries", now, 200, {
      attention: { state: "snoozed", section: "snoozed", reason: "until tomorrow 06:00" },
      view: { seenAt: null, snoozeUntil: iso(now + 20 * 3600_000) },
      unavailable: NOT_RUNNING,
    }),
  );
  add(
    base("t_z3", "Refactor the billing importer", now, 120, {
      attention: { state: "snoozed", section: "snoozed", reason: "until work is done" },
      running: true,
      view: { seenAt: null, snoozeUntil: iso(now + 22 * 3600_000), snoozeWhenDone: true },
    }),
  );
  add(
    base("t_z2", "Check deploy on Monday", now, 500, {
      project: "acme/ops",
      attention: { state: "snoozed", section: "snoozed", reason: "until Monday 06:00" },
      view: { seenAt: null, snoozeUntil: iso(now + 72 * 3600_000) },
      unavailable: NOT_RUNNING,
    }),
  );
  add(
    base("t_arch", "Archived thread", now, 900, {
      attention: { state: "archived", section: "archived", reason: "" },
      archivedAt: iso(now - 800 * 60_000),
      unavailable: NOT_RUNNING,
    }),
  );

  // 9999 settled threads: the count and the long list.
  for (let i = 0; i < 9999; i++) {
    const t = base(`t_set${i}`, i % 7 === 0 ? `Settled thread ${i} – ändrade översättningar` : `Settled thread ${i}`, now, 30 + i * 0.14, {
      project: i % 3 === 0 ? "acme/web" : "acme/api",
      attention: { state: "settled", section: "settled", reason: "" },
      settledAt: iso(now - (30 + i * 0.14) * 60_000),
      summary: "Settled.",
      unavailable: { ...NOT_RUNNING, "thread.settle": "Already settled" },
    });
    threads.push(t);
  }

  events.t_w3 = [...events.t_w3, ...previewEvents("t_w3", events.t_w3.length, now, true)];
  // V0.22 in the stress set: a long-titled parent, and a message from it.
  for (const t of threads) if (t.id === "t_w2") t.parent = "t_w3";
  events.t_w2 = [...events.t_w2, ev(events.t_w2.length + 1, now - 60_000, "t_w2", "user", { text: "Use the new index for the map tiles; " + LONG_URL, from: "t_w3" })];

  const t3: T3Candidate[] = [
    { t3Id: "t3_a", title: "Refactor booking calendar", cwd: "/home/owner/src/acme/web", profile: "claudeAlt", model: "claude-opus-5-5", updatedAt: iso(now - 3600_000), hasTranscript: true, prUrl: "https://github.com/acme/web/pull/4100" },
    { t3Id: "t3_b", title: LONG_TITLE, cwd: "/home/owner/src/" + LONG_PROJECT, profile: "claudeAlt", model: "claude-opus-5-5", updatedAt: iso(now - 7200_000), hasTranscript: false },
    { t3Id: "t3_c", title: "Översätt e-postmallar", cwd: "/home/owner/src/acme/mail", profile: "codex", model: "gpt-5", updatedAt: iso(now - 86400_000), hasTranscript: true },
  ];
  // V0.59, V0.60: changes that are not a PR, with actions, on one thread.
  const ops = threads.find((x) => x.id === "t_done");
  if (ops) {
    const iso = (min: number) => new Date(now - min * 60_000).toISOString();
    const dg = (s: string) => s.length.toString(16).padStart(16, "0");
    ops.changes = [
      // V0.65: the code change of the mock branch; its revision summaries show above the files.
      {
        id: "c_lasso", key: "branch:fix/map-lasso-mobile", kind: "code", title: "Lasso works on mobile", branch: "fix/map-lasso-mobile", phase: "working", phaseBy: "agent",
        revisions: [
          { id: "3f2a9c1", summary: "Lasso selects on touch; render skips features out of view", by: "agent", at: iso(200) },
          { id: "51d0aa3", summary: "Map tests for 20 render cases", by: "agent", at: iso(185) },
          { id: "7be1c0d", summary: "Optional cluster tier and warm nodes in the search module; thicker stroke for selected features", by: "agent", at: iso(20) },
        ],
        createdAt: iso(210), updatedAt: iso(20),
      },
      // V0.changes-code-only (4): changes that are not code, as an old record holds them: Fyr ended them at load, and
      // they stand under Done here.
      {
        id: "c_scale", key: "k8s:prod/api replicas", kind: "infra", title: "Raise api replicas to 4", targets: ["k8s:gke-prod/api"], phase: "abandoned", phaseBy: "observed", label: "Fyr no longer tracks changes that are not code",
        revisions: [{ id: "sha256:9f2c41d07aa1", summary: "replicas 2 → 4, readiness probe 5 s", by: "agent", at: iso(70) }],
        evidence: [{ kind: "log", url: "https://console.cloud.google.com/logs", title: "pod logs" }],
        createdAt: iso(80), updatedAt: iso(62), done: { at: iso(62), by: "observed", reason: "abandoned" },
      },
      {
        id: "c_runbook", key: "doc:runbook", kind: "doc", title: "Runbook for api scaling", phase: "abandoned", phaseBy: "observed", label: "Fyr no longer tracks changes that are not code",
        evidence: [{ kind: "doc", url: "https://docs.example/runbook", title: "draft" }],
        createdAt: iso(50), updatedAt: iso(40), done: { at: iso(40), by: "observed", reason: "abandoned" },
      },
      // V0.66: a PR whose checks fail, with annotations on the diff lines, and a merged PR that GitHub deployed.
      {
        id: "c_lasso", key: "pr:acme/web#4123", kind: "code", title: "Map: draw only visible features", branch: "fix/map-lasso-mobile", targets: ["repo:github.com/acme/web"],
        phase: "review", phaseBy: "observed",
        conditions: [{ type: "checks", status: "fail", reason: "CI failed", source: "observed", at: iso(20) }],
        need: { cause: "failure", text: "CI failed", head: "93a7e57c1d2b3a4f5e6d7c8b9a0f1e2d3c4b5a69" },
        evidence: [{ kind: "pr", url: "https://github.com/acme/web/pull/4123", title: "#4123" }],
        annotations: [
          { path: "web/src/map/render.ts", line: 44, level: "failure", title: "no-undef", message: "'stats' is not defined.", check: "lint" },
          { path: "web/src/map/render.ts", line: 210, level: "warning", message: "Function renderMap has a complexity of 14. Maximum allowed is 12.", check: "lint" },
          { path: "web/test/map.test.ts", line: 7, endLine: 9, level: "failure", message: "Snapshot `map case 7` mismatched\n- 3 features\n+ 2 features", check: "test" },
        ],
        createdAt: iso(40), updatedAt: iso(20),
      },
      {
        id: "c_cal", key: "pr:acme/web#4100", kind: "code", title: "Refactor booking calendar", targets: ["repo:github.com/acme/web"], phase: "live", phaseBy: "observed",
        conditions: [
          { type: "checks", status: "ok", source: "observed", at: iso(300) },
          { type: "deploy:staging", status: "ok", source: "observed", at: iso(200) },
          { type: "deploy", status: "ok", source: "observed", at: iso(180) },
        ],
        evidence: [
          { kind: "pr", url: "https://github.com/acme/web/pull/4100", title: "#4100" },
          { kind: "deployment", url: "https://example.com", title: "production" },
        ],
        createdAt: iso(600), updatedAt: iso(180), done: { at: iso(180), by: "observed", reason: "live" }, // V0.98
      },
      // V0.136: the agent asked to merge while CI runs; the ask is held, the card says so, and nobody is asked yet.
      {
        id: "c_nightly", key: "pr:acme/collector#846", kind: "code", title: "fix(nightly): run every source and alarm on a short night", targets: ["repo:github.com/acme/collector"],
        phase: "review", phaseBy: "observed",
        need: { cause: "waiting", ask: "d_nightly", action: "pr.merge", head: "7333251aa0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5", text: "Waiting for CI: 3 of 5 checks done", muted: true },
        pr: { url: "https://github.com/acme/collector/pull/846", repo: "acme/collector", number: 846, state: "OPEN", checks: "pending", head: "7333251aa0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5", mergeState: "BLOCKED" },
        conditions: [{ type: "checks", status: "pending", source: "observed", at: iso(3) }],
        evidence: [{ kind: "pr", url: "https://github.com/acme/collector/pull/846", title: "#846" }],
        createdAt: iso(8), updatedAt: iso(3),
      },
      // V0.97: a green PR the host reads as ready to merge; its Merge names the head, and Merge all green takes it.
      {
        id: "c_rows", key: "pr:acme/web#4131", kind: "code", title: "Placement rows load in one read", targets: ["repo:github.com/acme/web"],
        phase: "review", phaseBy: "observed", ready: "Merge", need: { cause: "ready", text: "Merge", head: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678" },
        pr: { url: "https://github.com/acme/web/pull/4131", repo: "acme/web", number: 4131, state: "OPEN", checks: "pass", review: "APPROVED", head: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", mergeState: "CLEAN" },
        conditions: [{ type: "checks", status: "ok", source: "observed", at: iso(30) }],
        evidence: [{ kind: "pr", url: "https://github.com/acme/web/pull/4131", title: "#4131" }],
        offers: [{ id: "fyr.merge", label: "Merge", run: "fyr", effect: "pr.merge", primary: true, by: "observed", digest: dg("fyr.merge") }],
        createdAt: iso(60), updatedAt: iso(30),
      },
    ];
    // V0.68: a script in a scratch folder, as on the owner's phone: its confirmation shortens the paths.
    const scratch = "/home/owner/.local/share/fyr/scratch/20261115-130828-find-the-acme-claude-long-term-token";
    ops.offers = [
      { id: "smoke", on: "thread", label: "Run smoke tests", run: "script", argv: ["make", "smoke"], by: "agent", digest: dg("smoke") },
      { id: "token", on: "thread", label: "Set the Acme token", run: "script", argv: ["bash", `${scratch}/set-acme-token.sh`], cwd: scratch, by: "agent", digest: dg("token") },
    ];
  }
  return { threads, events, t3 };
}

function sparseFixture(now: number): Fixture {
  const threads = [
    base("t_only", "Nix flake update", now, 30, { project: "dotfiles", summary: "Updated nixpkgs.", unavailable: NOT_RUNNING }),
    base("t_set", "Old work", now, 200, { attention: { state: "settled", section: "settled", reason: "" }, unavailable: NOT_RUNNING }),
  ];
  const events: Record<string, FyrEvent[]> = {};
  for (const t of threads) events[t.id] = simpleEvents(t.id, now, "Update the flake", "Updated nixpkgs.");
  return { threads, events, t3: [] };
}


// ?mock=rail: the exact content of the first (light) frame of the rail direction (decision 0006).
export function railNow(real: number): number {
  const d = new Date(real);
  d.setHours(14, 32, 0, 0);
  return d.getTime();
}

function railFixture(now: number): Fixture {
  const min = (m: number) => iso(now - m * 60_000);
  const at = (h: number, m: number) => {
    const d = new Date(now);
    d.setHours(h, m, 0, 0);
    return d.toISOString();
  };
  const threads: ThreadDTO[] = [];
  const events: Record<string, FyrEvent[]> = {};
  const quiet = { ...NOT_RUNNING };
  const ask: Decision = {
    id: "d_rail",
    kind: "ask",
    createdAt: min(2),
    agent: "explore gestures",
    questions: [
      {
        question: "Which gesture should close the filter sheet?",
        header: "Close gesture",
        multiSelect: false,
        options: [
          { label: "Swipe down", description: "Matches the iOS sheet the app already uses." },
          { label: "Tap outside", description: "Simpler; conflicts with the lasso on small screens." },
          { label: "Both", description: "Most forgiving; two code paths to test." },
        ],
      },
      {
        question: "Which devices should the new test cover?",
        header: "Devices",
        multiSelect: true,
        options: [
          { label: "iPhone mini", description: "The device from the bug report." },
          { label: "Pixel 8", description: "The most common Android phone." },
          { label: "iPad", description: "The sheet is a popover there." },
        ],
      },
    ],
  };
  threads.push(
    base("t_lasso", "Fix map lasso on mobile", now, 2, {
      summary: "The lasso no longer closes the sheet, and the tests pass. Before I wire the close gesture, two choices for you.",
      tasks: [
        task("lt0", "Explore sheet gestures on iPad", "running", now, 2, { activity: "Read src/ui/Sheet.tsx", toolUses: 9, tokens: 61_400 }),
        task("lt1", "explore gestures", "completed", now, 4, { summary: "All other sheets close on swipe down.", toolUses: 14, tokens: 143_218 }),
        task("lt2", "explore gestures", "completed", now, 4, { summary: "iOS uses swipe down for sheets.", toolUses: 6, tokens: 52_000 }),
        task("lt3", "explore gestures", "completed", now, 4, { summary: "The lasso uses pointer capture.", toolUses: 4, tokens: 31_000 }),
      ],
      model: "claude-sonnet-5-5",
      branch: "fix/map-lasso-mobile",
      attention: { state: "asking", section: "needs", reason: "", group: "answer" },
      decisions: [ask],
      pr: { url: "https://github.com/acme/web/pull/1234", repo: "acme/web", number: 1234, state: "OPEN", checks: "pass", conflict: false },
      unavailable: { ...quiet, "thread.settle": "A question is open" },
    }),
    base("t_e2e", "Speed up e2e suite", now, 9, {
      attention: { state: "failed", section: "needs", reason: "API error", group: "unblock" },
      failure: { class: "api", message: "stopped after 14 min", at: min(9) },
      unavailable: { "thread.interrupt": "No run is going" },
    }),
    base("t_cards", "Listing cards v2", now, 21, {
      attention: { state: "blocked", section: "needs", reason: "CI failed", group: "unblock" },
      summary: "lint: 2 errors in CardGrid.tsx",
      pr: { url: "https://github.com/acme/web/pull/88", repo: "acme/web", number: 88, state: "OPEN", checks: "fail", conflict: false },
      unavailable: quiet,
    }),
    base("t_triage", "Triage sprint issues", now, 60, {
      project: "acme/ops",
      // V0.wakeups-via-watch: the agent set a wake-up for tomorrow 06:00; the row and the header show it
      wakes: [{ rule: "w_triage", at: iso(new Date(new Date(now).setHours(30, 0, 0, 0)).getTime()), note: "Check which of the 6 issues got an owner\nand ping the rest" }],
      // V0.120 (5): its model was turned off since; it shows as "opus 5.4 (off)" until another is picked.
      model: "claude-opus-5-4",
      attention: { state: "done", section: "seen", reason: "" },
      summary: "Filed 6 issues, closed 2 duplicates",
      unavailable: quiet,
    }),
  );
  // V0.143: a live agent that takes nothing Fyr writes; Needs you, with Restart above the composer.
  threads.push(
    base("t_stuck", "Area tree production switch", now, 38, {
      project: "acme/web",
      running: true,
      runStartedAt: min(39),
      activity: "Starting",
      tasks: [task("st1", "Run Cloud SQL proxy to the clone", "running", now, 120)],
      outbox: [{ id: "m_stuck", state: "sent" }],
      unresponsive: { since: min(51), at: min(38), reason: "it did not take a message within 1 min; a background task holds the process: Run Cloud SQL proxy to the clone" },
      attention: { state: "blocked", section: "needs", reason: "Agent not responding: it did not take a message within 1 min", group: "unblock" },
      unavailable: { "thread.retry": "The last run did not fail", "thread.markUnseen": "A run is going" },
    }),
  );
  // V0.header-one-block: a thread with a long title (the header test opens it).
  if (MOCK_LONGTITLE)
    threads.push(
      base("t_longtitle", LONG_TITLE, now, 12, {
        project: "acme/web",
        attention: { state: "done", section: "seen" },
      }),
    );
  const working: [string, string, string, number, number, "working" | "waiting"][] = [
    ["t_babysit", "Babysit PR #4170", "Bash git rebase origin/main", 0, 3, "working"],
    ["t_mapperf", "Map perf", "Bash yarn vitest src/map", 2, 12, "working"],
    ["t_deploy", "Deploy check", "waiting on monitor: deploy", 0, 9, "waiting"],
    ["t_search", "Search index", "Read src/index/build.rs", 1, 22, "working"],
  ];
  for (const [id, title, act, subs, m, state] of working) {
    const waiting = state === "waiting";
    threads.push(
      base(id, title, now, 0.1, {
        running: !waiting,
        runStartedAt: waiting ? null : min(m),
        activity: waiting ? "" : act,
        subagents: subs,
        tasks: waiting && MOCK_BGWAIT
          ? [task("dq1", "Wait for both experiments to finish", "running", now, 35.5, { type: "local_bash", background: true, tool: "Bash", quietSince: min(12) })] // 35.5: mid-minute, so the front's clock (the host's, less the reply's delay) reads "35 min", not "34" under load (V0.fix-flakes)
          : waiting
          ? [
              task("dt1", "Design mobile translation", "running", now, m, { activity: "Running build", toolUses: 14, tokens: 143_218 }),
              task("dt2", "Watch the deploy", "running", now, m - 2, { activity: act, toolUses: 3, tokens: 12_000 }),
            ]
          : id === "t_mapperf"
            ? [task("mt1", "Profile the map render", "running", now, 6, { activity: "Bash yarn vitest src/map --reporter=verbose", toolUses: 21, tokens: 310_000 })]
            : [],
        attention: { state, section: "working", reason: waiting ? (MOCK_BGWAIT ? "Background: Wait for both experiments to finish" : "2 agents") : "" },
        unavailable: { "thread.retry": "Nothing failed", "thread.markUnseen": "A run is going" },
      }),
    );
  }
  const seen: [string, string, string, number, number, boolean][] = [
    ["t_poly", "Lasso polygon perf", "acme/web", 14, 20, true],
    ["t_nix", "Nix flake update", "dotfiles", 13, 5, false],
    ["t_pw", "Bump Playwright to 1.58", "acme/web", 11, 48, false],
    ["t_copy", "Rewrite onboarding copy", "acme/web", 10, 12, false],
    ["t_snap", "Fix flaky map snapshot test", "acme/web", 9, 30, false],
    ["t_dish", "Pick a dishwasher under 8 000 kr", "Garden", 8, 5, false],
  ];
  for (const [id, title, project, h, m, merged] of seen) {
    const t = base(id, title, now, 0, { project, summary: "Done.", unavailable: quiet });
    t.activityAt = at(h, m);
    t.updatedAt = at(h, m);
    if (merged) t.pr = { url: "https://github.com/acme/web/pull/1201", repo: "acme/web", number: 1201, state: "MERGED", checks: "pass", conflict: false };
    threads.push(t);
  }
  const tomorrow6 = new Date(now);
  tomorrow6.setDate(tomorrow6.getDate() + 1);
  tomorrow6.setHours(6, 0, 0, 0);
  for (let i = 0; i < 3; i++) {
    threads.push(
      base(`t_snz${i}`, ["Follow up on Stripe webhook retries", "Check deploy on Monday", "Renew the staging certificate"][i], now, 200 + i * 100, {
        attention: { state: "snoozed", section: "snoozed", reason: "" },
        view: { seenAt: null, snoozeUntil: new Date(tomorrow6.getTime() + i * 86400_000).toISOString() },
        unavailable: quiet,
      }),
    );
  }
  for (let i = 0; i < 11; i++) {
    threads.push(
      base(`t_set${i}`, `Settled thread ${i + 1}`, now, 90 + i * 60, {
        attention: { state: "settled", section: "settled", reason: "" },
        settledAt: min(90 + i * 60),
        unavailable: { ...quiet, "thread.settle": "Already settled" },
      }),
    );
  }
  for (const t of threads) if (t.id !== "t_lasso") events[t.id] = simpleEvents(t.id, now, `Work on: ${t.title}`, t.id === "t_triage" ? TRIAGE_TABLE : t.summary || "Done.", t.failure);

  // The conversation of the design's thread.
  const ev2: FyrEvent[] = [];
  let seq = 0;
  const T = now - 6 * 60_000;
  const push = (sec: number, type: string, data: unknown) => ev2.push(ev(++seq, T + sec * 1000, "t_lasso", type, data));
  push(0, "user", { text: "The lasso tool closes the filter sheet on iPhone mini. Fix it and open a PR.", images: 0 });
  push(0, "run.started", {});
  push(5, "msg", assistantText("I traced it to `FilterSheet.tsx`: the sheet closes on `pointerdown` on the backdrop, and the lasso starts on the same event."));
  push(6, "msg", toolUse("r1", "Read", { file_path: "src/map/FilterSheet.tsx" }));
  push(6.4, "msg", toolResult("r1"));
  push(10, "msg", toolUse("a1", "Task", { description: "explore gestures", prompt: "How do other sheets close?" }));
  push(10, "msg", toolUse("a2", "Task", { description: "explore gestures", prompt: "Which gestures does iOS use?" }));
  push(10, "msg", toolUse("a3", "Task", { description: "explore gestures", prompt: "Does the lasso use pointer capture?" }));
  push(30, "msg", toolUse("a1r", "Grep", { pattern: "onPointerDown" }, "a1"));
  push(31, "msg", toolResult("a1r", false, "a1"));
  push(60, "msg", assistantText("All other sheets close on swipe down.", "a1"));
  push(80, "msg", toolResult("a2"));
  push(95, "msg", toolResult("a3"));
  push(110, "msg", toolResult("a1"));
  const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i}`).join("\n");
  push(112, "msg", toolUse("e1", "Edit", { file_path: "src/map/FilterSheet.tsx", old_string: lines(18), new_string: lines(42) }));
  push(113, "msg", toolResult("e1"));
  push(115, "msg", toolUse("b1", "Bash", { command: "yarn test src/map/lasso" }));
  push(127, "msg", toolResult("b1"));
  push(130, "msg", assistantText("The lasso no longer closes the sheet, and the tests pass. Before I wire the close gesture, two choices for you."));
  push(131, "decision.opened", { decision: ask });
  events.t_lasso = ev2;
  events.t_mapperf = calmTurn("t_mapperf", now);
  // V0.164: ?undeclared= has the host tell Map perf's agent about the heavy work it did not ask for (N9).
  if (MOCK_UNDECLARED) {
    const last = events.t_mapperf[events.t_mapperf.length - 1];
    events.t_mapperf.push(ev(last.seq + 1, now - 150_000, "t_mapperf", "slot", UNDECLARED_NOTICE));
  }
  // V0.22: Search index was started by Map perf's agent, which also wrote to Babysit PR #4170.
  for (const t of threads) if (t.id === "t_search") t.parent = "t_mapperf";
  for (const t of threads) if (t.id === "t_dish") t.cwd = "/home/owner/scratch/2026-11-14-dishwasher";
  // V0.149: &runs=1 adds a thread whose agent asked the owner to run two commands (a root one with a Nix store path,
  // one that ran and failed) and wrote a third in prose; &rootprompt=no says no polkit agent runs on its host.
  if (MOCK_RUNS) {
    const mint = "/nix/store/prc4lv5v5kppd7rgz3h2fh9wq3n4zj9y-nfc-mint-0.3.1/bin/nfc-mint --device /dev/sdc1 --profile lab-badge --out /run/media/owner/SD/badges";
    const req = (id: string, command: string, why: string, root: boolean): Offer => ({ id, on: "thread", label: root ? "Run as root" : "Run", run: "script", argv: ["bash", "-euo", "pipefail", "-c", command], cwd: "/home/owner/src/badges", request: true, command, why, root, by: "agent", digest: `d-${id}` });
    const t = base("t_run", "Mint the lab badges", now, 1, {
      project: "acme/ops",
      cwd: "/home/owner/src/badges",
      summary: "The minting needs the SD card mounted and root for the NFC device.",
      attention: { state: "blocked", section: "needs", reason: "Run: Mount the SD card and mint the badge onto it", group: "approve", word: "Run", subject: "Mount the SD card and mint the badge onto it" },
      offers: [
        req("run-1", `udisksctl mount -b /dev/sdc1\n${mint}`, "Mount the SD card and mint the badge onto it", true),
        req("run-2", "nix build .#badges --print-build-logs", "Build the badge images", false),
        // V0.next-step-chips: a prompt chip and an optional command under the newest message
        { id: "fix-build", on: "thread", label: "Fix the badge build", run: "agent", prompt: "Fix the failing badge build.", in: "this", by: "agent", digest: "d-fix" },
        { ...req("run-3", "nix flake check\nnix run .#badges -- --dry-run", "Dry-run the minting", false), optional: true },
      ],
      runs: [{ action: "run-2", label: "Run", by: "owner", at: min(4), ok: false, exit: 1, endedAt: min(3), output: Array.from({ length: 16 }, (_, i) => `badges> building layer ${i + 1}/16`).join("\n") + "\nerror: builder for '/nix/store/x3k9-badges.drv' failed with exit code 1" }],
      ...(MOCK_ROOTPROMPT ? { rootPrompt: "no polkit authentication agent runs in your session" } : {}),
      unavailable: quiet,
    });
    threads.unshift(t);
    events.t_run = [
      ev(1, now - 6 * 60_000, "t_run", "user", { text: "Mint the lab badges onto the SD card.", images: 0 }),
      ev(2, now - 6 * 60_000 + 1000, "t_run", "run.started", {}),
      ev(3, now - 5 * 60_000 - 2000, "t_run", "run.requested", { on: "thread", action: "run-1" }),
      ev(4, now - 5 * 60_000 - 1000, "t_run", "run.requested", { on: "thread", action: "run-2" }),
      ev(5, now - 5 * 60_000, "t_run", "msg", assistantText("The minting needs a device:\n\n```\n! sudo " + mint + "\n! udisksctl mount -b /dev/sdc1\n```\n\nThe minting runs in a bwrap sandbox, so it cannot open the device itself.")),
      ev(6, now - 5 * 60_000 + 1000, "t_run", "run.ended", { failure: null, interrupted: false, durationMs: 60_000 }),
    ];
  }
  events.t_babysit = [
    ...events.t_babysit,
    ev(events.t_babysit.length + 1, now - 2 * 60_000, "t_babysit", "user", { text: "The map tests pass on map-perf now. Rebase on it before you merge #4170.", from: "t_mapperf" }),
  ];
  return { threads, events, t3: [] };
}


// ---- previews (V0.19): drawn screenshots and an HTML chart, no network ----

function svgShot(w: number, h: number, label: string, dark = false): string {
  const bg = dark ? "#0f1419" : "#f6f7f8";
  const bar = dark ? "#2a3a4a" : "#14212d";
  const line = dark ? "#26303a" : "#dde1e6";
  const rows = Array.from({ length: Math.min(40, Math.floor((h - 160) / 56)) }, (_, i) => `<rect x="${w * 0.06}" y="${150 + i * 56}" width="${w * (0.5 + ((i * 37) % 40) / 100)}" height="14" rx="4" fill="${line}"/>`).join("");
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"><rect width="${w}" height="${h}" fill="${bg}"/><rect width="${w}" height="${Math.min(110, h * 0.12)}" fill="${bar}"/><text x="${w * 0.06}" y="70" font-family="sans-serif" font-size="${Math.max(18, w / 40)}" fill="#f6f7f8">${label}</text>${rows}<circle cx="${w * 0.7}" cy="${h * 0.55}" r="${Math.min(w, h) * 0.12}" fill="#e8960f" opacity=".55"/></svg>`;
  return "data:image/svg+xml," + encodeURIComponent(svg);
}

const CHART_HTML = `<!doctype html><meta charset="utf-8"><style>
:root{color-scheme:light dark;--ink:#111820;--bar:#2f6fb0;--bg:#fff}
@media (prefers-color-scheme:dark){:root{--ink:#e8ecf0;--bar:#6fa8e8;--bg:#161d24}}
body{margin:0;padding:16px;font:13px/18px system-ui,sans-serif;color:var(--ink);background:var(--bg)}
.r{display:grid;grid-template-columns:130px 1fr 44px;gap:8px;align-items:center;height:28px}
.b{height:14px;border-radius:4px;background:var(--bar);width:0;transition:width .6s}
</style><h3 style="margin:0 0 10px">Test durations, src/map</h3><div id=c></div><p id=n></p><script>
const d=[["lasso.test.ts",14.2],["tiles.test.ts",9.8],["snapshot.test.ts",7.1],["filter.test.ts",3.4],["sheet.test.ts",2.2],["util.test.ts",1.1]];
const m=Math.max(...d.map(x=>x[1]));c.innerHTML=d.map(([f,s])=>'<div class=r><span>'+f+'</span><div><div class=b data-w='+(s/m*100)+'></div></div><span>'+s+' s</span></div>').join("");
requestAnimationFrame(()=>document.querySelectorAll(".b").forEach(b=>b.style.width=b.dataset.w+"%"));n.textContent="Scripts run in the sandbox; the page has no origin.";
</script>`;


// V0.47: a WebGL page (one red triangle; headless Chrome draws it with SwiftShader). It marks body[data-gl] with what it saw.
const GL_HTML = `<!doctype html><meta charset="utf-8"><style>html,body{margin:0;height:100%;background:#111}canvas{display:block;width:100%;height:100%;touch-action:none}</style><canvas id=c></canvas><script>
const c=document.getElementById("c");c.width=c.clientWidth||320;c.height=c.clientHeight||200;const g=c.getContext("webgl",{preserveDrawingBuffer:true});
if(!g){document.body.dataset.gl="no-webgl"}else{const sh=(t,src)=>{const s=g.createShader(t);g.shaderSource(s,src);g.compileShader(s);return s};const p=g.createProgram();
g.attachShader(p,sh(g.VERTEX_SHADER,"attribute vec2 a;void main(){gl_Position=vec4(a,0,1);}"));g.attachShader(p,sh(g.FRAGMENT_SHADER,"precision mediump float;void main(){gl_FragColor=vec4(1,0,0,1);}"));
g.linkProgram(p);g.useProgram(p);g.bindBuffer(g.ARRAY_BUFFER,g.createBuffer());g.bufferData(g.ARRAY_BUFFER,new Float32Array([-.8,-.8,.8,-.8,0,.8]),g.STATIC_DRAW);
g.enableVertexAttribArray(0);g.vertexAttribPointer(0,2,g.FLOAT,false,0,0);g.viewport(0,0,c.width,c.height);g.clearColor(0,0,0,1);g.clear(g.COLOR_BUFFER_BIT);g.drawArrays(g.TRIANGLES,0,3);
const px=new Uint8Array(4);g.readPixels(c.width>>1,c.height>>1,1,1,g.RGBA,g.UNSIGNED_BYTE,px);document.body.dataset.gl=px[0]>200&&px[1]<50?"ok":"pixel "+px.join(",")}
</script>`;

// V0.37: the live view page as the host stores it: the Fyr theme around a fragment, the bridge first in <head>.
// TestMockBridgeMatches (Go) keeps VIEW_BRIDGE equal to the host's bridge.
export const VIEW_BRIDGE = `<script>(function(){
var state={},subs=[],n=0,wait={};
function post(m){m.jsonrpc="2.0";parent.postMessage(m,"*")}
function pick(o,p){return p.split(".").reduce(function(a,k){return a==null?a:a[k]},o)}
function fill(){var els=document.querySelectorAll("[data-text]");for(var i=0;i<els.length;i++){var v=pick(state,els[i].getAttribute("data-text"));els[i].textContent=v==null?"":typeof v=="object"?JSON.stringify(v):String(v)}}
window.fyr={get state(){return state},onState:function(f){subs.push(f);try{f(state)}catch(e){console.error(e)}},
send:function(t){t=String(t||"").trim();if(!t)return Promise.resolve(false);return ask("ui/message",{role:"user",content:[{type:"text",text:t}]})},
run:function(a){a=String(a||"").trim();if(!a)return Promise.resolve(false);return ask("ui/action",{action:a})}};
function ask(m,p){var id="m"+(++n);return new Promise(function(r){wait[id]=r;setTimeout(function(){if(wait[id]){delete wait[id];r(false)}},15000);post({id:id,method:m,params:p})})}
addEventListener("message",function(e){if(e.source!==parent)return;var d=e.data;if(d&&d.id&&wait[d.id]){var r=wait[d.id];delete wait[d.id];r(!d.error);return}if(!d||d.method!=="ui/notifications/state")return;
state=(d.params&&d.params.state)||{};fill();subs.forEach(function(f){try{f(state)}catch(x){console.error(x)}})});
document.addEventListener("click",function(e){var b=e.target&&e.target.closest&&e.target.closest("[data-send],[data-action]");if(!b)return;e.preventDefault();if(b.getAttribute("data-fyr")==="busy")return;
b.setAttribute("data-fyr","busy");b.setAttribute("aria-busy","true");
(b.hasAttribute("data-action")?fyr.run(b.getAttribute("data-action")):fyr.send(b.getAttribute("data-send")||b.textContent)).then(function(ok){b.removeAttribute("aria-busy");b.setAttribute("data-fyr",ok?"sent":"failed");setTimeout(function(){if(b.getAttribute("data-fyr")!=="busy")b.removeAttribute("data-fyr")},ok?1800:3000)})});
var last=-1;function size(){var h=Math.ceil(document.documentElement.getBoundingClientRect().height);if(h!==last){last=h;post({method:"ui/notifications/size-changed",params:{height:h}})}}
if(window.ResizeObserver)new ResizeObserver(size).observe(document.documentElement);addEventListener("load",size);
document.addEventListener("DOMContentLoaded",function(){fill();size()});
post({method:"ui/notifications/initialized",params:{}});
})();</script><style>[data-send],[data-action]{transition:opacity .12s}[data-send]:active,[data-action]:active{transform:scale(.97)}[data-fyr=busy]{opacity:.55;cursor:progress;pointer-events:none}[data-fyr=sent]::after{content:" ✓ Sent"}[data-fyr=failed]::after{content:" · Not sent";color:#c0392b}</style>`;
const VIEW_THEME = `
:root{--bg:#ffffff;--fg:#111820;--muted:#56616d;--line:#dde1e6;--accent:#e8960f;--surface:#f6f7f8;color-scheme:light dark}
@media (prefers-color-scheme: dark){:root{--bg:#161d24;--fg:#e8ecf0;--muted:#9aa6b2;--line:#26303a;--accent:#f5a524;--surface:#0f1419}}
html,body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
body{padding:16px}*{box-sizing:border-box}img,svg,canvas,video{max-width:100%;height:auto}
table{border-collapse:collapse}td,th{border:1px solid var(--line);padding:4px 8px}
`;
const VIEW_FRAGMENT = `<style>
.hd{font:600 17px/24px system-ui,sans-serif;margin:0 0 10px}
.st{display:flex;flex-wrap:wrap;gap:6px 14px;margin:0 0 12px;padding:0;list-style:none;font-size:13px}
.st li{display:flex;align-items:center;gap:6px}.st i{width:9px;height:9px;border-radius:50%;background:var(--line)}
.st .done i{background:#2f9e5b}.st .now i{background:var(--accent)}.st .now{font-weight:600}
.nx{color:var(--muted);font-size:13px;margin:0 0 12px}
.bt{display:flex;gap:8px;flex-wrap:wrap}.bt button{font:500 13px/18px system-ui,sans-serif;padding:7px 12px;border-radius:8px;border:1px solid var(--line);background:var(--surface);color:var(--fg);cursor:pointer}
.bt button.p{background:var(--fg);color:var(--bg);border-color:var(--fg)}
body{padding:12px 16px}
</style>
<p class="hd" data-text="headline"></p>
<ul class="st" id="steps"></ul>
<p class="nx">Next: <span data-text="next"></span></p>
<div class="bt"><button class="p" data-send="Open the PR">Open the PR</button><button data-send="Run the e2e suite again">Run e2e again</button><button data-send="Pause; I will look first">Pause</button></div>
<script>
fyr.onState(function(s){var el=document.getElementById("steps");el.innerHTML="";(s.steps||[]).forEach(function(x){var li=document.createElement("li");li.className=x.state;li.innerHTML="<i></i>";li.appendChild(document.createTextNode(x.label));el.appendChild(li)})});
</script>`;
const VIEW_HTML = `<!doctype html><html><head>${VIEW_BRIDGE}<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Live view</title><style>${VIEW_THEME}</style></head><body>${VIEW_FRAGMENT}</body></html>`;

export const MOCK_SHOTS: Record<string, string> = {
  pv_phone: svgShot(390, 844, "Filter sheet, iPhone mini"),
  pv_desk: svgShot(1280, 800, "Map, desktop 1280 × 800"),
  pv_tall: svgShot(390, 2600, "Listing page, full", true),
};
export const MOCK_PREVIEWS: Record<string, string> = {
  pv_chart: "data:text/html;charset=utf-8," + encodeURIComponent(CHART_HTML),
  pv_view: "data:text/html;charset=utf-8," + encodeURIComponent(VIEW_HTML),
  pv_gl: "data:text/html;charset=utf-8," + encodeURIComponent(GL_HTML),
  // V0.47: a folder preview; the layout test serves web/test/fixtures/preview-site here (with the host's CSP).
  pv_site: "/api/previews/pv_site/index.html",
};

function previewEvents(subject: string, start: number, now: number, stress: boolean): FyrEvent[] {
  let seq = start;
  const at = (m: number) => iso(now - m * 60_000);
  const out: FyrEvent[] = [
    ev(++seq, Date.parse(at(9)), subject, "preview", { kind: "image", attachmentId: "pv_phone", title: "Filter sheet on iPhone mini", caption: "After the fix: the lasso no longer closes the sheet.", width: 390, height: 844 }),
    ev(++seq, Date.parse(at(8)), subject, "preview", { kind: "image", attachmentId: "pv_desk", title: "Map at desktop width", caption: "", width: 1280, height: 800, url: "http://localhost:5173/map?lasso=1" }),
    ev(++seq, Date.parse(at(7)), subject, "preview", { kind: "html", attachmentId: "pv_chart", title: "Test durations by file", caption: "lasso.test.ts takes 37 % of the run.", width: 640, height: 300 }),
  ];
  if (stress)
    out.push(
      ev(++seq, Date.parse(at(6)), subject, "preview", { kind: "image", attachmentId: "pv_tall", title: LONG_TITLE, caption: "Full page at 390 px: " + LONG_URL, width: 390, height: 2600, url: LONG_URL }),
      ev(++seq, Date.parse(at(5)), subject, "msg", assistantText("Inline: ![the sheet](/api/attachments/pv_phone) and an outside image stays a link: ![remote](https://example.com/x.png)")),
      ev(++seq, Date.parse(at(4)), subject, "preview", { kind: "html", attachmentId: "pv_gl", title: "WebGL triangle", caption: "A 3D canvas in the sandbox." }),
      ev(++seq, Date.parse(at(3)), subject, "preview", { kind: "html", attachmentId: "pv_site", title: "Folder preview", caption: "index.html with its own script, module, JSON and image." }),
    );
  return out;
}


/** V0.21: one turn with every kind of machine line, plumbing, card tools and a mixed group (?mock=rail, Map perf). */
function calmTurn(subject: string, now: number): FyrEvent[] {
  const out: FyrEvent[] = [];
  let seq = 0;
  const T = now - 20 * 60_000;
  const e = (sec: number, type: string, data: unknown) => out.push(ev(++seq, T + sec * 1000, subject, type, data));
  const cwd = "/home/owner/src/acme/web";
  e(0, "user", { id: "m_map1", text: "Map perf: the vitest suite for src/map takes 38 s. Find the slow tests and fix them.", images: 0 });
  e(0, "message.state", { id: "m_map1", state: "read" });
  e(0, "run.started", {});
  e(1, "msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "thinking", thinking: "The suite is slow; profile first." }] } });
  e(2, "msg", toolUse("ts0", "Bash", { command: 'settask "[web] map perf"' }));
  e(2.1, "msg", toolResult("ts0"));
  e(2.2, "msg", toolUse("td0", "TodoWrite", { todos: [{ content: "Profile", status: "in_progress" }] }));
  e(2.3, "msg", toolResult("td0"));
  e(3, "msg", assistantText("Three files take 31 of the 38 seconds. Let me look at them."));
  const calls: [string, string, Record<string, unknown>, number, boolean, string?][] = [
    ["m1", "Read", { file_path: `${cwd}/src/map/lasso.test.ts` }, 0.3, false],
    ["m2", "Read", { file_path: `${cwd}/src/map/tiles/very/deeply/nested/folder/structure/tiles.test.ts` }, 0.2, false],
    ["m3", "Read", { file_path: `${cwd}/src/map/snapshot.test.ts` }, 0.2, false],
    ["m4", "Grep", { pattern: "beforeEach", path: `${cwd}/src/map` }, 0.4, false],
    ["m5", "Bash", { command: "yarn vitest src/map --reporter=verbose --pool=forks 2>&1 | tee /tmp/vitest.log", description: "Run the map tests with timings" }, 38, false, "✓ lasso.test.ts (14.2 s)\n✓ tiles.test.ts (9.8 s)\n✓ snapshot.test.ts (7.1 s)"],
    ["m6", "WebSearch", { query: "vitest isolate false speed up jsdom" }, 2.1, false],
    ["m7", "Edit", { file_path: `${cwd}/src/map/lasso.test.ts`, old_string: "a\nb\nc", new_string: Array.from({ length: 12 }, (_, i) => `l${i}`).join("\n") }, 0.6, false],
    ["m8", "Bash", { command: "yarn vitest src/map 2>&1 | tail -n 400", description: "Run the map tests again" }, 31, true, Array.from({ length: 400 }, (_, i) => (i === 399 ? "FAIL src/map/snapshot.test.ts > renders the sheet: snapshot mismatch" : `  ✓ src/map/case-${i}.test.ts > renders tile ${i} (${(i % 9) + 1} ms)`)).join("\n")],
  ];
  // Calls run one after another: each result lands its duration after its call.
  let t = 4;
  for (const [id, name, input, dur, err, text] of calls) {
    e((t += 0.1), "msg", toolUse(id, name, input));
    e((t += dur), "msg", toolResult(id, err, null, text ?? "ok"));
  }
  e((t += 1), "task", { kind: "started", id: "mt1", description: "Profile the map render", status: "running" });
  e((t += 40), "task", { kind: "ended", id: "mt1", description: "Profile the map render", status: "completed", summary: "Render takes 4 ms a tile" });
  e((t += 1), "task", { kind: "ended", id: "mt2", description: "Bisect the snapshot failure", status: "failed", summary: "Could not start a browser" });
  // show: tool loading and the call itself are hidden; only the cards remain
  e((t += 1), "msg", toolUse("q1", "ToolSearch", { query: "select:mcp__fyr__show" }));
  e((t += 0.2), "msg", toolResult("q1"));
  for (const [id, pv, title, w, h, kind] of [
    ["s1", "pv_phone", "Filter sheet on iPhone mini", 390, 844, "image"],
    ["s2", "pv_chart", "Test durations by file", 640, 300, "html"],
  ] as const) {
    e((t += 0.3), "msg", toolUse(id, "mcp__fyr__show", { path: kind === "image" ? "/tmp/shot.png" : undefined, html: kind === "html" ? "<div>…</div>" : undefined, title }));
    e((t += 0.6), "msg", toolResult(id, false, null, `attachment ${pv} · ${w}×${h}`));
    e((t += 0.1), "preview", { kind, attachmentId: pv, title, caption: kind === "image" ? "After the fix: the lasso no longer closes the sheet." : "lasso.test.ts takes 37 % of the run.", width: w, height: h });
  }
  // effect_request: the decision and its result, never the call
  e((t += 1), "msg", toolUse("ef1", "mcp__fyr__effect_request", { action: "deploy.staging", args: { ref: "map-perf" }, reason: "Tests pass" }));
  e((t += 0.2), "msg", toolResult("ef1", false, null, "Requested; the owner decides."));
  e((t += 30), "decision.closed", { id: "d_dep", answer: "Approve" });
  e((t += 60), "effect.result", { action: "deploy.staging", ok: true, text: "Deploy succeeded · build 20261114-112026\n\nhttps://staging.example/build/20261114-112026\n12 services restarted, 0 errors." });
  e((t += 0.1), "user", { text: "[Fyr] Your effect_request deploy.staging succeeded: Deploy succeeded · build 20261114-112026", images: 0 });
  e((t += 2), "msg", toolUse("pl1", "mcp__fyr__pr_link", { url: "https://github.com/acme/web/pull/4182" }));
  e((t += 0.2), "msg", toolResult("pl1"));
  e((t += 1), "msg", assistantText("Snapshot test still fails on the sheet; the other two files now take 9 s instead of 24 s."));
  e((t += 1), "run.ended", { failure: null, interrupted: false, durationMs: 840_000 });
  // V0.33: a second run reads one message; the next one waits in the agent's queue.
  e((t += 60), "user", { id: "m_map2", text: "Fix the snapshot test too.", images: 0 });
  e(t, "message.state", { id: "m_map2", state: "read" });
  e(t, "run.started", {});
  e((t += 1), "msg", assistantText("The snapshot differs only in the sheet's shadow. I'll run that test alone first."));
  e((t += 2), "msg", toolUse("sn1", "Bash", { command: "yarn vitest src/map/snapshot", description: "Run the snapshot test" }));
  e((t += 30), "user", { id: "m_map3", text: "Keep the snapshot itself; only make the test faster.", images: 0 });
  e(t, "message.state", { id: "m_map3", state: "queued" });
  return out;
}

// ---- projects (V0.23, V0.55): every colour and icon kind, one folder each, and a label without a folder ----
export function mockProjects(now: number): Project[] {
  const ago = (h: number) => iso(now - h * 3600_000);
  return [
    // V0.116: an image icon the setup agent set (name and colour too); the glyph stays the fallback.
    { id: "p_web", name: "acme/web", color: "teal", icon: { kind: "line", value: "Globe" }, image: "nb1", by: { name: "owner", color: "agent", image: "agent" }, setup: { at: ago(20), thread: "t_setup" }, folder: "/home/owner/src/acme/web", profile: "claude", model: "claude-sonnet-5-5", threads: 9, lastUsed: ago(0.1) },
    { id: "p_ops", name: "acme/ops", color: "rose", icon: { kind: "emoji", value: "🚀" }, folder: "/home/owner/src/acme/ops", threads: 2, lastUsed: ago(1) },
    { id: "p_long", name: LONG_PROJECT, color: "indigo", icon: { kind: "monogram" }, folder: "/home/owner/src/" + LONG_PROJECT, threads: 3, lastUsed: ago(5) },
    { id: "p_dot", name: "dotfiles", color: "olive", icon: { kind: "line", value: "terminal" }, folder: "/home/owner/dotfiles", threads: 1, lastUsed: ago(2) },
    { id: "p_api", name: "acme/api", color: "sky", icon: { kind: "line", value: "server" }, folder: "/home/owner/src/acme/api", threads: 0, lastUsed: ago(30) },
    { id: "p_fyr", name: "Fyr", color: "brown", icon: { kind: "line", value: "FolderGit2" }, image: "fy1", by: { name: "agent", color: "agent", image: "agent" }, setup: { at: ago(48), thread: "t_setup" }, folder: "/home/owner/git/fyr", threads: 0, lastUsed: ago(48) },
    { id: "p_home", name: "Garden", color: "green", icon: { kind: "emoji", value: "🏡" }, threads: 1, lastUsed: ago(3) },
    { id: "p_infra", name: "acme/infra", color: "slate", icon: { kind: "line", value: "no-such-icon" }, folder: "/home/owner/src/acme/infra", threads: 1, lastUsed: ago(10) },
    { id: "p_maplab", name: "Map perf lab", color: "blue", icon: { kind: "line", value: "map" }, folder: "/home/owner/src/maplab", threads: 0, lastUsed: ago(70) },
    { id: "p_book", name: "Bokföring åt Åsa", icon: { kind: "monogram" }, color: "brown", folder: "/home/owner/ekonomi", threads: 0, lastUsed: ago(200) },
  ];
}

// V0.42: the mock folders that are git repositories (the folder picker's "Repos found").
const MOCK_REPOS = [
  "/home/owner/src/acme/web",
  "/home/owner/src/acme/web-docs",
  "/home/owner/src/acme/ops",
  "/home/owner/src/acme/api",
  "/home/owner/src/acme/infra",
  "/home/owner/src/" + LONG_PROJECT,
  "/home/owner/src/lasso-app",
  "/home/owner/dotfiles",
  "/home/owner/git/fyr",
  "/home/owner/git/fyr-folders",
  "/home/owner/git/solidcraft",
];

const MOCK_HOME = "/home/owner";
/** A small home folder for the folder picker (V0.42). */
function mockTree(extra: string[]): { dirs: Set<string>; repos: Set<string> } {
  const repos = new Set(MOCK_REPOS);
  const dirs = new Set<string>();
  const add = (p: string) => {
    for (let at = p; at.startsWith(MOCK_HOME); at = at.slice(0, at.lastIndexOf("/"))) dirs.add(at);
  };
  for (const p of [...repos, ...extra]) add(p);
  for (const p of ["Documents", "Downloads", "notes", "ekonomi", "git/fyr/web", "git/fyr/internal", "src/maplab", ".config/nvim", ".local/share/fyr/scratch", "src/acme/web/node_modules"]) add(`${MOCK_HOME}/${p}`);
  return { dirs, repos };
}

export interface MockServer {
  buildId: string;
  state(): StateDTO;
  subscribe(h: { thread(t: ThreadDTO): void; event(e: FyrEvent): void; project?(p: Project): void }): void;
  command(commandId: string, action: string, threadId: string | undefined, args: Record<string, unknown>): CommandReply;
  events(threadId: string, after: number, limit: number): FyrEvent[];
  t3Candidates(): T3Candidate[];
  pushKey(): string;
  pushSubscribe(subscription: PushSubscriptionJSON, device: string): { ok: boolean; error?: string };
  pushUnsubscribe(endpoint: string): { ok: boolean; error?: string };
  pushTest(): { ok: boolean; error?: string };
  /** subscriptions stored in memory, by endpoint */
  pushSubscriptions(): { endpoint: string; device: string }[];
  /** V0.42: the host folder picker over a small mock home folder */
  fsList(path: string): FsListing;
  fsRecent(): FsDir[];
  fsRepos(): FsDir[];
  fsMkdir(parent: string, name: string): string;
  /** the in-memory attachment store (V0.18) */
  putAttachment(name: string, mediaType: string, size: number): { id: string; name: string; size: number; mediaType: string; path: string };
  /** the PR tab's detail (V0.43) */
  prDetail(threadId: string): PrReply;
}

/** A P-256 public key in the VAPID format; the mock has no private key and never pushes. */
export const MOCK_VAPID_KEY = "BDJF2xtzzliD3JVf2tm-yBnwg5fyq3ePXxiuCwxmswDZykUsCBJdi4cRLPO3Q3EvmJ_7ngetcIYIviMRUSCaUV0";

export function createMockServer(scenario: string, real0: number): MockServer {
  // ?mock=rail runs on the design's clock (14:32 today), so times read as in the frame.
  const now0 = scenario === "rail" ? railNow(real0) : real0;
  const clock = () => now0 + (Date.now() - real0);
  const fx = scenario === "2" ? sparseFixture(now0) : scenario === "rail" ? railFixture(now0) : fullFixture(now0);
  const threads = new Map(fx.threads.map((t) => [t.id, t]));
  const events = fx.events;
  const replies = new Map<string, CommandReply>();
  const subs = new Map<string, { endpoint: string; device: string }>();
  const files = new Map<string, { id: string; name: string; mediaType: string; size: number }>();
  let fileSeq = 1;
  const attachmentsOf = (args: Record<string, unknown>) =>
    (Array.isArray(args.attachments) ? (args.attachments as string[]) : []).map((aid) => files.get(aid)).filter((x): x is NonNullable<typeof x> => !!x);
  let sub: { thread(t: ThreadDTO): void; event(e: FyrEvent): void; project?(p: Project): void; deploy?(p: DeployProgress): void } | null = null;
  let projects = mockProjects(now0);
  // V0.139: with &hosts=2 a project lives on lab, so its screen and New thread can show another host's project.
  // A front stamps every project with its host (V0.55); the fixture's are desk's.
  if (MOCK_HOSTS) projects = [...projects.map((p) => ({ ...p, host: p.host ?? "desk" })), { id: "p_arm", name: "Robot arm", folder: "/home/owner/experiments/robot-arm", host: "lab", threads: 0, lastUsed: new Date(now0 - 86_400_000).toISOString() },
    { id: "p_srv", name: "Server ops", folder: "/home/owner/ops", host: "host-b", threads: 0, lastUsed: new Date(now0 - 2 * 86_400_000).toISOString() }];
  // V0.46: ?quiet=<projectId> puts that project in quiet hours that started an hour ago and end in two hours, and
  // gives it a second window (weekends, ten hours ahead) so the project screen shows two rows (decision 0014 addendum).
  if (MOCK_QUIET) {
    const hm = (ms: number) => { const d = new Date(ms); return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`; };
    const quiet = [{ from: hm(now0 - 3600_000), to: hm(now0 + 2 * 3600_000) }, { from: hm(now0 + 10 * 3600_000), to: hm(now0 + 11 * 3600_000), days: ["sat", "sun"] }];
    projects = projects.map((p) => (p.id === MOCK_QUIET ? { ...p, quiet } : p));
  }
  // V0.46: the host computes quietUntil; the mock does the same for its own clock.
  const withQuiet = (p: Project): Project => {
    const u = mockQuietUntil(p.quiet, clock());
    const { quietUntil: _, ...rest } = p;
    return u ? { ...rest, quietUntil: u } : rest;
  };
  const tree = mockTree([...fx.threads.map((t) => t.cwd), ...projects.map((p) => p.folder ?? "")].filter((p) => p.startsWith(MOCK_HOME)));
  const expand = (p: string) => (p.trim() === "~" || p.trim() === "" ? MOCK_HOME : p.trim().startsWith("~/") ? MOCK_HOME + p.trim().slice(1) : p.trim()).replace(/\/+$/, "") || "/";
  const dirOf = (p: string): FsDir => ({ name: p.split("/").pop() ?? p, path: p, repo: tree.repos.has(p) || undefined });
  const putProject = (p: Project) => {
    projects = projects.some((x) => x.id === p.id) ? projects.map((x) => (x.id === p.id ? p : x)) : [...projects, p];
    setTimeout(() => sub?.project?.(withQuiet(p)), 30);
    return p;
  };
  const projectIdFor = (t: ThreadDTO) => t.projectId ?? projects.find((p) => p.name === t.project)?.id;
  // V0.30, V0.55: what "New project from this thread" starts with, as the host computes it (no scratch, no home, no
  // folder a project has).
  const draftFor = (t: ThreadDTO): ThreadDTO["newProject"] => {
    if (!t.cwd || t.cwd.includes("/scratch/") || t.cwd === MOCK_HOME || projects.some((p) => p.folder === t.cwd)) return undefined;
    return { name: t.cwd.split("/").pop() ?? "", folder: t.cwd };
  };
  const dtoOf = (t: ThreadDTO): ThreadDTO => {
    const pid = projectIdFor(t);
    const q = withQuiet(projects.find((p) => p.id === pid) ?? { name: "" }).quietUntil;
    // V0.33: the outbox is what the mock agent has not read yet, as the host sends it.
    const outbox = [...unread].filter(([, tid]) => tid === t.id).map(([id]) => ({ id, state: "queued" }));
    return mockHostOf({ ...t, projectId: pid, newProject: draftFor(t), quietUntil: q, outbox: outbox.length ? outbox : undefined }, clock());
  };
  let nextId = 1;

  const put = (t: ThreadDTO, patch: Partial<ThreadDTO>) => {
    const n: ThreadDTO = { ...t, ...patch, updatedAt: new Date(clock()).toISOString() };
    n.projectId = n.projectId ?? projectIdFor(n);
    threads.set(n.id, n);
    setTimeout(() => sub?.thread(dtoOf(n)), 30);
    return n;
  };
  const emit = (id: string, type: string, data: unknown) => {
    const list = (events[id] ??= []);
    const e = ev((list.at(-1)?.seq ?? 0) + 1, clock(), id, type, data);
    list.push(e);
    setTimeout(() => sub?.event(e), 20);
  };
  const quiet = { ...NOT_RUNNING };
  // V0.33: messages the mock agent has not read; a running thread reads them after a step.
  const unread = new Map<string, string>(Object.entries(events).flatMap(([tid, list]) => {
    const st = new Map<string, string>();
    for (const e of list) if (e.type === "message.state") st.set(String((e.data as { id?: string })?.id), String((e.data as { state?: string })?.state));
    return [...st].filter(([, v]) => v === "queued" || v === "sent").map(([mid]) => [mid, tid] as [string, string]);
  }));
  const deliver = (tid: string, text: string, args: Record<string, unknown>, running: boolean) => {
    const id = `m_new${nextId++}`;
    const imgs = Array.isArray(args.images) ? args.images.length : 0;
    emit(tid, "user", { id, text, images: imgs, attachments: attachmentsOf(args), ...(args.from === "view" ? { from: "view" } : {}) });
    if (!running) {
      emit(tid, "message.state", { id, state: "read" });
      return id;
    }
    emit(tid, "message.state", { id, state: "queued" });
    unread.set(id, tid);
    const th = threads.get(tid);
    if (th) put(th, {});
    setTimeout(() => {
      if (unread.delete(id)) {
        emit(tid, "message.state", { id, state: "read" });
        const t2 = threads.get(tid);
        if (t2) put(t2, {});
      }
    }, 6000);
    return id;
  };
  const runFor = (id: string, reply: string) => {
    const t = threads.get(id)!;
    put(t, {
      running: true,
      runStartedAt: new Date(clock()).toISOString(),
      activity: "Thinking",
      failure: null,
      attention: { state: "working", section: "working", reason: "" },
      unavailable: { "thread.retry": "Nothing failed", "thread.markUnseen": "A run is going" },
    });
    emit(id, "run.started", {});
    setTimeout(() => {
      const cur = threads.get(id)!;
      if (!cur.running) return;
      emit(id, "msg", assistantText(reply));
      emit(id, "run.ended", { failure: null, interrupted: false, durationMs: 1800 });
      // V0.37: a view thread's run updates its view at the end.
      const live = cur.liveView && cur.live ? { ...cur.live, state: { ...cur.live.state, headline: reply }, headline: reply, updatedAt: new Date(clock()).toISOString(), checkedAt: new Date(clock()).toISOString() } : cur.live;
      put(cur, {
        live,
        running: false,
        activity: "",
        summary: reply,
        activityAt: new Date(clock()).toISOString(),
        decisions: [],
        attention: { state: "done", section: "seen", reason: "" },
        unavailable: quiet,
      });
    }, 1800);
  };

  const exec = (action: string, t: ThreadDTO | undefined, args: Record<string, unknown>): CommandReply => {
    if (action === "thread.create") {
      const id = `t_new${nextId++}`;
      // V0.42, V0.55: the project's folder, else a fresh scratch folder.
      const pr = args.projectId ? projects.find((x) => x.id === args.projectId) : undefined;
      if (args.projectId && !pr) return { ok: false, error: `no project ${args.projectId}` };
      const slug = String(args.text ?? "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "thread";
      const cwd = String(args.cwd || pr?.folder || `${MOCK_HOME}/.local/share/fyr/scratch/20261114-093005-${slug}`);
      const nt = base(id, String(args.title || args.text || "New thread").slice(0, 80), clock(), 0, {
        cwd,
        project: pr?.name ?? "",
        projectId: pr?.id,
        profile: String(args.profile ?? "claude"),
        model: String(args.model ?? "claude-opus-5-5"),
        liveView: args.view === true,
        // V0.132: the thread runs on its project's host, else the one the owner picked.
        ...((pr?.host ?? args.host) ? { host: String(pr?.host ?? args.host) } : {}),
      });
      threads.set(id, nt);
      events[id] = [];
      const mid = deliver(id, String(args.text ?? ""), args, false);
      runFor(id, "Started on it. (mock)");
      return { ok: true, threadId: id, result: { messageId: mid } };
    }
    if (action === "project.update") {
      const p = projects.find((x) => x.id === args.id);
      if (!p) return { ok: false, error: "No such project" };
      const patch: Partial<Project> = {};
      for (const k of ["name", "color", "icon", "profile", "model", "quiet", "priority", "agentSettle"] as const) if (k in args) (patch as Record<string, unknown>)[k] = args[k] ?? undefined;
      // V0.116: a changed value is the owner's; the image's upload or remove too.
      const by = { ...p.by };
      for (const k of ["name", "color"] as const) if (k in args && args[k] !== p[k]) by[k] = "owner";
      if ("image" in args) (patch.image = args.image ? String(args.image) : undefined), (by.image = "owner");
      patch.by = by;
      if ("folder" in args) patch.folder = args.folder ? expand(String(args.folder)) : undefined;
      const np = putProject({ ...p, ...patch });
      // V0.46: the threads carry the project's quietUntil, so they change with it.
      if ("quiet" in args) for (const t of threads.values()) if (projectIdFor(t) === np.id) setTimeout(() => sub?.thread(dtoOf(t)), 40);
      return { ok: true };
    }
    if (action === "action.run") {
      // V0.60: a press; a script ends ok a moment later with a line of output.
      if (!t) return { ok: false, error: "no thread" };
      const on = String(args.on ?? "");
      const ch = t.changes?.find((c) => c.id === on);
      const offers = ch ? ch.offers : on === "view" ? t.live?.offers : t.offers;
      const o = offers?.find((x) => x.id === args.action);
      if (!o) return { ok: false, error: `no action ${args.action}` };
      if (o.digest !== args.digest) return { ok: false, error: "The action changed since you saw it; look at it again." };
      if (o.run === "script" || o.run === "fyr") {
        const at = new Date(clock()).toISOString();
        const run = { action: o.id, label: o.label, by: "owner", at, running: true, ok: false };
        type Runs = NonNullable<ThreadDTO["runs"]>;
        // #31: a change, the live view and the thread each hold their runs.
        const withRuns = (cur: ThreadDTO, f: (rs: Runs | undefined) => Runs): Partial<ThreadDTO> =>
          ch ? { changes: cur.changes!.map((c) => (c.id === ch.id ? { ...c, runs: f(c.runs) } : c)) } : on === "view" ? { live: { ...cur.live, runs: f(cur.live?.runs) } } : { runs: f(cur.runs) };
        put(t, withRuns(t, (rs) => [...(rs ?? []), run]));
        setTimeout(() => {
          const cur = threads.get(t.id)!;
          const done = { ...run, running: false, ok: true, output: `$ ${(o.argv ?? [o.effect]).join(" ")}\nok (mock)`, endedAt: new Date(clock()).toISOString() };
          put(cur, withRuns(cur, (rs) => (rs ?? []).map((r) => (r === run || (r.action === run.action && r.running) ? done : r))));
        }, 1200);
      }
      return { ok: true, result: { on, action: o.id } };
    }
    if (action === "project.setupRun") {
      // V0.116: Set up automatically: a thread in the project's folder; the mock agent sets nothing.
      const p = projects.find((x) => x.id === args.id);
      if (!p) return { ok: false, error: `no project ${args.id}` };
      if (!p.folder) return { ok: false, error: "a project needs a folder to be set up" };
      putProject({ ...p, setup: { at: new Date(clock()).toISOString(), thread: "t_setup" } });
      return { ok: true, result: { id: p.id, thread: "t_setup" } };
    }
    if (action === "project.link") {
      const p = projects.find((x) => x.id === args.id);
      if (!p) return { ok: false, error: `no project ${args.id}` };
      putProject({ ...p, link: args.link ? String(args.link) : undefined });
      return { ok: true, result: { id: p.id } };
    }
    if (action === "project.delete") {
      const p = projects.find((x) => x.id === args.id);
      if (!p) return { ok: false, error: `no project ${args.id}` };
      projects = projects.filter((x) => x.id !== p.id);
      setTimeout(() => sub?.project?.({ id: p.id, deleted: true } as Project), 30);
      for (const t of threads.values()) if (projectIdFor(t) === p.id) put(t, { projectId: undefined, project: "" });
      return { ok: true, result: { id: p.id } };
    }
    if (action === "project.create") {
      const name = String(args.name ?? "").trim();
      if (!name) return { ok: false, error: "A project needs a name" };
      const folder = args.folder ? expand(String(args.folder)) : undefined;
      const ft = args.threadId ? threads.get(String(args.threadId)) : undefined;
      if (args.threadId && !ft) return { ok: false, error: `no thread ${args.threadId}` };
      const p = putProject({ id: `p_new${nextId++}`, name, folder, threads: ft ? 1 : 0, lastUsed: new Date(clock()).toISOString(), ...(typeof args.host === "string" && args.host ? { host: args.host } : {}) }); // V0.132
      if (ft) put(ft, { projectId: p.id, project: p.name });
      return { ok: true, result: { id: p.id } };
    }
    if (action === "t3.adopt") {
      const ids = (args.t3Ids as string[]) ?? [];
      for (const tid of ids) {
        const c = fx.t3.find((x) => x.t3Id === tid);
        if (!c) continue;
        const id = `t_t3_${tid}`;
        const nt = base(id, c.title, clock(), 0, { cwd: c.cwd, project: c.cwd.split("/").slice(-2).join("/"), source: "t3", unavailable: quiet });
        threads.set(id, nt);
        events[id] = [];
        setTimeout(() => sub?.thread(nt), 30);
      }
      fx.t3 = fx.t3.filter((c) => !ids.includes(c.t3Id));
      return { ok: true };
    }
    if (action === "host.deployNow") {
      // V0.83: the drain stops and the mock host switches; a real one restarts here.
      const d = mockDeploy();
      if (d?.phase !== "drain") return { ok: false, error: "No update is waiting for running threads" };
      setTimeout(() => sub?.deploy?.({ ...d, phase: "switch", blocking: undefined, at: new Date().toISOString() }), 30);
      return { ok: true };
    }
    if (!t) return { ok: false, error: "No such thread" };
    const reason = t.unavailable?.[action];
    if (reason) return { ok: false, error: reason };
    const idle = (state: ThreadDTO["attention"]["state"], section: ThreadDTO["attention"]["section"], extra: Partial<ThreadDTO> = {}) =>
      put(t, { attention: { state, section, reason: "" }, ...extra });
    switch (action) {
      case "thread.send": {
        deliver(t.id, String(args.text ?? ""), args, t.running);
        if (!t.running) runFor(t.id, "Got it. Working on that now. (mock reply)");
        break;
      }
      case "thread.cancelMessage": {
        const mid = String(args.messageId ?? "");
        if (unread.get(mid) !== t.id) return { ok: false, error: "The agent has already read this message" };
        unread.delete(mid);
        emit(t.id, "message.state", { id: mid, state: "cancelled" });
        put(t, {});
        break;
      }
      case "thread.answer": {
        const d = t.decisions?.find((x) => x.id === args.decisionId);
        const ans = args.answers ? Object.values(args.answers as Record<string, string>).join(" · ") : args.allow ? "Allowed" : "Denied";
        emit(t.id, "decision.closed", { id: args.decisionId, answer: ans });
        if (d?.kind === "effect" && args.allow) emit(t.id, "effect.result", { action: d.effect?.action, ok: true, text: "Merged #4170 with squash." });
        // V0.101: an answered or declined ask leaves Needs you.
        put(t, { decisions: (t.decisions ?? []).filter((x) => x.id !== args.decisionId), changes: (t.changes ?? []).map((x) => (x.need?.ask && x.need.ask === args.decisionId ? { ...x, need: undefined } : x)) });
        runFor(t.id, "Thanks, continuing with that answer. (mock)");
        break;
      }
      case "thread.interrupt":
        emit(t.id, "run.ended", { failure: null, interrupted: true, durationMs: 100 });
        idle("seen", "seen", { running: false, activity: "", unavailable: quiet });
        break;
      case "thread.retry":
        runFor(t.id, "Retried, and it worked this time. (mock)");
        break;
      case "thread.dismiss":
        idle("seen", "seen", { failure: null });
        break;
      // V0.104: the owner's pin; snooze, settle and archive drop it.
      case "thread.pin":
        put(t, { pinnedAt: new Date(clock()).toISOString() });
        break;
      case "thread.unpin":
        put(t, { pinnedAt: null });
        break;
      case "thread.settle":
        if (args.now && t.pinnedAt) return { ok: false, error: "A pinned thread settles only by its own Settle" }; // V0.162
        if (args.now && t.running) return { ok: false, error: "A running thread settles only by its own Settle" }; // V0.203
        idle("settled", "settled", { settledAt: new Date(clock()).toISOString(), pinnedAt: null, settleWhenDone: false });
        break;
      case "thread.cancelWake": // V0.wakeups-via-watch
        put(t, { wakes: (t.wakes ?? []).filter((w) => w.rule !== args.rule) });
        break;
      case "thread.settleWhenDone": // V0.203
        put(t, { settleWhenDone: true });
        break;
      case "thread.unsettle":
        idle("seen", "seen", { settledAt: null, settleWhenDone: false });
        break;
      case "thread.snooze":
        idle("snoozed", "snoozed", { view: { snoozeUntil: String(args.until) }, pinnedAt: null });
        break;
      case "thread.snoozeWhenDone": // V0.185
        if (!t.running) return { ok: false, error: "Nothing is working" };
        put(t, { attention: { state: "snoozed", section: "snoozed", reason: "until work is done" }, view: { seenAt: null, snoozeUntil: new Date(clock() + 24 * 3600_000).toISOString(), snoozeWhenDone: true }, pinnedAt: null });
        break;
      case "thread.unsnooze":
        idle("seen", "seen", { view: { snoozeUntil: null } });
        break;
      case "thread.archive":
        idle("archived", "archived", { archivedAt: new Date(clock()).toISOString(), pinnedAt: null });
        break;
      case "thread.unarchive":
        idle("seen", "seen", { archivedAt: null });
        break;
      case "thread.markSeen":
        if (t.attention.state === "done") idle("seen", "seen", { view: { seenAt: new Date(clock()).toISOString() } });
        break;
      case "thread.markUnseen":
        idle("done", "seen"); // V0.101, V0.189: as the host sends it; the client shows it in Unread
        break;
      case "thread.changesSeen":
        break; // V0.65: the mock keeps its fixed look, so every visit shows the same "new since"
      case "thread.fileViewed":
        if (args.viewed === true) mockViewed.set(`${t.id}:${String(args.path)}`, String(args.blob ?? ""));
        else mockViewed.delete(`${t.id}:${String(args.path)}`);
        break;
      case "thread.rename":
        put(t, { title: String(args.title ?? t.title) });
        break;
      case "thread.regenerateTitle":
        // V0.108: the host asks a model; the mock answers at once with a fixed title.
        put(t, { title: "Title written from the conversation" });
        break;
      case "thread.setProject": {
        if (args.projectId === "") {
          // V0.30: No project, by hand.
          put(t, { projectId: "", project: "" });
          break;
        }
        const p = projects.find((x) => x.id === args.projectId);
        if (!p) return { ok: false, error: "No such project" };
        put(t, { projectId: p.id, project: p.name });
        break;
      }
      case "thread.setView":
        put(t, { liveView: args.on === true });
        break;
      case "thread.setModel":
        put(t, { model: String(args.model ?? t.model) });
        break;
      case "change.end": {
        // V0.72: the owner ends a change; its actions go.
        const c = (t.changes ?? []).find((x) => x.id === args.change || x.key === args.change);
        if (!c) return { ok: false, error: `no change ${String(args.change)}` };
        if (c.phase === "abandoned" || c.phase === "reverted") return { ok: false, error: `"${c.title}" is already ${c.phase}` };
        const phase = args.phase === "reverted" ? "reverted" : "abandoned";
        const label = String(args.reason ?? "").trim() || undefined;
        put(t, { changes: (t.changes ?? []).map((x) => (x === c ? { ...x, phase, phaseBy: "owner" as const, label, offers: [], need: undefined, done: { at: new Date(clock()).toISOString(), by: "owner" as const, reason: phase }, updatedAt: new Date(clock()).toISOString() } : x)) });
        break;
      }
      case "change.done": {
        // V0.98 (4): the owner's Done; the change leaves the board.
        const c = (t.changes ?? []).find((x) => x.id === args.change || x.key === args.change);
        if (!c) return { ok: false, error: `no change ${String(args.change)}` };
        if (c.done) return { ok: false, error: `"${c.title}" is already done` };
        const at = new Date(clock()).toISOString();
        put(t, { changes: (t.changes ?? []).map((x) => (x === c ? { ...x, done: { at, by: "owner" as const, reason: "done" }, need: undefined, updatedAt: at } : x)) });
        break;
      }
      case "change.park":
      case "change.unpark": {
        // V0.84: Not now and its Undo; the host keeps a fingerprint of what the owner saw.
        const c = (t.changes ?? []).find((x) => x.id === args.change || x.key === args.change);
        if (!c) return { ok: false, error: `no change ${String(args.change)}` };
        const parked = action === "change.park" ? { at: new Date(clock()).toISOString(), seen: "mock" } : undefined;
        // V0.101: the host leaves a parked change out of Needs you until its head changes.
        if (action === "change.park" && c.need) parkedNeeds.set(c.id, c.need);
        const need = action === "change.park" ? undefined : (c.need ?? parkedNeeds.get(c.id));
        put(t, { changes: (t.changes ?? []).map((x) => (x === c ? { ...x, parked, need } : x)) });
        break;
      }
      case "change.refresh": {
        // V0.101: Refresh now reads GitHub and git now; the mock reads the PR as checked now.
        const c = (t.changes ?? []).find((x) => x.id === args.change || x.key === args.change);
        if (!c) return { ok: false, error: `no change ${String(args.change)}` };
        const at = new Date(clock()).toISOString();
        put(t, { changes: (t.changes ?? []).map((x) => (x === c && x.pr ? { ...x, pr: { ...x.pr, checkedAt: at } } : x)) });
        break;
      }
      case "thread.dismissPR":
        if (!t.pr || t.pr.state === "OPEN") return { ok: false, error: "The PR is still open" };
        put(t, { pr: null });
        break;
      // V0.43: the owner's PR actions report as an effect.result by the owner.
      case "pr.merge": {
        if (!t.pr || (t.pr.state ?? "OPEN") !== "OPEN") return { ok: false, error: "The PR is not open" };
        const pr = t.pr;
        setTimeout(() => {
          emit(t.id, "effect.result", { action: "pr.merge", by: "owner", ok: true, text: `You merged #${pr.number} (${String(args.method ?? "squash")}).\n\n✓ Merged pull request ${pr.repo}#${pr.number}` });
          put(t, { pr: { ...pr, state: "MERGED" } });
        }, 600);
        break;
      }
      case "pr.rerunFailed": {
        if (!t.pr || (t.pr.state ?? "OPEN") !== "OPEN") return { ok: false, error: "The PR is not open" };
        const n = t.pr.number;
        setTimeout(() => emit(t.id, "effect.result", { action: "pr.rerunFailed", by: "owner", ok: true, text: `You re-ran the failed checks of #${n}.\n\nRe-ran 1 failed workflow run of 9c1e0b7: CI.` }), 400);
        break;
      }
      case "thread.handover": {
        // V0.45: the mock hands over at once, with full history.
        const to = String(args.profile ?? "");
        if (!to || to === t.profile) return { ok: false, error: "Pick another profile" };
        const from = t.profile;
        put(t, { profile: to, model: String(args.model ?? ""), ...(args.effort !== undefined ? { effort: String(args.effort) } : {}) });
        emit(t.id, "handover", { from, to, model: args.model ?? "", rung: "resume" });
        break;
      }
      default:
        return { ok: false, error: `Unknown action ${action}` };
    }
    return { ok: true, threadId: t.id };
  };

  return {
    buildId: "mock",
    state() {
      return {
        now: new Date(clock()).toISOString(),
        host: "desk",
        hostId: "F7QK-2M9X-AB34-CD56",
        machine: "laptop", // V0.170
        user: "owner",
        buildId: "mock",
        deploy: mockDeploy(),
        threads: [...threads.values()].map(dtoOf),
        ...(MOCK_HOSTS ? { hosts: mockHosts(clock(), mockLabTrusted) } : {}),
        actions: [
          { name: "thread.settle", label: "Settle", key: "e", bulk: true },
          { name: "thread.settleWhenDone", label: "Settle when done" },
          { name: "thread.snooze", label: "Snooze", key: "s" },
          { name: "thread.snoozeWhenDone", label: "Snooze until work is done" },
          { name: "thread.archive", label: "Archive", key: "#" },
        ],
        profiles: [
          { id: "claude", name: "Claude", color: "#d97757", models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5"], defaultModel: "claude-opus-5-5", defaultEffort: "high",
            modelCaps: [{ id: "claude-opus-5-5", efforts: ["low", "medium", "high", "xhigh", "max"] }, { id: "claude-sonnet-5-5", efforts: ["low", "medium", "high", "xhigh", "max"] }, { id: "claude-haiku-5" }] },
          { id: "work", name: "Claude (work account)", color: "#2f6fb0", models: ["claude-sonnet-5-5"], defaultModel: "claude-sonnet-5-5" },
        ],
        projects: projects.map(withQuiet),
      } as StateDTO;
    },
    subscribe(h) {
      sub = h;
    },
    command(commandId, action, threadId, args) {
      const prev = replies.get(commandId);
      if (prev) return prev;
      const r = exec(action, threadId ? threads.get(threadId) : undefined, args);
      replies.set(commandId, r);
      return r;
    },
    prDetail(threadId) {
      return mockPrDetail(threads.get(threadId), clock());
    },
    events(threadId, after, limit) {
      const list = events[threadId] ?? [];
      if (after > 0) return list.filter((e) => e.seq > after).slice(0, limit);
      return list.slice(-limit);
    },
    t3Candidates() {
      return fx.t3;
    },
    fsList(path) {
      const p = expand(path);
      if (!(p === MOCK_HOME || p.startsWith(MOCK_HOME + "/")) || !tree.dirs.has(p)) throw new Error(`outside the home folder: no folder ${path}`);
      const dirs = [...tree.dirs].filter((d) => d.slice(0, d.lastIndexOf("/")) === p).sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase())).map(dirOf);
      return { path: p, parent: p === MOCK_HOME ? "" : p.slice(0, p.lastIndexOf("/")), dirs };
    },
    fsRecent() {
      const seen = new Set<string>();
      return [...threads.values()]
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
        .map((t) => t.cwd)
        .filter((c) => tree.dirs.has(c) && !seen.has(c) && !!seen.add(c))
        .slice(0, 30)
        .map(dirOf);
    },
    fsRepos() {
      return [...tree.repos].filter((r) => tree.dirs.has(r)).sort().map(dirOf);
    },
    fsMkdir(parent, name) {
      const p = expand(parent);
      const n = name.trim();
      if (!tree.dirs.has(p)) throw new Error(`outside the home folder: no folder ${parent}`);
      if (!n || n.includes("/") || n === "." || n === "..") throw new Error("a folder name cannot be empty or contain /");
      if (tree.dirs.has(`${p}/${n}`)) throw new Error(`${p}/${n} already exists`);
      tree.dirs.add(`${p}/${n}`);
      return `${p}/${n}`;
    },
    pushKey() {
      return MOCK_VAPID_KEY;
    },
    pushSubscribe(subscription, device) {
      if (!subscription?.endpoint) return { ok: false, error: "subscription.endpoint is required" };
      subs.set(subscription.endpoint, { endpoint: subscription.endpoint, device });
      return { ok: true };
    },
    pushUnsubscribe(endpoint) {
      subs.delete(endpoint);
      return { ok: true };
    },
    pushTest() {
      return subs.size ? { ok: true } : { ok: false, error: "No device has notifications on" };
    },
    putAttachment(name, mediaType, size) {
      const id = `a_${fileSeq++}`;
      files.set(id, { id, name, mediaType, size });
      return { id, name, size, mediaType, path: `/home/owner/.local/share/fyr/attachments/${id}/${name}` };
    },
    pushSubscriptions() {
      return [...subs.values()];
    },
  };
}

// ---- Changes tab (V0.25) ----

const LONG_CHANGE_PATH = `infrastructure/${LONG_PROJECT}/modules/opensearch/cluster/variables-for-the-search-cluster-and-its-dashboards.tf`;

/** V0.65: the mock's Viewed marks, `${threadId}:${path}` → blob. t_done starts with its test file viewed. */
const mockViewed = new Map<string, string>();

/** A stable fake blob id for a mock path. */
function mockBlob(path: string): string {
  let h = 0;
  for (const ch of path) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return h.toString(16).padStart(8, "0").repeat(5);
}

/** The branch diff of a mock thread; t_deploy is outside git. t_done also has a look 3 h ago (V0.65). */
export function mockChanges(threadId: string): ChangesDTO {
  if (threadId === "t_deploy") return { git: false };
  // V0.73 (2): a checkout on its base with nothing changed.
  if (threadId === "t_ended") return { git: true, root: "/home/owner/src/fyr", branch: "main", base: "origin/main", mergeBase: "262ec20a1b", commits: 0, head: "262ec20a1b2c3d4e5f60718293a4b5c6d7e8f901", truncated: false, files: [] };
  if (threadId === "t_done" && !mockViewed.has("t_done:init")) {
    mockViewed.set("t_done:init", "");
    mockViewed.set("t_done:web/test/map.test.ts", mockBlob("web/test/map.test.ts"));
    mockViewed.set("t_done:web/src/map/lasso.ts", mockBlob("web/src/map/lasso.ts"));
  }
  const files = [
    { path: "docs/old-notes.md", status: "deleted" as const, additions: 0, deletions: 12, binary: false },
    { path: LONG_CHANGE_PATH, status: "modified" as const, additions: 4, deletions: 2, binary: false, blob: mockBlob(LONG_CHANGE_PATH) },
    { path: "web/public/marker.png", status: "added" as const, additions: 0, deletions: 0, binary: true, blob: mockBlob("web/public/marker.png") },
    { path: "web/src/map/lasso.ts", oldPath: "web/src/map/select.ts", status: "renamed" as const, additions: 6, deletions: 2, binary: false, blob: mockBlob("web/src/map/lasso.ts") },
    { path: "web/src/map/render.ts", status: "modified" as const, additions: 42, deletions: 18, binary: false, blob: mockBlob("web/src/map/render.ts") },
    { path: "web/test/map.test.ts", status: "added" as const, additions: 64, deletions: 0, binary: false, blob: mockBlob("web/test/map.test.ts") },
  ].map((f) => ({ ...f, viewed: mockViewed.get(`${threadId}:${f.path}`) === (f.blob ?? "") && mockViewed.has(`${threadId}:${f.path}`) }));
  const c: ChangesDTO = {
    git: true,
    root: "/home/owner/src/acme/web",
    branch: "fix/map-lasso-mobile",
    base: "origin/main",
    mergeBase: "3f2a9c1d0e",
    commits: 3,
    head: "7be1c0d4a9f2e3b5c6d7e8f90a1b2c3d4e5f6a7b",
    truncated: false,
    files,
  };
  if (threadId === "t_done") {
    const pick = (p: string, add: number, del: number) => ({ ...files.find((f) => f.path === p)!, additions: add, deletions: del });
    c.since = {
      head: "51d0aa3c9e8b7f6a5d4c3b2a1908f7e6d5c4b3a2",
      at: new Date(Date.now() - 3 * 3600_000).toISOString(),
      mode: "ancestor",
      commits: 2,
      files: [pick(LONG_CHANGE_PATH, 2, 1), pick("web/src/map/render.ts", 3, 1)],
    };
  }
  return c;
}

/** One mock file's unified diff. */
export function mockFilePatch(path: string): FilePatchDTO {
  const head = (extra = "") => `diff --git a/${path} b/${path}\n${extra}--- a/${path}\n+++ b/${path}\n`;
  if (path.endsWith(".png")) return { path, patch: `diff --git a/${path} b/${path}\nnew file mode 100644\nBinary files /dev/null and b/${path} differ\n`, binary: true, truncated: false };
  if (path === LONG_CHANGE_PATH) {
    const patch =
      head("index 1a2b3c4..5d6e7f8 100644\n") +
      "@@ -12,7 +12,9 @@ variable \"cluster\" {\n" +
      '   type = object({\n     name = string\n-    size = number\n+    size          = number\n+    dedicated_master_count_for_the_cluster_when_it_runs_in_three_availability_zones = optional(number, 3)\n' +
      `+    description = "${"A very long description line without breaks that must scroll sideways inside the diff and never widen the page. ".repeat(3)}"\n` +
      "     zones = list(string)\n-    tier  = string\n+    tier  = optional(string, \"hot\")\n+    warm  = optional(bool, false)\n   })\n }\n";
    return { path, patch, binary: false, truncated: false };
  }
  if (path === "docs/old-notes.md") {
    const lines = Array.from({ length: 12 }, (_, i) => `-Note ${i + 1}: the old map notes.`).join("\n");
    return { path, patch: `diff --git a/${path} b/${path}\ndeleted file mode 100644\n--- a/${path}\n+++ /dev/null\n@@ -1,12 +0,0 @@\n${lines}\n`, binary: false, truncated: false };
  }
  if (path === "web/test/map.test.ts") {
    const lines = Array.from({ length: 20 }, (_, i) => `+test("map case ${i + 1}", () => expect(render(${i})).toMatchSnapshot());`).join("\n");
    return { path, patch: `diff --git a/${path} b/${path}\nnew file mode 100644\n--- /dev/null\n+++ b/${path}\n@@ -0,0 +1,20 @@\n${lines}\n`, binary: false, truncated: false };
  }
  const patch =
    head("index 0f1e2d3..4c5b6a7 100644\n") +
    "@@ -40,9 +40,12 @@ export function renderMap(map: MapState, opts: RenderOptions) {\n" +
    "   const layer = map.layers.find((l) => l.id === opts.layer);\n" +
    "   if (!layer) return;\n" +
    "-  for (const f of layer.features) drawFeature(ctx, f);\n" +
    "+  const visible = layer.features.filter((f) => inView(map.view, f.bbox));\n" +
    "+  for (const f of visible) drawFeature(ctx, f);\n" +
    "+  stats.drawn = visible.length;\n" +
    "   ctx.restore();\n" +
    " }\n" +
    "@@ -120,6 +123,6 @@ function drawFeature(ctx: CanvasRenderingContext2D, f: Feature) {\n" +
    "   ctx.beginPath();\n" +
    "-  ctx.lineWidth = 2;\n" +
    "+  ctx.lineWidth = f.selected ? 3 : 2;\n" +
    "   ctx.stroke();\n" +
    "\\ No newline at end of file\n";
  return { path, patch, binary: false, truncated: false };
}

// V0.46: ?quiet=<projectId> (read once at load, like ?deploy).
// V0.195: ?bgwait=1: the waiting thread of ?mock=rail waits for a background command that went quiet.
const MOCK_BGWAIT = typeof location !== "undefined" ? new URLSearchParams(location.search).get("bgwait") === "1" : false;
const MOCK_QUIET = typeof location !== "undefined" ? new URLSearchParams(location.search).get("quiet") : null;

/** The host's QuietUntil (internal/core/quiet.go) on the device's clock, for the mock only: any window holds, and
 *  windows that follow on join, at most a week ahead. */
export function mockQuietUntil(q: Project["quiet"], now: number): string | undefined {
  const hm = (s: string) => /^(\d{1,2}):(\d\d)$/.exec(s ?? "");
  const keys = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];
  const one = (w: QuietHours, at: number): number | null => {
    const f = hm(w.from), t = hm(w.to);
    if (!f || !t) return null;
    const span = +t[1] * 60 + +t[2] <= +f[1] * 60 + +f[2] ? 1 : 0;
    const d = new Date(at);
    for (const back of [1, 0]) {
      const day = new Date(d.getFullYear(), d.getMonth(), d.getDate() - back, 12);
      if (w.days?.length && !w.days.includes(keys[day.getDay()])) continue;
      const start = new Date(d.getFullYear(), d.getMonth(), d.getDate() - back, +f[1], +f[2]).getTime();
      const end = new Date(d.getFullYear(), d.getMonth(), d.getDate() - back + span, +t[1], +t[2]).getTime();
      if (at >= start && at < end) return end;
    }
    return null;
  };
  const list = quietList(q);
  const window = (at: number): number | null => {
    let best: number | null = null;
    for (const w of list) {
      const e = one(w, at);
      if (e !== null && (best === null || e > best)) best = e;
    }
    return best;
  };
  let end = window(now);
  if (end === null) return undefined;
  for (let i = 0; i < 64 && end - now < 7 * 24 * 3600_000; i++) {
    const next = window(end);
    if (next === null || next <= end) break;
    end = next;
  }
  return new Date(end).toISOString();
}

/** V0.39: ?deploy=<phase> in the mock shows an update in progress (the layout test uses it). */
// Read once at load: the phone app rewrites the URL when it starts.
const MOCK_DEPLOY = typeof location !== "undefined" ? new URLSearchParams(location.search).get("deploy") : null;
function mockDeploy() {
  const ph = MOCK_DEPLOY;
  if (!ph) return null;
  const started = new Date(Date.now() - 4 * 60_000).toISOString();
  return { phase: ph, startedAt: started, at: new Date().toISOString(), build: "20261114-160324", blocking: ph === "drain" ? ["t_mapperf", "t_lasso"] : undefined, detail: ph === "failed" ? "tests failed, nothing was switched: exit status 2\nFAIL TestX" : undefined };
}

/** V0.54: ?hosts=2 in the mock shows a front with two hosts, the second (host-b, with t_lasso and t_triage)
 *  offline for 25 minutes (the layout test uses it). Read once at load, as ?deploy=. */
const MOCK_LONGTITLE = typeof location !== "undefined" ? new URLSearchParams(location.search).get("longtitle") === "1" : false;
const MOCK_RUNS = typeof location !== "undefined" ? new URLSearchParams(location.search).get("runs") === "1" : false;
const MOCK_ROOTPROMPT = typeof location !== "undefined" ? new URLSearchParams(location.search).get("rootprompt") === "no" : false;
const MOCK_HOSTS = typeof location !== "undefined" ? new URLSearchParams(location.search).get("hosts") === "2" : false;
/** Decision 0029 S5: ?paused=1 has the memory guard freeze map perf's job. */
const MOCK_PAUSED = typeof location !== "undefined" ? new URLSearchParams(location.search).get("paused") === "1" : false;
/** V0.164: ?undeclared=1 (Map perf runs heavy work it did not ask for), =paused (the memory guard froze it), =2 (Map perf
 *  and Babysit PR #4170, and Nix flake update dismissed: the rail's menu and Settings' three states). Read once at load. */
const MOCK_UNDECLARED = typeof location !== "undefined" ? new URLSearchParams(location.search).get("undeclared") ?? "" : "";
export const UNDECLARED_NOTICE = {
  undeclared:
    "This thread runs heavy work Fyr did not admit: about 2.8 GB memory (20 % of laptop) and 2.1 cores (52 %) for 45 s, from `node test/layout.e2e.mjs`. Before a heavy job call mcp__fyr__slot_acquire {\"kind\":\"e2e\",\"command\":\"node test/layout.e2e.mjs\"}, run it once admitted, then call mcp__fyr__slot_release; a subagent's or a background run counts too. If it still runs, call it now. If it has ended, there is nothing to do for it, but ask before you run it or anything like it again. Fyr counts this use against the host either way, so other jobs wait; nothing is stopped, but if memory runs short Fyr may pause it.",
  mem: 2.8e9,
  cpu: 2.1,
};
const MOCK_REMOTE = new Set(["t_lasso", "t_triage"]);
/** V0.124: "lab" answers with another install id than the one the front first saw, until the owner trusts it. */
let mockLabTrusted = false;
export function trustMockLab() {
  mockLabTrusted = true;
}
// V0.132: the owner's host order (&hostOrder=lab,desk sets one), as the front sends hosts[] in it.
let mockHostOrder: string[] | null = typeof location !== "undefined" ? (new URLSearchParams(location.search).get("hostOrder")?.split(",").filter(Boolean) ?? null) : null;
export function setMockHostOrder(names: string[]) {
  mockHostOrder = names;
}
export function mockHosts(now: number, labTrusted = mockLabTrusted) {
  const hs = mockHostsRaw(now, labTrusted);
  const o = mockHostOrder;
  if (!o) return hs;
  const rank = (n: string) => (o.includes(n) ? o.indexOf(n) : o.length);
  return [...hs].sort((a, b) => rank(a.name) - rank(b.name));
}
function mockHostsRaw(now: number, labTrusted: boolean) {
  const iso = (ms: number) => new Date(ms).toISOString();
  return [
    { name: "desk", online: true, since: iso(now - 3 * 3600_000), id: "F7QK-2M9X-AB34-CD56", url: "http://127.0.0.1:7777", machine: "laptop", os: "linux", link: { state: "up", via: "pipe" },
      // V0.host-updates: runs the newest release
      update: { running: "c705ef5365660a8f", build: "c705ef5", version: "v1.0.0", channel: "stable", wanted: "c705ef5365660a8f", behind: 0, last: { build: "c705ef5", commit: "c705ef5365660a8f", result: "installed", startedAt: iso(now - 3 * 3600_000 - 120_000), endedAt: iso(now - 3 * 3600_000) } } },
    { name: "host-b", online: false, since: iso(now - 25 * 60_000), error: "unreachable", found: true, id: "M3RS-QX7P-5LKA-2WNE", url: "http://100.64.0.9:7778", machine: "server", os: "linux",
      // V0.host-updates: three commits behind; its last update rolled back and the release is held back
      update: { running: "9b1e04a77d2c", build: "9b1e04a", channel: "main", wanted: "c705ef5365660a8f", behind: 3, last: { build: "c705ef5", commit: "c705ef5365660a8f", result: "rolledBack", reason: "exited with code 1 after 2s", startedAt: iso(now - 50 * 60_000), endedAt: iso(now - 48 * 60_000) }, bad: { commit: "c705ef5365660a8f", why: "exited with code 1 after 2s", at: iso(now - 48 * 60_000) } } },
    { name: "lab", online: true, since: iso(now - 40 * 60_000), id: "B7LA-4QZE-9KDF-3HXW", url: "http://100.64.0.7:7778", machine: "desktop", os: "darwin", ...(labTrusted ? {} : { idWas: "AX2C-7RTM-8PLQ-6VYD" }),
      // V0.132: lab's own providers, so a thread started there offers them
      profiles: [
        { id: "claude", name: "Claude", color: "#d97757", models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"], defaultModel: "claude-sonnet-5-5" },
        { id: "zai", name: "Z.ai", color: "#5b6bd6", models: ["glm-5", "glm-4.6"], defaultModel: "glm-5" },
      ] },
    // V0.125: a host older than V0.124 (no id), on the LAN
    { name: "old-nuc", online: true, since: iso(now - 5 * 3600_000), url: "http://old-nuc.lan:7778" },
  ];
}
function mockHostOf(t: ThreadDTO, now: number): ThreadDTO {
  if (!MOCK_HOSTS) return t;
  if (t.host && t.host !== "desk") return t; // V0.132: made on another host
  if (!MOCK_REMOTE.has(t.id)) return { ...t, host: "desk" };
  return { ...t, host: "host-b", hostOffline: new Date(now - 25 * 60_000).toISOString() };
}

/** SPEC 0.5 V0.44: provider quota for the mock profiles; "work" is busy and stale. */
export function mockQuota(now: number): Quota[] {
  const at = (min: number) => new Date(now + min * 60_000).toISOString();
  return [
    { profile: "claude", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 34, resetsAt: at(140) }, { name: "7 d", usedPct: 61, resetsAt: at(3 * 1440) }], updatedAt: at(-2) },
    { profile: "work", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 86, resetsAt: at(45) }, { name: "7 d", usedPct: 40, resetsAt: at(5 * 1440) }], updatedAt: at(-14), stale: true, error: "Rate limited by the provider; trying again later", retryAt: at(6) },
  ];
}

/** SPEC 0.5 V0.80: a busy host: load past the cores, the agents at their cap; one thread in each fixture is hot
 * (t_w2, t_mapperf), one calm, one without a second read yet. */
export function mockLoad(): HostLoad[] {
  return withGuard(withLeftovers(withUndeclared(mockLoadBase(), MOCK_UNDECLARED), MOCK_LEFTOVERS), MOCK_GUARD);
}

/** Decision 0032: ?guard=1 has the memory guard's history: memory short twice in 15 minutes, Map perf's undeclared work
 *  frozen three times (the third still on), Babysit frozen once. ?guard=capped shows a thread let run after 8 pauses. */
const MOCK_GUARD = typeof location !== "undefined" ? new URLSearchParams(location.search).get("guard") ?? "" : "";
export function withGuard(list: HostLoad[], mode: string, now = railNow(Date.now())): HostLoad[] {
  if (!mode) return list;
  const [h, ...rest] = list;
  const ago = (sec: number) => new Date(now - sec * 1000).toISOString();
  const ev = (secAgo: number, kind: GuardEvent["kind"], extra: Partial<GuardEvent> = {}): GuardEvent => ({ at: ago(secAgo), kind, ...extra });
  const events: GuardEvent[] = [
    ev(14 * 60, "short", { headroom: 0.04, stall: "critical" }),
    ev(13.5 * 60, "freeze", { work: "undeclared", thread: "t_mapperf", n: 1, headroom: 0.04 }),
    ev(12 * 60, "recovered", { headroom: 0.31 }),
    ev(11.9 * 60, "thaw", { work: "undeclared", thread: "t_mapperf", reason: "memory" }),
    ev(8 * 60, "short", { headroom: 0.05, stall: "critical" }),
    ev(7.5 * 60, "freeze", { work: "undeclared", thread: "t_mapperf", n: 2, headroom: 0.05 }),
    ev(7 * 60, "freeze", { work: "undeclared", thread: "t_babysit", n: 1, headroom: 0.05 }),
    ev(5 * 60, "thaw", { work: "undeclared", thread: "t_babysit", reason: "gone" }),
    ev(4 * 60, "thaw", { work: "undeclared", thread: "t_mapperf", reason: "memory" }),
    ev(150, "freeze", { work: "undeclared", thread: "t_mapperf", n: mode === "capped" ? 8 : 3, headroom: 0.04 }),
  ];
  const guard: Guard = {
    short: true,
    shortSince: ago(8 * 60),
    max: 8,
    refreezeSec: 60,
    events,
    threads: [{ thread: "t_mapperf", n: mode === "capped" ? 8 : 3, frozen: true, frozenAt: ago(150), capped: mode === "capped" }, { thread: "t_babysit", n: 1, frozen: false, nextAt: new Date(now + 24_000).toISOString() }],
  };
  return [{ ...h, slots: { ...h.slots!, guard } }, ...rest];
}

/** V0.176: ?leftovers=1: Map perf left a Chrome and a mockup server, Nix flake update a port-forward under timeout, a
 *  dev server Babysit PR #4170's agent kept, a test binary in a cgroup of no thread, and two settled threads' worktrees. */
const MOCK_LEFTOVERS = typeof location !== "undefined" ? new URLSearchParams(location.search).get("leftovers") ?? "" : "";
export function withLeftovers(list: HostLoad[], mode: string, now = railNow(Date.now())): HostLoad[] {
  if (!mode) return list;
  const [h, ...rest] = list;
  const ago = (min: number) => new Date(now - min * 60_000).toISOString();
  const leftovers: Leftover[] = [
    { id: "41022@100", thread: "t_mapperf", pid: 41022, name: "chromium", command: "/nix/store/x-chromium/bin/chromium --headless --user-data-dir=/tmp/fyr-layout-a1", procs: 9, rss: 1.9e9, started: ago(65), seen: ago(60), endsAt: ago(-25) },
    { id: "41090@101", thread: "t_mapperf", pid: 41090, name: "http.server", command: "python3 -m http.server 8000", procs: 1, rss: 21e6, started: ago(60), seen: ago(58), endsAt: ago(-27) },
    { id: "38810@102", thread: "t_nix", pid: 38810, name: "kubectl port-forward", command: "timeout 12h kubectl port-forward svc/db 5432", procs: 2, rss: 62e6, started: ago(90), seen: ago(88), until: ago(-630) },
    { id: "39001@103", thread: "t_babysit", pid: 39001, name: "vite", command: "node /w/node_modules/.bin/vite --port 5173", procs: 3, rss: 180e6, started: ago(40), seen: ago(38), kept: { thread: "t_babysit", pid: 39001, until: ago(-180), why: "the dev server the owner asked for", by: "agent" } },
    { id: "37777@104", thread: "t_0ld", orphan: true, pid: 37777, name: "engine.test", command: "/tmp/go-build1/b001/engine.test -test.run=TestX", procs: 1, rss: 8e6, started: ago(360), seen: ago(1), endsAt: ago(-1) },
  ];
  const temps: Temp[] = [
    { path: "/home/me/fyr/.claude/worktrees/map-perf", kind: "worktree", thread: "t_mapperf", bytes: 412e6, safe: true, removeAt: ago(-5 * 60) }, // V0.fix-flakes: on the mock's clock, as every time above; Date.now() read "in 1 min" before 09:32
    { path: "/home/me/fyr/.claude/worktrees/nix-bump", kind: "worktree", thread: "t_nix", bytes: 3e6, safe: false, why: "2 commits not pushed" },
  ];
  return [{ ...h, leftovers, temps }, ...rest];
}

/** V0.176: host.leftoverEnd, host.leftoverKeep and host.tempRemove on the mock host's load, as the host answers them. */
export function mockLeftoverCommand(h: HostLoad, action: string, args: Record<string, unknown>, now = Date.now()): CommandReply {
  if (action === "host.tempRemove") {
    const t = h.temps?.find((x) => x.path === args.path);
    if (!t) return { ok: false, error: "not a listed folder" };
    if (!t.safe) return { ok: false, error: `kept: ${t.why ?? "not safe"}` };
    h.temps = h.temps!.filter((x) => x !== t);
    return { ok: true, result: { path: t.path } };
  }
  const l = h.leftovers?.find((x) => x.id === args.id);
  if (!l) return { ok: false, error: "no such leftover process" };
  if (action === "host.leftoverEnd") {
    h.leftovers = h.leftovers!.filter((x) => x !== l);
    return { ok: true, result: { id: l.id } };
  }
  const hours = typeof args.hours === "number" ? args.hours : 24;
  l.kept = { thread: l.thread, pid: l.pid, until: new Date(now + hours * 3600_000).toISOString(), why: "kept by the owner", by: "owner" };
  delete l.endsAt;
  return { ok: true, result: { id: l.id, hours } };
}

/** V0.164: the mock host's view of undeclared work (mode "" is none): the threads, the shares in the pool, the wait
 *  reason that names it, the thresholds and one command Fyr learned. */
export function withUndeclared(list: HostLoad[], mode: string, now = railNow(Date.now())): HostLoad[] {
  if (!mode) return list;
  const [h, ...rest] = list;
  const s = h.slots!;
  const use = (thread: string, mem: number, cpu: number, memPct: number, cpuPct: number, secs: number, command: string, extra: Partial<UndeclaredUse> = {}): UndeclaredUse => ({
    thread, mem, cpu, memPct, cpuPct, since: new Date(now - secs * 1000).toISOString(), command, paused: false, queued: false, overCpu: true, overMem: true, ...extra,
  });
  const und: UndeclaredUse[] = [use("t_mapperf", 2.8e9, 2.1, 20, 52, 180, "node test/layout.e2e.mjs", { paused: mode === "paused" })];
  if (mode === "2") und.push(use("t_babysit", 1.6e9, 1.2, 11, 30, 50, "yarn pre-push:check"));
  const admitted = { cpu: 13, mem: 16 };
  const uCpu = und.reduce((n, u) => n + u.cpuPct, 0);
  const uMem = und.reduce((n, u) => n + u.memPct, 0);
  const cpuPct = admitted.cpu + uCpu;
  const memPct = admitted.mem + uMem;
  const need = 45;
  const free = Math.max(0, 100 - cpuPct);
  // the host names the largest and "+N" for the others
  const names = `Map perf uses ${und[0].cpuPct} % undeclared${und.length > 1 ? ` +${und.length - 1}` : ""}`;
  const slots: Slots = {
    ...s,
    held: [{ owner: "t_w2", kind: "test", cpuPct: admitted.cpu, memPct: admitted.mem, pct: admitted.cpu }],
    waiting: [{ owner: "t_search", kind: "test", cpuPct: need, memPct: 10, pct: need }],
    why: `needs ≈ ${need} % CPU, ${free} % free (${names})`,
    needPct: need,
    cpuPct,
    memPct,
    cpuFreePct: free,
    memFreePct: 100 - memPct,
    usedPct: Math.max(cpuPct, memPct),
    tight: cpuPct >= memPct ? "cpu" : "memory",
    binding: "cpu",
    undeclared: und,
    undeclaredCpuPct: uCpu,
    undeclaredMemPct: uMem,
  };
  const threads = h.threads.map((t) => (t.id === "t_mapperf" ? { ...t, job: undefined, paused: undefined } : t));
  return [
    {
      ...h,
      slots,
      threads,
      headroom: mode === "paused" ? 0.06 : h.headroom,
      memoryStall: mode === "paused" ? "critical" : h.memoryStall,
      undeclaredSettings: { on: true, cpu: 0.25, memory: 0.08, after: 45 },
      heavyAdded: [{ pattern: "python3? build_admin_ny\\.py", kind: "build", at: new Date(now - 24 * 3600_000).toISOString(), refused: 0, learned: true }],
      servicePatterns: [
        { pattern: "(^|[\\s/])docker\\s+compose\\b.*\\sup\\b", builtIn: true },
        { pattern: "(^|[\\s/])(npm|pnpm|bun)\\s+run\\s+dev\\b", builtIn: true },
      ],
      rerun: { classes: ["make test"] }, // V0.rerun-classes
    },
    ...rest,
  ];
}
/** V0.rerun-classes: host.rerunClasses on the mock host's load, with the host's refusals. */
export function mockRerunCommand(h: HostLoad, args: Record<string, unknown>): { ok: boolean; error?: string; result?: Record<string, unknown> } {
  const list = h.rerun?.classes ?? [];
  const add = typeof args.add === "string" ? args.add.trim().split(/\s+/).join(" ") : "";
  const remove = typeof args.remove === "string" ? args.remove : "";
  if (add && !remove) {
    if (add.length > 200) return { ok: false, error: "a class is a command of 1 to 200 characters, e.g. make test" };
    h.rerun = { classes: list.includes(add) ? list : [...list, add] };
    return { ok: true, result: { add } };
  }
  if (remove && !add) {
    if (!list.includes(remove)) return { ok: false, error: "no such rerunnable class" };
    h.rerun = { classes: list.filter((c) => c !== remove) };
    return { ok: true, result: { remove } };
  }
  return { ok: false, error: "give add or remove: one class of 1 to 200 characters" };
}

/** V0.164: host.heavyRemove and host.undeclaredSettings on the mock host's load, with the host's refusals (an unknown
 *  pattern, a threshold out of range). */
export function mockUndeclaredCommand(h: HostLoad, action: string, args: Record<string, unknown>): { ok: boolean; error?: string; result?: Record<string, unknown> } {
  if (action === "host.servicePatterns") {
    // V0.services-not-jobs: as the host does it (a removed built-in pattern can be added back).
    const list = h.servicePatterns ?? [];
    if (typeof args.remove === "string" && args.remove) {
      if (!list.some((x) => x.pattern === args.remove)) return { ok: false, error: "no such service pattern" };
      h.servicePatterns = list.filter((x) => x.pattern !== args.remove);
    }
    if (typeof args.add === "string" && args.add.trim()) {
      try {
        new RegExp(args.add);
      } catch {
        return { ok: false, error: "not a valid pattern" };
      }
      if (!(h.servicePatterns ?? []).some((x) => x.pattern === args.add)) h.servicePatterns = [...(h.servicePatterns ?? []), { pattern: args.add.trim() }];
    }
    return { ok: true, result: args };
  }
  if (action === "host.heavyRemove") {
    if (!h.heavyAdded?.some((x) => x.pattern === args.pattern)) return { ok: false, error: "That pattern was not added" };
    h.heavyAdded = h.heavyAdded.filter((x) => x.pattern !== args.pattern);
    return { ok: true };
  }
  const s = h.undeclaredSettings;
  if (!s) return { ok: false, error: "This host does not detect undeclared work" };
  const share = (v: unknown) => typeof v === "number" && v >= 0.01 && v <= 1;
  if (args.cpu !== undefined && !share(args.cpu)) return { ok: false, error: "cpu is a share from 1 % to 100 %" };
  if (args.memory !== undefined && !share(args.memory)) return { ok: false, error: "memory is a share from 1 % to 100 %" };
  if (args.after !== undefined && !(typeof args.after === "number" && args.after >= 30 && args.after <= 120)) return { ok: false, error: "after is 30 to 120 seconds" };
  h.undeclaredSettings = { on: typeof args.on === "boolean" ? args.on : s.on, cpu: (args.cpu as number) ?? s.cpu, memory: (args.memory as number) ?? s.memory, after: (args.after as number) ?? s.after };
  return { ok: true, result: { ...h.undeclaredSettings } };
}

function mockLoadBase(): HostLoad[] {
  // V0.141: with ?hosts=2, lab's load follows the laptop's (calm, two free slots), as a front with two hosts shows.
  const lab: HostLoad[] = MOCK_HOSTS ? [{ host: "lab", cores: 16, load: [0.3, 0.5, 0.6], cpuPressure: 0.4, cgroups: true, agents: { cpu: 0.1, cpuMax: 8, memory: 1e9, memoryMax: 28 * 2 ** 30 }, threads: [], slots: { n: 2, held: [], waiting: [] } }] : [];
  return [...laptopLoad(), ...lab];
}
function laptopLoad(): HostLoad[] {
  return [
    {
      host: "laptop",
      cores: 8,
      load: [10.4, 7.9, 5.2],
      cpuPressure: 46.3,
      cgroups: true,
      agents: { cpu: 3.96, cpuMax: 4, memory: 9.1e9, memoryMax: 14 * 2 ** 30 },
      threads: [
        { id: "t_w1", cpu: 0.4, memory: 6e8 },
        { id: "t_w2", cpu: 2.7, memory: 3.2e9 },
        { id: "t_w3", memory: 2e8 },
        { id: "t_babysit", cpu: 0.3, memory: 5e8 },
        { id: "t_mapperf", cpu: 2.6, memory: 3e9, job: "yarn build", paused: MOCK_PAUSED || undefined },
        { id: "t_search", cpu: 1.0, memory: 9e8 },
      ],
      // V0.86: two jobs run, one thread waits. V0.163: heavy jobs in percent of the host, CPU and memory apart; the
      // head waits for CPU.
      slots: {
        n: 2,
        why: "needs ≈ 30 % CPU, 5 % free",
        held: [
          { owner: "t_w2", kind: "test", cpuPct: 50, memPct: 16, pct: 50 },
          { owner: "t_mapperf", kind: "build", cpuPct: 45, memPct: 25, pct: 45 },
        ],
        waiting: [{ owner: "t_search", kind: "test", cpuPct: 30, memPct: 10, pct: 30 }],
        cpuPct: 95,
        memPct: 41,
        cpuBudgetPct: 100,
        memBudgetPct: 100,
        cpuFreePct: 5,
        memFreePct: 59,
        usedPct: 95,
        tight: "cpu",
        binding: "cpu",
        needPct: 30,
      },
      // Decision 0029 S1: the host's signals and the measured job classes.
      provider: "cgroups",
      capacity: { cpu: 4, memory: 13 * 2 ** 30 },
      slowdown: 3.4,
      headroom: 0.31,
      memoryPressure: 1.2,
      memoryStall: "ok",
      agentsCpuPressure: 88,
      // V0.load-cleanup: oomd's rule on the agents' slice, and the stall it compares (the card's "oomd" row).
      oomdLimit: 85,
      unitMemoryStall: 3.2,
      jobs: [
        { class: "fyr: make test", runs: 14, cores: 2.3, demand: 3.4, peak: 2.1e9, wall: 95, at: "2026-11-17T09:12:00Z", learned: 14, cpuPct: 58, memPct: 15 },
        { class: "Acme: yarn screenshot:diff", runs: 6, cores: 1.1, demand: 1.9, peak: 6.4e9, wall: 437, at: "2026-11-17T08:40:00Z", learned: 6, cpuPct: 28, memPct: 46 },
        { class: "fyr: e2e", runs: 3, cores: 0.9, demand: 1.2, peak: 1.8e9, wall: 254, at: "2026-11-17T08:02:00Z", learned: 2, cpuPct: 50, memPct: 50 },
      ],
    },
  ];
}

/** SPEC V0.137, for screenshots and the layout test: &slots=queue puts the deploy's tests and three threads in the
 *  queue; &slots=two adds a second host (lab) with a queue of its own; &slots=none
 *  has no queue. */
export function mockQueueLoad(mode: string): HostLoad[] {
  const [h] = mockLoad();
  // V0.163: the same pool in percent; Fyr's own tests wait first, the head's reason is theirs.
  h.slots = {
    ...h.slots!,
    why: "needs ≈ 50 % CPU, 5 % free",
    needPct: 50,
    waiting: [
      { owner: "self-deploy", kind: "test", priority: true, cpuPct: 50, memPct: 25, pct: 50 },
      { owner: "t_search", kind: "test", cpuPct: 30, memPct: 10, pct: 30 },
      { owner: "t_pw", kind: "e2e", cpuPct: 20, memPct: 35, pct: 35 },
      { owner: "t_nix", kind: "build", cpuPct: 40, memPct: 20, pct: 40 },
    ],
  };
  if (mode === "none") h.slots.waiting = [];
  if (mode !== "two") return [h];
  return [h, { host: "lab", cores: 4, load: [1.2, 1, 0.8], cgroups: false, threads: [], slots: { n: 1, held: [{ owner: "t_snap", kind: "test" }], waiting: [{ owner: "t_poly", kind: "test" }] } }];
}

/** SPEC 0.5 V0.79: the agent's commands and skills, as a host serves them for any thread or folder. */
export const MOCK_COMMANDS: SlashCommand[] = [
  { name: "compact", description: "Clear the conversation history but keep a summary in context", argumentHint: "<optional custom summarization instructions>" },
  { name: "context", description: "Show the current context usage" },
  { name: "init", description: "Write a CLAUDE.md file with the codebase's documentation" },
  { name: "pr-comments", description: "Get the comments of a GitHub pull request" },
  { name: "review", description: "Review a pull request", argumentHint: "<pr-number>" },
  { name: "security-review", description: "Complete a security review of the pending changes on the current branch" },
  { name: "research", description: "Research a problem in parallel via web docs, web search, and codebase exploration", argumentHint: "<question>" },
  { name: "handover", description: "Session wrap-up: document state for resumption or handoff" },
  { name: "dev:check", description: "The repo's single local gate: tests, typecheck, lint and format by repo type" },
  { name: "dev:review", description: "Pre-PR review gate: one pass for correctness bugs and security only" },
];

// Fixtures for ?mock=1 (full stress set) and ?mock=2 (sparse: empty sections). A tiny in-memory fyrd:
// commands change the fixtures and push thread upserts, so the UI can be clicked through without a host.

import type { CommandReply, Decision, Project, TaskDTO, FyrEvent, StateDTO, T3Candidate, ThreadDTO } from "./types.ts";
import type { ChangesDTO, FilePatchDTO } from "./changes.ts";

export function mockScenario(search: string): string | null {
  const m = /[?&]mock=([^&]*)/.exec(search);
  if (!m || m[1] === "0" || m[1] === "") return null;
  return decodeURIComponent(m[1]);
}

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
    unavailable: {},
    ...extra,
  };
}

function task(id: string, description: string, status: TaskDTO["status"], now: number, startedMin: number, extra: Partial<TaskDTO> = {}): TaskDTO {
  return { id, description, type: "local_agent", status, activity: "", summary: "", toolUses: 0, tokens: 0, startedAt: iso(now - startedMin * 60_000), updatedAt: iso(now - Math.max(0, startedMin - 1) * 60_000), ...extra };
}

const NOT_RUNNING = { "thread.interrupt": "No run is going", "thread.retry": "Nothing failed" };

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
    e("msg", toolUse("tu2", "Task", { description: "explore gestures", prompt: "Explore how other sheets close" })),
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
    e("run.ended", { failure, interrupted: false, costUsd: 0.42, durationMs: 840_000 }),
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
        task("k2", "Explore sheet gestures", "running", now, 3, { activity: "Read src/ui/Sheet.tsx", toolUses: 14, tokens: 143_218 }),
        task("k3", "Check iPad popover", "completed", now, 20, { summary: "The popover already closes on swipe down.", toolUses: 6, tokens: 41_000 }),
        task("k4", "Run the visual diff", "failed", now, 25, { summary: "Playwright could not start: no display", toolUses: 2, tokens: 3_100 }),
        task("k5", "Översätt felmeddelanden", "stopped", now, 40, { activity: "Edit src/i18n/sv.json", toolUses: 1, tokens: 900 }),
      ],
      attention: { state: "asking", section: "needs", reason: "2 questions" },
      decisions: [askDecision(now)],
      summary: "The lasso no longer closes the sheet.",
      pr: { url: "https://github.com/acme/web/pull/1234", repo: "acme/web", number: 1234, state: "OPEN", checks: "pass", review: "", conflict: false },
      unavailable: { ...NOT_RUNNING, "thread.settle": "A question is open" },
    }),
    askEvents("t_ask", now),
  );
  add(
    base("t_fail", "Speed up e2e suite", now, 9, {
      attention: { state: "failed", section: "needs", reason: "API error" },
      failure: { class: "api", message: "API error 529: overloaded, stopped after 14 min", at: iso(now - 9 * 60_000) },
      unavailable: { "thread.interrupt": "No run is going" },
    }),
  );
  add(
    base("t_block", "Listing cards v2", now, 21, {
      attention: { state: "blocked", section: "needs", reason: "CI failed" },
      summary: "lint: 2 errors in CardGrid.tsx",
      pr: { url: "https://github.com/acme/web/pull/88", repo: "acme/web", number: 88, state: "OPEN", checks: "fail", conflict: false },
      unavailable: NOT_RUNNING,
    }),
  );
  add(
    base("t_approval", LONG_TITLE, now, 4, {
      project: LONG_PROJECT,
      attention: { state: "asking", section: "needs", reason: "Approval" },
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
      attention: { state: "asking", section: "needs", reason: "Update Fyr" },
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
      attention: { state: "asking", section: "needs", reason: "Merge" },
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
      attention: { state: "done", section: "needs", reason: "Done" },
      summary: "Filed 6 issues, closed 2 duplicates",
      unavailable: NOT_RUNNING,
    }),
  );
  add(
    base("t_fix", "Fix", now, 75, {
      project: LONG_PROJECT,
      attention: { state: "done", section: "needs", reason: "Done" },
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
  add(base("t_s2", "Nix flake update", now, 115, { project: "dotfiles", summary: "Updated nixpkgs to 26.05.", unavailable: NOT_RUNNING }));
  // V0.28: a thread with no project whose folder is a clone suggests one.
  add(
    base("t_s4", "Sketch the lasso tool", now, 320, {
      project: "",
      cwd: "/home/owner/src/lasso-app",
      summary: "Drew a first lasso over the map.",
      suggest: { name: "lasso-app", sources: [{ kind: "folder", host: "desk", path: "/home/owner/src/lasso-app" }, { kind: "repo", url: "github.com/owner/lasso-app" }] },
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


// ?mock=rail: the exact content of the first (light) frame of design/directions/04-rail.html.
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
      attention: { state: "asking", section: "needs", reason: "" },
      decisions: [ask],
      pr: { url: "https://github.com/acme/web/pull/1234", repo: "acme/web", number: 1234, state: "OPEN", checks: "pass", conflict: false },
      unavailable: { ...quiet, "thread.settle": "A question is open" },
    }),
    base("t_e2e", "Speed up e2e suite", now, 9, {
      attention: { state: "failed", section: "needs", reason: "API error" },
      failure: { class: "api", message: "stopped after 14 min", at: min(9) },
      unavailable: { "thread.interrupt": "No run is going" },
    }),
    base("t_cards", "Listing cards v2", now, 21, {
      attention: { state: "blocked", section: "needs", reason: "CI failed" },
      summary: "lint: 2 errors in CardGrid.tsx",
      pr: { url: "https://github.com/acme/web/pull/88", repo: "acme/web", number: 88, state: "OPEN", checks: "fail", conflict: false },
      unavailable: quiet,
    }),
    base("t_triage", "Triage sprint issues", now, 60, {
      project: "acme/ops",
      attention: { state: "done", section: "needs", reason: "Done" },
      summary: "Filed 6 issues, closed 2 duplicates",
      unavailable: quiet,
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
        tasks: waiting
          ? [
              task("dt1", "Design mobile translation", "running", now, m, { activity: "Running build", toolUses: 14, tokens: 143_218 }),
              task("dt2", "Watch the deploy", "running", now, m - 2, { activity: act, toolUses: 3, tokens: 12_000 }),
            ]
          : id === "t_mapperf"
            ? [task("mt1", "Profile the map render", "running", now, 6, { activity: "Bash yarn vitest src/map --reporter=verbose", toolUses: 21, tokens: 310_000 })]
            : [],
        attention: { state, section: "working", reason: waiting ? "2 agents" : "" },
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
  for (const t of threads) if (t.id !== "t_lasso") events[t.id] = simpleEvents(t.id, now, `Work on: ${t.title}`, t.summary || "Done.", t.failure);

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
  // V0.22: Search index was started by Map perf's agent, which also wrote to Babysit PR #4170.
  for (const t of threads) if (t.id === "t_search") t.parent = "t_mapperf";
  for (const t of threads) if (t.id === "t_dish") t.cwd = "/home/owner/scratch/2026-11-14-dishwasher";
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

export const MOCK_SHOTS: Record<string, string> = {
  pv_phone: svgShot(390, 844, "Filter sheet, iPhone mini"),
  pv_desk: svgShot(1280, 800, "Map, desktop 1280 × 800"),
  pv_tall: svgShot(390, 2600, "Listing page, full", true),
};
export const MOCK_PREVIEWS: Record<string, string> = {
  pv_chart: "data:text/html;charset=utf-8," + encodeURIComponent(CHART_HTML),
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
  e((t += 1), "run.ended", { failure: null, interrupted: false, costUsd: 0.42, durationMs: 840_000 });
  // V0.33: a second run reads one message; the next one waits in the agent's queue.
  e((t += 60), "user", { id: "m_map2", text: "Fix the snapshot test too.", images: 0 });
  e(t, "message.state", { id: "m_map2", state: "read" });
  e(t, "run.started", {});
  e((t += 2), "msg", toolUse("sn1", "Bash", { command: "yarn vitest src/map/snapshot", description: "Run the snapshot test" }));
  e((t += 30), "user", { id: "m_map3", text: "Keep the snapshot itself; only make the test faster.", images: 0 });
  e(t, "message.state", { id: "m_map3", state: "queued" });
  return out;
}

// ---- projects (V0.23): every colour and icon kind, a repo-only project and a label ----
export function mockProjects(now: number): Project[] {
  const ago = (h: number) => iso(now - h * 3600_000);
  const folder = (path: string) => ({ kind: "folder", host: "desk", path });
  const repo = (url: string) => ({ kind: "repo", url });
  return [
    { id: "p_web", name: "acme/web", color: "teal", icon: { kind: "line", value: "Globe" }, sources: [folder("/home/owner/src/acme/web"), repo("github.com/acme/web")], profile: "claude", model: "claude-sonnet-5-5", threads: 9, lastUsed: ago(0.1), cwd: "/home/owner/src/acme/web" },
    { id: "p_ops", name: "acme/ops", color: "rose", icon: { kind: "emoji", value: "🚀" }, sources: [folder("/home/owner/src/acme/ops")], threads: 2, lastUsed: ago(1), cwd: "/home/owner/src/acme/ops" },
    { id: "p_long", name: LONG_PROJECT, color: "indigo", icon: { kind: "monogram" }, sources: [folder("/home/owner/src/" + LONG_PROJECT), repo("github.com/acme/" + LONG_PROJECT)], threads: 3, lastUsed: ago(5), cwd: "/home/owner/src/" + LONG_PROJECT },
    { id: "p_dot", name: "dotfiles", color: "olive", icon: { kind: "line", value: "terminal" }, sources: [folder("/home/owner/dotfiles")], threads: 1, lastUsed: ago(2), cwd: "/home/owner/dotfiles" },
    { id: "p_api", name: "acme/api", color: "sky", icon: { kind: "line", value: "server" }, sources: [folder("/home/owner/src/acme/api")], threads: 0, lastUsed: ago(30), cwd: "/home/owner/src/acme/api" },
    { id: "p_fyr", name: "fyr", color: "violet", icon: { kind: "line", value: "FolderGit2" }, sources: [repo("github.com/meros/fyr")], threads: 0, lastUsed: ago(48) },
    { id: "p_home", name: "Garden", color: "green", icon: { kind: "emoji", value: "🏡" }, sources: [], threads: 1, lastUsed: ago(3) },
    { id: "p_infra", name: "acme/infra", color: "slate", icon: { kind: "line", value: "no-such-icon" }, sources: [folder("/home/owner/src/acme/infra")], threads: 1, lastUsed: ago(10), cwd: "/home/owner/src/acme/infra" },
    { id: "p_maplab", name: "Map perf lab", color: "blue", icon: { kind: "line", value: "map" }, sources: [folder("/home/owner/src/maplab")], threads: 0, lastUsed: ago(70), cwd: "/home/owner/src/maplab" },
    { id: "p_book", name: "Bokföring åt Åsa", icon: { kind: "monogram" }, color: "brown", sources: [folder("/home/owner/ekonomi")], threads: 0, lastUsed: ago(200), cwd: "/home/owner/ekonomi" },
  ];
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
  /** the in-memory attachment store (V0.18) */
  putAttachment(name: string, mediaType: string, size: number): { id: string; name: string; size: number; mediaType: string; path: string };
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
  let sub: { thread(t: ThreadDTO): void; event(e: FyrEvent): void; project?(p: Project): void } | null = null;
  let projects = mockProjects(now0);
  const putProject = (p: Project) => {
    projects = projects.some((x) => x.id === p.id) ? projects.map((x) => (x.id === p.id ? p : x)) : [...projects, p];
    setTimeout(() => sub?.project?.(p), 30);
    return p;
  };
  const projectIdFor = (t: ThreadDTO) => t.projectId ?? projects.find((p) => p.name === t.project)?.id;
  let nextId = 1;

  const put = (t: ThreadDTO, patch: Partial<ThreadDTO>) => {
    const n: ThreadDTO = { ...t, ...patch, updatedAt: new Date(clock()).toISOString() };
    n.projectId = n.projectId ?? projectIdFor(n);
    threads.set(n.id, n);
    setTimeout(() => sub?.thread(n), 30);
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
    emit(tid, "user", { id, text, images: imgs, attachments: attachmentsOf(args) });
    if (!running) {
      emit(tid, "message.state", { id, state: "read" });
      return id;
    }
    emit(tid, "message.state", { id, state: "queued" });
    unread.set(id, tid);
    setTimeout(() => {
      if (unread.delete(id)) emit(tid, "message.state", { id, state: "read" });
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
      emit(id, "run.ended", { failure: null, interrupted: false, costUsd: 0.01, durationMs: 1800 });
      put(cur, {
        running: false,
        activity: "",
        summary: reply,
        activityAt: new Date(clock()).toISOString(),
        decisions: [],
        attention: { state: "done", section: "needs", reason: "Done" },
        unavailable: quiet,
      });
    }, 1800);
  };

  const exec = (action: string, t: ThreadDTO | undefined, args: Record<string, unknown>): CommandReply => {
    if (action === "thread.create") {
      const id = `t_new${nextId++}`;
      const cwd = String(args.cwd ?? "");
      const nt = base(id, String(args.title || args.text || "New thread").slice(0, 80), clock(), 0, {
        cwd,
        project: cwd.split("/").slice(-2).join("/"),
        profile: String(args.profile ?? "claude"),
        model: String(args.model ?? "claude-opus-5-5"),
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
      for (const k of ["name", "color", "icon", "profile", "model"] as const) if (k in args) (patch as Record<string, unknown>)[k] = args[k];
      putProject({ ...p, ...patch });
      return { ok: true };
    }
    if (action === "project.create") {
      const name = String(args.name ?? "").trim();
      if (!name) return { ok: false, error: "A project needs a name" };
      const sources = Array.isArray(args.sources) ? (args.sources as Project["sources"]) : [];
      const folder = sources?.find((x) => x.kind === "folder") as { path?: string } | undefined;
      putProject({ id: `p_new${nextId++}`, name, sources, threads: 0, lastUsed: new Date(clock()).toISOString(), cwd: folder?.path });
      return { ok: true };
    }
    if (action === "project.createFrom") {
      const t = threads.get(String(args.threadId ?? ""));
      const sug = t?.suggest;
      if (!t || !sug || projectIdFor(t)) return { ok: false, error: "This thread has no project to make" };
      const folder = sug.sources.find((x) => x.kind === "folder") as { path?: string } | undefined;
      const p = putProject({ id: `p_new${nextId++}`, name: sug.name, sources: sug.sources, threads: 1, lastUsed: new Date(clock()).toISOString(), cwd: folder?.path });
      put(t, { projectId: p.id, project: p.name, suggest: undefined });
      return { ok: true };
    }
    if (action === "project.removeSource") {
      const p = projects.find((x) => x.id === args.id);
      const i = Number(args.index);
      if (!p || !Number.isInteger(i) || i < 0 || i >= (p.sources ?? []).length) return { ok: false, error: "No such source" };
      putProject({ ...p, sources: (p.sources ?? []).filter((_, k) => k !== i) });
      return { ok: true };
    }
    if (action === "project.addSource") {
      const p = projects.find((x) => x.id === args.id);
      if (!p) return { ok: false, error: "No such project" };
      putProject({ ...p, sources: [...(p.sources ?? []), args.source as NonNullable<Project["sources"]>[number]] });
      return { ok: true };
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
        break;
      }
      case "thread.answer": {
        const d = t.decisions?.find((x) => x.id === args.decisionId);
        const ans = args.answers ? Object.values(args.answers as Record<string, string>).join(" · ") : args.allow ? "Allowed" : "Denied";
        emit(t.id, "decision.closed", { id: args.decisionId, answer: ans });
        if (d?.kind === "effect" && args.allow) emit(t.id, "effect.result", { action: d.effect?.action, ok: true, text: "Merged #4170 with squash." });
        put(t, { decisions: (t.decisions ?? []).filter((x) => x.id !== args.decisionId) });
        runFor(t.id, "Thanks, continuing with that answer. (mock)");
        break;
      }
      case "thread.interrupt":
        emit(t.id, "run.ended", { failure: null, interrupted: true, costUsd: 0, durationMs: 100 });
        idle("seen", "seen", { running: false, activity: "", unavailable: quiet });
        break;
      case "thread.retry":
        runFor(t.id, "Retried, and it worked this time. (mock)");
        break;
      case "thread.dismiss":
        idle("seen", "seen", { failure: null });
        break;
      case "thread.settle":
        if (t.running) put(t, { settleWhenDone: true });
        else idle("settled", "settled", { settledAt: new Date(clock()).toISOString() });
        break;
      case "thread.unsettle":
        idle("seen", "seen", { settledAt: null, settleWhenDone: false });
        break;
      case "thread.snooze":
        idle("snoozed", "snoozed", { view: { snoozeUntil: String(args.until) } });
        break;
      case "thread.unsnooze":
        idle("seen", "seen", { view: { snoozeUntil: null } });
        break;
      case "thread.archive":
        idle("archived", "archived", { archivedAt: new Date(clock()).toISOString() });
        break;
      case "thread.unarchive":
        idle("seen", "seen", { archivedAt: null });
        break;
      case "thread.markSeen":
        if (t.attention.state === "done") idle("seen", "seen", { view: { seenAt: new Date(clock()).toISOString() } });
        break;
      case "thread.markUnseen":
        idle("done", "needs");
        break;
      case "thread.rename":
        put(t, { title: String(args.title ?? t.title) });
        break;
      case "thread.setProject": {
        const p = projects.find((x) => x.id === args.projectId);
        if (!p) return { ok: false, error: "No such project" };
        put(t, { projectId: p.id, project: p.name });
        break;
      }
      case "thread.setModel":
        put(t, { model: String(args.model ?? t.model) });
        break;
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
        user: "owner",
        buildId: "mock",
        threads: [...threads.values()].map((t) => ({ ...t, projectId: projectIdFor(t) })),
        actions: [
          { name: "thread.settle", label: "Settle", key: "e", bulk: true },
          { name: "thread.snooze", label: "Snooze", key: "s" },
          { name: "thread.archive", label: "Archive", key: "#" },
        ],
        profiles: [
          { id: "claude", name: "Claude", color: "#d97757", models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5"], defaultModel: "claude-opus-5-5" },
          { id: "work", name: "Claude (work account)", color: "#2f6fb0", models: ["claude-sonnet-5-5"], defaultModel: "claude-sonnet-5-5" },
        ],
        projects,
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
    events(threadId, after, limit) {
      const list = events[threadId] ?? [];
      if (after > 0) return list.filter((e) => e.seq > after).slice(0, limit);
      return list.slice(-limit);
    },
    t3Candidates() {
      return fx.t3;
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

/** The branch diff of a mock thread; t_deploy is outside git. */
export function mockChanges(threadId: string): ChangesDTO {
  if (threadId === "t_deploy") return { git: false };
  return {
    git: true,
    root: "/home/owner/src/acme/web",
    branch: "fix/map-lasso-mobile",
    base: "origin/main",
    mergeBase: "3f2a9c1d0e",
    commits: 3,
    truncated: false,
    files: [
      { path: "docs/old-notes.md", status: "deleted", additions: 0, deletions: 12, binary: false },
      { path: LONG_CHANGE_PATH, status: "modified", additions: 4, deletions: 2, binary: false },
      { path: "web/public/marker.png", status: "added", additions: 0, deletions: 0, binary: true },
      { path: "web/src/map/lasso.ts", oldPath: "web/src/map/select.ts", status: "renamed", additions: 6, deletions: 2, binary: false },
      { path: "web/src/map/render.ts", status: "modified", additions: 42, deletions: 18, binary: false },
      { path: "web/test/map.test.ts", status: "added", additions: 64, deletions: 0, binary: false },
    ],
  };
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

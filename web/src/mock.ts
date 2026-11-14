// Fixtures for ?mock=1 (full stress set) and ?mock=2 (sparse: empty sections). A tiny in-memory fyrd:
// commands change the fixtures and push thread upserts, so the UI can be clicked through without a host.

import type { CommandReply, Decision, FyrEvent, StateDTO, T3Candidate, ThreadDTO } from "./types.ts";

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
function toolResult(id: string, isError = false, parent: string | null = null) {
  return { type: "user", parent_tool_use_id: parent, message: { content: [{ type: "tool_result", tool_use_id: id, content: "ok", is_error: isError }] } };
}

function askEvents(id: string, now: number): FyrEvent[] {
  let s = 0;
  let t = now - 20 * 60_000;
  const e = (type: string, data: unknown, step = 20_000) => ev(++s, (t += step), id, type, data);
  return [
    e("user", { text: "The lasso tool closes the filter sheet on iPhone mini. Fix it and open a PR. Screenshot attached.", images: 1 }),
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
    base("t_s1", "Lasso polygon perf", now, 40, {
      summary: "Merged; the polygon is simplified before hit-testing.",
      pr: { url: "https://github.com/acme/web/pull/1201", repo: "acme/web", number: 1201, state: "MERGED", checks: "pass", conflict: false },
      unavailable: NOT_RUNNING,
    }),
  );
  add(base("t_s2", "Nix flake update", now, 115, { project: "dotfiles", summary: "Updated nixpkgs to 26.05.", unavailable: NOT_RUNNING }));
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

export interface MockServer {
  buildId: string;
  state(): StateDTO;
  subscribe(h: { thread(t: ThreadDTO): void; event(e: FyrEvent): void }): void;
  command(commandId: string, action: string, threadId: string | undefined, args: Record<string, unknown>): CommandReply;
  events(threadId: string, after: number, limit: number): FyrEvent[];
  t3Candidates(): T3Candidate[];
}

export function createMockServer(scenario: string, now0: number): MockServer {
  const fx = scenario === "2" ? sparseFixture(now0) : fullFixture(now0);
  const threads = new Map(fx.threads.map((t) => [t.id, t]));
  const events = fx.events;
  const replies = new Map<string, CommandReply>();
  let sub: { thread(t: ThreadDTO): void; event(e: FyrEvent): void } | null = null;
  let nextId = 1;

  const put = (t: ThreadDTO, patch: Partial<ThreadDTO>) => {
    const n: ThreadDTO = { ...t, ...patch, updatedAt: new Date().toISOString() };
    threads.set(n.id, n);
    setTimeout(() => sub?.thread(n), 30);
    return n;
  };
  const emit = (id: string, type: string, data: unknown) => {
    const list = (events[id] ??= []);
    const e = ev((list.at(-1)?.seq ?? 0) + 1, Date.now(), id, type, data);
    list.push(e);
    setTimeout(() => sub?.event(e), 20);
  };
  const quiet = { ...NOT_RUNNING };
  const runFor = (id: string, reply: string) => {
    const t = threads.get(id)!;
    put(t, {
      running: true,
      runStartedAt: new Date().toISOString(),
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
        activityAt: new Date().toISOString(),
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
      const nt = base(id, String(args.title || args.text || "New thread").slice(0, 80), Date.now(), 0, {
        cwd,
        project: cwd.split("/").slice(-2).join("/"),
        profile: String(args.profile ?? "claude"),
        model: String(args.model ?? "claude-opus-5-5"),
      });
      threads.set(id, nt);
      events[id] = [];
      const imgs = Array.isArray(args.images) ? args.images.length : 0;
      emit(id, "user", { text: String(args.text ?? ""), images: imgs });
      runFor(id, "Started on it. (mock)");
      return { ok: true, threadId: id };
    }
    if (action === "t3.adopt") {
      const ids = (args.t3Ids as string[]) ?? [];
      for (const tid of ids) {
        const c = fx.t3.find((x) => x.t3Id === tid);
        if (!c) continue;
        const id = `t_t3_${tid}`;
        const nt = base(id, c.title, Date.now(), 0, { cwd: c.cwd, project: c.cwd.split("/").slice(-2).join("/"), source: "t3", unavailable: quiet });
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
        const imgs = Array.isArray(args.images) ? args.images.length : 0;
        emit(t.id, "user", { text: String(args.text ?? ""), images: imgs });
        runFor(t.id, "Got it. Working on that now. (mock reply)");
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
        else idle("settled", "settled", { settledAt: new Date().toISOString() });
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
        idle("archived", "archived", { archivedAt: new Date().toISOString() });
        break;
      case "thread.unarchive":
        idle("seen", "seen", { archivedAt: null });
        break;
      case "thread.markSeen":
        if (t.attention.state === "done") idle("seen", "seen", { view: { seenAt: new Date().toISOString() } });
        break;
      case "thread.markUnseen":
        idle("done", "needs");
        break;
      case "thread.rename":
        put(t, { title: String(args.title ?? t.title) });
        break;
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
        now: new Date().toISOString(),
        user: "owner",
        buildId: "mock",
        threads: [...threads.values()],
        actions: [
          { name: "thread.settle", label: "Settle", key: "e", bulk: true },
          { name: "thread.snooze", label: "Snooze", key: "s" },
          { name: "thread.archive", label: "Archive", key: "#" },
        ],
        profiles: [
          { id: "claude", name: "Claude", color: "#d97757", models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5"], defaultModel: "claude-opus-5-5" },
          { id: "work", name: "Claude (work account)", color: "#2f6fb0", models: ["claude-sonnet-5-5"], defaultModel: "claude-sonnet-5-5" },
        ],
        projects: [
          { cwd: "/home/owner/src/acme/web", name: "acme/web" },
          { cwd: "/home/owner/src/acme/ops", name: "acme/ops" },
          { cwd: "/home/owner/src/" + LONG_PROJECT, name: LONG_PROJECT },
          { cwd: "/home/owner/dotfiles", name: "dotfiles" },
        ],
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
  };
}

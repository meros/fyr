import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation, callLabel, fullOutputOf, runLabel, cutOutput, friendlyName, groupStats, groupSummary, groupTone, hiddenCall, middleTruncate, pastTense, relPath, segments, type Item } from "../src/conversation.ts";

let seq = 0;
const T0 = Date.parse("2026-11-14T09:00:00Z");
const ev = (type: string, data: unknown, sec = seq) => ({ seq: ++seq, at: new Date(T0 + sec * 1000).toISOString(), subject: "t", type, data });
const use = (id: string, name: string, input: unknown = {}) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "tool_use", id, name, input }] } });
const res = (id: string, isError = false, content: unknown = "ok") => ev("msg", { type: "user", parent_tool_use_id: null, message: { content: [{ type: "tool_result", tool_use_id: id, is_error: isError, content }] } });
const text = (t: string) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text: t }] } });
const tools = (items: Item[]) => items.filter((i): i is Extract<Item, { kind: "tools" }> => i.kind === "tools");

test("card tools and plumbing are hidden, results matched by tool_use_id", () => {
  const items = buildConversation([
    use("a", "ToolSearch", { query: "select:mcp__fyr__show" }),
    res("a"),
    use("b", "mcp__fyr__show", { path: "/tmp/x.png" }),
    res("b", false, "attachment x"),
    ev("preview", { kind: "image", attachmentId: "x", title: "Shot", width: 1, height: 1 }),
    use("c", "TodoWrite", { todos: [] }),
    use("d", "Bash", { command: 'settask "[web] x"' }),
    res("c"),
    res("d"),
    use("e", "mcp__fyr__effect_request", { action: "deploy" }),
    res("e"),
    use("f", "AskUserQuestion", {}),
    use("g", "mcp__fyr__pr_link", { url: "u" }),
  ]);
  assert.deepEqual(items.map((i) => i.kind), ["preview"], "only the card remains");
});

test("a hidden call that failed shows as one error row", () => {
  const items = buildConversation([use("a", "mcp__fyr__show", { path: "/nope.png" }), res("a", true, "no such file"), use("b", "ToolSearch"), res("b", true, "no match")]);
  const t = tools(items);
  assert.equal(t.length, 1);
  assert.deepEqual(t[0].calls.map((c) => [c.name, c.isError, c.output]), [
    ["mcp__fyr__show", true, "no such file"],
    ["ToolSearch", true, "no match"],
  ]);
  assert.equal(hiddenCall({ name: "ToolSearch", isError: false, input: {} }), true);
  assert.equal(hiddenCall({ name: "Bash", isError: false, input: { command: "yarn test" } }), false);
});

test("summary wording: counted by kind, singular and plural, in order of first use", () => {
  const n = (name: string, description = "") => ({ name, description });
  assert.equal(groupSummary([n("Read"), n("Read"), n("Read"), n("Bash"), n("Bash"), n("WebSearch"), n("Edit")]), "Read 3 files · ran 2 commands · searched the web · edited 1 file");
  assert.equal(groupSummary([n("Read"), n("Grep"), n("Grep"), n("WebFetch")]), "Read 1 file · searched the code 2 times · fetched 1 page");
  assert.equal(groupSummary([n("Bash", "Run the Go tests"), n("Bash"), n("Bash"), n("Read"), n("Read"), n("Read")]), "Ran the Go tests and 2 more · read 3 files");
  assert.equal(groupSummary([n("mcp__github__get_pr"), n("mcp__github__get_pr")]), "Used GitHub get pr 2 times");
});

test("group stats: wall time and failures", () => {
  const items = buildConversation([use("a", "Read", { file_path: "/x" }, ), use("b", "Bash", { command: "false" }), res("a"), res("b", true, "exit 1")]);
  const g = tools(items)[0];
  const st = groupStats(g.calls);
  assert.equal(st.failed, 1);
  assert.equal(st.running, 0);
  assert.ok(st.durationMs !== null && st.durationMs >= 0);
  assert.deepEqual(segments(g.calls).map((s) => s.kind), ["fold"]);
});

test("MCP names read as words", () => {
  assert.equal(friendlyName("mcp__fyr__show"), "Show");
  assert.equal(friendlyName("mcp__github__get_pr"), "GitHub get pr");
  assert.equal(friendlyName("mcp__linear__create_issue"), "Linear create issue");
  assert.equal(friendlyName("mcp__acme__do_thing"), "Acme do thing");
  assert.equal(friendlyName("WebSearch"), "Web search");
  assert.equal(friendlyName("Read"), "Read");
});

test("labels: the agent's description first, else a sentence; paths relative and shortened", () => {
  const cwd = "/home/owner/src/fyr";
  assert.equal(callLabel("Bash", { command: "go test ./...", description: "Run the Go tests" }, cwd), "Run the Go tests");
  assert.equal(callLabel("Bash", { command: "CI=1 yarn test --run src/map" }, cwd), "Ran yarn test");
  assert.equal(callLabel("Read", { file_path: "/home/owner/src/fyr/web/src/app.tsx" }, cwd), "Read web/src/app.tsx");
  assert.equal(callLabel("Read", { file_path: "/home/owner/notes.md" }, cwd), "Read ~/notes.md");
  assert.equal(callLabel("Edit", { file_path: "/home/owner/src/fyr/web/src/thread.tsx", old_string: "a\nb\nc", new_string: Array(12).fill("x").join("\n") }, cwd), "Edited web/src/thread.tsx (+12 −3)");
  assert.equal(callLabel("Grep", { pattern: "presence" }, cwd), 'Searched for "presence"');
  assert.equal(callLabel("WebFetch", { url: "https://github.com/acme/web/pull/4182/files?diff=split" }, cwd), "Fetched github.com/acme…ll/4182/files?diff=split");
  assert.equal(callLabel("LS", { path: "/home/owner/src/fyr/web/src" }, cwd), "Listed web/src");
  assert.equal(relPath("/home/owner/src/fyr/web/src/very/deeply/nested/folder/structure/tiles.test.ts", cwd), "web/src/very/de…/structure/tiles.test.ts");
  assert.equal(middleTruncate("abcdefghij", 7), "ab…ghij");
  assert.equal(middleTruncate("short", 40), "short");
  assert.equal(pastTense("Run the Go tests"), "Ran the Go tests");
  assert.equal(pastTense("Frobnicate the cache"), "Frobnicate the cache");
});

test("long output is cut at 200 lines", () => {
  const t = Array.from({ length: 450 }, (_, i) => `line ${i}`).join("\n");
  const c = cutOutput(t);
  assert.equal(c.text.split("\n").length, 200);
  assert.equal(c.hidden, 250);
  assert.deepEqual(cutOutput("a\nb"), { text: "a\nb", hidden: 0 });
});

test("machine lines are quiet by default and full with tool details", () => {
  const evs = [
    ev("user", { text: "Deploy it" }),
    ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "thinking", thinking: "hmm" }] } }),
    text("Deploying."),
    ev("task", { kind: "started", id: "k", description: "Watch" }),
    ev("task", { kind: "ended", id: "k", description: "Watch", status: "completed", summary: "ok" }),
    ev("task", { kind: "ended", id: "j", description: "Bisect", status: "failed", summary: "no browser" }),
    ev("decision.closed", { id: "d", answer: "Approve" }),
    ev("effect.result", { action: "deploy.staging", ok: true, text: "Deploy succeeded · build 20261114-112026\n\nlong log" }),
    ev("user", { text: "[Fyr] Your effect_request deploy.staging succeeded" }),
    ev("run.ended", { failure: null, interrupted: false, costUsd: 0.42, durationMs: 840000 }),
  ];
  const calm = buildConversation(evs);
  assert.deepEqual(calm.map((i) => i.kind), ["user", "assistant", "task", "answer", "effect", "run"], "no thinking, no ok task lines, no [Fyr] note");
  const task = calm.find((i) => i.kind === "task") as Extract<Item, { kind: "task" }>;
  assert.equal(task.text, "Agent failed: no browser");
  const eff = calm.find((i) => i.kind === "effect") as Extract<Item, { kind: "effect" }>;
  assert.equal(eff.line, "Deploy succeeded · build 20261114-112026");
  assert.ok(eff.text.includes("long log"), "the full text stays one tap away");
  assert.equal((calm.at(-1) as Extract<Item, { kind: "run" }>).compact, true);
  const full = buildConversation(evs, { details: true });
  assert.deepEqual(full.map((i) => i.kind), ["user", "assistant", "task", "task", "task", "answer", "effect", "note", "run"]);
  assert.equal((full.at(-1) as Extract<Item, { kind: "run" }>).compact, false);
  assert.equal((full.find((i) => i.kind === "note") as Extract<Item, { kind: "note" }>).line, "Your effect_request deploy.staging succeeded");
});

// SPEC 0.5 V0.21: the end of a run reads as words, never as a bare symbol (the owner
// found a lone check mark under the last message odd).
test("run end label", () => {
  const run = (o: Partial<Extract<Item, { kind: "run" }>>) => ({ kind: "run", key: "k", failure: null, interrupted: false, compact: true, ...o }) as Extract<Item, { kind: "run" }>;
  assert.deepEqual(runLabel(run({ durationMs: 840000, costUsd: 0.42 })), { text: "Finished in 14 min", more: "$0.42" });
  assert.deepEqual(runLabel(run({ interrupted: true, durationMs: 120000 })), { text: "Stopped after 2 min", more: "" });
  assert.deepEqual(runLabel(run({})), { text: "Finished", more: "" });
  assert.equal(runLabel(run({ failure: { class: "api", message: "overloaded" } as never })).text, "Failed (api): overloaded");
});

// SPEC 0.5 V0.37: the text of the turn Fyr forced to update the live view shows as one
// quiet line (the owner's choice); the run's own answer stays a normal message.
test("the forced view turn is a quiet line", () => {
  const msg = (text: string) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text }] } });
  const items = buildConversation([
    ev("user", { text: "go" }),
    msg("Fixed; the e2e run is green."),
    ev("view.forced", {}),
    msg("Updated the view."),
    ev("run.ended", { failure: null, interrupted: false, durationMs: 60000 }),
    ev("user", { text: "next" }),
    msg("Next answer."),
  ]);
  const kinds = items.map((i) => (i.kind === "assistant" ? `assistant${i.quiet ? ":quiet" : ""}` : i.kind));
  assert.deepEqual(kinds, ["user", "assistant", "assistant:quiet", "run", "user", "assistant"]);
});

// SPEC 0.5 V0.38: a tool output the host cut keeps the event's seq, and the full event
// gives the whole output back for that call.
test("a cut tool output can be loaded whole", () => {
  const use = ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "tool_use", id: "u1", name: "Bash", input: { command: "ls" } }] } });
  const res = ev("msg", { type: "user", parent_tool_use_id: null, cut: true, message: { content: [{ type: "tool_result", tool_use_id: "u1", content: "head\n… [30 KB more; open the full output]" }] } });
  const items = buildConversation([ev("user", { text: "go" }), use, res]);
  const card = items.find((i) => i.kind === "tools") as Extract<Item, { kind: "tools" }>;
  assert.equal(card.calls[0].cutSeq, res.seq);
  const full = { seq: res.seq, data: { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "u1", content: [{ type: "text", text: "the whole output" }] }] } } };
  assert.equal(fullOutputOf(full, "u1"), "the whole output");
  assert.equal(fullOutputOf(full, "other"), null);
});

// SPEC 0.5 V0.16, V0.41, #91: a group whose last call succeeded after a failure is recovered (amber), not failed (red).
test("group tone: failed only when the last call failed; recovered when a later call succeeded", () => {
  const tone = (evs: unknown[]) => groupTone(groupStats(tools(buildConversation(evs as never))[0].calls));
  assert.equal(tone([use("a", "Read", { file_path: "/x" }), use("b", "Read", { file_path: "/y" }), res("a", true, "no such file"), res("b")]), "recovered");
  assert.equal(tone([use("a", "Read", { file_path: "/x" }), use("b", "Bash", { command: "false" }), res("a"), res("b", true, "exit 1")]), "failed");
  assert.equal(tone([use("a", "Read", { file_path: "/x" }), res("a")]), "ok");
  assert.equal(tone([use("a", "Read", { file_path: "/x" }), use("b", "Bash", { command: "sleep 9" }), res("a", true, "x")]), "running");
});

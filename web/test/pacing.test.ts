// SPEC 0.5 V0.41: pacing. The outcome reads at full weight; the work folds into one line per stretch.
import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation, pace, roles, liveRun, type Entry } from "../src/conversation.ts";

let seq = 0;
const ev = (type: string, data: unknown, sec?: number) => {
  seq++;
  return { seq, at: new Date(1_700_000_000_000 + (sec ?? seq) * 1000).toISOString(), subject: "t", type, data };
};
const text = (t: string) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text: t }] } });
const use = (id: string, name = "Bash", input: unknown = { command: "yarn test", description: "Run the tests" }) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "tool_use", id, name, input }] } });
const result = (id: string, isError = false) => ev("msg", { type: "user", parent_tool_use_id: null, message: { content: [{ type: "tool_result", tool_use_id: id, content: "ok", is_error: isError }] } });

/** "fold" or "<role>:<kind>[:text]" for each entry. */
function shape(entries: Entry[]): string[] {
  return entries.map((e) => (e.kind === "fold" ? `fold(${e.items.map((i) => i.kind).join(",")})` : `${e.role}:${e.item.kind}${e.item.kind === "assistant" ? `:${e.item.text}` : ""}`));
}

test("a finished run: the last message is the outcome, the steps before it fold into one line", () => {
  const items = buildConversation([
    ev("user", { text: "Fix it" }),
    ev("run.started", {}),
    text("I'll look before I touch anything."),
    use("a"),
    result("a"),
    text("Test first, then the code:"),
    use("b", "Read", { file_path: "/x/a.ts" }),
    result("b"),
    use("c", "Edit", { file_path: "/x/a.ts", old_string: "a", new_string: "b" }),
    result("c"),
    text("160 tests pass and the typecheck is clean."),
    ev("run.ended", { durationMs: 12_000 }),
  ]);
  assert.deepEqual(shape(pace(items)), ["outcome:user", "fold(assistant,tools,assistant,tools)", "outcome:assistant:160 tests pass and the typecheck is clean.", "end:run"]);
  const fold = pace(items).find((e) => e.kind === "fold") as Extract<Entry, { kind: "fold" }>;
  assert.equal(fold.text, "Ran the tests · read 1 file · edited 1 file");
  assert.equal(fold.steps, 5);
  assert.equal(fold.failed, 0);
  assert.ok(fold.durationMs !== null && fold.durationMs > 0);
});

test("failures are outcomes; a successful effect and an answer are steps", () => {
  const items = buildConversation([
    ev("run.started", {}),
    use("a"),
    result("a", true),
    ev("task", { kind: "ended", id: "k", description: "Bisect", status: "failed", summary: "no browser" }),
    ev("decision.closed", { answer: "Approve" }),
    ev("effect.result", { action: "deploy", ok: true, text: "Fyr now runs build 1" }),
    ev("effect.result", { action: "deploy", ok: false, text: "Deploy failed" }),
    ev("run.ended", { failure: { class: "api", message: "overloaded" } }),
  ]);
  assert.deepEqual(roles(items), ["process", "outcome", "process", "process", "outcome", "outcome"]);
  // a failed call keeps its mark on the line; a stretch of system lines only stays as it is
  assert.deepEqual(shape(pace(items)), ["process:tools", "outcome:task", "process:answer", "process:effect", "outcome:effect", "outcome:run"]);
});

test("while the run goes on, the newest message reads as the outcome and the steps after it stay open", () => {
  const evs = [
    ev("user", { text: "Go" }),
    ev("run.started", {}),
    text("A new deploy went live; not mine."),
    use("a"),
    result("a"),
    text("Test first:"),
    use("b"),
    result("b"),
    text("160 tests pass. I'll check the layout."),
    use("c", "Bash", { command: "make shots", description: "Take the screenshots" }),
    ev("effect.result", { action: "x", ok: true, text: "Done" }),
  ];
  const items = buildConversation(evs);
  assert.equal(liveRun(items), 1);
  assert.deepEqual(shape(pace(items)), ["outcome:user", "fold(assistant,tools,assistant,tools)", "outcome:assistant:160 tests pass. I'll check the layout.", "process:tools", "process:effect"]);
  const fold = pace(items)[1] as Extract<Entry, { kind: "fold" }>;
  assert.equal(fold.running, 0);
  // once a newer message arrives, the one before becomes a step and the finished run folds whole
  const later = pace(buildConversation([...evs, result("c"), text("The layout holds at 700 px."), ev("run.ended", {})]));
  assert.deepEqual(shape(later), ["outcome:user", "fold(assistant,tools,assistant,tools,assistant,tools,effect)", "outcome:assistant:The layout holds at 700 px.", "end:run"]);
});

test("the owner's message in the middle of a run splits the steps; one step alone is not folded", () => {
  const items = buildConversation([
    ev("run.started", {}),
    text("Checking the repo."),
    use("a"),
    result("a"),
    ev("user", { text: "Also make the panel hideable" }),
    use("b"),
    result("b"),
    text("Done: the panel hides with \\."),
    ev("run.ended", {}),
  ]);
  assert.deepEqual(shape(pace(items)), ["fold(assistant,tools)", "outcome:user", "process:tools", "outcome:assistant:Done: the panel hides with \\.", "end:run"]);
});

test("events without run markers: the last message before the owner's next one is the outcome", () => {
  const items = buildConversation([ev("user", { text: "a" }), text("one"), use("a"), result("a"), text("two"), ev("user", { text: "b" }), text("three")]);
  assert.deepEqual(roles(items), ["outcome", "process", "process", "outcome", "outcome", "outcome"]);
  assert.equal(liveRun(items), undefined);
});

test("with tool details on, nothing folds", () => {
  const items = buildConversation([ev("run.started", {}), text("one"), use("a"), result("a"), text("two"), ev("run.ended", {})], { details: true });
  assert.ok(!pace(items, { details: true }).some((e) => e.kind === "fold"));
  assert.ok(pace(items).some((e) => e.kind === "fold"));
});

test("subagents in a folded line are named as subagents", () => {
  const items = buildConversation([ev("run.started", {}), text("Exploring."), use("a", "Task", { description: "explore" }), use("b", "Task", { description: "explore" }), result("a"), result("b"), text("Found it."), ev("run.ended", {})]);
  const fold = pace(items).find((e) => e.kind === "fold") as Extract<Entry, { kind: "fold" }>;
  assert.equal(fold.text, "Started 2 subagents");
});

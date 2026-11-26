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
const toolUse = (id: string, name = "Bash", input: unknown = { command: "yarn test", description: "Run the tests" }) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "tool_use", id, name, input }] } });
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
    toolUse("a"),
    result("a"),
    text("Test first, then the code:"),
    toolUse("b", "Read", { file_path: "/x/a.ts" }),
    result("b"),
    toolUse("c", "Edit", { file_path: "/x/a.ts", old_string: "a", new_string: "b" }),
    result("c"),
    text("160 tests pass and the typecheck is clean."),
    ev("run.ended", { durationMs: 12_000 }),
  ]);
  assert.deepEqual(shape(pace(items)), ["outcome:user", "fold(assistant,tools,assistant,tools)", "outcome:assistant:160 tests pass and the typecheck is clean.", "end:run"]);
  const fold = pace(items).find((e) => e.kind === "fold") as Extract<Entry, { kind: "fold" }>;
  // V0.midrun-answer: the line names the messages it holds before the tools
  assert.equal(fold.text, "2 messages · Ran the tests · read 1 file · edited 1 file");
  assert.equal(fold.steps, 5);
  assert.equal(fold.failed, 0);
  assert.ok(fold.durationMs !== null && fold.durationMs > 0);
});

test("failures are outcomes; a successful effect and an answer are steps", () => {
  const items = buildConversation([
    ev("run.started", {}),
    toolUse("a"),
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
    toolUse("a"),
    result("a"),
    text("Test first:"),
    toolUse("b"),
    result("b"),
    text("160 tests pass. I'll check the layout."),
    toolUse("c", "Bash", { command: "make shots", description: "Take the screenshots" }),
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

test("the owner's message in the middle of a run splits the steps and closes the answer before it; one step alone is not folded", () => {
  const items = buildConversation([
    ev("run.started", {}),
    text("Checking the repo."),
    toolUse("a"),
    result("a"),
    ev("user", { text: "Also make the panel hideable" }),
    toolUse("b"),
    result("b"),
    text("Done: the panel hides with \\."),
    ev("run.ended", {}),
  ]);
  // V0.midrun-answer: the agent's last text before the message it read is an outcome too
  assert.deepEqual(shape(pace(items)), ["outcome:assistant:Checking the repo.", "process:tools", "outcome:user", "process:tools", "outcome:assistant:Done: the panel hides with \\.", "end:run"]);
});

test("events without run markers: the last message before the owner's next one is the outcome", () => {
  const items = buildConversation([ev("user", { text: "a" }), text("one"), toolUse("a"), result("a"), text("two"), ev("user", { text: "b" }), text("three")]);
  assert.deepEqual(roles(items), ["outcome", "process", "process", "outcome", "outcome", "outcome"]);
  assert.equal(liveRun(items), undefined);
});

test("with tool details on, nothing folds", () => {
  const items = buildConversation([ev("run.started", {}), text("one"), toolUse("a"), result("a"), text("two"), ev("run.ended", {})], { details: true });
  assert.ok(!pace(items, { details: true }).some((e) => e.kind === "fold"));
  assert.ok(pace(items).some((e) => e.kind === "fold"));
});

test("subagents in a folded line are named as subagents", () => {
  const items = buildConversation([ev("run.started", {}), text("Exploring."), toolUse("a", "Task", { description: "explore" }), toolUse("b", "Task", { description: "explore" }), result("a"), result("b"), text("Found it."), ev("run.ended", {})]);
  const fold = pace(items).find((e) => e.kind === "fold") as Extract<Entry, { kind: "fold" }>;
  assert.equal(fold.text, "1 message · Started 2 subagents");
});

// V0.midrun-answer: an answer the owner's mid-run message closed is not folded away.
const reported = (state: string) => [
  ev("user", { text: "Price a parallel coding workstation" }),
  ev("run.started", {}),
  text("A: the priced comparison table."),
  ev("user", { text: "Compare with on demand cloud runner prices", id: "m1" }),
  toolUse("a", "mcp__fyr__offer_action", { label: "x" }),
  ev("message.state", { id: "m1", state }),
  result("a"),
  text("Looking up the cloud prices."),
  toolUse("b"),
  result("b"),
  toolUse("c"),
  result("c"),
  toolUse("d"),
  result("d"),
  text("B: the cloud comparison."),
  ev("run.ended", {}),
];

test("the reported run: the answer before the owner's mid-run message and the last answer are both outcomes", () => {
  const items = buildConversation(reported("read"));
  assert.deepEqual(shape(pace(items)), [
    "outcome:user",
    "outcome:assistant:A: the priced comparison table.",
    "process:tools",
    "outcome:user",
    "fold(assistant,tools)",
    "outcome:assistant:B: the cloud comparison.",
    "end:run",
  ]);
  const fold = pace(items).find((e) => e.kind === "fold") as Extract<Entry, { kind: "fold" }>;
  assert.equal(fold.text, "1 message · Ran the tests and 2 more");
});

test("a mid-run message the agent never read closes no answer", () => {
  for (const state of ["cancelled", "lost"]) {
    const items = buildConversation(reported(state));
    const rs = roles(items);
    const a = items.findIndex((it) => it.kind === "assistant" && it.text.startsWith("A:"));
    assert.equal(rs[a], "process", state);
  }
});

test("a text in an earlier run is not closed by a message read in a later one", () => {
  const items = buildConversation([
    ev("run.started", {}),
    text("one"),
    toolUse("a"),
    result("a"),
    text("two"),
    ev("run.ended", {}),
    ev("run.started", {}),
    toolUse("b"),
    result("b"),
    ev("user", { text: "and?", id: "m2" }),
    ev("message.state", { id: "m2", state: "read" }),
    text("three"),
    ev("run.ended", {}),
  ]);
  assert.deepEqual(shape(pace(items)), ["fold(assistant,tools)", "outcome:assistant:two", "end:run", "process:tools", "outcome:user", "outcome:assistant:three", "end:run"]);
});

test("a fold of messages only names them", () => {
  const items = buildConversation([ev("run.started", {}), text("one"), text("two"), text("three"), ev("run.ended", {})]);
  const fold = pace(items).find((e) => e.kind === "fold") as Extract<Entry, { kind: "fold" }>;
  assert.equal(fold.text, "2 messages");
});

// V0.ask-closes-answer: the text the agent wrote before it asked the owner something is not folded away.
test("the reported run: the explanation before a question card and the closing line are both outcomes", () => {
  const items = buildConversation([
    ev("user", { text: "Go on" }),
    ev("run.started", {}),
    toolUse("a"),
    result("a"),
    text("## How a call is stored today\n\n| Value | What it says |\n|---|---|\n| No answer | how the call went |"),
    toolUse("q", "mcp__fyr__ask", { questions: [{ question: "Change the data model too?" }] }),
    result("q"),
    text("I'll wait for your answer."),
    ev("run.ended", {}),
  ]);
  assert.deepEqual(shape(pace(items)), [
    "outcome:user",
    "process:tools",
    "outcome:assistant:## How a call is stored today\n\n| Value | What it says |\n|---|---|\n| No answer | how the call went |",
    "outcome:assistant:I'll wait for your answer.",
    "end:run",
  ]);
});

test("AskUserQuestion and a Run card request close the text before them too; narration between still folds", () => {
  for (const name of ["AskUserQuestion", "mcp__fyr__run_request"]) {
    const items = buildConversation([
      ev("run.started", {}),
      text("Looking."),
      toolUse("a"),
      result("a"),
      text("Here is what I found."),
      toolUse("q", name, {}),
      result("q"),
      text("Next I check the logs."),
      toolUse("b"),
      result("b"),
      text("Done."),
      ev("run.ended", {}),
    ]);
    const rs = roles(items);
    const said = items.map((it, i) => (it.kind === "assistant" ? `${rs[i]}:${it.text}` : "")).filter(Boolean);
    assert.deepEqual(said, ["process:Looking.", "outcome:Here is what I found.", "process:Next I check the logs.", "outcome:Done."], name);
  }
});

test("a question in a later run does not close a text from an earlier run", () => {
  const items = buildConversation([
    ev("run.started", {}),
    text("First."),
    toolUse("a"),
    result("a"),
    text("Earlier answer."),
    ev("run.ended", {}),
    ev("run.started", {}),
    toolUse("q", "mcp__fyr__ask", {}),
    result("q"),
    ev("run.ended", {}),
  ]);
  const rs = roles(items);
  assert.deepEqual(
    items.map((it, i) => (it.kind === "assistant" ? `${rs[i]}:${it.text}` : "")).filter(Boolean),
    ["process:First.", "outcome:Earlier answer."],
  );
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation, summarizeInput, toolRows } from "../src/conversation.ts";
import { renderMarkdown } from "../src/markdown.ts";

let seq = 0;
const ev = (type: string, data: unknown) => ({ seq: ++seq, at: new Date(1_700_000_000_000 + seq * 1000).toISOString(), subject: "t", type, data });
const msg = (type: string, content: unknown[], parent: string | null = null) => ev("msg", { type, parent_tool_use_id: parent, message: { content } });

test("tool calls group, subagent messages fold under their Task, errors and run ends show", () => {
  const items = buildConversation([
    ev("user", { text: "hi", images: 2 }),
    msg("assistant", [{ type: "text", text: "Looking" }]),
    msg("assistant", [{ type: "tool_use", id: "t1", name: "Bash", input: { command: "yarn test" } }]),
    msg("user", [{ type: "tool_result", tool_use_id: "t1", is_error: true }]),
    msg("assistant", [{ type: "tool_use", id: "t2", name: "Task", input: { description: "explore" } }]),
    msg("assistant", [{ type: "tool_use", id: "s1", name: "Read", input: { file_path: "a.ts" } }], "t2"),
    msg("user", [{ type: "tool_result", tool_use_id: "s1" }], "t2"),
    msg("assistant", [{ type: "text", text: "found it" }], "t2"),
    msg("user", [{ type: "tool_result", tool_use_id: "t2" }]),
    ev("run.ended", { failure: { class: "api", message: "overloaded" }, interrupted: false }),
    ev("effect.result", { action: "pr.merge", ok: true, text: "Merged" }),
    ev("something.new", {}),
  ]);
  assert.deepEqual(items.map((i) => i.kind), ["user", "assistant", "tools", "run", "effect"]);
  const user = items[0] as Extract<(typeof items)[0], { kind: "user" }>;
  assert.equal(user.images, 2);
  const tools = items[2] as Extract<(typeof items)[0], { kind: "tools" }>;
  assert.equal(tools.calls.length, 2);
  assert.equal(tools.calls[0].isError, true);
  assert.equal(tools.calls[0].summary, "yarn test");
  assert.equal(tools.calls[1].subMessages, 3);
  assert.equal(tools.calls[1].subCalls[0].summary, "a.ts");
  assert.equal(tools.calls[1].subCalls[0].done, true);
});

test("markdown escapes raw HTML and unsafe links", () => {
  const html = renderMarkdown("<script>x</script>\n\nSee [a](javascript:alert) and <b>b</b> and [b](https://ok.example)");
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(!html.includes("javascript:"));
  assert.ok(html.includes('href="https://ok.example"'));
  assert.ok(html.includes("&lt;b&gt;b&lt;/b&gt;"), "inline HTML escaped");
});

test("consecutive Task calls fold into one subagents row; Edit shows a diff stat", () => {
  const call = (id: string, name: string) => ({ id, name, summary: "", label: name, description: "", input: {}, output: "", isError: false, done: true, startedAt: 0, durationMs: 1, subMessages: 0, subCalls: [], subTexts: [] });
  const rows = toolRows([call("1", "Read"), call("2", "Task"), call("3", "Task"), call("4", "Agent"), call("5", "Bash"), call("6", "Task")]);
  assert.deepEqual(rows.map((r) => (r.kind === "agents" ? `agents:${r.calls.length}` : r.call.name)), ["Read", "agents:3", "Bash", "agents:1"]);
  assert.equal(summarizeInput("Edit", { file_path: "a.ts", old_string: "x\ny", new_string: "z" }), "a.ts +1 −2");
});

test("a user event's attachments become chips", () => {
  const items = buildConversation([
    { seq: 1, at: "2026-11-14T09:00:00Z", subject: "t", type: "user", data: { text: "", attachments: [{ id: "a1", name: "x.png", mediaType: "image/png", size: 10 }, { bad: true }] } },
  ]);
  assert.equal(items[0].kind, "user");
  assert.deepEqual((items[0] as any).attachments, [{ id: "a1", name: "x.png", mediaType: "image/png", size: 10 }]);
});

// SPEC V0.133: a tool call the agent CLI refused without asking Fyr shows as
// one failed line that says it was not the owner; the same item on desktop
// and phone (the conversation is one component).
test("a CLI refusal shows as a line that names the tool and says it was not the owner", () => {
  const items = buildConversation([
    msg("assistant", [{ type: "tool_use", id: "r1", name: "Bash", input: { command: "cat build.output" } }]),
    msg("user", [{ type: "tool_result", tool_use_id: "r1", is_error: true, content: "The user doesn't want to take this action right now." }]),
    ev("tool.refused", { tool: "Bash", toolUseId: "r1", why: "Fyr had closed the agent's input, so the CLI could not ask for permission and refused this tool call; not the owner." }),
  ]);
  const line = items.find((i) => i.kind === "effect") as Extract<(typeof items)[0], { kind: "effect" }>;
  assert.ok(line, "no line for the refusal");
  assert.equal(line.ok, false);
  assert.equal(line.action, "refused");
  assert.match(line.line, /^Bash was refused without asking you/);
  assert.match(line.text, /not the owner/);
});

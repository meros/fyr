import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation } from "../src/conversation.ts";
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

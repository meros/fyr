import assert from "node:assert/strict";
import { test } from "node:test";
import { lengthen, shorten, type LongMode } from "../src/mock-long.ts";

const MODES: LongMode[] = ["word", "words"];

for (const mode of MODES) {
  test(`${mode}: what is shown grows, what the front compares does not`, () => {
    const t = lengthen({ id: "t_1", title: "Fix", status: "running", updatedAt: "2026-11-18T10:00:00Z", attention: { state: "done", section: "seen" } }, mode);
    assert.ok(t.title.startsWith("Fix") && t.title.length > 100);
    assert.deepEqual([t.id, t.status, t.updatedAt, t.attention], ["t_1", "running", "2026-11-18T10:00:00Z", { state: "done", section: "seen" }]);
  });

  test(`${mode}: a path keeps its folder and its file name`, () => {
    const { path } = lengthen({ path: "web/src/render.ts" }, mode);
    assert.ok(path.startsWith("web/src/") && path.endsWith("/render.ts") && path.length > 100);
  });

  test(`${mode}: the same name grows the same way, and only once`, () => {
    const a = lengthen({ host: "desk" }, mode).host;
    assert.equal(lengthen({ name: "desk" }, mode).name, a);
    assert.equal(lengthen({ host: a }, mode).host, a);
  });

  test(`${mode}: an event keeps its tool's name and grows its text`, () => {
    const e = lengthen({ seq: 1, at: "x", subject: "t", type: "msg", data: { message: { content: [{ type: "tool_use", name: "Bash", input: { command: "ls" } }, { type: "text", text: "Hi" }] } } }, mode);
    const [tool, text] = e.data.message.content as unknown as [{ name: string; input: { command: string } }, { text: string }];
    assert.equal(tool.name, "Bash");
    assert.ok(tool.input.command.length > 100 && text.text.length > 100);
  });

  test(`${mode}: what the front sends back is short again`, () => {
    const v = { path: "web/src/render.ts", repo: "acme/web", list: [{ title: "Fix" }] };
    assert.deepEqual(shorten(lengthen(v, mode), mode), v);
  });
}

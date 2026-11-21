import { test } from "node:test";
import assert from "node:assert/strict";
import { formatApproval, MAX_CMD_LINES } from "../src/approval.ts";
import { buildConversation, pace } from "../src/conversation.ts";

test("formatApproval omits duplicate CommandLine and lifts BypassSandbox", () => {
  const f = formatApproval({
    input: {
      command: "gh pr create --title test",
      CommandLine: "gh pr create --title test",
      BypassSandbox: true,
      WaitMsBeforeAsync: 10000,
    },
    judgeNote: "Touches external GitHub API",
  });
  assert.equal(f.cmd, "gh pr create --title test");
  assert.equal(f.outsideSandbox, true);
  assert.equal(f.truncated, false);
  assert.equal(f.judgeNote, "Touches external GitHub API");
  assert.deepEqual(f.rest, { WaitMsBeforeAsync: 10000 });
});

test("formatApproval clamps long commands and supports omitCwd", () => {
  const lines = Array.from({ length: MAX_CMD_LINES + 5 }, (_, i) => `line ${i + 1}`);
  const full = lines.join("\n");
  const f = formatApproval({ input: { command: full, cwd: "/src/acme", BypassSandbox: false } }, { omitCwd: true });
  assert.equal(f.truncated, true);
  assert.equal(f.hiddenLines, 5);
  assert.equal(f.cmd, lines.slice(0, MAX_CMD_LINES).join("\n"));
  assert.equal(f.fullCmd, full);
  assert.equal(f.outsideSandbox, false);
  assert.deepEqual(f.rest, {});
});

test("decision.autoApproved builds a quiet note that folds with process work", () => {
  const events = [
    { seq: 1, at: "2026-11-21T10:00:00Z", subject: "t_1", type: "user", data: { text: "Run tests" } },
    { seq: 2, at: "2026-11-21T10:00:01Z", subject: "t_1", type: "run.started", data: {} },
    {
      seq: 3,
      at: "2026-11-21T10:00:02Z",
      subject: "t_1",
      type: "decision.autoApproved",
      data: { tool: "Bash", summary: "go test ./...", reason: "Routine test run", model: "gemini-4-hydrogen" },
    },
    {
      seq: 4,
      at: "2026-11-21T10:00:03Z",
      subject: "t_1",
      type: "msg",
      data: {
        type: "assistant",
        message: { content: [{ type: "tool_use", id: "t1", name: "Bash", input: { command: "go test ./..." } }] },
      },
    },
    {
      seq: 5,
      at: "2026-11-21T10:00:04Z",
      subject: "t_1",
      type: "msg",
      data: { type: "assistant", message: { content: [{ type: "text", text: "Tests passed." }] } },
    },
    { seq: 6, at: "2026-11-21T10:00:05Z", subject: "t_1", type: "run.ended", data: {} },
  ];
  const items = buildConversation(events);
  const note = items.find((i) => i.kind === "note");
  assert.ok(note && note.kind === "note");
  assert.equal(note.line, "Auto-approved Bash (go test ./...) — Routine test run");
  assert.ok(note.text.includes("[gemini-4-hydrogen]"));

  // Under default calm pacing, the note folds together with the tool call.
  const entries = pace(items);
  assert.ok(entries.some((e) => e.kind === "fold" && e.items.some((i) => i.kind === "note")));
});

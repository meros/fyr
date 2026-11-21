// V0.readable-approvals: tool approval cards in plain words; raw tool name and JSON behind Details.
import { test } from "node:test";
import assert from "node:assert/strict";
import { describeApproval, humanKey } from "../src/approvals.ts";
import type { Decision } from "../src/types.ts";

const appr = (tool: string, input: Record<string, unknown> = {}): Pick<Decision, "tool" | "input"> => ({ tool, input });

test("mcp__fyr__offer_action reads as an offered action button with its prompt in prose", () => {
  const a = describeApproval(
    appr("mcp__fyr__offer_action", {
      id: "write-adr",
      label: "Write ADR and spec",
      on: "thread",
      prompt: "Write an architecture decision record in docs/decisions/ and a spec change note.",
    }),
    "/home/owner/git/acme",
  );
  assert.equal(a.label, "Offer action");
  assert.equal(a.subject, "Write ADR and spec");
  assert.equal(a.title, "Offer action “Write ADR and spec”?");
  assert.match(a.summary, /button under the agent’s message on this thread/i);
  assert.deepEqual(a.fields, [{ label: "Prompt", value: "Write an architecture decision record in docs/decisions/ and a spec change note." }]);
  assert.equal(a.showWhere, false);
  for (const s of [a.label, a.subject, a.title, a.summary, ...a.fields.map((f) => f.value)]) {
    assert.doesNotMatch(s, /mcp__fyr__|[{}]/);
  }
  assert.equal(a.details.tool, "mcp__fyr__offer_action");
  assert.equal(a.details.input.id, "write-adr");
});

test("mcp__fyr__offer_action handles new-thread, live-view and remove variants", () => {
  const next = describeApproval(appr("mcp__fyr__offer_action", { id: "x", label: "Start follow-up", on: "thread", in: "new", prompt: "Check logs" }));
  assert.match(next.summary, /starts a new thread/);

  const view = describeApproval(appr("mcp__fyr__offer_action", { id: "run-it", label: "Rebuild", on: "view", run: "script", argv: ["make", "build"] }));
  assert.match(view.summary, /live view.*runs a script/);
  assert.deepEqual(view.fields, [{ label: "Command", value: "make build", mono: true }]);

  const rm = describeApproval(appr("mcp__fyr__offer_action", { id: "write-adr", on: "thread", remove: true }));
  assert.equal(rm.label, "Remove action");
  assert.equal(rm.title, "Remove action “write-adr”?");
});

test("Bash keeps the command verbatim, shows description and marks folder inline", () => {
  const a = describeApproval(
    appr("Bash", { command: "git push origin main", description: "Push the release commit", cwd: "/home/owner/src/acme" }),
    "/home/owner/src/acme",
  );
  assert.equal(a.label, "Run command");
  assert.equal(a.subject, "git push origin main");
  assert.equal(a.title, "Run a command?");
  assert.equal(a.summary, "Push the release commit");
  assert.equal(a.command, "git push origin main");
  assert.equal(a.where, "~/src/acme");
  assert.equal(a.showWhere, true);
  assert.equal(a.details.cwd, "/home/owner/src/acme");
});

test("file tools shorten paths relative to the thread folder", () => {
  const e = describeApproval(appr("Edit", { file_path: "/home/owner/src/acme/web/src/phone.tsx", old_string: "a\nb", new_string: "c" }), "/home/owner/src/acme");
  assert.equal(e.label, "Edit file");
  assert.equal(e.subject, "web/src/phone.tsx");
  assert.equal(e.title, "Edit web/src/phone.tsx?");
  assert.equal(e.fields[0].label, "Replace (2 lines)");
  assert.equal(e.fields[0].mono, true);
  assert.equal(e.fields[1].label, "With (1 line)");

  const w = describeApproval(appr("Write", { file_path: "/home/owner/src/acme/README.md", content: "# Hello" }), "/home/owner/src/acme");
  assert.equal(w.label, "Write file");
  assert.equal(w.title, "Write README.md?");
  assert.equal(w.fields[0].label, "Content (1 line)");

  const bare = describeApproval(appr("Write", {}));
  assert.equal(bare.label, "Write file");
  assert.equal(bare.subject, "");
  assert.equal(bare.title, "Write a file?");
});

test("a file in the thread folder itself keeps its folder name in the path", () => {
  // relPath shortens a path inside the thread's folder to its tail (V0.readable-approvals), as effect cards do; a
  // sibling folder that merely starts with the same name is not the thread's folder and keeps its own tail.
  const own = describeApproval(appr("Edit", { file_path: "/home/owner/src/acme/spec.md" }), "/home/owner/src/acme");
  assert.equal(own.subject, "spec.md");
  const sibling = describeApproval(appr("Read", { file_path: "/home/owner/src/acme-next/spec.md" }), "/home/owner/src/acme");
  assert.equal(sibling.subject, "~/src/acme-next/spec.md");
});

test("generic MCP tools use friendly names, humanized keys and cap visible fields", () => {
  const a = describeApproval(
    appr("mcp__github__create_pull_request", {
      title: "Add readable approvals",
      base_branch: "main",
      head_branch: "readable-approvals",
      draft: true,
      reviewers: ["alice", "bob"],
      body: "Makes approval cards readable.",
      extra_one: "1",
      extra_two: "2",
    }),
  );
  assert.equal(a.label, "Allow GitHub create pull request");
  assert.equal(a.subject, "Add readable approvals");
  assert.equal(a.title, "Allow GitHub create pull request?");
  assert.equal(a.fields.length, 6);
  assert.equal(a.hiddenFields, 2);
  assert.deepEqual(a.fields[0], { label: "Title", value: "Add readable approvals" });
  assert.deepEqual(a.fields[1], { label: "Base branch", value: "main", mono: true });
  assert.deepEqual(a.fields[3], { label: "Draft", value: "Yes" });
  assert.deepEqual(a.fields[4], { label: "Reviewers", value: "alice, bob" });
  assert.equal(a.details.tool, "mcp__github__create_pull_request");
  assert.equal(a.details.input.extra_two, "2");
});

test("other Fyr MCP tools format naturally", () => {
  const w = describeApproval(appr("mcp__fyr__watch", { then: "settle", when: { kind: "changeReached", change: "c_1", phase: "landed" }, note: "Done when merged" }));
  assert.equal(w.label, "Watch to settle");
  assert.equal(w.title, "Settle when a condition is met?");
  assert.equal(w.fields[0].value, "When c_1 reaches landed");

  const r = describeApproval(appr("mcp__fyr__run_request", { command: "systemctl restart app", why: "Apply the new config", root: true }));
  assert.equal(r.label, "Request command");
  assert.equal(r.title, "Ask to run a command as root?");
  assert.equal(r.command, "systemctl restart app");
  assert.equal(r.showWhere, true);

  const s = describeApproval(appr("mcp__fyr__settle_request", { why: "PR merged" }));
  assert.equal(s.label, "Settle thread");
  assert.equal(s.title, "Mark this thread done?");
  assert.equal(s.summary, "PR merged");

  const p = describeApproval(appr("mcp__fyr__pr_create", { title: "Add feature", head: "feat/x", base: "main", draft: true }));
  assert.equal(p.label, "Open pull request");
  assert.equal(p.title, "Open pull request “Add feature”?");
  assert.equal(p.fields.length, 3);
});

test("missing tool falls back safely and humanKey formats snake and camel case", () => {
  const empty = describeApproval({ tool: undefined, input: undefined });
  assert.equal(empty.label, "Allow tool");
  assert.equal(empty.title, "Allow this tool?");
  assert.equal(humanKey("file_path"), "File path");
  assert.equal(humanKey("pullRequestNumber"), "Pull request number");
});

test("V0.tool-approval-judge signals survive into the readable body", () => {
  // The judge's note and the sandbox bypass are the card's, not the tool's, so every tool keeps them.
  const judged = describeApproval({ tool: "Bash", input: { command: "rm -rf build", BypassSandbox: true }, judgeNote: "Deletes outside the sandbox" });
  assert.equal(judged.outsideSandbox, true);
  assert.equal(judged.judgeNote, "Deletes outside the sandbox");
  assert.equal(judged.command, "rm -rf build");

  // CommandLine is the CLI's other spelling of the command (approval.ts), and it is never read twice.
  const argv = describeApproval({ tool: "Bash", input: { CommandLine: "gh pr merge 42 --squash" } });
  assert.equal(argv.command, "gh pr merge 42 --squash");
  assert.equal(argv.subject, "gh pr merge 42 --squash");

  const plain = describeApproval({ tool: "Read", input: { file_path: "/src/a.ts" } });
  assert.equal(plain.outsideSandbox, false);
  assert.equal(plain.judgeNote, "");
});

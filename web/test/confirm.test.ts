// SPEC V0.68: what Fyr's own confirmation says, for a script, a fyr effect and a destructive action.
import { test } from "node:test";
import assert from "node:assert/strict";
import { deleteProjectAsk, offerAsk, removeProviderAsk, shortArg, shortFolder } from "../src/confirm.ts";
import type { Offer } from "../src/types.ts";

const SCRATCH = "/home/owner/.local/share/fyr/scratch/20261115-130828-find-the-acme-claude-long-term-token";
const offer = (p: Partial<Offer>): Offer => ({ id: "a", label: "Set the Acme token", run: "script", argv: ["bash", `${SCRATCH}/set-acme-token.sh`], by: "agent", digest: "d1", ...p });

test("V0.68: a path is shortened: home as ~, inside a scratch folder as …/<folder>/", () => {
  assert.equal(shortArg(`${SCRATCH}/set-acme-token.sh`), "…/20261115-130828-find-the-acme-claude-long-term-token/set-acme-token.sh");
  assert.equal(shortArg("/home/owner/src/fyr/scripts/x.sh"), "~/src/fyr/scripts/x.sh");
  assert.equal(shortArg("/usr/bin/make"), "/usr/bin/make");
  assert.equal(shortArg("deploy/api"), "deploy/api", "a relative argument stays as written");
  assert.equal(shortArg("--context"), "--context");
  assert.equal(shortFolder(SCRATCH), "~/…/scratch/20261115-130828-find-the-acme-claude-long-term-token");
  assert.equal(shortFolder(`${SCRATCH}/sub`), "~/…/scratch/20261115-130828-find-the-acme-claude-long-term-token/sub");
  assert.equal(shortFolder("/home/owner/src/fyr"), "~/src/fyr");
  assert.equal(shortFolder("/srv/app"), "/srv/app");
});

test("V0.68: a script asks with its label, the short command, the folder and Run", () => {
  const a = offerAsk(offer({ cwd: SCRATCH }), "/home/owner/src/fyr")!;
  assert.equal(a.title, "Set the Acme token");
  assert.equal(a.confirm, "Run");
  assert.ok(!a.danger);
  assert.equal(a.command?.short, "bash …/20261115-130828-find-the-acme-claude-long-term-token/set-acme-token.sh");
  assert.equal(a.command?.full, `bash ${SCRATCH}/set-acme-token.sh`);
  assert.equal(a.command?.folder, "~/…/scratch/20261115-130828-find-the-acme-claude-long-term-token");
  assert.equal(a.command?.folderFull, SCRATCH);
});

test("V0.68: arguments keep their shell quoting; the thread's folder is the default; a bound action says so", () => {
  const a = offerAsk(offer({ argv: ["sh", "-c", "echo 'hi there'", "/home/owner/My Files/x"], revision: "3f2a9c1" }), "/home/owner/src/fyr")!;
  assert.equal(a.command?.short, `sh -c 'echo '\\''hi there'\\''' '~/My Files/x'`);
  assert.equal(a.command?.full, `sh -c 'echo '\\''hi there'\\''' '/home/owner/My Files/x'`);
  assert.equal(a.command?.folder, "~/src/fyr");
  assert.equal(a.command?.note, "Only on revision 3f2a9c1");
  assert.equal(offerAsk(offer({}))?.command?.folder, undefined, "no folder known: no folder line");
});

test("V0.68: a fyr action says in words what happens and its button is its label; an agent action asks nothing", () => {
  const m = offerAsk(offer({ label: "Merge", run: "fyr", effect: "pr.merge", argv: undefined }))!;
  assert.equal(m.title, "Merge");
  assert.equal(m.confirm, "Merge");
  assert.equal(m.text, "Fyr merges the pull request on GitHub. The agent gets the result.");
  assert.equal(m.command, undefined);
  const r = offerAsk(offer({ label: "Re-run failed checks", run: "fyr", effect: "pr.rerunFailed", argv: undefined }))!;
  assert.equal(r.text, "Fyr re-runs the failed checks on GitHub. The agent gets the result.");
  assert.equal(offerAsk(offer({ run: "agent", prompt: "Fix it", argv: undefined })), null);
});

test("V0.68, V0.58: Delete project states the consequence, with a red Delete project", () => {
  const a = deleteProjectAsk({ name: "Garden", threads: 12 });
  assert.equal(a.title, "Delete Garden?");
  assert.equal(a.text, "Its 12 threads stay and land in a project whose folder covers them, or in No project. The folder on disk is not touched.");
  assert.equal(a.confirm, "Delete project");
  assert.equal(a.danger, true);
  assert.match(deleteProjectAsk({ name: "A", threads: 1 }).text!, /^Its 1 thread stays and lands/);
  assert.match(deleteProjectAsk({ name: "A" }).text!, /^It has no threads\./);
});

test("V0.68: Remove provider is destructive too", () => {
  const a = removeProviderAsk({ name: "Codex" });
  assert.equal(a.title, "Remove Codex?");
  assert.equal(a.text, "Its stored key is deleted too.");
  assert.equal(a.confirm, "Remove provider");
  assert.equal(a.danger, true);
});

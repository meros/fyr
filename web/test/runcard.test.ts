// SPEC V0.149: the Run card: a command the agent wants the owner to run, wrapped, with the host, the folder and why.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cardLook, cardsKey, copyText, dismissArgs, foldOutput, placeCards, pressRunCard, requests, rerunAsk, shortStore, splitProse, stopArgs } from "../src/runcard.ts";
import { actionEventLine } from "../src/offers.ts";
import { buildConversation, roles, type Item } from "../src/conversation.ts";
import type { FyrEvent, Offer, OfferRun } from "../src/types.ts";

const at = "2026-11-17T09:00:00Z";
const req = (p: Partial<Offer>): Offer => ({
  id: "run-1",
  on: "thread",
  label: "Run",
  run: "script",
  argv: ["bash", "-euo", "pipefail", "-c", "udisksctl mount -b /dev/sdc1"],
  cwd: "/home/owner/src/fyr",
  request: true,
  command: "udisksctl mount -b /dev/sdc1",
  why: "Mount the SD card",
  by: "agent",
  digest: "d1",
  ...p,
});
const run = (p: Partial<OfferRun>): OfferRun => ({ action: "run-1", label: "Run", by: "owner", at, ok: false, ...p });

test("V0.149 (2): a Nix store path is shortened in the middle; everything else stays", () => {
  assert.equal(shortStore("sudo /nix/store/prc4lv5v5kppd7rgz3h2fh9wq3n4zj9y-mint-0.1/bin/mint --dev /dev/sdc1"), "sudo /nix/store/prc4…-mint-0.1/bin/mint --dev /dev/sdc1");
  assert.equal(shortStore("ls /nix/store"), "ls /nix/store");
  assert.equal(shortStore("udisksctl mount -b /dev/sdc1"), "udisksctl mount -b /dev/sdc1");
});

test("V0.149 (2): the thread's run requests, in the order the agent asked", () => {
  const plain: Offer = { id: "apply", label: "Apply", run: "script", argv: ["make"], by: "agent", digest: "x" };
  assert.deepEqual(requests({ offers: [plain, req({}), req({ id: "run-2" })] }).map((o) => o.id), ["run-1", "run-2"]);
  assert.deepEqual(requests({}), []);
});

test("V0.149 (2), (3): a fresh request: the host, why, the folder and Run", () => {
  const l = cardLook(req({}), [], { host: "lab" });
  assert.equal(l.title, "Run on lab");
  assert.equal(l.why, "Mount the SD card");
  assert.equal(l.folder, "~/src/fyr");
  assert.equal(l.run, "Run");
  assert.equal(l.runOff, null);
  assert.equal(l.close, "Not now");
  assert.equal(l.state, "fresh");
  assert.equal(cardLook(req({}), [], {}).title, "Run");
});

test("V0.149 (4): root: Run as root, the polkit line, and off with the reason when no agent can ask", () => {
  const ok = cardLook(req({ root: true }), [], { host: "desk" });
  assert.equal(ok.run, "Run as root");
  assert.equal(ok.rootLine, "The password prompt shows on desk's screen.");
  assert.equal(ok.runOff, null);
  const no = cardLook(req({ root: true }), [], { host: "lab", rootPrompt: "no polkit authentication agent runs in your session" });
  assert.ok(no.runOff);
  assert.equal(no.rootLine, "No password prompt can show on lab: no polkit authentication agent runs in your session. Copy the command and run it in a terminal there.");
  assert.equal(no.folder, "~/src/fyr · as root");
});

test("V0.149 (3): running shows Stop and the streamed output; after it the exit code, Run again and Close", () => {
  const r = cardLook(req({}), [run({ running: true, output: "mounting…\n" })], {});
  assert.equal(r.state, "running");
  assert.equal(r.output, "mounting…\n");
  assert.ok(r.runOff);
  const done = cardLook(req({}), [run({ ok: false, exit: 2, output: "error: busy", endedAt: at })], {});
  assert.equal(done.state, "failed");
  assert.equal(done.result, "Failed (exit 2)");
  assert.equal(done.run, "Run again");
  assert.equal(done.close, "Close");
  assert.equal(cardLook(req({}), [run({ ok: true, endedAt: at })], {}).result, "Succeeded (exit 0)");
});

test("V0.149 (3): Copy takes the full command; a root request with sudo", () => {
  assert.equal(copyText(req({})), "udisksctl mount -b /dev/sdc1");
  assert.equal(copyText(req({ root: true })), "sudo udisksctl mount -b /dev/sdc1");
  assert.equal(copyText(req({ root: true, command: "cd /etc/nixos\nnixos-rebuild switch" })), "sudo bash -euo pipefail -c 'cd /etc/nixos\nnixos-rebuild switch'");
});

test("V0.149 (3): the output folds after 12 lines", () => {
  const long = Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join("\n");
  const f = foldOutput(long);
  assert.equal(f.hidden, 18);
  assert.ok(f.text.startsWith("line 19"));
  assert.equal(foldOutput("a\nb").hidden, 0);
});

test("V0.149: Stop and Not now name the request", () => {
  assert.deepEqual(stopArgs(req({})), { on: "thread", action: "run-1" });
  assert.deepEqual(dismissArgs(req({})), { on: "thread", action: "run-1" });
});

test("V0.149 (8): a `! cmd` block and a shell block after \"run this\" are commands; other code is not", () => {
  const msg = "The minting needs a device:\n\n```\n! sudo /nix/store/prc4lv5v5kppd7-x/bin/mint\n! udisksctl mount -b /dev/sdc1\n```\n\nThe minting runs in a bwrap sandbox:";
  const parts = splitProse(msg);
  assert.deepEqual(parts.map((p) => p.kind), ["md", "cmd", "md"]);
  assert.equal(parts[1].text, "sudo /nix/store/prc4lv5v5kppd7-x/bin/mint\nudisksctl mount -b /dev/sdc1");
  assert.ok(parts[2].text.includes("bwrap"));
  assert.deepEqual(splitProse("Please run this:\n\n```bash\nnix build .#x\n```").map((p) => p.kind), ["md", "cmd"]);
  assert.deepEqual(splitProse("Here is the code:\n\n```bash\nnix build .#x\n```").map((p) => p.kind), ["md"]);
  assert.deepEqual(splitProse("Done."), [{ kind: "md", text: "Done." }]);
});

test("V0.149 (6): the conversation lines of a request: the press, its result, Not now", () => {
  assert.equal(actionEventLine({ request: true, why: "Mount the SD card", root: true, label: "Run as root" }).line, "You ran “Mount the SD card” as root");
  assert.equal(actionEventLine({ request: true, why: "Mount the SD card", ended: true, ok: false, exit: 2 }).line, "“Mount the SD card” failed (exit 2)");
  assert.equal(actionEventLine({ request: true, why: "Mount the SD card", dismissed: true }).line, "You chose Not now for “Mount the SD card”");
  assert.equal(actionEventLine({ request: true, why: "Mount the SD card", dismissed: true, ran: true }).line, "You closed “Mount the SD card”");
});

const asked = (key: string, action: string): Item => ({ kind: "runcard", key, action });
const said = (key: string): Item => ({ kind: "assistant", key, text: key });

test("V0.run-cards-in-conversation: a card stands where it was asked; asked again, at the newest place", () => {
  const p = placeCards([asked("a", "run-1"), said("m"), asked("b", "run-2"), asked("c", "run-1")], ["run-1", "run-2"]);
  assert.deepEqual(p.items.map((i) => i.key), ["m", "b", "c"]);
  assert.deepEqual(p.loose, []);
});

test("V0.run-cards-in-conversation: a closed request has no place; one the log does not show stands at the end", () => {
  const p = placeCards([asked("a", "closed"), said("m")], ["old"]);
  assert.deepEqual(p.items.map((i) => i.key), ["m"]);
  assert.deepEqual(p.loose, ["old"]);
});

test("V0.run-cards-in-conversation: the conversation asks a run.requested for a card, and never folds it", () => {
  const ev = (seq: number, type: string, data: unknown) => ({ seq, at: "2026-11-18T10:00:00Z", thread: "t", type, data }) as unknown as FyrEvent;
  const items = buildConversation([ev(1, "run.started", {}), ev(2, "run.requested", { on: "thread", action: "run-1" })]);
  const card = items.find((i) => i.kind === "runcard");
  assert.equal(card?.kind === "runcard" && card.action, "run-1");
  assert.equal(roles(items)[items.indexOf(card!)], "outcome");
});

test("V0.run-cards-in-conversation: the view follows a card whose output grows", () => {
  const o = { offers: [{ id: "run-1", request: true } as Offer] };
  const a = cardsKey({ ...o, runs: [{ action: "run-1", label: "Run", by: "owner", at: "", running: true, ok: false, output: "a" }] });
  const b = cardsKey({ ...o, runs: [{ action: "run-1", label: "Run", by: "owner", at: "", running: true, ok: false, output: "ab" }] });
  assert.notEqual(a, b);
});

test("V0.run-card-rerun: a succeeded Run card asks before running again; fresh and failed run directly", async () => {
  const o = req({ root: true });
  assert.equal(rerunAsk(o, "fresh", { host: "desk" }), null);
  assert.equal(rerunAsk(o, "running", { host: "desk" }), null);
  assert.equal(rerunAsk(o, "failed", { host: "desk" }), null);
  const a = rerunAsk(o, "ok", { host: "desk" })!;
  assert.equal(a.title, "Run again?");
  assert.equal(a.text, "“Mount the SD card” already succeeded on desk.");
  assert.equal(a.confirm, "Run again as root");
  assert.equal(a.command?.full, "udisksctl mount -b /dev/sdc1");
  assert.equal(a.command?.folder, "~/src/fyr");
  assert.equal(a.command?.note, "Runs as root");
  assert.equal(rerunAsk(req({}), "ok")?.confirm, "Run again");
  assert.equal(rerunAsk(req({}), "ok")?.text, "“Mount the SD card” already succeeded.");

  const sent: unknown[] = [];
  const onRun = (action: string, args?: Record<string, unknown>) => void sent.push([action, args]);
  let asks = 0;
  const no = async () => (asks++, false);
  const yes = async () => (asks++, true);

  assert.equal(await pressRunCard(o, "fresh", { host: "desk" }, no, onRun), true);
  assert.equal(await pressRunCard(o, "failed", { host: "desk" }, no, onRun), true);
  assert.equal(asks, 0);
  assert.equal(sent.length, 2);

  assert.equal(await pressRunCard(o, "ok", { host: "desk" }, no, onRun), false);
  assert.equal(asks, 1);
  assert.equal(sent.length, 2, "cancelling Run again sends nothing");

  assert.equal(await pressRunCard(o, "ok", { host: "desk" }, yes, onRun), true);
  assert.equal(asks, 2);
  assert.equal(sent.length, 3);
  assert.deepEqual(sent[2], ["action.run", { on: "thread", action: "run-1", digest: "d1" }]);
});

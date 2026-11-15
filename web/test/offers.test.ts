import { test } from "node:test";
import assert from "node:assert/strict";
import { actionEventLine, settleLine, changeChip, changeEventLine, offerDetail, phaseText, pressArgs, rail, shownOffers, unavailable } from "../src/offers.ts";
import type { Change, Offer } from "../src/types.ts";

const at = "2026-11-15T09:00:00Z";
const change = (p: Partial<Change>): Change => ({ id: "c_1", key: "k", kind: "infra", title: "api replicas", phase: "working", phaseBy: "agent", createdAt: at, updatedAt: at, ...p });
const offer = (p: Partial<Offer>): Offer => ({ id: "a", label: "Apply", run: "script", argv: ["terraform", "apply", "plan.out"], by: "agent", digest: "d1", ...p });

test("the phase rail and its words; a reported phase says so (V0.59 (8))", () => {
  assert.deepEqual(rail({ phase: "landed" }).steps.map((s) => s.state), ["done", "done", "now", "ahead"]);
  assert.equal(rail({ phase: "reverted" }).end, "reverted");
  assert.equal(phaseText({ phase: "landed", phaseBy: "agent", label: "applied" }), "landed (reported) · applied");
  assert.equal(phaseText({ phase: "review", phaseBy: "observed" }), "review");
});

test("actions: phases, a bound revision, a running one (V0.60 (3))", () => {
  const c = change({ phase: "review", revisions: [{ id: "p2", by: "agent", at }] });
  assert.equal(unavailable(offer({ revision: "p1" }), [], c), "The change has a newer revision than p1");
  assert.equal(unavailable(offer({}), [{ action: "a", label: "Apply", by: "owner", at, running: true, ok: false }], c), "It is running");
  assert.equal(unavailable(offer({ revision: "p2" }), [], c), null);
  const shown = shownOffers([offer({ id: "x", when: ["landed"] }), offer({ id: "y" }), offer({ id: "z", primary: true })], c);
  assert.deepEqual(shown.map((o) => o.id), ["z", "y"]);
  assert.deepEqual(pressArgs("c_1", offer({})), { on: "c_1", action: "a", digest: "d1" });
});

test("what a press does is shown before it (V0.60 (7))", () => {
  assert.equal(offerDetail(offer({ argv: ["bash", "-c", "make e2e && echo ok"] }), "/src/app"), "Runs bash -c 'make e2e && echo ok' in /src/app");
  assert.equal(offerDetail(offer({ run: "agent", prompt: "Fix CI", in: "new", argv: undefined })), "Starts a new thread: Fix CI");
  assert.equal(offerDetail(offer({ run: "fyr", effect: "pr.merge", argv: undefined })), "Fyr merges the PR");
});

test("the inbox chip: the most urgent change that is not a PR, only without an open PR (V0.59 (8))", () => {
  const prChange = change({ id: "c_pr", title: "PR", evidence: [{ kind: "pr", url: "u" }], conditions: [{ type: "checks", status: "fail", source: "observed", at }] });
  const failing = change({ id: "c_2", title: "Raise api replicas to four now", conditions: [{ type: "health", status: "fail", source: "agent", at }] });
  const quiet = change({ id: "c_3", title: "Doc", phase: "review" });
  const chip = changeChip({ changes: [prChange, quiet, failing], pr: null });
  assert.equal(chip?.text, "Raise api replicas to f… · health failed +1");
  assert.equal(chip?.cls, "fail");
  assert.equal(changeChip({ changes: [prChange, quiet, failing], pr: null }, true)?.text, "health failed +1");
  assert.equal(changeChip({ changes: [failing], pr: { url: "u", number: 1, state: "OPEN" } as never }), null);
  assert.equal(changeChip({ changes: [change({ phase: "abandoned" })], pr: null }), null);
});

test("conversation lines for change and action events", () => {
  assert.equal(changeEventLine({ op: "status", change: { title: "api", phase: "landed", phaseBy: "agent", label: "applied" } }), "Change “api” · landed (reported) · applied");
  assert.deepEqual(actionEventLine({ label: "Apply", title: "the change “api”" }), { line: "You pressed “Apply” on the change “api”", ok: true });
  assert.deepEqual(actionEventLine({ label: "Roll back", ended: true, ok: false, exit: 3 }), { line: "“Roll back” failed (exit 3)", ok: false });
});

// review 2026-11-15, SPEC V0.59 (7): settle_when on a change shows on the thread's "settles when" chip.
test("settleLine names the change and the phase it waits for", () => {
  assert.equal(settleLine({ changes: [] }), null);
  assert.equal(settleLine({ settleWhenDone: true }), "settles when done");
  assert.equal(settleLine({ settleWhenMerged: true }), "settles when merged");
  const changes = [change({ id: "c_1", key: "replicas", title: "api replicas" })];
  assert.equal(settleLine({ changes, settleWhen: { change: "c_1", phase: "live" } }), "settles when api replicas is live");
  assert.equal(settleLine({ changes, settleWhen: { change: "replicas", phase: "landed" } }), "settles when api replicas is landed", "by key too");
  assert.equal(settleLine({ changes, settleWhen: { change: "c_9", phase: "live" } }), "settles when c_9 is live", "an unknown change by its id");
});

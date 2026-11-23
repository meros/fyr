import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation, pace } from "../src/conversation.ts";
import { LAND, leftAt, newSince, pickStop, stopsOf, type Placed } from "../src/jumps.ts";

let seq = 0;
const ev = (type: string, data: unknown) => ({ seq: ++seq, at: new Date(1_700_000_000_000 + seq * 1000).toISOString(), subject: "t", type, data });
const say = (text: string) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text }] } });
const tool = (id: string) => [
  ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "tool_use", id, name: "Bash", input: { command: "make test" } }] } }),
  ev("msg", { type: "user", parent_tool_use_id: null, message: { content: [{ type: "tool_result", tool_use_id: id, content: "ok" }] } }),
];

/** Three turns of the owner, each with work that folds; one message from another thread; one Run card asked. */
function conversation() {
  return buildConversation([
    ev("user", { text: "first", images: 0 }),
    ev("run.started", {}),
    ...tool("a1"),
    say("read the code"),
    ...tool("a2"),
    ...tool("a3"),
    say("done with one"),
    ev("run.ended", { failure: null, interrupted: false }),
    ev("user", { text: "from another thread", images: 0, from: "t_other" }),
    ev("user", { text: "second", images: 0 }),
    ev("run.started", {}),
    ev("run.requested", { on: "thread", action: "mount" }),
    ev("run.requested", { on: "thread", action: "ran" }),
    say("run these"),
    ev("run.ended", { failure: null, interrupted: false }),
    ev("user", { text: "Pressed", images: 0, from: "view" }),
  ]);
}

test("V0.conversation-jumps: the owner's messages and the waiting Run cards are the stops; folds stay folded", () => {
  const items = conversation();
  const entries = pace(items);
  assert.ok(entries.some((e) => e.kind === "fold"), "the work folds");
  const stops = stopsOf(entries, [], ["loose"], new Set(["mount", "loose"]));
  const text = (k: string) => {
    const it = items.find((i) => i.key === k);
    return it?.kind === "user" ? it.text : it?.kind === "runcard" ? `card ${it.action}` : k;
  };
  // A message from another thread is not the owner's; a card already run ("ran") waits for nobody.
  assert.deepEqual(stops.map((s) => `${s.kind}:${text(s.key)}`), ["own:first", "own:second", "needs:card mount", "own:Pressed", "needs:card-loose"]);
});

test("V0.conversation-jumps: the messages the agent has not read are stops after the rest", () => {
  const items = conversation();
  const pending = buildConversation([ev("user", { id: "m9", text: "later", images: 0 }), ev("message.state", { id: "m9", state: "queued" })], { outbox: ["m9"] });
  const stops = stopsOf(pace(items), pending, [], new Set());
  assert.equal(stops.at(-1)?.key, pending[0].key);
});

// Stops 400 px apart; the view is 600 px tall.
const placed: Placed[] = [
  { key: "o1", kind: "own", top: 0, bottom: 40 },
  { key: "c1", kind: "needs", top: 200, bottom: 320 },
  { key: "o2", kind: "own", top: 400, bottom: 440 },
  { key: "o3", kind: "own", top: 800, bottom: 840 },
  { key: "c2", kind: "needs", top: 1000, bottom: 1100 },
  { key: "o4", kind: "own", top: 1200, bottom: 1240 },
];
const at = (top: number) => ({ top, bottom: top + 600 });

test("V0.conversation-jumps: previous and next own message go by where the view stands", () => {
  assert.equal(pickStop(placed, "own", 1, at(500)), "o3");
  assert.equal(pickStop(placed, "own", -1, at(500)), "o2");
  // A stop a jump landed on (at the landing line) is neither previous nor next.
  assert.equal(pickStop(placed, "own", 1, at(800 - LAND)), "o4");
  assert.equal(pickStop(placed, "own", -1, at(800 - LAND)), "o2");
  // Past the last, before the first: nothing; own messages do not wrap.
  assert.equal(pickStop(placed, "own", 1, at(1300)), null);
  assert.equal(pickStop(placed, "own", -1, at(0)), null);
});

test("V0.conversation-jumps: from the stop a jump landed on, while it is in view, the next jump steps in order", () => {
  // At the end the view cannot bring o3 to the top: o4 below it is still next, then nothing.
  const end = at(700);
  assert.equal(pickStop(placed, "own", 1, end), "o3");
  assert.equal(pickStop(placed, "own", 1, end, "o3"), "o4");
  assert.equal(pickStop(placed, "own", 1, end, "o4"), null);
  assert.equal(pickStop(placed, "own", -1, end, "o4"), "o3");
  // Scrolled away from it, the landed stop no longer counts.
  assert.equal(pickStop(placed, "own", 1, at(0), "o4"), "o2");
});

test("V0.conversation-jumps: the next card that waits goes round to the first", () => {
  assert.equal(pickStop(placed, "needs", 1, at(0), null, true), "c1");
  assert.equal(pickStop(placed, "needs", 1, at(300), null, true), "c2");
  assert.equal(pickStop(placed, "needs", 1, at(1100), null, true), "c1");
  assert.equal(pickStop(placed.filter((s) => s.kind === "own"), "needs", 1, at(0), null, true), null);
});

test("V0.conversation-jumps: the end arrow counts what arrived since the owner left the end", () => {
  const items = conversation();
  const mark = leftAt(items);
  assert.equal(newSince(items, mark), 0);
  seq = 0;
  const before = buildConversation([ev("user", { text: "go", images: 0 }), ev("run.started", {}), say("one")]);
  const m = leftAt(before);
  seq = 0;
  // The same log and then: a tool call (not counted), two messages, a message from another thread, the owner's own.
  const after = buildConversation([ev("user", { text: "go", images: 0 }), ev("run.started", {}), say("one"), ...tool("x"), say("two"), say("three"), ev("run.ended", { failure: null, interrupted: false }), ev("user", { text: "hi", images: 0, from: "t_other" }), ev("user", { text: "mine", images: 0 })]);
  assert.equal(newSince(after, m), 3);
  // Nothing counted when the owner left: everything that counts is new.
  assert.equal(newSince(after, ""), 4);
  // A mark that is gone (another thread's list) counts nothing.
  assert.equal(newSince(after, "nope"), 0);
});

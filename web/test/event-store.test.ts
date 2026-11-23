// V0.event-store: a timeline grows by appending, only the timelines opened last stay in memory, and the conversation is
// built from the new events only.
import { test } from "node:test";
import assert from "node:assert/strict";
import { EVENTS_KEPT, initialStore, liveEvent, mergeEvents, mergeSeq, openTimeline, resumeSeq, setConn } from "../src/store.ts";
import { buildConversation, conversationCache } from "../src/conversation.ts";
import type { FyrEvent } from "../src/types.ts";

const at = (n: number) => new Date(1_700_000_000_000 + n * 1000).toISOString();
const e = (seq: number, subject = "h/a", text = String(seq)): FyrEvent => ({ seq, at: at(seq), subject, type: "user", data: { text } });
const seqs = (l: FyrEvent[] | undefined) => (l ?? []).map((x) => x.seq);

test("events newer than the last append in order and keep every earlier event as it was", () => {
  const cur = [e(1), e(2)];
  const next = mergeSeq(cur, [e(3), e(4)]);
  assert.deepEqual(seqs(next), [1, 2, 3, 4]);
  assert.equal(next[0], cur[0]);
  assert.equal(next[1], cur[1]);
  assert.deepEqual(seqs(cur), [1, 2], "the old array is not changed");
  assert.equal(mergeSeq(cur, []), cur, "nothing new: the same array");
});

test("events out of order or already there are merged by seq; the newer copy of a seq wins", () => {
  assert.deepEqual(seqs(mergeSeq([e(1), e(4)], [e(3), e(2)])), [1, 2, 3, 4]);
  assert.deepEqual(seqs(mergeSeq([e(1), e(2)], [e(5), e(3)])), [1, 2, 3, 5]);
  const again = mergeSeq([e(1), e(2, "h/a", "old")], [e(2, "h/a", "new")]);
  assert.deepEqual(seqs(again), [1, 2]);
  assert.equal(again[1].data?.text, "new");
});

test("mergeEvents replaces a timeline, and a live event goes only to a kept one", () => {
  let s = mergeEvents(initialStore(), "h/a", [e(3), e(1), e(2)], true);
  s = liveEvent(s, e(4));
  s = liveEvent(s, e(2));
  assert.deepEqual(seqs(s.events["h/a"]), [1, 2, 3, 4]);
  assert.equal(liveEvent(s, e(9, "h/b")), s, "a thread not kept takes no live event");
  s = mergeEvents(s, "h/a", [], true);
  assert.deepEqual(s.events["h/a"], []);
});

test("only the open thread and the ones opened just before it stay; a dropped one forgets its reconnect point", () => {
  let s = initialStore();
  const ids = Array.from({ length: EVENTS_KEPT + 2 }, (_, i) => `h/t${i}`);
  for (const id of ids) {
    s = openTimeline(s, id);
    s = mergeEvents(s, id, [e(1, id)], true);
  }
  assert.deepEqual(Object.keys(s.events), ids.slice(-EVENTS_KEPT));
  // A reconnect point is kept for a kept timeline only.
  s = setConn(setConn(s, "live"), "offline");
  assert.equal(resumeSeq(s, ids[ids.length - 1]), 1);
  // Opening a kept one again makes it the newest: the oldest kept is the next to go.
  const again = ids[ids.length - EVENTS_KEPT];
  s = openTimeline(s, again);
  assert.equal(Object.keys(s.events).at(-1), again);
  s = openTimeline(s, "h/new");
  assert.equal(s.events[ids[ids.length - EVENTS_KEPT + 1]], undefined);
  assert.equal(s.resumeFrom[ids[ids.length - EVENTS_KEPT + 1]], undefined);
  assert.ok(s.events[again]);
  assert.equal(Object.keys(s.events).length, EVENTS_KEPT - 1, "the new one is not loaded yet; its place is kept for it");
  // Opening the newest again changes nothing.
  const s2 = openTimeline(s, again);
  assert.equal(openTimeline(s2, again), s2);
});

// A timeline with what the builder keeps between events: a held message read later, a tool call and its result, a run.
function timeline(): FyrEvent[] {
  let n = 0;
  const ev = (type: string, data: Record<string, unknown>): FyrEvent => ({ seq: ++n, at: at(n), subject: "h/a", type, data });
  const msg = (content: unknown[], type = "assistant") => ev("msg", { type, parent_tool_use_id: null, message: { content } });
  return [
    ev("user", { text: "Fix it", id: "m1" }),
    ev("message.state", { id: "m1", state: "queued" }),
    ev("run.started", {}),
    ev("message.state", { id: "m1", state: "read" }),
    msg([{ type: "text", text: "Looking." }]),
    msg([{ type: "tool_use", id: "c1", name: "Bash", input: { command: "make test", description: "Run the tests" } }]),
    ev("user", { text: "Also the docs", id: "m2" }),
    msg([{ type: "tool_result", tool_use_id: "c1", content: "ok" }], "user"),
    msg([{ type: "tool_use", id: "c2", name: "Read", input: { file_path: "/x/a.ts" } }]),
    msg([{ type: "tool_result", tool_use_id: "c2", content: "a", is_error: true }], "user"),
    msg([{ type: "text", text: "Done." }]),
    ev("run.ended", { durationMs: 5000 }),
  ];
}

test("a conversation built event by event is the one built whole, after every event", () => {
  const evs = timeline();
  const build = conversationCache();
  let list: FyrEvent[] = [];
  for (const ev of evs) {
    list = mergeSeq(list, [ev]);
    for (const outbox of [undefined, ["m2"], []]) {
      const got = structuredClone(build(list, { outbox }));
      assert.deepEqual(got, buildConversation(list, { outbox }), `after seq ${ev.seq}, outbox ${outbox}`);
    }
  }
});

test("a timeline that changed before its end, or other options, builds again from the start", () => {
  const evs = timeline();
  const build = conversationCache();
  build(evs.slice(0, 6));
  // An older page merged in front: not an append.
  const paged = mergeSeq(evs.slice(3, 8), evs.slice(0, 3));
  assert.deepEqual(structuredClone(build(paged)), buildConversation(paged));
  assert.deepEqual(structuredClone(build(paged, { details: true })), buildConversation(paged, { details: true }));
  assert.deepEqual(structuredClone(build(evs.slice(0, 4))), buildConversation(evs.slice(0, 4)), "a shorter timeline");
});

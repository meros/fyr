import { test } from "node:test";
import assert from "node:assert/strict";
import { capCalls, VISIBLE_CALLS, type ToolCall } from "../src/conversation.ts";

const call = (id: string, isError = false): ToolCall => ({
  id,
  name: "Read",
  summary: id,
  label: `Read ${id}`,
  description: "",
  input: {},
  output: "",
  isError,
  done: true,
  startedAt: 0,
  durationMs: 0,
  subMessages: 0,
  sub: [],
});
const ids = (rows: ReturnType<typeof capCalls>) => rows.map((r) => (r.kind === "more" ? `+${r.count}` : r.call.id));

// SPEC 0.5 V0.26: an opened tool group shows at most the last 3 calls and "+N more tools"
test("a long tool group shows the last 3 calls behind a +N more row", () => {
  assert.equal(VISIBLE_CALLS, 3);
  const calls = ["a", "b", "c", "d", "e", "f"].map((x) => call(x));
  assert.deepEqual(ids(capCalls(calls, false)), ["+3", "d", "e", "f"]);
  const more = capCalls(calls, false)[0];
  assert.equal(more.kind === "more" && more.label, "+3 more tools");
});

// SPEC 0.5 V0.26: expanding shows every call; short groups have no more row
test("expanded or short groups show every call", () => {
  const calls = ["a", "b", "c", "d"].map((x) => call(x));
  assert.deepEqual(ids(capCalls(calls, true)), ["a", "b", "c", "d"]);
  assert.deepEqual(ids(capCalls(calls.slice(0, 3), false)), ["a", "b", "c"]);
  assert.deepEqual(ids(capCalls([], false)), []);
});

// SPEC 0.5 V0.26: a failed call is never hidden behind the more row
test("failed calls before the last 3 stay visible, in order", () => {
  const calls = [call("a"), call("b", true), call("c"), call("d"), call("e"), call("f"), call("g")];
  assert.deepEqual(ids(capCalls(calls, false)), ["+3", "b", "e", "f", "g"]);
  const one = capCalls([call("a"), call("b"), call("c"), call("d")], false)[0];
  assert.equal(one.kind === "more" && one.label, "+1 more tool");
  // Nothing left to hide: no more row.
  assert.deepEqual(ids(capCalls([call("a", true), call("b"), call("c"), call("d")], false)), ["a", "b", "c", "d"]);
});

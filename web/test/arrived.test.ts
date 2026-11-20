import { test } from "node:test";
import assert from "node:assert/strict";
import { arrivalOnOpen, arrivalText, arrive } from "../src/arrived.ts";

test("an arrival counts the threads still needing the owner, past the one answered and the one reached", () => {
  const a = arrive({ id: "a", title: "Fix login" }, "b", ["a", "b", "c", "d"]);
  assert.deepEqual(a, { from: "a", fromTitle: "Fix login", to: "b", left: 2 });
  assert.equal(arrivalText(a).more, "moved on to this one · 2 more need you");
  assert.equal(arrivalText({ ...a, left: 1 }).more, "moved on to this one · 1 more needs you");
  assert.equal(arrivalText({ ...a, left: 0 }).more, "moved on to the last one that needs you");
  assert.equal(arrive({ id: "a", title: "" }, "b", []).fromTitle, "Untitled");
});

test("the bar stays on the thread reached until the owner leaves it, and never comes back", () => {
  let a = arrivalOnOpen(arrive({ id: "a", title: "A" }, "b", ["a", "b"]), "a"); // a front still shows a for a frame
  assert.ok(a && !a.reached);
  a = arrivalOnOpen(a, "b");
  assert.ok(a?.reached);
  assert.equal(arrivalOnOpen(a, "b"), a);
  assert.equal(arrivalOnOpen(a, "a"), null); // back to the one answered: done
  assert.equal(arrivalOnOpen(a, "c"), null);
  assert.equal(arrivalOnOpen(a, null), null);
  assert.equal(arrivalOnOpen(arrive({ id: "a", title: "A" }, "b", []), "c"), null);
});

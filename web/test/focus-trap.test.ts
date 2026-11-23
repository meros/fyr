// V0.composer-focus-reach (2): every modal panel keeps Tab inside it; nested panels: only the newest traps.
import { test } from "node:test";
import assert from "node:assert/strict";
import { dropTrap, isTopTrap, pushTrap, tabTarget } from "../src/focus-trap.ts";

test("V0.composer-focus-reach (2): Tab and ⇧Tab wrap at both ends of the panel's controls", () => {
  const list = ["close", "field", "ok"];
  assert.equal(tabTarget(list, "close", false), "field");
  assert.equal(tabTarget(list, "ok", false), "close", "Tab from the last control goes to the first, not into the page behind");
  assert.equal(tabTarget(list, "close", true), "ok", "⇧Tab from the first goes to the last");
  assert.equal(tabTarget(list, "field", true), "close");
});

test("V0.composer-focus-reach (2): from outside the panel (the panel itself, the page) Tab enters at the first control, ⇧Tab at the last", () => {
  assert.equal(tabTarget(["a", "b"], null, false), "a");
  assert.equal(tabTarget(["a", "b"], null, true), "b");
  assert.equal(tabTarget(["a", "b"], "elsewhere", false), "a");
  assert.equal(tabTarget([], null, false), null, "a panel with nothing to focus keeps it");
});

test("V0.composer-focus-reach (2): a confirmation over a dialog traps; when it closes the dialog traps again", () => {
  const dialog = {};
  const confirm = {};
  pushTrap(dialog);
  assert.ok(isTopTrap(dialog));
  pushTrap(confirm);
  assert.ok(isTopTrap(confirm) && !isTopTrap(dialog));
  dropTrap(confirm);
  assert.ok(isTopTrap(dialog));
  dropTrap(dialog);
  assert.ok(!isTopTrap(dialog));
});

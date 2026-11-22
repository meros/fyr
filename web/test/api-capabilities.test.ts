// The shell asks the Api what it supports (cache, pushes) instead of asking whether it is the mock.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { createMockApi } from "../src/mock-api.ts";

test("a mock scenario keeps nothing on the device and sends no push settings", () => {
  const api = createMockApi("default");
  assert.equal(api.cache, false);
  assert.equal(api.pushes, false);
});

test("the shell never asks api.mock; the real Api supports both", () => {
  const src = (f: string) => readFileSync(join(import.meta.dirname, "..", "src", f), "utf8");
  for (const f of ["app.tsx", "app-connection.tsx", "app-state.tsx", "app-hosts.tsx"]) assert.doesNotMatch(src(f), /\bapi\.mock\b/, f);
  assert.match(src("api.ts"), /mock: false,\n\s+cache: true,\n\s+pushes: true,/);
});

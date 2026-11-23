// The shell asks the Api what it supports (cache, pushes) instead of asking whether it is the mock.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { createApi } from "../src/api.ts";
import { createMockApi } from "../src/mock-api.ts";

test("a mock scenario keeps nothing on the device and sends no push settings", () => {
  const api = createMockApi("default");
  assert.equal(api.cache, false);
  assert.equal(api.pushes, false);
});

test("the shell never asks api.mock", () => {
  const src = (f: string) => readFileSync(join(import.meta.dirname, "..", "src", f), "utf8");
  for (const f of ["app.tsx", "app-connection.tsx", "app-state.tsx", "app-hosts.tsx"]) assert.doesNotMatch(src(f), /\bapi\.mock\b/, f);
});

test("the real Api keeps the cache and sends push settings; a mock= of nothing is the real one", async () => {
  for (const search of ["", "?mock=0", "?mock="]) {
    const api = await createApi(search);
    assert.deepEqual({ mock: api.mock, cache: api.cache, pushes: api.pushes }, { mock: false, cache: true, pushes: true }, search);
  }
  assert.equal((await createApi("?mock=default")).mock, true);
});

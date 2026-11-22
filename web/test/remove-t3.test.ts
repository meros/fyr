// SPEC V0.remove-t3 (#68): T3 adoption is removed from both fronts and the API client.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

test("V0.remove-t3: desktop inbox and phone menu have no Adopt from T3 entry", () => {
  const srcDir = new URL("../src/", import.meta.url).pathname;
  for (const file of ["inbox.tsx", "phone.tsx"]) {
    const body = readFileSync(join(srcDir, file), "utf8");
    assert.doesNotMatch(body, /Adopt from T3/i, `${file} must not expose Adopt from T3`);
  }
  const bad: string[] = [];
  for (const f of readdirSync(srcDir)) {
    if (!/\.tsx?$/.test(f)) continue;
    const body = readFileSync(join(srcDir, f), "utf8");
    if (/Adopt from T3|AdoptDialog|\/t3\/candidates|t3\.adopt|T3Candidate|t3Candidates/.test(body)) {
      bad.push(f);
    }
  }
  assert.deepEqual(bad, [], "no client module may reference T3 adoption");
});

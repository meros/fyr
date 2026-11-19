// Decision 0031 2.6, V0.forget-front: the client does not know which front it is connected through, except in the
// places that need it: Settings → This device (settings-ui.tsx, which app.tsx hands the name), the update banner
// (deploy-ui.tsx, with hostfeed.ts, which picks the deploy of the front's own host) and the build check (buildid.ts).
// The modules that read and keep the front's state (api.ts, hostfeed.ts, store.ts, decode.ts, cache) carry it. A guard
// over the sources, so a new label or fallback that asks "which front?" fails here first.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const SRC = join(import.meta.dirname, "..", "src");
const files = readdirSync(SRC).filter((f) => /\.tsx?$/.test(f) && !f.startsWith("mock"));
const read = (f: string) => readFileSync(join(SRC, f), "utf8").replace(/\/\*[\s\S]*?\*\/|\/\/.*$/gm, "");

test("only the modules of 2.6 read the front's identity", () => {
  const ALLOWED = new Set(["app.tsx", "settings-ui.tsx", "deploy-ui.tsx", "buildid.ts", "hostfeed.ts", "store.ts", "api.ts", "decode.ts"]);
  const pat = /\b(?:store|fullStore|p\.store)\.(?:host|hostId|machine)\b|\bfront\.front\b/;
  const bad = files.filter((f) => !ALLOWED.has(f) && pat.test(read(f)));
  assert.deepEqual(bad, []);
  // app.tsx reads it once: to hand the name to Settings → This device.
  assert.deepEqual(read("app.tsx").match(/\b(?:store|fullStore)\.(?:host|hostId|machine)\b/g) ?? [], ["store.host"]);
});

test("no label says which host is this install, and the self fallbacks are gone", () => {
  for (const f of files) {
    const s = read(f);
    assert.ok(!/\(this install\)/i.test(s), `${f} labels a host "(this install)"`);
    assert.ok(!/\bhostLabel\b|\bhostRows\b/.test(s), `${f} keeps a self fallback`);
  }
  for (const f of ["host-settings.ts", "hosts.ts", "filters.ts", "host-mark.ts", "host-pick-ui.tsx"]) assert.ok(!/\bself\b/.test(read(f)), `${f} still takes the front's own host`);
});

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
  const pat = /\b(?:store|fullStore|rawStore|p\.store)\.(?:host|hostId|machine|front)\b|\bfront\.front\b/;
  const bad = files.filter((f) => !ALLOWED.has(f) && pat.test(read(f)));
  assert.deepEqual(bad, []);
  // app.tsx reads it once: to hand the name to Settings → This device. The banner reads it through store.ts
  // (bannerDeploy). Slice 2b: the store has no host, hostId or machine of the front's at all.
  assert.deepEqual(read("app.tsx").match(/\b(?:store|fullStore|rawStore)\.(?:host|hostId|machine|front)\b/g) ?? [], ["store.front"]);
  assert.ok(!/^\s*(?:host|hostId|machine):/m.test(read("store.ts").split("export interface Store {")[1].split("\n}")[0]), "the Store keeps no host of the front's");
});

// Decision 0031 slice 2b (#453): no call can leave its host out or default to one. The type does most of it (hostid.ts:
// a string is not a HostId); this guard holds the rest: the patterns that made a call go to "" or to the first host.
test("slice 2b: no empty host id, no host by name, no first-host fallback", () => {
  for (const f of files) {
    const s = read(f);
    assert.ok(!/\bhostId\s*\?\?\s*""/.test(s), `${f} sends a host id that may be "" (\`hostId ?? ""\`)`);
    assert.ok(!/\bhostIdByName\b|\bhostForProject\b|\bprofilesOfHost\b|\bhostOf\(/.test(s), `${f} keeps a host-by-name or first-host helper`);
    assert.ok(!/hosts?\??\.?\[0\]\??\.(?:id|name)\b/.test(s) || f === "host-settings.ts", `${f} takes the first host's id or name`);
  }
  // Every Api method that reaches a host takes a HostId, never a string.
  const api = read("api.ts").split("export interface Api {")[1].split("\n}")[0];
  assert.ok(!/\(host\??: string/.test(api), "an Api method takes its host as a string");
  assert.ok(!/\(host\??: string/.test(read("urls.ts")), "the URL function takes its host as a string");
});

test("no label says which host is this install, and the self fallbacks are gone", () => {
  for (const f of files) {
    const s = read(f);
    assert.ok(!/\(this install\)/i.test(s), `${f} labels a host "(this install)"`);
    assert.ok(!/\bhostLabel\b|\bhostRows\b/.test(s), `${f} keeps a self fallback`);
  }
  for (const f of ["host-settings.ts", "hosts.ts", "filters.ts", "host-mark.ts", "host-pick-ui.tsx"]) assert.ok(!/\bself\b/.test(read(f)), `${f} still takes the front's own host`);
});

// #42: the mock fixtures are not in the production bundle. The build splits them into a chunk that only
// import() reaches (mock mode), and the service worker precaches every chunk.
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const web = new URL("..", import.meta.url).pathname;

test("app.js and what it imports statically carry no fixture text; the mock chunk does; sw.js precaches every chunk", (t) => {
  const out = mkdtempSync(join(tmpdir(), "fyr-bundle-"));
  t.after(() => rmSync(out, { recursive: true, force: true }));
  execFileSync(process.execPath, [join(web, "scripts/build.mjs"), "--out", out], { stdio: "pipe" });

  // The files the browser loads at start: app.js and its static imports, transitively (not import("…")).
  const loaded = new Set<string>();
  const walk = (file: string) => {
    if (loaded.has(file)) return;
    loaded.add(file);
    const src = readFileSync(join(out, file), "utf8");
    for (const m of src.matchAll(/(?:\bfrom\s*|\bimport\s*)"\.\/([^"]+\.js)"/g)) walk(m[1]);
  };
  walk("app.js");

  const scripts = readdirSync(out).filter((f) => f.endsWith(".js") && f !== "sw.js");
  const mock = scripts.filter((f) => readFileSync(join(out, f), "utf8").includes("Burst message"));
  assert.equal(mock.length, 1, `one chunk has the fixtures: ${mock}`);
  assert.ok(!loaded.has(mock[0]), `${mock[0]} is loaded at start: ${[...loaded]}`);
  for (const f of loaded) assert.ok(!readFileSync(join(out, f), "utf8").includes("Burst message"), `${f} has fixture text`);

  const sw = readFileSync(join(out, "sw.js"), "utf8");
  for (const f of scripts) assert.ok(sw.includes(`"/${f}"`), `sw.js does not precache /${f}`);
});

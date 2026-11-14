import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { PALETTE, PALETTE_NAMES, colorName, filterByProject, lineName, matchProjects, messageFrom, monogram, paletteColor, pickerOrder, projectDefaults, projectOf, projectSubtitle, resolveIcon, sourceFromInput, startedBy } from "../src/projects.ts";
import type { Project } from "../src/types.ts";
import { thread } from "./helpers.ts";

const hue = (hex: string) => {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
  const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
  if (!d) return -1;
  const h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return (h * 60 + 360) % 360;
};

test("ten hues, light and dark, never amber", () => {
  assert.equal(PALETTE_NAMES.length, 10);
  assert.equal(paletteColor("teal", "x", false), "#1d7a74");
  assert.equal(paletteColor("teal", "x", true), "#4cc1b6");
  assert.ok(!("amber" in PALETTE) && !("orange" in PALETTE) && !("yellow" in PALETTE));
  assert.notEqual(colorName("amber", "fyr"), "amber", "an unknown colour falls back to a palette hue");
  assert.equal(colorName("amber", "fyr"), colorName(undefined, "fyr"), "picked from the name, stable");
  const beam = hue("#e8960f");
  for (const [n, c] of Object.entries(PALETTE)) {
    for (const v of [c.light, c.dark]) {
      const h = hue(v);
      if (h < 0 || n === "slate") continue;
      const dist = Math.min(Math.abs(h - beam), 360 - Math.abs(h - beam));
      assert.ok(dist > 12, `${n} ${v} is too close to the beam amber (${dist.toFixed(0)}°)`);
    }
  }
});

test("the palette tokens in app.css match the palette", () => {
  const css = readFileSync(new URL("../src/app.css", import.meta.url), "utf8");
  for (const [n, c] of Object.entries(PALETTE)) {
    assert.ok(css.includes(`--p-${n}:${c.light};`), `light --p-${n}`);
    assert.ok(css.includes(`--p-${n}:${c.dark};`), `dark --p-${n}`);
  }
});

test("monogram: 1 or 2 letters", () => {
  assert.equal(monogram("acme/web"), "AW");
  assert.equal(monogram("fyr"), "F");
  assert.equal(monogram("Bokföring åt Åsa"), "BÅ");
  assert.equal(monogram("öl-listan"), "ÖL");
  assert.equal(monogram("—"), "?");
});

test("icons: emoji, curated line icons (T3 names too), else the monogram", () => {
  assert.deepEqual(resolveIcon({ kind: "emoji", value: "🏡 hem" }, "Garden"), { kind: "emoji", value: "🏡" });
  // The host omits an empty value (Go omitempty): {"kind":"monogram"} must not crash.
  for (const kind of ["monogram", "emoji", "line"] as const) assert.deepEqual(resolveIcon(JSON.parse(`{"kind":"${kind}"}`), "acme"), { kind: "monogram", value: "A" });
  assert.deepEqual(resolveIcon({ kind: "line", value: "FolderGit2" }, "x"), { kind: "line", value: "folder-git-2" });
  assert.deepEqual(resolveIcon({ kind: "line", value: "Home" }, "x"), { kind: "line", value: "house" });
  assert.deepEqual(resolveIcon({ kind: "line", value: "no-such-icon" }, "acme/infra"), { kind: "monogram", value: "NI" });
  assert.deepEqual(resolveIcon(undefined, "fyr"), { kind: "monogram", value: "F" });
  assert.deepEqual(resolveIcon({ kind: "emoji", value: "  " }, "fyr"), { kind: "monogram", value: "F" });
  assert.equal(lineName("bar_chart"), "chart-column");
});

const projects: Project[] = [
  { id: "p_web", name: "acme/web", cwd: "/home/o/src/web", lastUsed: "2026-11-14T10:00:00Z", profile: "claude", model: "sonnet" },
  { id: "p_fyr", name: "fyr", sources: [{ kind: "repo", url: "github.com/meros/fyr" }], lastUsed: "2026-11-10T10:00:00Z" },
  { id: "p_home", name: "Garden", sources: [], lastUsed: "2026-11-13T10:00:00Z" },
  { id: "p_old", name: "archive" },
];

test("picker order, subtitles and search", () => {
  assert.deepEqual(pickerOrder(projects).map((p) => p.id), ["p_web", "p_home", "p_fyr", "p_old"]);
  assert.equal(projectSubtitle(projects[0]), "~/src/web");
  assert.equal(projectSubtitle({ name: "x", cwd: "/home/owner/src/x" }), "~/src/x");
  assert.equal(projectSubtitle(projects[1]), "github.com/meros/fyr");
  assert.equal(projectSubtitle(projects[2]), "No source: a label");
  assert.deepEqual(matchProjects(projects, "meros").map((p) => p.id), ["p_fyr"]);
  assert.deepEqual(matchProjects(projects, "label").map((p) => p.id), ["p_home", "p_old"]);
});

test("project filter and a thread's project", () => {
  const ts = {
    a: thread("a", "seen", "seen", { project: "acme/web", projectId: "p_web" }),
    b: thread("b", "seen", "seen", { project: "Garden", projectId: "p_home" }),
    c: thread("c", "seen", "seen", { project: "acme/web" }),
  };
  assert.deepEqual(Object.keys(filterByProject(ts, "p_web", projects)).sort(), ["a", "c"], "by id, else by name");
  assert.equal(filterByProject(ts, null, projects), ts);
  assert.equal(projectOf(ts.b, projects)?.name, "Garden");
});

test("picking a project fills its folder and default profile and model", () => {
  const profiles = [{ id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" }];
  assert.deepEqual(projectDefaults(projects[0], profiles), { cwd: "/home/o/src/web", profile: "claude", model: "sonnet" });
  assert.deepEqual(projectDefaults(projects[2], profiles), { cwd: undefined, profile: undefined, model: undefined });
});

test("started-by and cross-thread messages", () => {
  const parent = thread("p", "working", "working", { title: "Map perf", project: "acme/web", projectId: "p_web" });
  const ts = { p: parent };
  assert.deepEqual(startedBy({ parent: "p" }, ts), { id: "p", title: "Map perf" });
  assert.deepEqual(startedBy({ parent: "gone" }, ts), { id: "gone", title: "another thread" });
  assert.equal(startedBy({ parent: null }, ts), null);
  assert.equal(messageFrom("p", ts, projects)?.project?.id, "p_web");
  assert.equal(messageFrom(undefined, ts, projects), null);
});

test("one input adds a folder or a repo; the host fills in its own name", () => {
  assert.deepEqual(sourceFromInput(" /home/me/src/fyr/ "), { kind: "folder", path: "/home/me/src/fyr/" });
  assert.deepEqual(sourceFromInput("~/src/fyr"), { kind: "folder", path: "~/src/fyr" });
  assert.deepEqual(sourceFromInput("github.com/me/fyr"), { kind: "repo", url: "github.com/me/fyr" });
  assert.deepEqual(sourceFromInput("git@github.com:me/fyr.git"), { kind: "repo", url: "git@github.com:me/fyr.git" });
  assert.equal(sourceFromInput("   "), null);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { PALETTE, PALETTE_NAMES, colorName, filterByProject, lineName, matchProjects, messageFrom, monogram, paletteColor, pickerOrder, projectDefaults, projectName, projectOf, projectSubtitle, resolveIcon, startedBy } from "../src/projects.ts";
import type { Project } from "../src/types.ts";
import { thread } from "./helpers.ts";

test("a thread without a project shows No project (a thread in the home folder, V0.23)", () => {
  assert.equal(projectName({ project: "", projectId: undefined }, []), "No project");
  const app = { id: "p1", name: "App", color: "teal", icon: { kind: "monogram" }, threads: 1, lastUsed: "", folder: "" } as unknown as Project;
  assert.equal(projectName({ project: "app", projectId: "p1" }, [app]), "App");
});

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
  { id: "p_web", name: "acme/web", folder: "/home/o/src/web", lastUsed: "2026-11-14T10:00:00Z", profile: "claude", model: "sonnet" },
  { id: "p_fyr", name: "fyr", folder: "/home/owner/git/fyr", lastUsed: "2026-11-10T10:00:00Z" },
  { id: "p_home", name: "Garden", lastUsed: "2026-11-13T10:00:00Z" },
  { id: "p_old", name: "archive" },
];

test("picker order, subtitles and search", () => {
  assert.deepEqual(pickerOrder(projects).map((p) => p.id), ["p_web", "p_home", "p_fyr", "p_old"]);
  assert.equal(projectSubtitle(projects[0]), "~/src/web");
  assert.equal(projectSubtitle({ name: "x", folder: "/home/owner/src/x" }), "~/src/x");
  assert.equal(projectSubtitle(projects[1]), "~/git/fyr");
  assert.equal(projectSubtitle(projects[2]), "No folder: filed by hand");
  assert.deepEqual(matchProjects(projects, "git/fyr").map((p) => p.id), ["p_fyr"]);
  assert.deepEqual(matchProjects(projects, "by hand").map((p) => p.id), ["p_home", "p_old"]);
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

test("SPEC 0.5 V0.55: New project from this thread starts from the host's folder, else a name only", async () => {
  const { newProjectDraft } = await import("../src/projects.ts");
  assert.deepEqual(newProjectDraft({ newProject: { name: "Garden", folder: "/home/o/Garden" } }), { name: "Garden", folder: "/home/o/Garden" });
  // A scratch folder, the home folder or a folder a project has: just a name field.
  assert.deepEqual(newProjectDraft({}), { name: "", folder: "" });
});

test("SPEC 0.5 V0.55: one folder, its words, the scratch default", async () => {
  const projects = await import("../src/projects.ts");
  const { newThreadFolder, NO_FOLDER_LINE, FOLDER_LINE, localProject } = projects;
  assert.equal(FOLDER_LINE, "Threads started in this folder or below land here.");
  assert.deepEqual(newThreadFolder("", undefined), { label: "A fresh scratch folder", scratch: true });
  assert.deepEqual(newThreadFolder("", { folder: "/home/owner/src/a" }), { cwd: "/home/owner/src/a", label: "~/src/a", scratch: false });
  assert.deepEqual(newThreadFolder(" /home/owner/x ", { folder: "/home/owner/src/a" }), { cwd: "/home/owner/x", label: "~/x", scratch: false });
  assert.match(NO_FOLDER_LINE, /by hand/);
  assert.doesNotMatch(NO_FOLDER_LINE + FOLDER_LINE, /source|main|clones/i);
  assert.equal(localProject("p", "A", "/home/owner/src/a").folder, "/home/owner/src/a");
  assert.equal(localProject("p", "A", "").folder, undefined);
  // The model of several folders, the main star and the repo line is gone.
  for (const gone of ["projectFolders", "projectRepos", "repoLine", "sourceText", "MAIN_LINE", "FOLDERS_LINE", "createFolderLine", "suggestionFor", "makeProjectLabel"]) assert.ok(!(gone in projects), gone);
});

test("SPEC 0.5 V0.30: the picker's first row names what it makes", async () => {
  const { newRowLabel } = await import("../src/projects.ts");
  assert.equal(newRowLabel("", 3, true), "New project from this thread");
  assert.equal(newRowLabel("", 3, false), "New project");
  assert.equal(newRowLabel("hem", 1, true), "New project from this thread");
  assert.equal(newRowLabel("Kök ", 0, true), "New project “Kök”");
  assert.equal(newRowLabel("Kök", 0, false), "New project “Kök”");
});

test("SPEC 0.5 V0.30: a new thread starts on the inbox filter, else the last project used, else the most recent", async () => {
  const { startProject } = await import("../src/projects.ts");
  const ps = [
    { id: "a", name: "A", lastUsed: "2026-11-10T00:00:00Z" },
    { id: "b", name: "B", lastUsed: "2026-11-14T00:00:00Z" },
  ] as Project[];
  assert.equal(startProject(ps, "a", "b"), "a");
  assert.equal(startProject(ps, null, "a"), "a");
  assert.equal(startProject(ps, "gone", "gone"), "b");
  assert.equal(startProject([], null, null), undefined);
});

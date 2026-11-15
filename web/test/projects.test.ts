import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { PALETTE, moveToast, PALETTE_NAMES, colorName, deleteQuestion, filterByProject, lineName, matchProjects, messageFrom, monogram, paletteColor, pickerOrder, projectDefaults, projectName, projectOf, projectSubtitle, resolveIcon, startedBy } from "../src/projects.ts";
import type { Project } from "../src/types.ts";
import { thread } from "./helpers.ts";
import { applySnapshot, initialStore, upsertProject } from "../src/store.ts";

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
  assert.equal(monogram("norban/web"), "NW");
  assert.equal(monogram("fyr"), "F");
  assert.equal(monogram("Bokföring åt Övik"), "BÅ");
  assert.equal(monogram("öl-listan"), "ÖL");
  assert.equal(monogram("—"), "?");
});

test("icons: emoji, curated line icons (T3 names too), else the monogram", () => {
  assert.deepEqual(resolveIcon({ kind: "emoji", value: "🏡 hem" }, "Hemmet"), { kind: "emoji", value: "🏡" });
  // The host omits an empty value (Go omitempty): {"kind":"monogram"} must not crash.
  for (const kind of ["monogram", "emoji", "line"] as const) assert.deepEqual(resolveIcon(JSON.parse(`{"kind":"${kind}"}`), "norban"), { kind: "monogram", value: "N" });
  assert.deepEqual(resolveIcon({ kind: "line", value: "FolderGit2" }, "x"), { kind: "line", value: "folder-git-2" });
  assert.deepEqual(resolveIcon({ kind: "line", value: "Home" }, "x"), { kind: "line", value: "house" });
  assert.deepEqual(resolveIcon({ kind: "line", value: "no-such-icon" }, "norban/infra"), { kind: "monogram", value: "NI" });
  assert.deepEqual(resolveIcon(undefined, "fyr"), { kind: "monogram", value: "F" });
  assert.deepEqual(resolveIcon({ kind: "emoji", value: "  " }, "fyr"), { kind: "monogram", value: "F" });
  assert.equal(lineName("bar_chart"), "chart-column");
});

const projects: Project[] = [
  { id: "p_web", name: "norban/web", folder: "/home/o/src/web", lastUsed: "2026-11-14T10:00:00Z", profile: "claude", model: "sonnet" },
  { id: "p_fyr", name: "fyr", folder: "/home/meros/git/fyr", lastUsed: "2026-11-10T10:00:00Z" },
  { id: "p_home", name: "Hemmet", lastUsed: "2026-11-13T10:00:00Z" },
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
    a: thread("a", "seen", "seen", { project: "norban/web", projectId: "p_web" }),
    b: thread("b", "seen", "seen", { project: "Hemmet", projectId: "p_home" }),
    c: thread("c", "seen", "seen", { project: "norban/web" }),
  };
  assert.deepEqual(Object.keys(filterByProject(ts, "p_web", projects)).sort(), ["a", "c"], "by id, else by name");
  assert.equal(filterByProject(ts, null, projects), ts);
  assert.equal(projectOf(ts.b, projects)?.name, "Hemmet");
});

test("picking a project fills its folder and default profile and model", () => {
  const profiles = [{ id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" }];
  assert.deepEqual(projectDefaults(projects[0], profiles), { cwd: "/home/o/src/web", profile: "claude", model: "sonnet" });
  assert.deepEqual(projectDefaults(projects[2], profiles), { cwd: undefined, profile: undefined, model: undefined });
});

test("started-by and cross-thread messages", () => {
  const parent = thread("p", "working", "working", { title: "Map perf", project: "norban/web", projectId: "p_web" });
  const ts = { p: parent };
  assert.deepEqual(startedBy({ parent: "p" }, ts), { id: "p", title: "Map perf" });
  assert.deepEqual(startedBy({ parent: "gone" }, ts), { id: "gone", title: "another thread" });
  assert.equal(startedBy({ parent: null }, ts), null);
  assert.equal(messageFrom("p", ts, projects)?.project?.id, "p_web");
  assert.equal(messageFrom(undefined, ts, projects), null);
});

test("SPEC 0.5 V0.55: New project from this thread starts from the host's folder, else a name only", async () => {
  const { newProjectDraft } = await import("../src/projects.ts");
  assert.deepEqual(newProjectDraft({ newProject: { name: "Hemmet", folder: "/home/o/Hemmet" } }), { name: "Hemmet", folder: "/home/o/Hemmet" });
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

test("SPEC 0.5 V0.30, V0.61: the picker's first row names what it makes", async () => {
  const { newRowLabel } = await import("../src/projects.ts");
  assert.equal(newRowLabel("", false, true), "New project from this thread");
  assert.equal(newRowLabel("", false, false), "New project");
  assert.equal(newRowLabel("hem", false, true), "New project “hem”", "a partial match still names the new project");
  assert.equal(newRowLabel("Hemmet", true, true), "New project from this thread", "an exact name is the existing project");
  assert.equal(newRowLabel("Kök ", false, false), "New project “Kök”");
});

test("SPEC 0.5 V0.61: Enter picks an existing project only by its exact name; else it makes the new one", async () => {
  const { enterAction, matchProjects } = await import("../src/projects.ts");
  const list = [{ id: "p1", name: "fyr", folder: "/home/me/git/fyr" }, { id: "p2", name: "Hemmet" }] as Project[];
  const enter = (q: string, canCreate = true) => enterAction(list, matchProjects(list, q), q, canCreate);
  assert.deepEqual(enter("fyr2"), { create: "fyr2" });
  assert.deepEqual(enter("fy"), { create: "fy" }, "a partial match is not picked");
  assert.deepEqual(enter("git"), { create: "git" }, "a folder match is not picked");
  assert.equal((enter(" HEMMET ") as { pick: Project }).pick.id, "p2");
  assert.equal((enter("fy", false) as { pick: Project }).pick.id, "p1", "the filter has no New project row: first match");
  assert.equal(enter("   "), null);
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

test("a deleted project leaves the store (V0.58)", () => {
  const a = { id: "p1", name: "App" } as Project;
  const b = { id: "p2", name: "Hemmet" } as Project;
  let s = upsertProject(upsertProject(initialStore(), a), b);
  s = upsertProject(s, { id: "p1", deleted: true } as unknown as Project);
  assert.deepEqual(s.projects.map((p) => p.id), ["p2"]);
  assert.equal(upsertProject(s, { id: "p9", deleted: true } as unknown as Project).projects.length, 1, "an unknown id changes nothing");
});

// #42: a project frame that crossed the snapshot in flight keeps its newer copy; an older frame changes nothing.
test("projects keep the copy with the higher rev", () => {
  const frame = { id: "p1", name: "New name", rev: 20 } as Project;
  let s = upsertProject(initialStore(), frame);
  s = applySnapshot(s, { now: "", user: "", threads: [], actions: [], profiles: [], projects: [{ id: "p1", name: "Old name", rev: 10 } as Project, { id: "p2", name: "B", rev: 10 } as Project] }, Date.now());
  assert.deepEqual(s.projects.map((p) => p.name), ["New name", "B"]);
  s = upsertProject(s, { id: "p2", name: "Older", rev: 5 } as Project);
  assert.equal(s.projects[1].name, "B", "an older frame is ignored");
  s = upsertProject(s, { id: "p2", name: "Same rev", rev: 10 } as Project);
  assert.equal(s.projects[1].name, "Same rev", "an equal rev replaces");
  s = applySnapshot(s, { now: "", user: "", threads: [], actions: [], profiles: [], projects: [{ id: "p1", name: "Newest", rev: 30 } as Project] }, Date.now());
  assert.deepEqual(s.projects.map((p) => p.name), ["Newest"], "a newer snapshot wins and drops what it lacks");
  s = upsertProject(s, { id: "p1", name: "No rev" } as Project);
  assert.equal(s.projects[0].name, "No rev", "without a rev on both sides the frame replaces");
});

test("Delete project asks once, with what happens to the threads (V0.58)", () => {
  assert.equal(deleteQuestion({ name: "Hemmet", threads: 12 }), "Delete Hemmet? Its 12 threads stay and land in a project whose folder covers them, or in No project. The folder on disk is not touched.");
  assert.match(deleteQuestion({ name: "A", threads: 1 }), /Its 1 thread stays and lands/);
  assert.match(deleteQuestion({ name: "A" }), /It has no threads\./);
});

test("SPEC 0.5 V0.63: while New project is open, the new-thread chip shows the project being made", async () => {
  const { projectChip } = await import("../src/projects.ts");
  const old = { id: "p1", name: "fyr", folder: "/home/me/git/fyr" } as Project;
  assert.equal(projectChip(old, null).name, "fyr");
  assert.equal(projectChip(old, null).isNew, false);
  const c = projectChip(old, { name: "Kök", folder: "" });
  assert.equal(c.name, "Kök");
  assert.equal(c.isNew, true);
  assert.equal(c.sub, "New · no folder");
  assert.equal(projectChip(old, { name: "  ", folder: "" }).name, "New project");
  assert.equal(projectChip(undefined, null).name, "No project");
});

// review 2026-11-15: a failed move shows its error on both shells (the phone showed nothing).
test("moveToast says where the threads went, or the error", () => {
  assert.deepEqual(moveToast(1, "App", null), { text: "Filed in App", error: false });
  assert.deepEqual(moveToast(1, null, null), { text: "No project now", error: false });
  assert.deepEqual(moveToast(3, "App", null), { text: "Filed 3 threads in App", error: false });
  assert.deepEqual(moveToast(2, null, null), { text: "2 threads have no project now", error: false });
  assert.deepEqual(moveToast(2, "App", "No such project"), { text: "No such project", error: true });
});

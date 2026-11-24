// V0.dialogs-review: the rules of the dialogs that are pure: New thread's draft and New project step, the project
// search's Enter, the snooze date and time, the first focus of a dialog and the Merge dialog's remembered method.
// The phone's Back layers are in back-layer.test.ts.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  chooseHost,
  chooseProject,
  closeQuestion,
  draftLine,
  draftOf,
  draftReady,
  draftWhen,
  filesBack,
  folderValue,
  keptFiles,
  moreSummary,
  folderHint,
  folderNote,
  formHost,
  initialForm,
  loadDraft,
  madeProject,
  makingProject,
  NEW_DRAFT_KEY,
  promptPlaceholder,
  restoreDraft,
  saveDraft,
  settleForm,
  startBlock,
  type FormCtx,
  type NewThreadForm,
} from "../src/new-thread.ts";
import { enterAction, matchProjects, matchRank } from "../src/projects.ts";
import { customProblem, customStart, customUntil, fieldTime } from "../src/snooze.ts";
import { snoozePresets } from "../src/store.ts";
import { firstFocus, isTopTrap, openTraps, pushTrap, dropTrap } from "../src/focus-trap.ts";
import { MERGE_METHOD_KEY, mergeConfirm, mergeMethod, mergeMethodLine, mergeTitle, notMerged, rememberedMergeMethod, rememberMergeMethod } from "../src/pr.ts";
import { confirmFocus } from "../src/confirm.ts";
import { barOffers, stripMerge } from "../src/offers.ts";
import { arrowRow } from "../src/snooze.ts";
import { mappingLine } from "../src/model-choice.ts";
import { keyHints } from "../src/keys.ts";
import type { ChangeNeed, Offer, Profile, Project } from "../src/types.ts";
import { hid } from "./helpers.ts";

function memory(init: Record<string, string> = {}) {
  const m = new Map(Object.entries(init));
  return { m, getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) };
}

const claude: Profile = { id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" };
const zai: Profile = { id: "zai", name: "Z.ai", color: "#111", models: ["glm-5"] };
const hosts = [
  { name: "desk", id: hid("desk"), online: true },
  { name: "lab", id: hid("lab"), online: true },
];
const projects: Project[] = [
  { id: "p_web", name: "acme/web", folder: "/home/o/web", host: "desk", hostId: hid("desk") },
  { id: "p_api", name: "acme/api", folder: "/home/o/api", host: "desk", hostId: hid("desk") },
  { id: "p_arm", name: "Robot arm", folder: "/home/o/arm", host: "lab", hostId: hid("lab") },
];
const ctx: FormCtx = { projects, profiles: { desk: [claude], lab: [claude, zai] }, hosts };
const fresh = () => settleForm(initialForm(), ctx, null, {});
const hand = (f: NewThreadForm, what: "host" | "project" | "agent"): NewThreadForm => ({ ...f, hand: { ...f.hand, [what]: true } });

// ---- (3) the draft ----

test("V0.dialogs-review (3): a form with something typed is a draft; picks alone are not", () => {
  const f = fresh();
  assert.equal(draftOf(f, ctx), null, "an untouched form");
  assert.equal(draftOf(hand(chooseProject(f, projects[1], ctx), "project"), ctx), null, "a pick without a word typed");
  assert.deepEqual(draftOf({ ...f, text: "Fix the login" }, ctx), { text: "Fix the login", title: "" }, "the defaults are not in the draft");
  assert.deepEqual(draftOf({ ...f, title: "Login" }, ctx), { text: "", title: "Login" });
  assert.equal(draftOf({ ...f, text: "   " }, ctx), null, "blank text is nothing typed");
});

test("V0.dialogs-review (3): the draft keeps the picks made by hand and puts them back", () => {
  let f = hand(chooseHost(fresh(), hid("lab"), ctx), "host");
  f = hand({ ...f, profile: "zai", model: "glm-5", effortPick: "" }, "agent");
  f = { ...f, text: "Map the arm", title: "Arm", liveView: true, picked: "/home/o/experiments" };
  const d = draftOf(f, ctx)!;
  assert.deepEqual(d, { text: "Map the arm", title: "Arm", liveView: true, host: hid("lab"), handHost: true, folder: "/home/o/experiments", profile: "zai", model: "glm-5" });

  const kv = memory();
  saveDraft(kv, d);
  assert.deepEqual(loadDraft(kv), d, "it survives storage (a reload)");
  const back = restoreDraft(fresh(), loadDraft(kv)!, ctx);
  assert.equal(back.text, "Map the arm");
  assert.equal(back.title, "Arm");
  assert.equal(back.liveView, true);
  assert.equal(formHost(back, ctx), hid("lab"));
  assert.equal(back.picked, "/home/o/experiments");
  assert.deepEqual([back.profile, back.model], ["zai", "glm-5"]);
  assert.deepEqual(draftOf(back, ctx), d, "a restored draft is the same draft again");

  saveDraft(kv, null);
  assert.equal(kv.m.has(NEW_DRAFT_KEY), false, "a started thread, or an emptied form, removes it");
  assert.equal(loadDraft(kv), null);
  assert.equal(loadDraft(memory({ [NEW_DRAFT_KEY]: "{broken" })), null);
  assert.equal(loadDraft(null), null);
});

test("V0.dialogs-review (3): a project picked by hand comes back, No project too; one that is gone leaves the default", () => {
  const picked = { ...hand(chooseProject(fresh(), projects[1], ctx), "project"), text: "x" };
  assert.equal(draftOf(picked, ctx)!.project, "p_api");
  assert.equal(restoreDraft(fresh(), draftOf(picked, ctx)!, ctx).projectId, "p_api");
  const none = { ...hand(chooseProject(fresh(), null, ctx), "project"), text: "x" };
  assert.equal(draftOf(none, ctx)!.project, "");
  assert.equal(restoreDraft(fresh(), draftOf(none, ctx)!, ctx).projectId, undefined);
  const gone = restoreDraft(fresh(), { text: "x", title: "", host: hid("desk"), project: "p_deleted" }, ctx);
  assert.equal(gone.projectId, fresh().projectId, "the form's own start project stays");
  assert.equal(gone.text, "x");
});

test("V0.dialogs-review (3): a draft whose host is gone keeps its words and drops that host's picks", () => {
  const d = { text: "Map the arm", title: "", host: hid("old"), folder: "/home/o/x", profile: "zai", model: "glm-5", newProject: { name: "Zebra", folder: "/home/o/z" } };
  const f = restoreDraft(fresh(), d, ctx);
  assert.equal(f.text, "Map the arm");
  assert.equal(formHost(f, ctx), hid("desk"));
  assert.equal(f.picked, "");
  assert.equal(f.profile, "claude");
  assert.deepEqual(f.draft, { name: "Zebra", folder: "" }, "the new project's name stays, its folder was on the other host");
  assert.equal(f.creating, null, "round 2 (11): the step comes back closed, so no restored form has a primary button that makes a project");
});

test("V0.dialogs-review (3): picks wait for the projects and providers they name; the words do not", () => {
  const empty: FormCtx = { projects: [], profiles: {}, hosts };
  const d = { text: "typed", title: "", host: hid("lab"), project: "p_arm", profile: "zai", model: "glm-5" };
  assert.equal(draftReady(d, empty), false);
  assert.equal(draftReady(d, ctx), true);
  assert.equal(draftReady({ text: "typed", title: "" }, empty), true, "a draft of words alone waits for nothing");
  const early = restoreDraft(settleForm(initialForm(), empty, null, {}), d, empty, {}, "words");
  assert.equal(early.text, "typed");
  assert.equal(early.projectId, null, "no pick is made on a state that has not arrived");
  // The owner types on; the picks arrive and leave the words alone.
  const late = restoreDraft(settleForm({ ...early, text: "typed more" }, ctx, null, {}), d, ctx, {}, "picks");
  assert.equal(late.text, "typed more");
  assert.equal(late.projectId, "p_arm");
  assert.deepEqual([late.profile, late.model], ["zai", "glm-5"]);
});

// ---- (12) the New project step ----

test("V0.dialogs-review (12): the New project step keeps its name and folder until the project is made", () => {
  let f: NewThreadForm = { ...fresh(), picking: true, creating: "Zebra", draft: { name: "Zebra", folder: "/home/o/zebra" } };
  // Back: the step closes, what was typed stays.
  f = { ...f, creating: null };
  assert.equal(makingProject(f), null, "nothing is being made while the step is closed");
  assert.deepEqual(f.draft, { name: "Zebra", folder: "/home/o/zebra" });
  // Another project picked: still there.
  f = chooseProject(f, projects[1], ctx);
  assert.deepEqual(f.draft, { name: "Zebra", folder: "/home/o/zebra" });
  // The draft has it, with the step closed.
  assert.deepEqual(draftOf(f, ctx)!.newProject, { name: "Zebra", folder: "/home/o/zebra" });
  // Made: gone.
  f = madeProject({ ...f, creating: "Zebra" }, "p_z", "Zebra", "/home/o/zebra", ctx);
  assert.equal(f.draft, null);
  assert.equal(f.projectId, "p_z");
});

test("V0.dialogs-review (3): a thread that fails to start after its project was made keeps that project in the draft", () => {
  // "Create project and start": the project is made, then thread.create fails. The draft written then must name the
  // project, or the next New thread opens on the default project and Start files the thread there.
  const step: NewThreadForm = { ...fresh(), text: "Fix the login redirect", picking: true, creating: "Zebra", draft: { name: "Zebra", folder: "" } };
  const f = madeProject(step, "p_z", "Zebra", "", ctx);
  const d = draftOf(f, ctx)!;
  assert.equal(d.project, "p_z", "the made project is the owner's pick");
  assert.equal(d.newProject, undefined, "and is not made a second time");
  const made = { ...ctx, projects: [...ctx.projects, f.made!] };
  const back = restoreDraft(settleForm(initialForm(), made, null, {}), d, made);
  assert.equal(back.projectId, "p_z", "the next New thread opens on it");
});

test("V0.dialogs-review (12): another host keeps the step open with its name, drops what was on the old host, and says so", () => {
  const making: NewThreadForm = { ...fresh(), picking: true, creating: "Zebra", draft: { name: "Zebra", folder: "/home/o/zebra" } };
  const f = chooseHost(making, hid("lab"), ctx);
  assert.equal(f.creating, "Zebra", "the step stays open");
  assert.deepEqual(f.draft, { name: "Zebra", folder: "" });
  assert.equal(f.reset, "Not on this host: the new project's folder (it is on desk).");

  const picked = chooseHost({ ...hand(chooseProject(fresh(), projects[1], ctx), "project"), picked: "/home/o/other" }, hid("lab"), ctx);
  assert.equal(picked.reset, "Not on this host: the project you picked and the folder you picked (they are on desk).");
  assert.equal(chooseHost(fresh(), hid("lab"), ctx).reset, "", "defaults that follow the host are not a loss");
  assert.equal(chooseProject(picked, projects[2], ctx).reset, "", "the next pick answers the note");
});

// ---- (6) the folder line ----

test("V0.dialogs-review (6): while a project is being made the folder line is about that project", () => {
  const cur = projects[0];
  assert.equal(folderHint(cur, { scratch: true }, { name: "Zebra", folder: "" }), "Zebra is new and has no folder: the thread starts in a fresh scratch folder.");
  assert.equal(folderHint(cur, { cwd: "/home/o/zebra", label: "~/zebra", scratch: false }, { name: "Zebra", folder: "/home/o/zebra" }), "Zebra is new: the thread starts in ~/zebra.");
  assert.equal(folderHint(cur, { scratch: true }, { name: " ", folder: "" }), "The new project has no folder: the thread starts in a fresh scratch folder.");
  assert.equal(folderHint(cur, { cwd: "/home/o/z", label: "~/z", scratch: false }, { name: "", folder: "/home/o/z" }), "The new project is not named yet: the thread starts in ~/z.");
  assert.equal(folderNote(cur, { cwd: "/home/o/web", scratch: false }), "", "the project's own folder needs no line: the Project field shows it");
  assert.equal(folderNote(cur, { cwd: "/home/o/elsewhere", scratch: false }), "Filed in acme/web.");
  assert.equal(folderNote(undefined, { scratch: true }), "No project: a new folder just for this thread.");
});

// ---- (5, 23) why Start is off ----

test("V0.dialogs-review (5, 23): Start is off with nothing to send or an offline host, with one short reason", () => {
  assert.equal(startBlock(null, ""), "", "it can start");
  assert.equal(startBlock("Nothing to send", ""), "Write a prompt or attach a file.");
  assert.equal(startBlock("Waiting for uploads", ""), "Waiting for uploads.");
  assert.equal(startBlock(null, "lab is offline since 19:40. Its folders cannot be read and nothing can be made on it until it is back; pick another host or wait."), "lab is offline since 19:40.");
  assert.equal(startBlock(null, "host-a.example.com is offline. Its folders cannot be read."), "host-a.example.com is offline.", "a dot inside a name ends no sentence");
  assert.equal(startBlock(null, "Pick the host it starts on."), "Pick the host it starts on.");
  assert.equal(startBlock("Nothing to send", "lab is offline."), "Write a prompt or attach a file.", "the prompt comes first");
});

test("V0.dialogs-review (26): the prompt says the agent, never a provider's name", () => {
  assert.equal(promptPlaceholder(false), "What should the agent do?");
  assert.equal(promptPlaceholder(true), "Add a note (optional)");
  const c = { zone: "list", hasSelection: true, picked: 0, view: null, card: null, cardMulti: false, running: false, failed: false, undo: false, inText: false } as const;
  assert.deepEqual(keyHints({ ...c, dialog: "new" })[0], ["⏎", "start"], "the key hint uses the button's word");
  assert.deepEqual(keyHints({ ...c, dialog: "snooze", snoozeCount: 6 })[1], ["t", "date and time"]);
});

// ---- (2) Enter in the project search ----

test("V0.dialogs-review (2): Enter in the project search takes the first row: a match, and New project only when none", () => {
  const list = [{ id: "p1", name: "acme/api", folder: "/home/o/src/api" }, { id: "p2", name: "acme/web" }, { id: "p3", name: "api" }] as Project[];
  const enter = (q: string, canCreate = true) => enterAction(matchProjects(list, q), q, canCreate);
  assert.deepEqual(enter("acme"), { pick: list[0] }, "a part of a name picks the first match, never a new project");
  assert.deepEqual(enter("web"), { pick: list[1] });
  assert.deepEqual(enter("api"), { pick: list[2] }, "the project named exactly so, before an earlier partial match");
  assert.deepEqual(enter("Zebra"), { create: "Zebra" }, "nothing matches: the only row is New project");
  assert.equal(enter("Zebra", false), null, "the filter has no New project row");
  assert.equal(enter("  "), null);
});

// ---- (15) snooze: a date and time ----

test("V0.dialogs-review (15): the snooze date and time starts on tomorrow morning and takes no time before now", () => {
  const now = new Date(2026, 10, 24, 22, 15);
  assert.equal(fieldTime(now), "2026-11-24T22:15");
  // Round 2 (22): the hour is the "Tomorrow morning" preset's, one idea of morning in the dialog (it was 09:00).
  const morning = snoozePresets(now).find((x) => x.label === "Tomorrow morning")!.until;
  assert.equal(customStart(now).getTime(), morning.getTime());
  assert.equal(fieldTime(customStart(now)), "2026-11-25T06:00");
  assert.equal(fieldTime(customStart(new Date(2026, 11, 31, 8, 0))), "2027-01-01T06:00", "over the year's end");
  assert.equal(customProblem("2026-11-25T09:00", now), "");
  assert.equal(customProblem("2026-11-24T09:00", now), "Pick a time after now.", "why the button is off, as text");
  assert.equal(customProblem("", now), "Pick a date and time.");
  assert.equal(customUntil("2026-11-25T09:00", now)?.getTime(), new Date(2026, 10, 25, 9, 0).getTime());
  assert.equal(customUntil("2026-11-24T22:15", now), null, "now is not later");
  assert.equal(customUntil("2026-11-24T09:00", now), null, "earlier today");
  assert.equal(customUntil("", now), null);
  assert.equal(customUntil("not a time", now), null);
});

// ---- (1, 10) the open panels ----

test("V0.dialogs-review (10): a dialog's focus starts on its first control that is not the ×", () => {
  const isClose = (x: string) => x === "x";
  assert.equal(firstFocus(["x", "first choice", "second"], isClose), "first choice");
  assert.equal(firstFocus(["search", "x"], isClose), "search");
  assert.equal(firstFocus(["x"], isClose), "x", "a dialog with nothing else");
  assert.equal(firstFocus([], isClose), undefined);
});

test("V0.dialogs-review (1): the open panels are counted, so the app's keys know when one is up", () => {
  const before = openTraps();
  const a = {};
  const b = {};
  pushTrap(a);
  pushTrap(b);
  assert.equal(openTraps(), before + 2);
  dropTrap(b);
  dropTrap(a);
  assert.equal(openTraps(), before);
});

test("V0.trap-gone-with-panel: a panel that left the page is not open, before its cleanup has run", () => {
  const before = openTraps();
  const gone = { el: { isConnected: false } as HTMLElement };
  const up = { el: { isConnected: true } as HTMLElement };
  pushTrap(up);
  pushTrap(gone);
  assert.equal(openTraps(), before + 1, "only the panel still in the page counts");
  assert.equal(isTopTrap(up), true, "the newest panel in the page is the top one");
  dropTrap(gone);
  dropTrap(up);
  assert.equal(openTraps(), before);
});

// ---- (27) the Merge dialog ----

test("V0.dialogs-review (27): the Merge dialog starts on the method the repository was merged with last", () => {
  const kv = memory();
  assert.equal(rememberedMergeMethod(kv, "acme/web"), "squash", "nothing remembered: squash");
  rememberMergeMethod(kv, "acme/web", "rebase");
  rememberMergeMethod(kv, "acme/api", "merge");
  assert.equal(rememberedMergeMethod(kv, "acme/web"), "rebase");
  assert.equal(rememberedMergeMethod(kv, "acme/api"), "merge");
  assert.equal(rememberedMergeMethod(kv, "acme/other"), "squash");
  assert.equal(rememberedMergeMethod(memory({ [MERGE_METHOD_KEY]: '{"acme/web":"fast-forward"}' }), "acme/web"), "squash", "a method this client does not have");
  assert.equal(rememberedMergeMethod(memory({ [MERGE_METHOD_KEY]: "[broken" }), "acme/web"), "squash");
  assert.equal(rememberedMergeMethod(null, "acme/web"), "squash");
});

// ---- round 2: the draft says what it is, and does not move the form ----

test("V0.dialogs-review round 2: the draft has a time, and its line says when, where and what was not kept", () => {
  const now = new Date(2026, 10, 24, 10, 0).getTime();
  const at = (d: number, h: number, m = 0) => new Date(2026, 10, d, h, m).getTime();
  assert.equal(draftWhen(at(24, 9, 5), now), "today 09:05");
  assert.equal(draftWhen(at(23, 16, 40), now), "yesterday 16:40");
  assert.equal(draftWhen(at(20, 8), now), "Fri 08:00");
  assert.equal(draftWhen(at(12, 8), now), "12 Nov");
  assert.equal(draftWhen(new Date(2025, 11, 30, 8).getTime(), now), "30 Dec 2025");

  const f = { ...fresh(), text: "rewrite the module" };
  const d = draftOf(f, ctx, { at: at(23, 16, 40), files: 2 })!;
  assert.equal(d.at, at(23, 16, 40));
  assert.equal(d.files, 2);
  const kv = memory();
  saveDraft(kv, d);
  assert.deepEqual(loadDraft(kv), d, "the time and the file count survive storage");
  const back = restoreDraft(fresh(), d, ctx);
  assert.equal(draftLine(d, back, ctx, now), "Draft from yesterday 16:40 · in acme/web");
  assert.equal(draftLine(d, back, ctx, now, { lost: 2 }), "Draft from yesterday 16:40 · in acme/web · 2 files were not kept");
  assert.equal(draftLine(d, back, ctx, now, { lost: 1 }), "Draft from yesterday 16:40 · in acme/web · 1 file was not kept");
  assert.equal(draftLine({ text: "x", title: "" }, back, ctx, now), "Draft · in acme/web", "a draft stored before it had a time");
});

test("V0.dialogs-review round 2: a form of files alone is a draft; files come back on their host only, and the rest is counted", () => {
  assert.equal(draftOf(fresh(), ctx, { files: 0 }), null);
  const d = draftOf(fresh(), ctx, { files: 1, at: 5 })!;
  assert.deepEqual(d, { text: "", title: "", at: 5, files: 1 });
  const chip = (key: string, status: "done" | "uploading" | "error", id?: string) => ({ key, name: `${key}.png`, size: 1, mediaType: "image/png", status, progress: 1, ...(id ? { id } : {}) });
  const chips = [chip("a", "done", "att_1"), chip("b", "uploading"), chip("c", "error"), chip("d", "done")];
  assert.deepEqual(keptFiles(chips).map((c) => c.key), ["a"], "only an upload the host has can be put back");
  const held = { host: hid("desk"), chips: keptFiles(chips) };
  const three = { text: "look", title: "", files: 3 };
  assert.deepEqual(filesBack(held, hid("desk"), three), { chips: held.chips, lost: 2 });
  assert.deepEqual(filesBack(held, hid("lab"), three), { chips: [], lost: 3 }, "another host: an upload is one host's");
  assert.deepEqual(filesBack(null, hid("desk"), three), { chips: [], lost: 3 }, "after a reload the page holds none");
  assert.deepEqual(filesBack(held, hid("desk"), null), { chips: [], lost: 0 }, "no draft, no files");
  assert.deepEqual(filesBack({ host: undefined, chips: held.chips }, undefined, three), { chips: [], lost: 3 });
  // Closing asks only when it would lose something for good: a file still uploading in a form without words.
  assert.equal(closeQuestion("", [chip("b", "uploading")]), "A file is still uploading. It is not kept when you close now.");
  assert.equal(closeQuestion("", [chip("b", "uploading"), chip("e", "uploading")]), "2 files are still uploading. They are not kept when you close now.");
  assert.equal(closeQuestion("words", [chip("b", "uploading")]), null, "with words the draft comes back and says what did not");
  assert.equal(closeQuestion("", [chip("a", "done", "att_1")]), null, "an uploaded file is kept");
  assert.equal(closeQuestion("", []), null);
});

test("V0.dialogs-review round 2 (3): a draft does not move the form from the project it was opened on", () => {
  const now = Date.now();
  // A provider picked by hand on lab, no project picked. New thread opens from the inbox filtered on acme/api (desk).
  let made = hand({ ...chooseHost(fresh(), hid("lab"), ctx), profile: "zai", model: "glm-5" }, "agent");
  made = { ...made, text: "x" };
  const d = draftOf(made, ctx)!;
  assert.equal(d.host, hid("lab"));
  assert.equal(d.handHost, undefined, "the host followed nothing the owner picked");
  const opened = settleForm(initialForm(), ctx, "p_api", {});
  const back = restoreDraft(opened, d, ctx, {}, "all", true);
  assert.equal(formHost(back, ctx), hid("desk"), "the form stays on the filter's host");
  assert.equal(back.projectId, "p_api", "and on the filter's project");
  assert.equal(back.profile, "claude", "the provider picked on another host is dropped");
  assert.equal(back.text, "x");
  assert.equal(draftLine(d, back, ctx, now), "Draft · in acme/api");

  // A host picked by hand moves the form only when no filter chose the project.
  const handHost = draftOf({ ...hand(chooseHost(fresh(), hid("lab"), ctx), "host"), text: "x" }, ctx)!;
  assert.equal(handHost.handHost, true);
  assert.equal(formHost(restoreDraft(opened, handHost, ctx, {}, "all", true), ctx), hid("desk"));
  assert.equal(formHost(restoreDraft(fresh(), handHost, ctx, {}, "all", false), ctx), hid("lab"));

  // A project picked by hand in the draft is the one exception, and the line says so.
  const picked = draftOf({ ...hand(chooseProject(fresh(), projects[2], ctx), "project"), text: "x" }, ctx)!;
  const moved = restoreDraft(opened, picked, ctx, {}, "all", true);
  assert.equal(moved.projectId, "p_arm");
  assert.equal(formHost(moved, ctx), hid("lab"));
  assert.equal(draftLine(picked, moved, ctx, now), "Draft · in Robot arm, the project picked in the draft");
  const none = draftOf({ ...hand(chooseProject(fresh(), null, ctx), "project"), text: "x" }, ctx)!;
  assert.equal(draftLine(none, restoreDraft(opened, none, ctx, {}, "all", true), ctx, now), "Draft · no project, as picked in the draft");
  // One that is gone: the form's own project stays, and the line says the draft's is not offered.
  const gone = { text: "x", title: "", host: hid("desk"), project: "p_deleted" };
  assert.equal(draftLine(gone, restoreDraft(opened, gone, ctx, {}, "all", true), ctx, now), "Draft · in acme/api · the draft's project is not offered here");
  assert.equal(draftLine(gone, opened, ctx, now, { pending: true }), "Draft · in acme/api", "not said while the picks still wait for the host's state");
});

test("V0.dialogs-review round 2 (8): a look at another host and back keeps the project and folder picked by hand", () => {
  const picked = { ...hand(chooseProject(fresh(), projects[1], ctx), "project"), picked: "/home/o/other" };
  const lab = chooseHost(picked, hid("lab"), ctx);
  assert.equal(lab.projectId, "p_arm");
  assert.equal(lab.hand?.project, false, "the project lab assigned is not the owner's pick");
  assert.equal(lab.reset, "Not on this host: the project you picked and the folder you picked (they are on desk).");
  const desk = chooseHost(lab, hid("desk"), ctx);
  assert.equal(desk.projectId, "p_api", "the pick is back");
  assert.equal(desk.picked, "/home/o/other");
  assert.equal(desk.hand?.project, true);
  assert.equal(desk.reset, "", "and nothing is blamed on a pick the owner never made");
  // Without a pick by hand nothing is kept or said.
  const plain = chooseHost(chooseHost(fresh(), hid("lab"), ctx), hid("desk"), ctx);
  assert.equal(plain.reset, "");
  assert.equal(plain.hand?.project, false);
  // A pick made on the second host is kept for it too.
  const onLab = hand(chooseProject(lab, null, ctx), "project");
  const again = chooseHost(chooseHost(onLab, hid("desk"), ctx), hid("lab"), ctx);
  assert.equal(again.projectId, undefined, "No project, picked by hand on lab, is back on lab");
  assert.equal(again.hand?.project, true);
});

test("V0.dialogs-review round 2: the Folder row and the More fold say what they hold", () => {
  const own = { cwd: "/home/o/web", label: "~/web", scratch: false };
  assert.deepEqual(folderValue(projects[0], own), { label: "The project's folder", plain: true }, "not the same path a third time");
  assert.deepEqual(folderValue(projects[0], { cwd: "/home/o/x", label: "~/x", scratch: false }), { label: "~/x", plain: false });
  assert.deepEqual(folderValue(undefined, { label: "A fresh scratch folder", scratch: true }), { label: "A fresh scratch folder", plain: true });
  assert.equal(moreSummary({ title: "", liveView: false }), "More: title, live view");
  assert.equal(moreSummary({ title: "", liveView: false }, ["every provider's quota"]), "More: title, live view, every provider's quota");
  assert.equal(moreSummary({ title: "", liveView: true }), "More: Live view on", "a closed fold never hides a value");
  assert.equal(moreSummary({ title: " Arm ", liveView: true }, ["every provider's quota"]), "More: a title, Live view on");
});

// ---- round 2 (7): Enter in the project search takes the best match of the list ----

test("V0.dialogs-review round 2 (7): Enter takes the name that answers best, never the first substring", () => {
  const long: Project = { id: "p_long", name: "acme-platform-infrastructure-terraform", folder: "/home/o/long", host: "desk", hostId: hid("desk") };
  const infra: Project = { id: "p_infra", name: "acme/infra", folder: "/home/o/infra", host: "desk", hostId: hid("desk") };
  const list = [long, infra, ...projects];
  const enter = (q: string, canCreate = true) => enterAction(matchProjects(list, q), q, canCreate);
  assert.deepEqual(matchProjects(list, "infra").map((p) => p.id), ["p_long", "p_infra"], "the list keeps the owner's order (V0.121)");
  assert.deepEqual(enter("infra"), { pick: infra }, "a word of the name is it: before a name in which a longer word only starts with it");
  assert.deepEqual(enter("plat"), { pick: long }, "the start of a word");
  assert.deepEqual(enter("acme"), { pick: long }, "two names start with it: the earlier in the owner's order");
  assert.deepEqual(enter("acme/infra"), { pick: infra }, "the exact name");
  assert.deepEqual(enter("ACME/WEB"), { pick: projects[0] });
  assert.deepEqual(enter("structure"), { pick: long }, "only a part of a name: the substring match");
  assert.deepEqual([long, infra].map((p) => matchRank(p, "infra")), [3, 2]);
  assert.equal(matchRank(infra, "acme/infra"), 0);
  assert.equal(matchRank(infra, "acme"), 1);
  assert.equal(matchRank(long, "form"), 4, "inside a word");
  assert.equal(matchRank({ ...infra, name: "x" }, "infra"), 5, "only its folder line");
  assert.equal(matchRank(infra, "zzz"), -1);
  assert.deepEqual(enter("zzz"), { create: "zzz" });
  assert.equal(enter("zzz", false), null);
  // Never a project that is not in the list shown (another host's): the list is the form's host's.
  const desk = list.filter((p) => p.hostId === hid("desk"));
  assert.deepEqual(enterAction(matchProjects(desk, "Robot arm"), "Robot arm", true), { create: "Robot arm" });
});

// ---- round 2: confirmations, the Merge dialog, the snooze arrows ----

test("V0.dialogs-review round 2 (10): a red confirmation starts on Cancel; any other on its primary button", () => {
  assert.equal(confirmFocus({ danger: true }), "cancel");
  assert.equal(confirmFocus({}), "confirm");
});

test("V0.dialogs-review round 2 (1, 13): the Merge dialog names what it merges, and says its refusal on a line of its own", () => {
  const run = async () => null;
  assert.equal(mergeTitle({ number: 4170 }), "Merge #4170");
  assert.equal(mergeTitle({}), "Merge this PR");
  assert.equal(mergeTitle({ number: 212, sensitive: "promotes to production" }), "Merge #212, a sensitive PR");
  assert.equal(mergeConfirm({ number: 4170, choose: true }, "squash"), "Squash and merge #4170");
  assert.equal(mergeConfirm({ number: 4170, choose: true }, "rebase"), "Rebase and merge #4170");
  assert.equal(mergeConfirm({ number: 4170 }, "rebase"), "Merge #4170", "no method is named where the host takes none");
  assert.equal(mergeConfirm({ sha: "e5f6071aaaa" }, "squash"), "Merge e5f6071");
  assert.equal(notMerged("no connection to the host"), "Not merged: no connection to the host");
  assert.equal(mergeTitle({ sensitive: "promotes to production" }), "Merge this sensitive PR");
  // Round 3 (P2-4): where the host takes no choice the dialog names the method the ask carries, on the button too.
  assert.equal(mergeMethod("squash"), "squash");
  assert.equal(mergeMethod("fast-forward"), undefined);
  assert.equal(mergeConfirm({ number: 212, method: "squash" }, "rebase"), "Squash and merge #212", "the ask's method, not the dialog's remembered one");
  assert.equal(mergeMethodLine({ method: "squash" }), "Method: Squash and merge, as the agent asked. To pick another, merge from the thread's PR tab.");
  assert.equal(mergeMethodLine({}), "The merge uses the method its change asks for: squash unless the agent named another. To pick another, merge from the thread's PR tab.");
  void run;
});

test("V0.dialogs-review round 2 (13): a change card has one Merge: the strip's, and the action bar leaves its offer out", () => {
  const merge = { id: "fyr.merge", label: "Merge", run: "fyr", effect: "pr.merge", by: "observed", digest: "d" } as Offer;
  const rerun = { id: "fyr.rerun", label: "Re-run failed checks", run: "fyr", effect: "pr.rerunFailed", by: "observed", digest: "d" } as Offer;
  const c = (need?: Partial<ChangeNeed>) => ({ phase: "review" as const, offers: [merge, rerun], ...(need ? { need: { text: "", ...need } as ChangeNeed } : {}) });
  assert.equal(stripMerge(c({ cause: "ready", head: "e5f6071" }))?.id, "fyr.merge");
  assert.deepEqual(barOffers(c({ cause: "ready", head: "e5f6071" }), [merge, rerun]).map((o) => o.id), ["fyr.rerun"]);
  assert.equal(stripMerge(c({ cause: "ask", action: "pr.merge", head: "e5f6071" }))?.id, "fyr.merge", "the agent's merge ask is that merge too");
  assert.equal(stripMerge(c({ cause: "ready" })), null, "no head, no Merge on the strip");
  assert.equal(stripMerge(c({ cause: "ask", action: "fyr.deploy" })), null);
  assert.equal(stripMerge(c({ cause: "failure" })), null);
  assert.equal(stripMerge(c({ cause: "waiting", action: "pr.merge", muted: true })), null, "a muted need has no button");
  assert.deepEqual(barOffers(c(), [merge, rerun]).map((o) => o.id), ["fyr.merge", "fyr.rerun"], "without the strip the bar has it");
  assert.deepEqual(barOffers(undefined, [merge]).map((o) => o.id), ["fyr.merge"]);
});

test("V0.dialogs-review round 2 (12, review A12): the snooze arrows from outside the rows", () => {
  assert.equal(arrowRow(-1, 7, true), 0, "Down from the ×: the first choice");
  assert.equal(arrowRow(-1, 7, false), 6, "Up from the ×: the last, not the one before it");
  assert.equal(arrowRow(6, 7, true), 0);
  assert.equal(arrowRow(0, 7, false), 6);
  assert.equal(arrowRow(2, 7, true), 3);
  assert.equal(arrowRow(-1, 0, true), -1);
});

test("V0.dialogs-review round 2 (14): Hand over to the same model keeps it, and names the effort only", () => {
  const a: Profile = { id: "claude", name: "Claude", color: "#000", models: ["opus"], defaultModel: "opus" };
  const b: Profile = { ...a, id: "claude-b", name: "Claude (b)" };
  assert.equal(mappingLine({ model: "opus" }, a, b, { model: "opus", effort: "high" }), "Keeps opus · effort high.", "not “opus → opus · high: the nearest…”");
  assert.equal(mappingLine({ model: "opus", effort: "low" }, a, b, { model: "opus", effort: "high" }), "Keeps opus; effort low → high.");
  assert.equal(mappingLine({ model: "opus", effort: "high" }, a, b, { model: "opus", effort: "high" }), "Keeps opus · high.");
});

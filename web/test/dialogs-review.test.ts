// V0.dialogs-review: the rules of the dialogs that are pure: New thread's draft and New project step, the project
// search's Enter, the snooze date and time, the first focus of a dialog, the Merge dialog's remembered method, and
// the phone's Back layers.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  chooseHost,
  chooseProject,
  draftOf,
  draftReady,
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
import { enterAction, matchProjects } from "../src/projects.ts";
import { customStart, customUntil, fieldTime } from "../src/snooze.ts";
import { firstFocus, openTraps, pushTrap, dropTrap } from "../src/focus-trap.ts";
import { MERGE_METHOD_KEY, rememberedMergeMethod, rememberMergeMethod } from "../src/pr.ts";
import { createLayers } from "../src/back-layer.ts";
import { keyHints } from "../src/keys.ts";
import type { Profile, Project } from "../src/types.ts";
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
  assert.deepEqual(d, { text: "Map the arm", title: "Arm", liveView: true, host: hid("lab"), folder: "/home/o/experiments", profile: "zai", model: "glm-5" });

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
  const d = { text: "Map the arm", title: "", host: hid("old"), folder: "/home/o/x", profile: "zai", model: "glm-5", newProject: { name: "Zebra", folder: "/home/o/z", open: true } };
  const f = restoreDraft(fresh(), d, ctx);
  assert.equal(f.text, "Map the arm");
  assert.equal(formHost(f, ctx), hid("desk"));
  assert.equal(f.picked, "");
  assert.equal(f.profile, "claude");
  assert.deepEqual(f.draft, { name: "Zebra", folder: "" }, "the new project's name stays, its folder was on the other host");
  assert.equal(f.creating, "Zebra");
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
  assert.deepEqual(draftOf(f, ctx)!.newProject, { name: "Zebra", folder: "/home/o/zebra", open: false });
  // Made: gone.
  f = madeProject({ ...f, creating: "Zebra" }, "p_z", "Zebra", "/home/o/zebra", ctx);
  assert.equal(f.draft, null);
  assert.equal(f.projectId, "p_z");
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
  assert.equal(folderHint(cur, { scratch: true }, { name: " ", folder: "" }), "The new project is new and has no folder: the thread starts in a fresh scratch folder.");
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
  const enter = (q: string, canCreate = true) => enterAction(list, matchProjects(list, q), q, canCreate);
  assert.deepEqual(enter("acme"), { pick: list[0] }, "a part of a name picks the first match, never a new project");
  assert.deepEqual(enter("web"), { pick: list[1] });
  assert.deepEqual(enter("api"), { pick: list[2] }, "the project named exactly so, before an earlier partial match");
  assert.deepEqual(enter("Zebra"), { create: "Zebra" }, "nothing matches: the only row is New project");
  assert.equal(enter("Zebra", false), null, "the filter has no New project row");
  assert.equal(enter("  "), null);
});

// ---- (15) snooze: a date and time ----

test("V0.dialogs-review (15): the snooze date and time starts on tomorrow 09:00 and takes no time before now", () => {
  const now = new Date(2026, 10, 24, 22, 15);
  assert.equal(fieldTime(now), "2026-11-24T22:15");
  assert.equal(fieldTime(customStart(now)), "2026-11-25T09:00");
  assert.equal(fieldTime(customStart(new Date(2026, 11, 31, 8, 0))), "2027-01-01T09:00", "over the year's end");
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

// ---- (4) the phone's Back layers ----

/** A history whose traversals land later, as a browser's do: go() queues, `land` delivers the popstate. */
function fakeHistory() {
  const entries: unknown[] = [{ screen: { name: "new" }, sheet: null }];
  let at = 0;
  const queued: number[] = [];
  return {
    h: {
      get state() {
        return entries[at];
      },
      pushState(s: unknown) {
        entries.splice(at + 1);
        entries.push(s);
        at = entries.length - 1;
      },
      go(n: number) {
        queued.push(n);
      },
    },
    /** the queued traversals happen; returns how many did */
    land(onPop: () => void): number {
      const n = queued.length;
      for (const d of queued.splice(0)) {
        at = Math.max(0, at + d);
        onPop();
      }
      return n;
    },
    /** the owner's Back */
    back(onPop: () => void) {
      at = Math.max(0, at - 1);
      onPop();
    },
    depth: () => at,
    length: () => entries.length,
  };
}
const tick = () => new Promise<void>((r) => setTimeout(r, 0));

test("V0.dialogs-review (4): Back closes the top-most layer, and the screen under it stays", () => {
  const f = fakeHistory();
  const ls = createLayers(f.h, () => {});
  const closed: string[] = [];
  ls.open(() => closed.push("list"));
  ls.open(() => closed.push("folder picker"));
  assert.equal(f.depth(), 2, "one entry per layer");
  assert.deepEqual((f.h.state as { screen: unknown }).screen, { name: "new" }, "a layer's entry is the screen's own state");
  f.back(() => ls.popped());
  assert.deepEqual(closed, ["folder picker"]);
  f.back(() => ls.popped());
  assert.deepEqual(closed, ["folder picker", "list"]);
  assert.equal(f.depth(), 0, "still on the screen");
  assert.equal(ls.depth(), 0);
  f.back(() => ls.popped()); // nothing left: the navigation's own Back
  assert.equal(closed.length, 2);
});

test("V0.dialogs-review (4): a layer closed on the page takes its entry out, and that traversal is not a Back", async () => {
  const f = fakeHistory();
  const ls = createLayers(f.h, () => {});
  const closed: string[] = [];
  const list = ls.open(() => closed.push("list"));
  const step = ls.open(() => closed.push("step"));
  // A project was made: the step and the list close in one turn, in either order.
  ls.close(list);
  ls.close(step);
  await tick();
  assert.equal(f.land(() => ls.popped()), 1, "one traversal for both entries");
  assert.equal(f.depth(), 0);
  assert.deepEqual(closed, [], "no layer was told to close again");
});

test("V0.dialogs-review (4): a navigation waits for a layer's entry on its way out, and never lands on top of a layer", async () => {
  const f = fakeHistory();
  const ls = createLayers(f.h, () => {});
  const order: string[] = [];
  const confirm = ls.open(() => order.push("cancelled"));
  ls.close(confirm); // the owner pressed the confirmation's button
  ls.settled(() => order.push(`navigate at ${f.depth()}`)); // what it confirmed navigates at once
  assert.equal(order.length, 0, "not before the entry is out");
  await tick();
  f.land(() => ls.popped());
  assert.deepEqual(order, ["navigate at 0"]);

  // Start with the project list open: the list's entry goes first, then the thread replaces the screen's entry.
  ls.open(() => order.push("list closed"));
  assert.equal(f.depth(), 1);
  ls.settled(() => order.push(`replace at ${f.depth()}`));
  await tick();
  f.land(() => ls.popped());
  assert.deepEqual(order, ["navigate at 0", "replace at 0"]);
  // With nothing open a navigation runs at once.
  ls.settled(() => order.push("now"));
  assert.equal(order.at(-1), "now");
});

test("V0.dialogs-review (4): a layer that opens while another's entry leaves waits for it; the header's × steps over all", async () => {
  const f = fakeHistory();
  const ls = createLayers(f.h, () => {});
  const closed: string[] = [];
  const list = ls.open(() => closed.push("list"));
  ls.close(list);
  ls.open(() => closed.push("step")); // the list became the New project step in the same turn
  assert.equal(f.depth(), 1, "no entry pushed under a traversal that is on its way");
  await tick();
  f.land(() => ls.popped());
  assert.equal(f.depth(), 1, "the list's entry left, the step's came");
  assert.equal(ls.depth(), 1);
  assert.equal(ls.drop(), 1, "leaving the screen steps over the step's entry too");
  assert.equal(ls.depth(), 0);
  assert.deepEqual(closed, []);
});

test("V0.dialogs-review (4): a traversal that never lands does not hold navigation for ever", async () => {
  const f = fakeHistory();
  let give: () => void = () => {};
  const ls = createLayers(f.h, (fn) => (give = fn));
  const l = ls.open(() => {});
  ls.close(l);
  let ran = false;
  ls.settled(() => (ran = true));
  await tick();
  assert.equal(ran, false);
  give(); // the timeout
  assert.equal(ran, true);
});

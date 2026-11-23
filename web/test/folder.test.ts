// V0.folder-chip (#26): the header's folder chip and the card a click on it opens.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cardLines } from "../src/card.ts";
import { folderCard, folderChipText, folderLabel, threadFolder } from "../src/folder.ts";
import type { Change } from "../src/types.ts";

const at = "2026-11-20T10:00:00Z";
const change = (extra: Partial<Change>): Change => ({ id: "c1", key: "k", kind: "code", title: "Fix the lasso", phase: "working", phaseBy: "agent", createdAt: at, updatedAt: at, ...extra });
const WT = { repo: "github.com/acme/web", path: "/home/owner/src/acme/web/.claude/worktrees/fix-map-lasso", branch: "fix/map-lasso", base: "origin/main", madeByFyr: true };

test("a repo folder: its last path part and its branch", () => {
  const v = threadFolder({ cwd: "/home/owner/src/acme/web", branch: "map-perf" });
  assert.deepEqual(folderChipText(v), { name: "web", branch: "map-perf" });
  assert.equal(v.path, "/home/owner/src/acme/web");
  assert.equal(folderLabel(v), "Folder web, branch map-perf");
  assert.deepEqual(cardLines(folderCard(v, "host-a")), ["Path: /home/owner/src/acme/web", "Branch: map-perf", "Host: host-a"]);
});

test("a scratch folder without git: only the folder's name, no branch", () => {
  const v = threadFolder({ cwd: "/home/owner/.local/share/fyr/scratch/20261114-093005-dishwasher", branch: "" });
  assert.deepEqual(folderChipText(v), { name: "20261114-093005-dishwasher", branch: "" });
  assert.equal(folderLabel(v), "Folder 20261114-093005-dishwasher");
  assert.deepEqual(cardLines(folderCard(v)), ["Path: /home/owner/.local/share/fyr/scratch/20261114-093005-dishwasher"]);
});

test("a worktree Fyr made: the repo's name and the worktree's branch, the thread's own folder in the card", () => {
  const v = threadFolder({ cwd: "/home/owner/src/acme/web", branch: "main", changes: [change({ branch: "fix/map-lasso", workspace: WT })] });
  assert.deepEqual(folderChipText(v), { name: "web", branch: "fix/map-lasso" });
  assert.equal(v.path, WT.path);
  assert.deepEqual(cardLines(folderCard(v, "host-a")), [
    `Path: ${WT.path}`,
    "Branch: fix/map-lasso",
    "Worktree: github.com/acme/web · Fyr made it for “Fix the lasso”",
    "Thread: /home/owner/src/acme/web · the thread's own folder",
    "Host: host-a",
  ]);
});

test("a worktree that is not Fyr's, or whose change is done or ended, leaves the thread's folder", () => {
  const cwd = "/home/owner/src/acme/web";
  for (const c of [change({ workspace: { ...WT, madeByFyr: false } }), change({ workspace: WT, done: { at, by: "owner", reason: "done" } }), change({ workspace: WT, phase: "abandoned" })]) {
    assert.deepEqual(folderChipText(threadFolder({ cwd, branch: "main", changes: [c] })), { name: "web", branch: "main" });
  }
});

test("of two Fyr worktrees, the newest change's", () => {
  const old = change({ id: "a", title: "Old", workspace: { ...WT, path: "/r/.claude/worktrees/old", branch: "old" }, touchedAt: "2026-11-20T09:00:00Z" });
  const neu = change({ id: "b", title: "New", workspace: { ...WT, path: "/r/.claude/worktrees/new", branch: "new" }, touchedAt: "2026-11-20T11:00:00Z" });
  assert.equal(threadFolder({ cwd: "/r", branch: "main", changes: [old, neu] }).branch, "new");
});

test("long names are cut in the middle; the card and Copy path keep them whole", () => {
  const name = "a-very-long-folder-name-for-the-acme-billing-service";
  const branch = "feature/add-a-really-long-branch-name-for-the-header-chip";
  const v = threadFolder({ cwd: `/srv/${name}/`, branch });
  const c = folderChipText(v);
  assert.equal(c.name.length, 28);
  assert.ok(c.name.startsWith("a-very-") && c.name.includes("…") && c.name.endsWith("service"), c.name);
  assert.equal(c.branch.length, 32);
  assert.ok(c.branch.startsWith("feature/") && c.branch.endsWith("header-chip"), c.branch);
  assert.equal(v.name, name);
  assert.equal(v.path, `/srv/${name}/`);
  assert.equal(cardLines(folderCard(v))[1], `Branch: ${branch}`);
});

test("the root folder and an empty folder still read", () => {
  assert.equal(threadFolder({ cwd: "/" }).name, "/");
  assert.equal(threadFolder({ cwd: "" }).name, "No folder");
});

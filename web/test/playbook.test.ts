// V0.project-playbook: the playbook's pure parts: the page's order and words, a linked project's playbook, and where
// a merged change is among its project's stages.
import { test } from "node:test";
import assert from "node:assert/strict";
import { entryTitle, learnArgs, newestPlaybook, playbookEntries, playbookSummary, probeLine, probesWaiting, stageText, stageTrail, stagesOf } from "../src/playbook.ts";
import { linkProjects } from "../src/projects.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Change, PlaybookEntry, Project } from "../src/types.ts";
import { hid } from "./helpers.ts";

const prod: PlaybookEntry = { slot: "stage", name: "prod", how: "a release tag", by: "owner" };
const dev: PlaybookEntry = { slot: "stage", name: "dev", how: "automatic on merge", url: "https://dev.example.com", probe: "check {sha}", by: "agent" };
const issue: PlaybookEntry = { slot: "issue", how: "GitHub issues", by: "agent" };
const p = { playbook: [issue, prod, dev] };

test("the page lists the stages in the order a change reaches them, then the notes", () => {
  assert.deepEqual(stagesOf(p).map((e) => e.name), ["dev", "prod"]);
  assert.deepEqual(playbookEntries(p).map(entryTitle), ["Stage dev", "Stage prod · live", "Issues"]);
  assert.deepEqual(playbookEntries(undefined), []);
});

test("a probe that waits for the owner is said first", () => {
  assert.equal(probesWaiting(p), 1);
  assert.equal(playbookSummary(p), "1 probe waits for you · 2 stages · 1 note");
  assert.equal(playbookSummary({ playbook: [{ ...dev, probeOk: true }] }), "1 stage");
  assert.equal(playbookSummary({}), "Nothing learned yet");
  assert.match(probeLine(dev), /waits for your yes/);
  assert.match(probeLine({ ...dev, probeOk: true }), /after each merge/);
  assert.match(probeLine(prod), /No probe/);
});

test("an edit sends the whole entry, since the host replaces it", () => {
  assert.deepEqual(learnArgs("p1", dev, { how: "on merge, 5 min" }), { id: "p1", slot: "stage", name: "dev", how: "on merge, 5 min", url: "https://dev.example.com", probe: "check {sha}" });
  assert.deepEqual(learnArgs("p1", issue), { id: "p1", slot: "issue", how: "GitHub issues" });
});

const change = (phase: Change["phase"], conditions: Change["conditions"] = [], evidence: Change["evidence"] = []) => ({ phase, conditions, evidence });
const ok = (type: string) => ({ type, status: "ok" as const, source: "observed" as const, at: "2026-11-24T10:00:00Z" });

test("a merged change shows each stage reached, failed or ahead", () => {
  const stages = stagesOf(p);
  assert.deepEqual(stageTrail(change("review"), stages), []);
  assert.deepEqual(stageTrail(change("landed"), []), []);
  assert.equal(stageText(stageTrail(change("landed"), stages)), "dev next · prod next");
  const onDev = stageTrail(change("landed", [ok("deploy:dev")], [{ kind: "deployment", url: "https://pr-9.dev.example.com", title: "dev" }]), stages);
  assert.equal(stageText(onDev), "on dev · prod next");
  assert.equal(onDev[0].url, "https://pr-9.dev.example.com", "the address the deployment gave wins over the playbook's");
  assert.equal(stageText(stageTrail(change("live", [ok("deploy:dev"), ok("deploy")]), stages)), "on dev · on prod");
  assert.equal(stageText(stageTrail(change("landed", [{ ...ok("deploy:dev"), status: "fail" }]), stages)), "dev failed · prod next");
  assert.equal(stageTrail(change("abandoned"), stages).length, 0);
});

test("a linked project shows the playbook written last", () => {
  assert.deepEqual(newestPlaybook([{ playbook: [issue], playbookAt: "2026-11-24T10:00:00Z" }, { playbook: [dev], playbookAt: "2026-11-24T11:00:00Z" }, {}]).playbook, [dev]);
  assert.deepEqual(newestPlaybook([{}, {}]), {});
  // An emptied playbook is the newest value too.
  assert.deepEqual(newestPlaybook([{ playbook: [issue], playbookAt: "2026-11-24T10:00:00Z" }, { playbookAt: "2026-11-24T11:00:00Z" }]).playbook, undefined);
  const host = (id: string, name: string) => ({ id: hid(id), name, online: true }) as HostInfo;
  const rec = (h: string, pid: string, extra: Partial<Project>): Project => ({ id: `${h}/${pid}`, rid: pid, hostId: hid(h), name: "Fyr", link: "l_1", ...extra });
  const [one] = linkProjects([rec("A", "p1", {}), rec("B", "p9", { playbook: [dev], playbookAt: "2026-11-24T11:00:00Z" })], [host("A", "desk"), host("B", "mac")]);
  assert.deepEqual(one.playbook, [dev]);
});

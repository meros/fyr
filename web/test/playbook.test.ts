// V0.project-playbook: the playbook's pure parts: the page's order and words, a linked project's playbook, and where
// a merged change is among its project's stages.
import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_LESSONS, addableSlots, broken, entryKey, entryTitle, learnArgs, lessonNameError, pendingLesson, newestPlaybook, playbookEntries, playbookSummary, playbookWaiting, probeLine, removeEntryAsk, stageHealth, stageNameError, stageText, stageTrail, stagesOf, suggestionLines } from "../src/playbook.ts";
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
  assert.equal(playbookWaiting(p), 1);
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

// V0.playbook-moments
test("an edit keeps the deploy command and the owner's check", () => {
  const e: PlaybookEntry = { ...dev, deployCmd: "ship {sha}", ownerChecks: true };
  assert.deepEqual(learnArgs("p1", e, { ownerChecks: false }), { id: "p1", slot: "stage", name: "dev", how: "automatic on merge", url: "https://dev.example.com", probe: "check {sha}", deployCmd: "ship {sha}" });
});

test("a suggestion and a probe in doubt wait for the owner and are said in the summary", () => {
  const sug: PlaybookEntry = { ...prod, suggest: { how: "the release workflow", probe: "rel {sha}" } };
  const doubt: PlaybookEntry = { ...dev, probeOk: true, missed: 2 };
  assert.equal(playbookWaiting({ playbook: [sug, doubt, issue] }), 2);
  assert.equal(playbookSummary({ playbook: [sug, doubt, issue] }), "1 suggestion · 1 probe in doubt · 2 stages · 1 note");
  assert.deepEqual(suggestionLines(sug), ["the release workflow", "Probe: rel {sha}"]);
  assert.deepEqual(suggestionLines({ ...prod, suggest: { how: prod.how } }), ["The same as yours"]);
  assert.match(probeLine(doubt), /did not see the last 2 merges/);
  assert.doesNotMatch(probeLine({ ...doubt, missed: 1 }), /doubt/);
});

test("Add offers a stage and the notes that are missing", () => {
  assert.deepEqual(addableSlots(p), ["stage", "verify", "promote", "run", "rollback", "lesson"]);
  const six = Array.from({ length: 6 }, (_, i): PlaybookEntry => ({ slot: "stage", name: `s${i}`, by: "owner" }));
  assert.equal(addableSlots({ playbook: six }).includes("stage"), false);
  assert.equal(stageNameError("Dev"), null);
  assert.match(stageNameError("my stage") ?? "", /lower case/);
});

// V0.playbook-probe-health
test("a probe that cannot answer is said on its stage, in the summary and in the pipeline", () => {
  const run: PlaybookEntry = { ...dev, probeOk: true };
  const stuck: PlaybookEntry = { ...run, last: { state: "timeout", exit: -1, note: "applied: 899b950", at: "2026-11-24T18:35:00Z" } };
  const gone: PlaybookEntry = { ...run, last: { state: "error", exit: 127, note: "sh: check: not found" } };
  assert.equal(broken(stuck) && broken(gone), true);
  assert.equal(broken({ ...stuck, probeOk: false }), false, "a probe Fyr does not run is not broken");
  assert.equal(broken({ ...run, last: { state: "waiting", exit: 1 } }), false, "not there yet is a probe that works");
  assert.match(probeLine(stuck, "18:35"), /^Stopped at the 30 s limit since 18:35.*must not wait/);
  assert.match(probeLine(gone), /^Cannot run \(exit 127\), so no change is seen here\.$/);
  assert.match(probeLine({ ...run, last: { state: "waiting", exit: 1 } }, "18:35"), /Last answer since 18:35: not here yet \(exit 1\)/);
  assert.match(probeLine({ ...run, last: { state: "reached" } }, "18:35"), /^Works: it last saw a change here 18:35\.$/);
  assert.equal(playbookWaiting({ playbook: [stuck, issue] }), 1);
  assert.equal(playbookSummary({ playbook: [stuck, { ...prod, probe: "p {sha}", probeOk: true, missed: 2 }, issue] }), "1 probe fails · 1 probe in doubt · 2 stages · 1 note");
  assert.deepEqual([stuck, run, dev, prod, { ...run, missed: 2 }].map((e) => stageHealth(e).state), ["bad", "ok", "ask", "blind", "bad"]);
  assert.equal(stageHealth(stuck).word, "probe fails");
});

test("Remove asks first, and says what goes with a stage", () => {
  assert.deepEqual(removeEntryAsk(prod), { title: "Remove Stage prod · live?", text: "Fyr no longer follows merged changes to this stage, and its probe and commands are gone.", confirm: "Remove entry", danger: true });
  assert.match(removeEntryAsk(issue).text, /no longer told/);
});

// V0.playbook-lessons: a lesson is named, listed after the notes, counted apart; one an agent only suggested waits for
// the owner, says its text as the suggestion, and is not counted as a lesson until taken.
test("a lesson is named and listed last; a suggested one waits for the owner", () => {
  const taken: PlaybookEntry = { slot: "lesson", name: "e2e-first", how: "Run make e2e before saying done.", by: "owner" };
  const asked: PlaybookEntry = { slot: "lesson", name: "lint-web", by: "agent", suggest: { how: "Run the web lint too.", at: "2026-11-25T10:00:00Z" } };
  const q = { playbook: [taken, issue, asked, { ...prod }] };
  assert.deepEqual(playbookEntries(q).map(entryTitle), ["Stage prod · live", "Issues", "Lesson e2e-first", "Lesson lint-web"]);
  assert.equal(entryKey(taken), "lesson:e2e-first");
  assert.ok(pendingLesson(asked) && !pendingLesson(taken));
  assert.deepEqual(suggestionLines(asked), ["Run the web lint too."]);
  assert.equal(playbookWaiting(q), 1);
  assert.equal(playbookSummary(q), "1 suggestion · 1 stage · 1 note · 1 lesson");
  assert.match(removeEntryAsk(asked).text, /suggestion is gone/);
  assert.deepEqual(learnArgs("p1", taken), { id: "p1", slot: "lesson", name: "e2e-first", how: "Run make e2e before saying done." });
  assert.equal(lessonNameError("e2e-first"), null);
  assert.ok(lessonNameError("Run e2e first"));
  assert.ok(addableSlots(q).includes("lesson"));
  const full = { playbook: Array.from({ length: MAX_LESSONS }, (_, i): PlaybookEntry => ({ slot: "lesson", name: `l${i}`, how: "x", by: "owner" })) };
  assert.ok(!addableSlots(full).includes("lesson"));
});

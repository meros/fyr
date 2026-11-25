// The project playbook (V0.project-playbook): what a project does around and after a pull request, in a fixed set of
// slots. Agents learn it, the owner corrects it, and Fyr runs a stage's probe itself after a merge. Pure.

import type { Change, PlaybookEntry, Project } from "./types.ts";

/** The slots, in the order the page lists them; a stage has a name. */
export const PLAYBOOK_SLOTS = ["stage", "verify", "promote", "issue", "run", "rollback", "lesson"] as const;

const SLOT_TITLE: Record<string, string> = { verify: "Verify", promote: "Promote", issue: "Issues", run: "Run locally", rollback: "Roll back", lesson: "Lesson" };

/** A slot that holds several entries, each with a name: a stage, and a lesson (V0.playbook-lessons). */
export const namedSlot = (slot: string) => slot === "stage" || slot === "lesson";

/** At most this many lessons in a project (the host's MaxLessons). */
export const MAX_LESSONS = 12;

/** An entry's key in its playbook: the slot, or stage:<name> and lesson:<name>. */
export const entryKey = (e: Pick<PlaybookEntry, "slot" | "name">) => (namedSlot(e.slot) ? `${e.slot}:${e.name ?? ""}` : e.slot);

/** V0.playbook-lessons: a lesson an agent suggested that the owner has not taken: it has no text of its own, and no
 *  agent is told it yet. */
export const pendingLesson = (e: Pick<PlaybookEntry, "slot" | "how">) => e.slot === "lesson" && !e.how;

/** A stage that makes a change live: marked so, or named prod or production. */
export function isLiveStage(e: Pick<PlaybookEntry, "slot" | "name" | "live">): boolean {
  const n = (e.name ?? "").trim().toLowerCase();
  return e.slot === "stage" && (!!e.live || n === "prod" || n === "production");
}

/** The stages in the order a merged change reaches them: as learned, the live ones last (as the host orders them). */
export function stagesOf(p: Pick<Project, "playbook"> | undefined): PlaybookEntry[] {
  const st = (p?.playbook ?? []).filter((e) => e.slot === "stage");
  return [...st.filter((e) => !isLiveStage(e)), ...st.filter(isLiveStage)];
}

/** The playbook in the page's order: the stages, then one entry per slot. */
export function playbookEntries(p: Pick<Project, "playbook"> | undefined): PlaybookEntry[] {
  const rest = PLAYBOOK_SLOTS.slice(1).flatMap((s) => (p?.playbook ?? []).filter((e) => e.slot === s));
  return [...stagesOf(p), ...rest];
}

/** A linked project's playbook is the one written last, whichever host is shown (the front copies it to the rest). */
export function newestPlaybook(members: Pick<Project, "playbook" | "playbookAt">[]): Pick<Project, "playbook" | "playbookAt"> {
  const at = (m: Pick<Project, "playbookAt">) => Date.parse(m.playbookAt ?? "") || 0;
  const best = members.reduce((a, b) => (at(b) > at(a) ? b : a));
  return best.playbook?.length || best.playbookAt ? { playbook: best.playbook, playbookAt: best.playbookAt } : {};
}

/** An entry's title on the page: "Stage dev", "Stage prod · live", "Issues". */
export function entryTitle(e: PlaybookEntry): string {
  if (e.slot === "lesson") return `Lesson ${e.name}`;
  if (e.slot !== "stage") return SLOT_TITLE[e.slot] ?? e.slot;
  return `Stage ${e.name}${isLiveStage(e) ? " · live" : ""}`;
}

/** V0.playbook-probe-health: a probe Fyr runs that cannot answer: it was stopped at the limit or could not run the
 *  last time (the host's Broken). No change reaches the stage while it is so. */
export const broken = (e: Pick<PlaybookEntry, "probe" | "probeOk" | "last">) => !!e.probe && !!e.probeOk && (e.last?.state === "timeout" || e.last?.state === "error");

/** What Fyr does with a stage's probe, in the words under it. `since` is when it began to answer so ("14:20"), from
 *  the page's clock; without it the line names no time. */
export function probeLine(e: Pick<PlaybookEntry, "probe" | "probeOk" | "missed" | "last">, since = ""): string {
  if (!e.probe) return "No probe: Fyr cannot tell when a change is here.";
  if (!e.probeOk) return "Not run yet: it waits for your yes.";
  const at = since ? ` since ${since}` : "";
  if (e.last?.state === "timeout") return `Stopped at the 30 s limit${at}, so no change is seen here. A probe answers at once: it must not wait for the deploy.`;
  if (e.last?.state === "error") return `Cannot run (exit ${e.last.exit ?? -1})${at}, so no change is seen here.`;
  if (doubted(e)) return `In doubt: it did not see the last ${e.missed} merges here within a day. Check the command.`;
  if (e.last?.state === "reached") return `Works: it last saw a change here${since ? ` ${since}` : ""}.`;
  if (e.last?.state === "waiting") return `Runs after each merge. Last answer${at}: not here yet (exit ${e.last.exit ?? 1}).`;
  return "Fyr runs this after each merge, until the stage holds the commit.";
}

/** V0.playbook-probe-health: how Fyr follows a stage, for the pipeline over the entries: its probe runs (ok), waits
 *  for the owner's yes (ask), fails or is in doubt (bad), or there is none (blind: only GitHub Deployments can say). */
export function stageHealth(e: Pick<PlaybookEntry, "probe" | "probeOk" | "missed" | "last">): { state: "ok" | "ask" | "bad" | "blind"; word: string } {
  if (!e.probe) return { state: "blind", word: "no probe" };
  if (!e.probeOk) return { state: "ask", word: "waits for your yes" };
  if (broken(e)) return { state: "bad", word: "probe fails" };
  if (doubted(e)) return { state: "bad", word: "probe in doubt" };
  return { state: "ok", word: "probe runs" };
}

/** The one question before an entry is removed. */
export function removeEntryAsk(e: PlaybookEntry): { title: string; text: string; confirm: string; danger: true } {
  return { title: `Remove ${entryTitle(e)}?`, text: e.slot === "stage" ? "Fyr no longer follows merged changes to this stage, and its probe and commands are gone." : pendingLesson(e) ? "The agent's suggestion is gone." : "Agents here are no longer told this.", confirm: "Remove entry", danger: true };
}

/** The folded group's summary: "2 stages · 3 notes", with the probes that wait first. */
export function playbookSummary(p: Pick<Project, "playbook"> | undefined): string {
  const all = p?.playbook ?? [];
  if (!all.length) return "Nothing learned yet";
  const stages = all.filter((e) => e.slot === "stage");
  const waits = stages.filter((e) => e.probe && !e.probeOk).length;
  const sug = all.filter((e) => e.suggest).length;
  const fails = stages.filter(broken).length;
  const doubt = stages.filter((e) => doubted(e) && !broken(e)).length;
  const lessons = all.filter((e) => e.slot === "lesson" && !pendingLesson(e)).length;
  const notes = all.filter((e) => !namedSlot(e.slot)).length;
  const n = (k: number, one: string) => `${k} ${one}${k === 1 ? "" : "s"}`;
  return [waits ? `${n(waits, "probe")} ${waits === 1 ? "waits" : "wait"} for you` : "", sug ? n(sug, "suggestion") : "", fails ? `${n(fails, "probe")} ${fails === 1 ? "fails" : "fail"}` : "", doubt ? `${n(doubt, "probe")} in doubt` : "", stages.length ? n(stages.length, "stage") : "", notes ? n(notes, "note") : "", lessons ? n(lessons, "lesson") : ""]
    .filter(Boolean)
    .join(" · ");
}

/** What on the playbook waits for the owner (V0.playbook-moments): a probe to allow, a suggestion to answer, a probe
 *  that missed the last merges or cannot answer (V0.playbook-probe-health). The group opens by itself while this is
 *  not 0. */
export function playbookWaiting(p: Pick<Project, "playbook"> | undefined): number {
  return (p?.playbook ?? []).filter((e) => (e.slot === "stage" && e.probe && !e.probeOk) || e.suggest || doubted(e) || broken(e)).length;
}

/** V0.playbook-moments: a stage whose probe did not see the last two merges within a day (the host's DoubtAfter). */
export const doubted = (e: Pick<PlaybookEntry, "missed">) => (e.missed ?? 0) >= 2;

/** V0.playbook-moments: the slots the owner can still add by hand: a stage (up to six) and a lesson (up to twelve,
 *  V0.playbook-lessons), each note once. */
export function addableSlots(p: Pick<Project, "playbook"> | undefined): PlaybookEntry["slot"][] {
  const all = p?.playbook ?? [];
  const count = (s: string) => all.filter((e) => e.slot === s).length;
  return PLAYBOOK_SLOTS.filter((s) => (s === "stage" ? count(s) < 6 : s === "lesson" ? count(s) < MAX_LESSONS : !all.some((e) => e.slot === s)));
}

/** A slot's name in the Add menu. */
export const slotTitle = (s: string) => (s === "stage" ? "Stage" : (SLOT_TITLE[s] ?? s));

/** A stage's name as the host takes it, or why not. */
export function stageNameError(name: string): string | null {
  return /^[a-z0-9][a-z0-9._-]{0,23}$/.test(name.trim().toLowerCase()) ? null : "Name the environment in lower case, such as dev or prod";
}

/** V0.playbook-lessons: a lesson's name as the host takes it (the same rule as a stage's), or why not. */
export function lessonNameError(name: string): string | null {
  return stageNameError(name) ? "Name it in a few words in lower case with dashes, such as e2e-before-done" : null;
}

/** What differs in an agent's suggestion, one line per field, for the page. */
export function suggestionLines(e: PlaybookEntry): string[] {
  const s = e.suggest;
  if (!s) return [];
  const out: string[] = [];
  if ((s.how ?? "") !== (e.how ?? "")) out.push(s.how ? s.how : "No text");
  if ((s.url ?? "") !== (e.url ?? "")) out.push(s.url ? `Address: ${s.url}` : "No address");
  if ((s.probe ?? "") !== (e.probe ?? "")) out.push(s.probe ? `Probe: ${s.probe}` : "No probe");
  if ((s.deployCmd ?? "") !== (e.deployCmd ?? "")) out.push(s.deployCmd ? `Deploy command: ${s.deployCmd}` : "No deploy command");
  if (!!s.ownerChecks !== !!e.ownerChecks) out.push(s.ownerChecks ? "You check each change here" : "Nobody is asked to check here");
  if (!!s.live !== !!e.live) out.push(s.live ? "This stage is live" : "This stage is not live");
  return out.length ? out : ["The same as yours"];
}

export interface StageStep {
  name: string;
  state: "reached" | "failed" | "ahead";
  /** where the stage is looked at: the address the change's deployment gave, else the playbook's */
  url?: string;
  live: boolean;
}

/** Where a merged change is among its project's stages (V0.project-playbook): each stage reached (its deploy
 *  condition is ok, the production one for a live stage), failed, or ahead. Empty before the merge, for an ended
 *  change and for a project without stages. */
export function stageTrail(c: Pick<Change, "phase" | "conditions" | "evidence">, stages: PlaybookEntry[]): StageStep[] {
  if (!stages.length || (c.phase !== "landed" && c.phase !== "live")) return [];
  return stages.map((s) => {
    const live = isLiveStage(s);
    const k = c.conditions?.find((x) => x.type === (live ? "deploy" : `deploy:${s.name}`));
    const state = (live && c.phase === "live") || k?.status === "ok" ? "reached" : k?.status === "fail" ? "failed" : "ahead";
    const url = c.evidence?.find((e) => e.kind === "deployment" && e.title === s.name)?.url ?? s.url;
    return { name: s.name ?? "", state, live, ...(url ? { url } : {}) };
  });
}

/** The trail in words, for the done row and a tooltip: "on dev · prod next". */
export function stageText(steps: StageStep[]): string {
  return steps.map((s) => (s.state === "reached" ? `on ${s.name}` : s.state === "failed" ? `${s.name} failed` : `${s.name} next`)).join(" · ");
}

/** The arguments of project.learn that write entry `e` again with `patch` (the host replaces the whole entry). */
export function learnArgs(id: string, e: Pick<PlaybookEntry, "slot"> & Partial<PlaybookEntry>, patch: Partial<Pick<PlaybookEntry, "how" | "probe" | "url" | "deployCmd" | "ownerChecks">> = {}): Record<string, unknown> {
  const x = { ...e, ...patch };
  return { id, slot: x.slot, ...(x.name ? { name: x.name } : {}), ...(x.how ? { how: x.how } : {}), ...(x.url ? { url: x.url } : {}), ...(x.probe ? { probe: x.probe } : {}), ...(x.deployCmd ? { deployCmd: x.deployCmd } : {}), ...(x.live ? { live: true } : {}), ...(x.ownerChecks ? { ownerChecks: true } : {}), ...(x.evidence ? { evidence: x.evidence } : {}) };
}

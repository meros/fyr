# 0043 Settle-time retros: a thread that went wrong is turned into findings, each routed to where it can be fixed (the project's lessons, the repository, Fyr itself, the setup), and each fix is checked by what happens after

Date: 2026-11-25 · Status: **proposed**; nothing built · Builds on V0.project-playbook, V0.playbook-moments, V0.playbook-probe-health, decision 0020 (one state machine, the agent feed), decision 0024 (facts beat claims, the owner's corrections), decision 0029 (admission), decision 0041 (the usage ledger) · Spec: 3.15 (a `lesson` slot and counters on `PlaybookEntry`), a new record `Finding`, 5.12, 7.6, with a change file per slice

## Context

A thread that goes wrong teaches something: the agent said it was done and CI failed twice, the owner wrote "no, run the e2e tests too", the change was abandoned or reverted, the agent waited half an hour on a notice that never came, a probe instruction made it write a probe that sleeps. When the thread settles, that is gone. Where the fix belongs differs:

| What went wrong | Who can fix it | Today |
|---|---|---|
| The agent did not know how this project is verified | the project: a line in every later agent's prompt | only if an agent calls `project_learn` or the owner says it |
| A test is missing, a test is flaky, `CLAUDE.md` is wrong | the repository: an issue or a pull request | only if someone files it |
| A Fyr notice misled the agent, a tool refused, a prompt line or tool description taught it the wrong thing, a wait never woke | Fyr itself: its prompts, tools, notices, gates | only when the owner notices and opens a thread on Fyr's repository (V0.playbook-probe-health came from such a look at a host's log) |
| The model or effort was too small for the work, the host ran out of memory, a provider's quota cut the run | the setup: a project's default profile, the host | only if the owner sees the pattern |

Fyr sees every one of these threads, the facts around them and how they ended. It is the one place where this can be learned across threads and projects. What it lacks: nothing turns a settled thread into findings, nothing routes a finding to whoever can fix it, and no fix is ever checked against what happened after.

What is built and is reused:

| Piece | Where | What it gives this record |
|---|---|---|
| Itemized entries with provenance (`by`, `thread`, `evidence`), an agent's write kept as `suggest` the owner takes or dismisses | `internal/core/playbook.go`, V0.playbook-moments (4) | the shape of a finding and the owner's gate |
| A probe's outcome counted (`missed`, `last`), doubt after two misses | V0.playbook-moments (5), V0.playbook-probe-health | the pattern for checking an entry against later facts |
| A run with a fixed brief that records with one tool and changes nothing | `playbookBrief`, `project.playbookRun` | the shape of the retro run |
| Facts Fyr reads itself: CI per head, review threads, merge, `abandoned`/`reverted`, deploy stages, waits and their wakes, refused tools, failure classes, guard holds and freezes, quota stops | decision 0024, `internal/core/change.go`, `notices.go`, the guard log | outcome labels nobody has to write |
| The owner's messages and corrections are Inputs with a source | 5.1, `internal/core/corrections.go` | "the owner had to correct the agent" is observable |
| Chips, Run cards and issue filing through the playbook's `issue` slot | 5.13, V0.project-playbook | how a finding becomes an issue or a thread at one press |
| Tokens per thread and per system job | decision 0041 | what the retros cost, measured apart |

## What the research says

Self-improving agent systems in 2025–2026 change one of three things: the context (a playbook of strategies), a skill or memory bank, or the harness around the model. The results that apply to Fyr (each is the authors' own and mostly an unreplicated preprint):

- **Edit items, never rewrite the whole.** ACE ([arXiv 2510.04618](https://arxiv.org/abs/2510.04618), ICLR 2026) keeps the context as items grown by a generate → reflect → curate loop with small delta updates and helpful/harmful counters per item. Rewriting the whole with a model makes it collapse into short generic advice ("context collapse"; "brevity bias"). It learns from execution feedback without labels.
- **Distilled procedure beats raw trajectories, and a bank must be curated.** CODESKILL ([arXiv 2605.25430](https://arxiv.org/html/2605.25430)) adds, merges or drops skills; REM ([arXiv 2610.27377](https://arxiv.org/pdf/2610.27377)) finds abstract procedural memories transfer better than trajectories. The survey on self-evolving coding agents ([arXiv 2608.03392](https://arxiv.org/html/2608.03392v2)) names the failure modes: stale, redundant, too repository-specific, contaminated by failed runs.
- **The harness is where the gains are, if each edit is checked.** Agentic Harness Engineering ([arXiv 2604.25850](https://arxiv.org/html/2604.25850v2)) keeps the model fixed, edits only the harness from trace evidence, ties every edit to a prediction checked by the next round, and reverts the ones that fail (Terminal-Bench 2: 69.7 → 77.0 %). The Darwin Gödel Machine ([arXiv 2505.22954](https://arxiv.org/abs/2505.22954)) rewrites its own agent code against a benchmark (SWE-bench 20 → 50 %).
- **The judge must be out of the learner's reach.** The Darwin Gödel Machine gamed its own objective once; work on harness tampering ([arXiv 2609.00069](https://arxiv.org/pdf/2609.00069)) finds agents tamper with an evaluator they can reach and stop when it is locked; proxy gains drift from real gains the longer a loop optimizes. Practitioner guidance (Thoughtworks Radar, "self-improving harness", Trial; Arize's observe → find failures → change → evaluate loop) agrees: bounded edits, regression checks, humans set the objective and the limits.

For Fyr that means: findings are small items with evidence; each goes to whoever can fix it, through a press of the owner; each fix names the failure it should make rarer; Fyr checks that with facts the agent cannot write, and says when a fix did not help.

## Decision

### 1. Triggers: facts, not claims

When a thread settles (`thread.settle` by the owner or a taken `settle_request`; not an archive of an idle thread), the host computes its triggers from facts Fyr reads and the owner's own Inputs, never from what the working agent reports:

| Trigger | Read from |
|---|---|
| `ci-after-done`: a required check failed on a head pushed after the agent said the change was ready, or after a merge ask | CI per head, `change_request` |
| `owner-correction`: the owner's message after the agent's first done that the cheap classifier (decision 0033's) reads as a correction, or the owner's Abandon / Done on a card | owner Inputs, `corrections.go` |
| `reverted`: the change ended `reverted`, or a later PR in the project reverts its merge commit | change phase, git |
| `rolled-back`: a stage the change reached stopped holding it | deploy observer |
| `review-rework`: review threads the owner opened that the agent had to answer | V0.review-thread-notice |
| `stalled`: a wait that ended at its limit, not by its condition; a background command Fyr woke the thread about while it used no CPU; a Run card or ask the agent opened twice for the same thing | rules, the idle wake, asks |
| `refused`: a tool call Fyr refused, a gate hold that the agent worked around, a probe or deploy command that could not run | effects, gate log, probe `last` |
| `resource`: a freeze, a hold past 10 minutes, a kill-and-rerun, an overload or quota stop | guard log, failure classes |

A thread with no trigger runs nothing: a clean thread costs nothing. The triggers are recorded on the thread whatever happens next (slice 1), so their rates are known before anything spends tokens.

### 2. The retro: a short run that writes findings

- With one or more triggers, the host queues a retro: a system job (counted apart in the usage ledger) that runs with a small model and a fixed brief, like `playbookBrief`. Its input is narrow: the trigger facts, the owner's messages after the agent's first done, the failing check names and their last lines, the notices and tool refusals around each trigger, the project's playbook and lessons, and the open findings. Not the whole transcript.
- Its only tool is `finding {route, text, evidence, trigger, fix?, replaces?}`. It changes no file, files nothing, asks nothing.
- The brief: at most three findings; each specific (a thing to do or change, not "be careful"); each with evidence (the facts and the thread); prefer adding evidence to an open finding over a new one; none when the cause was one-off (a runner outage, a network error, the owner changing their mind). Name the route by who can fix it (3).
- A project's switch (`retro: "off"`, default on) and quiet hours stop it. The run waits for a slot like any system job (decision 0029).

### 3. Routes: where a finding goes

A finding is a record of the host (`Finding {id, route, project?, text, evidence[], trigger, fix?, state, by, at, checks?}`), shown in one list. Its route decides what Take does:

| Route | Who fixes it | Take does |
|---|---|---|
| `lesson` | every later agent in the project | the text becomes a `lesson` entry in the project's playbook (4), in every agent's prompt there |
| `repo` | the project's repository (a missing or flaky test, a wrong line in `CLAUDE.md`, a missing script) | opens a thread in the project's folder with the finding as its brief, which files an issue where the playbook's `issue` slot says, or makes the pull request; the owner's normal merge ask follows |
| `fyr` | Fyr itself (a notice, a tool, a prompt line, a tool description, a gate, a wait) | opens a thread on Fyr's own repository with the finding, scrubbed (5), which files an issue there or makes the change |
| `setup` | the owner's setup (a project's default model or effort, a host's memory, a provider) | shows the setting it names, with the change the finding proposes; the owner makes it |

Dismiss closes a finding with a reason the next retro reads, so the same finding is not written again. A new finding that matches an open one (same route, same trigger, `replaces`) adds its evidence instead: three threads behind one finding say more than one.

### 4. Lessons in the playbook

- `PlaybookEntry.slot` gains `lesson`: up to 12 per project, one or two sentences (`how`, 400 characters), with `evidence` and `trigger`. A lesson holds what an agent in this project should do differently, not general advice and not what `CLAUDE.md` already says. At 12, a new lesson must name the one it replaces.
- Lessons come from a taken `lesson` finding, or the owner writes one. An agent's `project_learn {slot: "lesson"}` makes a finding, never a lesson.
- Taken lessons go into the agents' prompt after the stages, each with its counts when it has any.

### 5. Checking a fix: the prediction and the counts

- Every taken finding carries its `trigger` and a scope: the project (`lesson`, `repo`, `setup`) or every project on the host (`fyr`). From the moment its fix is in place (a lesson taken; the PR that closes its issue merged and, for `fyr`, live on this host by the `deployable` tag), Fyr counts the settled threads in that scope that had the trigger (`hurt`) and those that ended with a merged change without it (`helped`). These are the host's own counts, like `missed`, and move no stamp.
- From `hurt` ≥ 3 and `hurt` > `helped`, the fix is in doubt: the finding opens again with its counts ("taken 12 Nov; since then 4 threads still failed CI after done, 3 did not"), and the next retro is told. A lesson nobody needed for 60 days or 30 settled threads is offered for removal. Fyr never removes, rewrites or reverts anything by itself.
- The counts are a rough signal, not proof: a lesson about e2e tests is scored by every thread in the project, also ones that never touched the client. The page shows counts, never a score or a percentage.

### 6. Limits

- **The judge is out of the agent's reach.** Triggers and counts come from facts and owner Inputs only; no tool writes them, and the retro cannot see or change how they are computed.
- **Nothing changes without a press.** A lesson reaches a prompt only by the owner's Take; a `repo` or `fyr` finding becomes code only through a thread, a pull request and the owner's merge ask, under the same CI as any change. A finding cannot weaken a gate, a required check, an approval policy or a probe's yes, and a change made from one that edits a test or a gate together with what that test judges is marked on its merge ask.
- **Private stays private.** Findings and lessons live in Fyr's store. A `repo` or `fyr` finding is rewritten for a public repository before its thread starts: no quotes of the owner, no names of other projects, hosts or people, no paths or usage numbers (V0.public-repo); the thread's brief repeats the rule, and the owner sees the text before the issue is filed.

### 7. The page, the same on the desktop and the phone

- **Findings** is one list (Settings → Findings, and a group on each project's page for that project's findings): each with its route, text, evidence (links to the threads), counts once taken, and Take / Dismiss. Its summary counts what waits for the owner, as the Playbook group counts probes that wait for a yes.
- The project's Playbook group shows its lessons with their counts and Remove (it asks first).
- A settled thread with triggers shows them under its outcome, and one line when its retro wrote a finding: "Left a finding: run the e2e tests before done (lesson)".

## Slices

1. **Triggers.** Computed at settle and shown on the thread, no run. Answers how often each fires per project before anything costs tokens.
2. **Lessons.** The `lesson` slot, the owner writes them, Take / Remove on the page, in the prompt.
3. **The retro and findings.** The system job, the `Finding` record and list, routes `lesson` and `setup`.
4. **Routes `repo` and `fyr`.** The threads Take starts, the scrubbing, the issue filing.
5. **Counts and doubt.**

Each slice is a change file under `docs/changes/` and stands alone; slice 1 decides whether slice 3 is worth its tokens.

## Options not taken

- **Write lessons into `CLAUDE.md`.** Every tool reads it, but the repository may be public, the owner's words would leak, and every lesson would be a pull request. A `repo` finding still can propose a `CLAUDE.md` line through a reviewed PR.
- **Keep the whole trace as memory and retrieve from it.** Raw trajectories transfer worse than distilled items and grow without bound.
- **Let the retro apply its findings.** The fastest loop, and the one every source warns against: no gate on what enters every later prompt or Fyr's own code.
- **A DGM-style search over Fyr's harness against a benchmark.** Fyr has no benchmark of agent behaviour, and one person's threads are too few to search with; real threads and their facts are the evaluation, one reviewed change at a time. Automated search fits only where an evaluator exists: the guard's constants against the load simulation, a separate record.
- **One retro per thread, always.** Most threads go fine; a run for them spends tokens to write "nothing to learn".
- **Score fixes by an A/B split.** Cleaner attribution, but it withholds a taken fix from half the agents, and there are too few threads for it to say much.

## Open questions

1. Should a `fyr` finding from one install reach Fyr's repository only through its owner, or may installs of the same owner pool their counts (several hosts seeing the same trigger)?
2. Should a retro also run for a thread the owner settles halfway, without a change?
3. Is 12 lessons per project right, and should a lesson be able to name paths (only threads that touch `web/`) so its counts stop scoring unrelated work?
4. Should a linked project share its lessons and their counts across hosts (V0.linked-values-sync), or keep them per host as probe answers are?

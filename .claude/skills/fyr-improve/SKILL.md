---
name: fyr-improve
description: "Improve Fyr from how it was really used: measure the owner's clarity, the throughput of the threads and the owner's control from the host's event log and threads, find the causes (bugs, noise, gaps, sprawl, load), confirm each in code, start one focused fix thread per cause, follow them to merge, and check that the numbers moved. Use when the owner asks to analyse the day's threads, says it is hard to tell what goes on, asks Fyr to improve itself, or on its daily schedule."
argument-hint: (optional) a window like 24h, 3d or 2026-11-25T08:00 | "report" to measure and report only | "daily" to also schedule the next run
---

# fyr-improve: Fyr's self-improvement loop

Run it in a Fyr thread whose folder is the Fyr checkout. Arguments: **$ARGUMENTS**.

## What it optimises

Fyr exists so that one owner can run many agents and still know what each does and decide what matters. Every finding is judged against three goals, in this order:

1. **Control, never lost.** The owner still approves what they approve today: merges outside an auto-merge setting, Run cards, decisions, anything outward-facing. A change that would remove an ask, widen an auto-approval or let an agent act for the owner is not a fix you start: it is a `type:decision` issue and a question to the owner.
2. **Clarity.** From the inbox row alone, the owner can tell what a thread does, whether it needs them and why. Signs that this failed: the owner asks "status?", a row shows the wrong PR or state, a title says `Image`, a wait has no visible reason.
3. **Throughput.** Work gets from first message to merged and live with few wasted turns, holds, false failures and reruns, and few owner presses that add no decision.

A finding can be a bug, but it often is not: a noisy notice, a missing default, guidance agents keep ignoring, a thread that grew too big, an overloaded host. All of these count when they hurt one of the goals.

## Rules

- **The repository is public.** Thread texts, titles, project names, paths, hosts and numbers from the log are private. They stay in the scratchpad and in Fyr threads. Issues, PRs, `docs/changes/` and tests get anonymised evidence (`acme`, `host-a`, "a thread with 9 PRs").
- **The coordinator does not write the fixes.** This thread measures, decides and dispatches. A fix happens in its own thread, worktree and PR. The exception is this skill's own files.
- **Never merge, never bypass a guard.** Fyr asks for merges. A guard or gate that blocks is a finding, not an obstacle.
- **Confirm before you dispatch.** A signal is a lead, not a bug. Another model's analysis or a summary is not evidence. Read the thread's log and the code path. Dispatch only with a concrete cause, or with "find the cause" stated as the task.
- **Respect the host and GitHub.** Today's worst noise was a host at load 50 and an exhausted REST quota. Start at most 6 fix threads a run (Fyr allows 12 an hour), batch `gh` reads, and never poll.

## 0. Start

```bash
S=<this session's scratchpad>; mkdir -p "$S"
python3 -I .claude/skills/fyr-improve/signals.py --since ${WINDOW:-24h} > "$S/signals.md"
python3 -I .claude/skills/fyr-improve/signals.py --since ${WINDOW:-24h} --json > "$S/signals.json"
```

`signals.py` reads `~/.local/share/fyr/fyr.db` read-only (`FYR_DATA` or `--db` for another path). It prints a **scorecard** and the leads:

- notices by template, and those that only woke an agent for a short turn;
- the owner asking what goes on;
- unasked-question nudges;
- merge refusals and merge asks that were not shown;
- failed effects;
- holds and freezes;
- run failures;
- threads with many PRs or repos;
- titles to judge.

If the event schema changed and the script breaks, fix the script first, in this skill's PR.

Then gather what is already moving, so nothing is dispatched twice:

- `mcp__fyr__threads` per state (`working`, `waiting`, `asking`, `failed`) and with the query `Fyr`. It returns at most 30, so query narrowly.
- `mcp__fyr__change_list {scope: "project"}` for Fyr's open changes.
- `gh issue list --repo meros/fyr --label type:bug --state open --json number,title,labels --limit 50`
- `git log --since=<window> --oneline origin/main` and the `docs/changes/` files added in the window.
- The last run's report, if this thread or a pinned one has it: its scorecard is the baseline.

## 1. Read the threads behind the leads

Pick the threads that carry the most signal: owner prods, short wakes, nudges, refusals, holds, sprawl. Read them with `mcp__fyr__thread_read {id, last: 1500}`. A small `last` can return nothing when the tail is all tool calls.

With more than about 8 threads, fan out: one `Explore` or `general-purpose` subagent per 4–6 threads. Give each the thread ids, the goals above, and this output schema, one row per event:

`thread | local time | kind | what the owner saw | what was true | evidence (event or quote) | cost (turns, minutes, presses) | suspected cause`

The kinds are listed in section 2.

Ask the subagents for facts, not verdicts.

## 2. Classify into causes

Group the events by root cause, not by thread. Most kinds hurt one goal mainly; the ones on the "any" line can hurt any of them.

- **Clarity:**
  - **wrong-state:** a row or chip says something false.
  - **wrong-row-info:** the row shows the wrong PR, a stale summary or a bad title.
  - **hidden-wait:** a wait whose reason the owner can't see.
  - **interrupt:** something took focus while the owner typed.
- **Throughput:**
  - **noise-wake:** a notice that only produced a "nothing to do" turn.
  - **false-gate:** a merge blocked by something that was not the change's fault.
  - **lost-signal:** something the agent should have heard and didn't.
  - **retry-churn:** the same failure repeated.
  - **load:** host CPU or memory, freezes, holds.
  - **external:** GitHub 5xx, provider quota.
- **Control:**
  - **control-gap:** an agent acted where the owner should have chosen, or a guard was bypassed.
  - **press-without-decision:** the owner pressed something that carried no real choice.
- **Any goal:**
  - **sprawl:** one thread, many PRs, repos or pivots.
  - **guidance-gap:** agents keep doing the wrong thing that Fyr's instructions could prevent.
  - **workflow:** the owner's or another project's process, not Fyr's.

For each cause, write down:

- **Scope:** how often it happened and in how many threads.
- **Cost:** turns, minutes, owner presses, owner prods.
- **Evidence:** at least one concrete event.
- **Code path:** `file:line`, once confirmed.
- **Goal:** which one it hurts.
- **Kind of fix:** code, spec, guidance or workflow.

## 3. Confirm and rank

For each cause:

1. **Check it is still there.** It may have been fixed in the window. Check `git log origin/main`, open PRs and working threads. If it was fixed, note it and check later that its signal fell.
2. **Find the code path.** Read the code that produced the event: the notice text gives the grep string (`rg -n "<fixed part of the template>" internal web/src`). Know why it happened.
3. **Check the spec.** Read the spec section (`docs/SPEC.md`) or the `docs/changes/` entry that governs it:
   - If the code breaks the spec, it is a bug.
   - If the spec allows it, it is a spec change.
   - If it needs a choice between options, it is a decision.

Rank by cost times frequency. Control findings come first, then clarity, then throughput.

Drop a cause when:

- it was a single event with no harm;
- it is fully explained by an outage outside Fyr that Fyr already handles well;
- it is a workflow matter of another project. Offer that as a chip instead: `mcp__fyr__offer_action {in: "new"}`.

## 4. Dispatch

One cause, one thread, one PR. A coupled pair is allowed: the code change plus its guidance text.

For each cause you keep:

1. **File an issue** on `meros/fyr`. Use the `type:bug` or `type:spec` label plus `area:*`, fill in the Spec field, and give anonymised evidence and the confirmed code path.
2. **Start a thread** with `mcp__fyr__thread_new {cwd: <Fyr checkout>, title, text}`. The title is short and specific, in the language of the work. The text is the brief:

```
<Cause in one sentence>. Goal it hurts: <control|clarity|throughput>.
Evidence: <threads (ids), events, counts; what the owner saw vs what was true>.
Confirmed cause: <file:line and why>, or "find the cause" as step 1.
Wanted: <the behaviour, as a rule a test can check>. Not wanted: <what would cost control or clarity>.
Closes #<issue>. Follow docs/PROCESS.md: spec or docs/changes/ entry, red test, fix, one PR, merge ask.
If you find it is not a bug, say why on the issue, close it, and settle.
When the PR is open, tell thread <this thread's id> with thread_send: the PR, the cause, the fix in two lines.
```

For a cause that needs the owner (control, a decision, a trade-off between goals), do not dispatch. Collect these and ask at most four in one `mcp__fyr__ask`, each with options, the recommended one first.

For **sprawl** and **guidance-gap**, the fix is usually in what Fyr tells agents or in a one-time note from Fyr. Check what the earlier fixes already did before adding more text: more instructions are also noise.

## 5. Follow through

End the turn after dispatching. The threads report with `thread_send`, and Fyr reports merges and deploys.

On each report:

- Check that the PR fixes the confirmed cause and does not trade away control.
- Check that its CI is not red for a reason that should itself be a finding.
- Leave merging to Fyr and the owner.

When a fix is live (the prod stage, see the playbook), run `signals.py` again over a window after the deploy. The lead it targeted should fall. If it did not, reopen the issue with the numbers.

## 6. Report

Write the report as the thread's last message. If the owner asked for something to share, publish it as a page.

1. **Verdict, in one line:** how much of the friction was Fyr, how much load or outside causes, how much workflow.
2. **Scorecard:** this run against the last one. Show:
   - owner prods;
   - short wakes as a share of runs;
   - unasked-question nudges and the share that were false;
   - merge refusals;
   - holds;
   - PRs merged and their median lead time;
   - median owner wait per decision.
3. **Top causes, at most 6:** the goal each hurts, its cost, and its status (dispatched to `t_…`, fixed by #N, waiting for your decision, or dropped and why).
4. **What changed since the last run:** the fixes that went live and whether their signal fell.

If the argument was `daily`, arm the next run with `mcp__fyr__watch {when: {kind: "at", at: <tomorrow 16:30 local>}, then: "notify", note: "/fyr-improve 24h daily"}`. Then call `mcp__fyr__settle_request` when nothing waits.

## Known traps

- The `[Fyr]` notices an agent receives are Fyr's own output: a noisy one is a finding even if every agent handled it right.
- "CI passed", "merged" and "merge succeeded" notices to an agent that has nothing left to do are the largest source of short wakes. Before treating a short wake as noise, check what the agent did in that turn.
- The owner's "status?" in a thread is the plainest sign that clarity failed: find out what the row showed at that moment.
- A long first message becomes a truncated title. A title alone is not a cause: check whether the thread's focus also drifted.
- Holds and freezes are what an overloaded host looks like. Count them, but the cause is usually concurrency (too many heavy jobs) or a leak, not the gate.
- Close what you start: nix shells and other helpers you launch should run under `timeout`.

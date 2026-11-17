# V0.164 Undeclared heavy work: detect it, steer the agent, count it, pause it only under memory pressure

Date: 2026-11-17 · Status: **designed; the owner answered Q1–Q4** · For the implementation thread · Builds on V0.163 (heavy jobs as a share of the host, CPU and memory apart), which must be on main first.

Check the number at PR time: V0.157–V0.161 are the auth slices, V0.162 the pin fix, V0.163 the percentage change.

## 1. Why

- The gate (V0.144) knows heavy work by command patterns. After it, 24 % of the heavy runs of 17 Nov still ran without asking (52 % before).
- On 17 Nov two such jobs, a layout e2e run at 2.8 GB and `yarn pre-push:check`, drove a memory thrash, and systemd-oomd killed agents.
- Every agent already runs in its own cgroup (`agents/<thread>`), and the host reads each one's CPU and memory every 5 s whether or not the thread asked (decision 0029 S1). So the host can see heavy work that no pattern names.

The owner agreed to four things: detect from the cgroups, steer the agent once, count the use in the budget, and contain it only under real memory pressure, by a freeze and never a kill. This design refines those four and adds nothing.

## 2. Words

| Word | Meaning |
|---|---|
| **asked** | The thread called `slot_acquire` and Fyr admitted it (V0.163: "Admitted: …"). The tool names stay. |
| **undeclared** (owner, Q2) | Heavy work of a thread that was not admitted for it. The owner sees this word; the agent's message says what to call. |
| **idle level** | What a thread uses between jobs: its CLI and the servers it keeps running. In the code: `baseline`. |
| **over** | A thread's use above its idle level passes a threshold. |
| **flagged** | Over for long enough, with no admission. One flag is one **episode**: from the flag to the moment the use is back down. |

"Slot" and "reservation" are not used towards the owner (V0.163).

## 3. The owner's answers (17 Nov)

1. **Loudness:** iteration 1's A for the row and the rail (text, a hatched part of the meter, a count), B for the open thread (a strip). The thread stays in Working; no Needs you cause and no notification.
2. **Word:** "undeclared".
3. **Dismiss:** hides the mark until the work stops. The use still counts, and the memory guard may still pause it.
4. **Pause order:** undeclared work is paused before a job that asked.

## 4. Detection

### 4.1 What is read

Per thread cgroup under `agents/`, at the sampler's tick (every 5 s, `cgroup.Sampler`), for every thread, admitted or not:

- **CPU:** cores between two readings (`cpu.stat` usage_usec), as today (`ThreadLoad.CPU`).
- **Memory:** the working set, `memory.current` less `inactive_file` from `memory.stat`, the same reading `agents/` as a whole got in the V0.146 hotfix. The raw `memory.current` holds page cache: a `git grep` over a large tree would look like a heavy job. `ThreadLoad.Memory` keeps its raw meaning for the row; the detector gets a new field.

`agents/probe` and `agents/self-deploy` are never read for this.

### 4.2 The idle level (per thread, in memory only)

- It has two numbers: memory and CPU.
- A reading is **quiet** when the thread is not admitted, its memory is under idle level + half the memory threshold, and its 15 s CPU mean is under half the CPU threshold.
- The idle level is the **median of the quiet readings of the last 10 min**. Only quiet readings enter it, so a running job never becomes the idle level, however long it runs.
- The first reading of a thread seeds it. A thread is read within 5 s of its CLI's start, long before a job.
- A slowly growing CLI (a long transcript, one more MCP server) moves the idle level with it, because each step is under half a threshold.
- After fyrd restarts, the first reading seeds again. Heavy work already running then is missed, not flagged falsely. The real headroom rule of admission still sees it.

### 4.3 The thresholds (host settings, shares of the host)

| Setting | Default | On the desktop (4 cores, 14 GB for agents) |
|---|---|---|
| `undeclaredCpu` | 0.25 of the agents' cores | 1.0 core |
| `undeclaredMemory` | 0.08 of the agents' memory | 1.1 GB |
| `undeclaredAfter` | 45 s (30–120) | |
| `undeclared` | `"on"`; `"off"` turns all of V0.164 off on this host | |

Capacity is V0.142 (1)'s. No absolute number is in the rules (decision 0029 section 0 (a)).

### 4.4 Over, flagged, ended

The window W is `undeclaredAfter`, cut in three equal parts (15 s each by default).

- **CPU over:** in each of the three parts, the mean cores above the idle level are at or above the CPU threshold. One slow reading inside a part does not break it; a burst shorter than about two parts never flags.
- **Memory over:** the lowest working set of the whole window, less the idle level, is at or above the memory threshold. A peak that falls back does not flag.
- **Flagged:** CPU over or memory over, the thread is not admitted now, and the host has a provider and `undeclared` is on. A thread that is queued (it asked and waits) and runs its job anyway is flagged like any other.
- **Ended:** both measures are under half their threshold for W, or the thread is admitted, or its CLI ends. Half is the hysteresis: a job that dips does not end and restart its episode.
- **Use** of a flagged thread, the number shown and counted: CPU is the mean cores above the idle level over W; memory is the working set now less the idle level.

### 4.5 The commands behind it

At the flag, and again at each tick while it lasts (the newest wins), the host names the command:

1. Take the thread's cgroup's processes that started after the episode's first over reading less 15 s, and whose parent is older or outside the cgroup. These are the **job roots**. The CLI, its MCP servers and anything else that was already there are **residents** and are never roots.
2. Of the roots, take the one whose subtree holds the most memory (the sum of its processes' RSS).
3. Its command is the newest Bash command the gate hook saw from this thread whose text is found in the root's command line (the same containment S5 uses, the other way round). When none matches (a process an MCP server started), it is the root's own command line.
4. The text is cut to 300 characters. It stays on the host (KV), and only the owner sees it.

An episode's record, kept when it ends (KV `undeclared`, the newest 100): thread, project, command, start, end, peak memory above the idle level, mean cores, whether the agent was told, whether it was paused.

### 4.6 Suggestions

- An episode whose command matches no heavy pattern makes or updates a **suggestion** (KV `heavy-suggestions`, at most 30, the newest first).
- The proposed pattern is the segment of the command that the heaviest root runs (`heavy.segments`, V0.144 (1)), reduced to its program and its first argument that is not a flag, escaped: `yarn pre-push:check` → `yarn pre-push:check`; `node test/layout.e2e.mjs --shard 2` → `node test/layout\.e2e\.mjs`. The proposed kind is `heavy.KindOf` of it.
- Episodes with the same proposed pattern are one suggestion: how often it was seen, in how many threads, the last thread and time, and the largest memory, cores and length.
- An episode whose command already matches a pattern makes no suggestion (the job was released early, or the gate is off).
- **Add** (owner only, `host.heavySuggestion {id, do: "add", pattern, kind}`): the pattern, as the owner edited it, joins the host setting `heavyAdded`, which the matcher reads after `heavyCommands` or the defaults. From then on the gate refuses the command (V0.144 (2)). A pattern that does not compile is an error on the row.
- **Ignore** (`do: "ignore"`): the suggestion goes, and its pattern is never suggested again on this host (kept in the same KV).
- **Remove** (`host.heavyRemove {pattern}`): takes an added pattern out again.

## 5. Steering: one message

- When a thread is flagged, its agent gets one notice, `Urgency: Next`: at the main agent's next tool call, or with the next message that starts a run. It never wakes an idle agent: waking it only to be told costs a turn and stops nothing.
- **At most one per episode and at most one per run.** A second episode in the same run gets none. An episode that lasts across runs is told once.
- A subagent runs most undeclared work (91 of 177 runs on 16–17 Nov), and the main agent may make no tool call while it waits for one. So the first Bash call of a subagent of a flagged thread also carries the text, as `additionalContext` of the gate hook's answer, once per episode. It does not use up the main agent's notice.
- The text, with the host's numbers (`Key: "heavy.undeclared"`):

  > This thread runs heavy work Fyr did not admit: about 2.8 GB memory (20 % of desk) and 2.1 cores (52 %) for 45 s, from `node test/layout.e2e.mjs`. Before a heavy job call mcp__fyr__slot_acquire {"kind":"e2e","command":"node test/layout.e2e.mjs"}, run it once admitted, then call mcp__fyr__slot_release; a subagent's or a background run counts too. Call it now for the job that runs. Fyr counts this use against the host either way, so other jobs wait; nothing is stopped, but if memory runs short Fyr may pause it.

  For a queued thread the second sentence is: "You are queued and not admitted yet: do not run the job until Fyr tells you." Only the dimension that is over is named when the other is under a tenth of its threshold.
- The conversation shows it as one machine line: "Fyr told the agent: heavy work it did not ask for (2.8 GB, 2.1 cores); ask before a heavy job". It unfolds to the whole text.

## 6. Counting it in the budget

- In V0.163's `Decide`, each flagged thread's use (4.4) is one more `Need`, added to `used` beside the admitted jobs. It is **not** added to the count of held jobs: "one job always runs" looks at admitted jobs only, so a job that asked can always start when none runs.
- It counts from the flag to the end of the episode, dismissed or not. What is marked is what is counted.
- **A thread's own undeclared use does not count against its own request.** When a flagged thread asks, admission is decided as if its use were not there: its request stands in for it. So coming clean is cheap. Once admitted, it is a normal job and its episode ends.
- The wait reason names it when it is what binds: "needs ≈ 45 % CPU, 20 % free (Map perf uses 52 % undeclared)"; with several threads, the largest and "+1".
- `heavy` in `GET /api/load` gains, per host, `undeclared: [{thread, cpu, mem, cpuPct, memPct, since, command, told, dismissed, paused}]` and `undeclaredCpuPct`, `undeclaredMemPct`. The used percentages include them.
- AIMD (V0.154, V0.163) is untouched. The memory rules of admission already read the real headroom, so they hold whether or not the detector saw the work.

## 7. Containing it: a freeze, only under real memory pressure

- **When:** only while S5's condition holds (V0.151 (2)): the memory headroom is under the reserve, or the stall level is critical. Never for CPU.
- **What first (owner, Q4):** the newest flagged thread, by the start of its episode, whose memory is over. A job that asked is frozen (V0.151) only when no such thread is left to freeze. One thing is frozen at a time on a host, across both kinds, and each episode at most once.
- **Never** when it would help nobody: with no admitted job and no other flagged thread running, nothing is frozen. A freeze frees no memory; it stops growth and gives the others room to finish.
- **Never** the CLI, its MCP servers or any resident, and never the self-deploy. The agent is not blocked: it may go on, and a Bash call into the frozen job simply waits for the thaw.
- **How:** the job roots (4.5) and all their descendants move into `agents/<thread>/undeclared/`, a sibling of S5's `job/` so a declared and an undeclared job never mix, and that cgroup gets `cgroup.freeze` 1. `JobGroups` gets the same walk with roots instead of a needle. A process that ends during the move is skipped.
- **Thaw:** V0.151 (3)'s rule: the headroom is at least the reserve + 5 % and the stall level is ok for 30 s; after 10 min at most; or when the episode ends, the thread is admitted or the thread is released. The cgroup is removed once empty.
- **Dismiss does not exempt** a thread (owner, Q3).
- The agent is told at its next tool call, in V0.151's words: "Fyr paused your undeclared heavy work (`node test/layout.e2e.mjs`): memory is short on this host (memory: 6 % free). Its processes are frozen, not killed, and resume when memory has recovered for 30 s, within 10 min at most. A timeout inside the job may fire after it resumes; check its result. Ask with mcp__fyr__slot_acquire before a heavy job." And at the thaw: "Your heavy work runs again: Fyr thawed it."

## 8. What the owner sees

Mockups: `mock/i2.html` (final), `mock/i1.html` (the two variants the owner chose between). The new CSS is `mock/rogue.css`. Desktop and phone are the same unless a reason is given here.

1. **Row** (one component, `web/src/row-ui.tsx`): the second line of a flagged thread starts with the warning mark and "undeclared · 2.8 GB · 2.1 cores" in the beam ink, before what the thread does. Paused: "undeclared · paused: memory" with the pause mark. Only the measure that is over is written when the other is under a tenth of its threshold. A dismissed thread's row has no mark.
2. **Rail** (desktop only; the phone shows no host load, V0.99 (3)): the host's meter draws the undeclared share as a hatched beam part after the admitted part. Under the meter a small beam button holds the mark and the number of flagged threads that are not dismissed. One thread: a press opens it. Several: a menu of them (`menu-ui.tsx`), each with its use. The meter's tooltip adds "undeclared 52 %" per resource and the threads' names. The hatched part stays for a dismissed thread; the button does not count it.
3. **Thread:** a strip under the header, on both fronts: "Heavy work Fyr did not admit · 2.8 GB (20 % of desk) · 2.1 cores (52 %) · 3 min", then the command, "counted against desk's heavy jobs" and "agent told 14:29". Buttons: **Add as heavy job…** (opens Settings → Automation at this command's suggestion; absent when the command already matches a pattern) and **Dismiss**. On the phone the buttons sit under the text at 44 px (touch). Paused, the strip turns to the harbour wash, says "Paused by Fyr: memory is short on desk (6 % free). Frozen, not killed; it resumes when memory has recovered for 30 s, within 10 min at most." and has no buttons.
4. **The queue section:** the wait reason of 6.
5. **Settings → Automation → Heavy jobs**, above today's list of learned jobs:
   - **Undeclared now:** a row per flagged thread: title, project, command, use, how long, "agent told 14:29". **Open** and **Dismiss**; a dismissed one stays, dimmed, with "dismissed, still counted" and **Show again**. The group is absent when no thread is flagged.
   - **Suggested heavy jobs:** per suggestion the command, "Seen 3 times in 2 threads, last 14:29 in Map perf · up to 2.8 GB, 2.1 cores, 4 min", the pattern in a field, the kind, **Add** and **Ignore**. Absent when there is none.
   - **Heavy jobs you added:** each pattern with its kind, when it was added and how often the gate has refused it since; **Remove**. Absent when there is none.
   - **When a thread counts as heavy:** the three thresholds, each with what it is on this host now ("Now 1.0 of desk's 4 cores"). They save like the page's other fields.
   - With several hosts the page's host picker chooses the host, as for the learned jobs.
6. **Dismiss** is the owner command `host.undeclaredDismiss {thread, on}` (owner only, with a receipt). It holds for the episode; a later episode of the same thread is flagged again.
7. A read-only front (an offline host, a viewer) shows all of it and disables the buttons, as elsewhere in Settings.

## 9. Edge cases

| Case | What happens |
|---|---|
| **A subagent** runs the job | It is in the thread's cgroup, so it is the thread's use. The subagent's first Bash call carries the message too (5). The thread's admission covers it (V0.144 (2)). |
| **A background task**, or a job that outlives the run | It is still in the cgroup: flagged and counted while it runs. The notice waits for the agent's next tool call or run; no wake. |
| **The agent's own baseline** (the CLI: 0.01–0.05 cores, 0.7–1 GB raw; MCP servers, language servers, a dev server left running: 1–3 GB in all) | It is the idle level (4.2), learned per thread. Nothing absolute. |
| **Chrome for the live view and `show`** | It is fyrd's child in `server/`, not in a thread's cgroup. It is never read here. |
| **A browser the agent drives** (an MCP server's Chrome) | It is the thread's work. It flags only if it passes a threshold for W. That matches V0.144, where anything that starts headless Chrome is heavy. |
| **A dev server** that takes 2 GB and then idles | Memory over: flagged once, counted while it lives. The owner dismisses it; the agent can also ask for it, and V0.163's idle hold then counts its memory and almost no CPU. |
| **A short spike** (a typecheck, one test file, `go build`) | Under two window parts of CPU, or memory that falls back within W: never over. |
| **A loaded host**, where the same typecheck takes 60 s | It gets few cores then, so its mean stays under the threshold. Sustained cores are what is measured, not wall time. |
| **Admitted, but far above its learned cost** | Not undeclared. V0.148 (5) already tells the thread once and counts the live use; V0.151 may pause it. Nothing new. |
| **Admitted for a cheap job, and a second heavy one beside it** | The hold's live use is the thread's cgroup above its idle level, so it is counted already. Not flagged. |
| **Released, and the job still runs** | Not admitted and over: flagged after W. |
| **Queued, and runs it anyway** | Flagged; it keeps its place in the queue; its own use does not count against its own request (6). |
| **The same command again in a later run** | A new episode, a new message, the same suggestion counted once more. |
| **fyrd restarts during an episode** | The idle level is seeded from the first reading, so the running work is missed until it ends. The record of the open episode is closed at the restart. |
| **Linux without cgroups, and macOS** | See 10: detect, tell, count; no freeze. |
| **A host with no provider at all**, or `undeclared: "off"`, or heavy jobs off | Nothing of V0.164 runs. |
| **The owner's own processes** outside `agents/` | Never read. |

## 10. Hosts without cgroups: count only, no freeze

- On Linux without the tree, and on macOS, a thread's use is its process tree under its CLI's pid, read every 5 s: on Linux from `/proc/<pid>/stat` and `statm`, on macOS from one `ps -A -o pid=,ppid=,rss=,time=`.
- Cores are the growth of the tree's CPU time over the gap (the time of children that ended in between is lost, so it reads low). Memory is the sum of RSS (shared pages count more than once, so it reads high).
- Detection, the message, the suggestions, the counting and all of section 8 work from these numbers.
- **Nothing is frozen:** there is no job boundary to freeze without the CLI. The strip and the row never say "paused" there.
- A host whose provider gives no capacity keeps the fixed count (V0.146 (2)); nothing of V0.164 runs there.

This part may land as a second commit of the same PR. If the process-tree reader grows large, ship sections 4–9 for cgroup hosts first and say in the PR that section 10 follows, with its own spec amendment.

## 11. The spec row (for docs/SPEC.md; adjust to what was built)

> | V0.164 | → V0.163, V0.151, V0.148 (5), V0.144, V0.142, decision 0029 section 5 (the option not chosen then). **A thread that runs heavy work without asking is seen from its cgroup, its agent is told once, its use counts against the host, and under real memory pressure it is paused, never killed.** *Amends decision 0029 Q2 (work that did not ask now counts), V0.151 (2) (what is paused first) and V0.142 (7) (Settings).* (owner, 17 Nov: "do it"; two undeclared jobs, a layout e2e run at 2.8 GB and `yarn pre-push:check`, drove a memory thrash and systemd-oomd killed agents; the pattern gate still missed 24 % of the heavy runs.) (1) **Idle level.** Per thread the host keeps the median of its quiet readings of the last 10 min (every 5 s: cores, and the working set, `memory.current` less `inactive_file`); a reading is quiet when the thread is not admitted and is under its idle level + half a threshold; the first reading seeds it. (2) **Flag.** Thresholds are host settings, shares of what the host gives its agents: `undeclaredCpu` 0.25, `undeclaredMemory` 0.08, `undeclaredAfter` 45 s (30–120), `undeclared: "off"` turns it off. A thread that is not admitted is flagged when, over the window cut in three parts, its mean cores above the idle level reach the CPU threshold in every part, or its lowest working set above the idle level reaches the memory threshold. The episode ends when both are under half their threshold for the window, the thread is admitted or its CLI ends. (3) **Command.** The processes of the cgroup started in the episode whose parent is older (never the CLI or what it kept running) are the job's roots; the heaviest root's command is the thread's newest Bash command found in its command line, else that command line, cut to 300 characters. Ended episodes are kept (KV `undeclared`, 100). (4) **Suggestions.** A command that matches no heavy pattern becomes a suggestion (its program and first argument, escaped; seen how often, where, how large); the owner adds it (`host.heavySuggestion`, into the host setting `heavyAdded`, read with the patterns, so the gate refuses it from then on), ignores it for good, or removes an added one (`host.heavyRemove`). (5) **Told once.** The agent gets one notice per episode and at most one per run, at its next tool call, never a wake: "This thread runs heavy work Fyr did not admit: about 2.8 GB memory (20 % of desk) and 2.1 cores (52 %) for 45 s, from `…`. Before a heavy job call mcp__fyr__slot_acquire {…}, … Fyr counts this use against the host either way, so other jobs wait; nothing is stopped, but if memory runs short Fyr may pause it."; a subagent's first Bash call of the episode carries it too. (6) **Counted.** A flagged thread's use (mean cores over the window, working set now, both above the idle level) is added to the used CPU and memory of admission (V0.163), not to the number of running jobs; a thread's own use does not count against its own request; the wait reason names it ("needs ≈ 45 % CPU, 20 % free (Map perf uses 52 % undeclared)"); `GET /api/load` adds `heavy.undeclared` and the two shares. (7) **Paused.** Only while memory is short (V0.151 (2)'s condition): the newest flagged thread whose memory is over has its roots and their descendants moved to `agents/<thread>/undeclared/` and frozen, before any job that asked; one thing frozen per host, each episode once, not when no other heavy work runs, never the CLI; it thaws by V0.151 (3)'s rule or when the episode ends. Its agent is told as in V0.151. (8) **Shown** (desktop and phone, one component each): the row's line starts "undeclared · 2.8 GB · 2.1 cores" (or "undeclared · paused: memory") in the beam ink; the rail's meter draws the undeclared share hatched, with a count under it that opens the thread or a menu of them (desktop only: the phone shows no host load); the thread has a strip with the use, the command, "counted against desk's heavy jobs", when the agent was told, Add as heavy job… and Dismiss; Settings → Automation → Heavy jobs lists Undeclared now, Suggested heavy jobs, Heavy jobs you added and the thresholds. Dismiss (`host.undeclaredDismiss`, owner only) hides the mark for the episode; the use still counts and may still be paused. (9) **Without cgroups** (Linux without the tree, macOS): the use is the process tree under the CLI (`/proc`, `ps`); (1)–(6) and (8) hold; nothing is frozen. (10) **Not flagged:** an admitted thread above its learned cost (V0.148 (5)); the self-deploy; the probe; fyrd's own Chrome. |

Also amend, in their rows: decision 0029 (a line under section 5 and the owner's answers: Q2 reversed on 17 Nov by V0.164), V0.151 (2) ("*Amended by V0.164: undeclared work is paused first*") and V0.142 (7).

## 12. Test plan

Red first. Go tests use a fake cgroup tree in a temp dir (as `internal/cgroup/*_test.go` do) and a fake clock; no test starts a real fyrd with the owner's `FYR_*` environment.

### 12.1 Detection (`internal/cgroup`, a new `undeclared_test.go`)

| # | Test | Expect |
|---|---|---|
| D1 | A thread at 0.03 cores and 0.9 GB for 10 min | Never flagged; the idle level is 0.9 GB. |
| D2 | From idle, 2 cores for 40 s on a 4-core host (threshold 1.0, W 45 s) | Not flagged at 40 s; flagged at the first tick where all three parts are over (45–50 s). |
| D3 | 4 cores for 20 s, then idle | Never flagged (a short spike). |
| D4 | 2 cores with one 5 s reading at 0 inside a part | Still flagged: the part's mean holds. |
| D5 | Memory +1.5 GB held for 45 s at 0.1 cores (threshold 1.1 GB) | Flagged on memory; the use names memory only. |
| D6 | Memory +3 GB for 20 s, back to idle | Never flagged. |
| D7 | `memory.current` +4 GB of which `inactive_file` is 3.8 GB | Never flagged: page cache is not the working set. |
| D8 | Thresholds as shares: the same trace on a 16-core, 64 GB host | Not flagged at 2 cores (threshold 4); flagged at 5. |
| D9 | The settings `undeclaredCpu` 0.5, `undeclaredAfter` 30, `undeclared: "off"` | Each changes the result as it says. |
| D10 | An admitted thread at 3 cores | Never flagged; its readings do not enter the idle level. |
| D11 | Admitted, released, the job goes on at 2 cores | Flagged W after the release. |
| D12 | Flagged, then 0.6 cores (over half the threshold) for 2 min | Still the same episode. Then 0.2 cores for W: ended. |
| D13 | A job at 2 cores for 30 min | One episode; the idle level is unchanged afterwards. |
| D14 | The idle level grows from 0.8 to 1.6 GB in steps of 0.2 GB every 5 min | Never flagged; the idle level follows. |
| D15 | A thread whose first reading is 3.8 GB (a restart mid-job) | Not flagged; idle level 3.8 GB. |
| D16 | `agents/probe`, `agents/self-deploy` at 4 cores | Never flagged. |
| D17 | A thread with an MCP server and a dev server at 2.4 GB from its start, flat | Never flagged (no false flag on a baseline). |

### 12.2 The command and the suggestions

| # | Test | Expect |
|---|---|---|
| C1 | A fake `/proc`: the CLI, an old MCP server, and a new `zsh -c "yarn pre-push:check"` with children | The root is the zsh; the command is the Bash command the gate saw. |
| C2 | No Bash command matches the root | The root's own command line, cut to 300 characters. |
| C3 | Two episodes with `node test/layout.e2e.mjs --shard 1` and `--shard 2` in two threads | One suggestion: seen 2 times in 2 threads, pattern `node test/layout\.e2e\.mjs`, kind e2e. |
| C4 | The command matches a default pattern | An episode record, no suggestion. |
| C5 | Add with an edited pattern | It is in `heavyAdded`; `heavy.Matcher` now names its class; the gate refuses the command without an admission; the suggestion is gone. |
| C6 | Add with a pattern that does not compile | An error; nothing stored. |
| C7 | Ignore, then the same command again | No new suggestion. |
| C8 | Remove | The gate lets the command through again. |
| C9 | Any of the three by a caller who is not the owner | Refused. |

### 12.3 The message (`internal/engine`, `internal/core`, `internal/agent`)

| # | Test | Expect |
|---|---|---|
| N1 | A flag in a run | One notice with key `heavy.undeclared`, urgency Next, the text of 5 with the host's numbers and the kind of the command. |
| N2 | The episode goes on for 10 min in the same run | Still one. |
| N3 | It ends, and a second episode starts in the same run | No second notice. |
| N4 | A new run, a new episode | One more. |
| N5 | One episode across two runs | One in all. |
| N6 | The agent is idle (a background task runs the job) | No wake; the notice is taken at the next run. |
| N7 | A subagent's Bash call (hook input with `agent_id`) while its thread is flagged | The gate hook's answer has the text as `additionalContext`, once per episode; the main agent's notice is still queued. |
| N8 | A queued thread | The text says it is queued and not admitted. |
| N9 | The conversation's line | The event renders as "Fyr told the agent: …" (web unit test). |

### 12.4 Counting (`internal/slots`, beside V0.163's tests)

| # | Test | Expect |
|---|---|---|
| B1 | One admitted job at 40 % CPU, a flagged thread at 50 %, the head needs 30 % | Waits; the reason is "needs ≈ 30 % CPU, 10 % free (… uses 50 % undeclared)"; binding CPU. |
| B2 | The same without the flagged thread | Admitted. |
| B3 | Memory: a flagged thread at 5 GB of 14, the head needs 6 GB, one admitted job at 4 GB | Waits on memory, with the reason naming it. |
| B4 | A CPU-heavy flagged thread and a memory-heavy head | Admitted: the dimensions are apart (V0.163). |
| B5 | No admitted job, a flagged thread at 90 % CPU, a head arrives | Admitted: one job that asked always runs. The memory rules can still hold it. |
| B6 | The flagged thread itself asks for a job that needs 50 % while it uses 50 % undeclared | Decided without its own use; admitted when the rest fits; its episode ends. |
| B7 | A dismissed flagged thread | Counted as before. |
| B8 | The episode ends | The next tick admits what now fits. |
| B9 | `GET /api/load` | `heavy.undeclared` lists the thread with its use, shares, command, `told`, `dismissed`; the used percentages include it. |
| B10 | A host without a provider | The fixed count; nothing listed. |

### 12.5 The freeze (`internal/engine`, `internal/cgroup`)

| # | Test | Expect |
|---|---|---|
| F1 | Memory fine (headroom 30 %, stall ok or elevated), a flagged thread at 6 GB | Nothing frozen. |
| F2 | Slowdown 5×, memory fine | Nothing frozen (never for CPU). |
| F3 | Headroom under the reserve; a flagged thread (memory over) and an admitted job | The thread's roots and their descendants are in `agents/<thread>/undeclared/`, `cgroup.freeze` is 1; the CLI and the MCP server are still in the thread's cgroup; the admitted job is not frozen. |
| F4 | Two flagged threads | The one whose episode started last is frozen; only one. |
| F5 | A flagged thread that is over on CPU only, and an admitted job | The thread is not frozen; V0.151's rule picks among the admitted jobs. |
| F6 | The only heavy work on the host is one flagged thread | Not frozen. |
| F7 | Frozen, then headroom at reserve + 5 % and stall ok for 30 s | Thawed; the agent is told. Before 30 s: still frozen. |
| F8 | Frozen for 10 min with memory still short | Thawed, and not frozen again in this episode. |
| F9 | Frozen, and the thread asks and is admitted; or its CLI ends | Thawed; the cgroup is removed once empty. |
| F10 | A dismissed thread under pressure | Frozen like any other. |
| F11 | A root exits during the move | No error; the rest moved. |
| F12 | A host without cgroups under pressure | Nothing frozen; the thread is still flagged and counted. |
| F13 | `paused` in the load and the two notices (paused, runs again) | As in 7. |

### 12.6 Without cgroups

| # | Test | Expect |
|---|---|---|
| P1 | A fake `/proc` tree under a CLI pid growing by 2 core-seconds per second | Flagged after W; the use is about 2 cores. |
| P2 | A canned `ps` output (macOS form) for two threads | Each thread's memory is the sum of its own tree's RSS. |
| P3 | D1, D3 and D6 with this reader | The same results. |

### 12.7 Web (`web/test`, node tests; then `web/test/layout.e2e.mjs`)

- Unit: the row's text for flagged, memory only, paused and dismissed; the meter's two parts and its tooltip; the rail button's count and its menu with two threads; the strip's text and its buttons (no Add when the command matches a pattern; none while paused); the wait reason; the Settings lines.
- A mock state `?mock=rail&undeclared=1` (and `=paused`, `=2` for two threads, one dismissed) feeds all of it.
- Layout, at 1280 × 800 and 390 × 844, light and dark: the row keeps its height; the mark and the word are in the beam ink; the rail's middle is no taller than V0.152 allows with the button present (it does not scroll at 1000 px); the strip is one block under the header and the conversation keeps its scroll; on the phone every button of the strip and of the new Settings rows is at least 44 px high; nothing is wider than its screen.
- Desktop and phone parity: the same texts in the row, the strip and Settings (one component each); the rail part is desktop only, with the reason next to the code.

### 12.8 By hand, in a scratch fyrd (temp data dir, no `FYR_*` from the owner's environment)

1. Start a thread whose agent runs `stress-ng --vm 1 --vm-bytes 2G --timeout 180s` (or a Node loop) in Bash without asking. After about 45 s: the row, the rail and the strip show it, the agent's next tool call carries the message once, and a second thread's `slot_acquire` waits with the reason naming the first.
2. Add its suggestion in Settings. Run the command again: the gate refuses it.
3. Lower `slotMemoryReserve`'s headroom artificially (a test hook or a small `agents/memory.high` in the scratch tree) with one admitted job running: the undeclared work is paused, the row and the strip say so, and it resumes 30 s after memory is back.
4. Screenshots of all of section 8 at desktop and phone width go in the PR.

## 13. Where it goes in the code

| What | Where |
|---|---|
| Working set per thread, the reading history, idle levels, the flag, episodes | `internal/cgroup`: a new `undeclared.go` beside `load.go`, driven from `Sampler.Tick`; publish its view lock-free like `Jobs.View` (V0.147: read files outside the lock, never call the engine under it). |
| The roots, the move and the freeze | `internal/cgroup/jobcg.go`: the same walk with roots, a second child directory. |
| The extra `Need` in admission, the own-use rule, the reason | `internal/slots/admit.go` (`Decide`) and the `pool.Admit` closure in `cmd/fyrd/main.go`. |
| The freeze order, the thaw, the notices | `internal/engine/slots.go` (the guard that freezes today), `internal/core/feed.go` (`slotNotices`). |
| The subagent's copy | `internal/agent/hooks.go`, beside `GateNudge`. |
| The settings, `heavyAdded` | `cmd/fyrd/config.go`, `internal/heavy`. |
| The commands | Beside `host.jobReset`. |
| Row, rail, strip, Settings, the mock | `web/src/row-ui.tsx`, `load.ts`, `load-ui.tsx`, `thread.tsx`, `phone.tsx`, `settings-ui.tsx`, `mock.ts`; CSS from `mock/rogue.css`. |
| The process-tree reader | `internal/cgroup`: `proc` and darwin files beside `host_darwin.go`. |

## 14. Not in V0.164

- Killing anything, or refusing an agent's tool calls because it is flagged.
- A freeze on hosts without cgroups.
- Adding patterns without the owner.
- Counting swap, I/O or network.
- A Needs you cause or a push notification for a flag (owner, Q1).
- Job records and learned costs from undeclared episodes: a class is learned only from admitted runs.

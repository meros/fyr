# 0022 Keep the drain, with a forced restart that resumes the cut turns

Date: 2026-11-16 · Status: accepted (issue 76) · Amends V0.13 (4) · Spec row V0.83

**Context.** Today the `claude` CLI is a child of fyrd and talks to it over pipes (`internal/claude/client.go`), so a restart of fyrd ends every running turn. A deploy that changes fyrd therefore drains first (V0.13 (4)): it waits until no thread runs, up to 30 minutes. A deploy that changes only the web client already switches without a restart (V0.81, issue 75). The question was whether a running turn should survive a restart, so that a Go change does not have to wait either.

**Options.**

| # | Option | Cost | Chosen |
|---|---|---|---|
| 1 | **A small runner process holds the CLI pipes.** One long-lived process per host (or per thread) owns each CLI's stdin and stdout and relays them to fyrd over a Unix socket; fyrd reconnects after a restart and replays what it missed. | A protocol between fyrd and the runner that must stay stable across versions; the runner must almost never change; process ownership moves out of fyrd's unit (KillMode). | no, for now |
| 2 | **Pass the pipes across the restart.** fyrd hands its open descriptors to the new fyrd (systemd's FD store, or exec with inherited descriptors). | Every restart path (crash, rollback, launcher) must keep the descriptors; the in-flight requests on each pipe must be handed over; the state that goes with them needs a format both versions read. | no, for now |
| 3 | **Restart without a drain and resume.** Kill the turn, restart, send "continue" with `--resume`. | The tool call in flight and its output are lost; the conversation stays. | as the owner's way out, not the default |
| 4 | **Keep the drain.** Accept the wait for Go changes; V0.81 already removes it for client-only deploys. | A deploy can wait up to 30 minutes for a long turn. | **yes, the default** |

**Decision.**

1. **The drain stays the default (option 4).** A deploy with server changes still waits until no thread runs, and the banner says which threads it waits for (V0.39).
2. **The owner can force it (option 3 on demand).** While the deploy waits for at least one running thread, the update banner offers **Restart now**. A press opens Fyr's confirmation (V0.68), which names the threads whose turns end and says each gets "continue"; its primary button is red. After the confirmation the client sends `host.deployNow`, an owner-only host command, and the drain loop stops at once and switches.
3. **The cut turns resume.** The deployer stores the threads that were running at the press (`cut` in `pending_deploy`). When the new daemon is up, each of them gets the message "continue" from the owner through the normal send path, so it shows in the conversation and starts a run; the session id is stored, so the run resumes the session with `--resume`. A thread the owner settled or archived in the meantime gets nothing.
4. **Options 1 and 2 are declined for now, and option 3 is not the default.** The decision comment on issue 76, 16 Nov: "Options 1 to 3 are declined for now: the handover and the runner cost a process-ownership change (KillMode), a hand-over of in-flight requests, and a state format that must stay compatible across versions." Option 3 lives on only as the owner's explicit press in point 2. Options 1 and 2 can each come back as its own decision; option 1 would also give the per-thread cgroups of issue 74 a place to live.

**Consequences.**

- A forced restart loses the tool call that was in flight in each cut turn and its output; the agent sees "continue" and picks the work up from the conversation. The confirmation says so in product words before the owner presses.
- The interrupted processes end by the normal stop of the unit (SIGINT to the process group, `KillSignal`); no new kill path exists.
- The requester's own thread is cut too when it runs; it gets "continue" at once and the deploy's result later, after the probation (V0.13).
- The "continue" command id is derived from the build and the thread, so a second start of the same build (a crash in the probation window) sends nothing twice (I7).
- A thread that cannot take a message after the restart (an open question, a handover) keeps waiting for the owner, as any send would.
- The client-only deploy (V0.81) has no drain, so it never shows the button.

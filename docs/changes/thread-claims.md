# V0.thread-claims: what a thread waits on is one ordered list, and every surface says the same words for it

→ SPEC 6.2a (new). Amends V0.101, V0.143, V0.185, V0.203, V0.watch-waits, V0.gate-hold-visible, V0.waiting-for-command, V0.189.

The cause: where a thread stands (Your move, Moving without you, Finished) was decided in three places that had grown apart. The attention was one chain of conditions; the snooze until work is done and "settle when done" asked two other functions whether the thread needs the owner and whether its work goes on; the placement invariant was a copy of the chain, so it agreed with it whatever it did. Each new wait was added to one of them. And each surface had its own word for a state: the row said "Merge", the header chip "Blocked", the push "Done", the section "Finished".

What was wrong, as found by reading both sides against each other:

1. A thread in a handover read Finished (or Failed, the usual reason for the handover).
2. A notice that woke an idle agent, and a pressed agent action, left the thread Finished until the turn's first report.
3. A command held at the gate, and a command the owner started for the agent, were Waiting on the row but idle for "settle when done" and the snooze until work is done: the thread could settle under its own command.
4. A merge ask held for CI on a PR Fyr had not read into a change yet read Finished.
5. A run the owner stopped read "Finished, unread".
6. A reason that changes with the clock alone (a quota wait that becomes "resuming") was not sent again: only a change of state was.
7. A held ask that waits on something else than checks was glued to the PR number ("Waiting for CI on #846Waiting for GitHub…").
8. The header chips said the state's code name: "Blocked" over a row that says "Merge", "Asking" over "1 question", "Done" and "Seen" under a section called "Finished". Their icon turned whenever a run was live, also while the thread waited for the owner's answer, and not while it was Starting.
9. Most waits (CI, a quota reset, a held or queued command) showed their reason on the inbox row only; inside the thread it was a tooltip on the desktop and nothing on the phone.
10. A failed row put the whole error message in the state's place; an agent that does not respond had no word at all.

The change:

1. **Claims** (`internal/core/claims.go`, SPEC 6.2a). `Claims(t, now, on)` lists what the thread waits on, in the order of precedence, each claim the owner's move or work that goes on. The attention is the first claim; `NeedsOwnerAt`, `NeedsOwnerNowAt` and `WorkGoingAt` read the same list. The order is the one the chain had, with the handover placed above the quota wait and the failure.
2. **Quiet claims** keep what `workGoing` counted and the row never showed (an unread message, a queued notice, an agent's Rule that is not a watch).
3. **Starting** is set by the take that delivers a waking notice and by a pressed agent action, as it is by the owner's message.
4. **The invariant** checks the placement against the raw fields and against the two families, not against a copy of the chain: a finished thread has no run, start, task, hold, handover, failure or offer; a thread in Moving has work going on; a thread in Your move needs the owner.
5. **Words.** `stateChip` and `stateLine` (`web/src/rows.ts`) and one `StateChip` component for both fronts. The chip says the row's word; its icon turns only while the state is working. A failed row reads "Failed · <the failure>". The Details sheet and a row's spoken label say the sections' words ("Finished, unread", "Finished, seen"); the push says "Finished" and a failure's reason as the row has it. An agent that does not respond has the word "Not responding".
6. **A wait says why above the composer** on both fronts, except the agent's own watches, which have their strip with a Cancel.
7. A run that ends interrupted marks the result seen. The tick sends a thread again when its reason changed.

A side effect of (3): a restart of Fyr between a waking notice's take and the agent's start now ends as a failure the owner sees ("Fyr restarted during the run", with Retry), as it does for the owner's own message; until now the notice was lost silently and the thread read Finished.

Not changed, on purpose: a timed snooze is still broken only by a new question or failure; a failure on a change still waits 5 minutes for the agent before it is the owner's; a live run that has a command queued or held still reads Working (the agent is told to do other work); a merge that was started still reads "Merge" until GitHub reports it merged (a marker for a merge in flight is its own change). The Rules and `Available` have no auto-merge setting, so a merge Fyr does by itself still counts as the owner's for the snooze until work is done.

Tests: `TestClaimOrderIsPinned`, `TestEachClaimAloneAgrees`, `TestClaimsComeInOrder`, `TestClaimsFixWrongPlaces`, `TestWakeReadsStarting`, `TestInterruptedRunIsSeen`, `TestCIWaitTextIsNotGlued`; `web/test/state-chip.test.ts`.

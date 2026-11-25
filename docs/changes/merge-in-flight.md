# V0.merge-in-flight: a merge that was started reads Merging in Moving without you, not Merge in Your move

→ SPEC 6.2a. Amends V0.thread-claims, V0.101, V0.97, V0.200.

The cause: nothing on a change said that its merge had been started. After the press on Merge, the yes to a merge ask, the merge command or an auto-merge, the PR was still open and green until the poller read it as merged, so the change read Ready and the thread stayed in Your move with the word "Merge", as if the press had done nothing. Only the Merge button's own press marked its offer as running; the other three ways left the offer open.

The change:

1. **The marker.** A change has `merging {head, at}` while a merge Fyr started on its PR is in flight. It is set in one place, after the reducer in `Reduce`, for every effect that runs `pr.merge`: the four ways a merge starts all end as that effect.
2. **Its end.** A refused or lost merge removes it at once (the PR reads as it is again). A merge GitHub took keeps it until the poller reads the PR as merged or closed. A head that moved removes it. With no news it ends 10 minutes after the last (`MergingFor`, the merge's own time limit); `NextCheck` asks the clock for that reduce, so a snooze until work is done never stays overdue.
3. **What reads it.** The change's need is muted with cause `merging` (not Ready, and not its failure). The thread has the claim `merging`: Waiting, "Merging #N", after a command that runs and before the settle offer; another change that is ready still comes first. The change is not Ready in the thread DTO, and its Merge offer is unavailable ("It is merging"), however the merge was started.
4. **Quiet.** A thread that waits on a merge Fyr approved by the repo's setting sends no push (V0.200), as the Ready PR of that merge did before.

Tests: `TestMergeInFlight`, `TestClaimOrderIsPinned`, the auto-merge quiet test.

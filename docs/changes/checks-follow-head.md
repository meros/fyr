# V0.checks-follow-head: a watch on a PR's checks waits for a read of GitHub made after it was armed

→ V0.97, decision 0024 section 8 (a fact belongs to a head). Fixes #120.

1. **The cause.** An agent pushes head B and arms a watch on `checks ok`. Until the poller reads the PR again, the change still holds head A and its `checks: ok`, so the watch fired at once on A's result. The poller sets checks to pending as soon as it reads B (a repo with CI reports an empty or queued list as pending), but a watch armed before that read did not wait for it.
2. **A watch waits for a fresh read.** A watch the agent arms on a PR fact (`checks`, `review`, `conflict`) of an open PR holds only on a PR read made after it was armed. Until then that part of the watch is unknown: it does not fire, and `not` does not turn it true.
3. **Read at once.** Arming such a watch reads the thread's PRs and git now (as Refresh does), so the wait is one read of GitHub, not one poll interval. A watch armed on a green head that does not move fires on that read.
4. **Fyr's own rules** are not anchored: they are armed inside the read they act on.

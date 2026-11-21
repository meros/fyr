# V0.thread-self-wake-churn: a thread whose CLI starts turns by itself that end with nothing behind them is a thread invariant

→ V0.thread-invariants (a new runtime invariant in its group), V0.agy-reactive-wakeup and V0.agy-open-tool-settle (the settle timer that closes a turn the CLI started by itself), V0.quota-wait (a run the provider's quota refused), V0.wakeups-via-watch (KindRunStarted exists only for a turn no send opened).

A thread whose agent ran on the `agy` CLI woke itself 47 times in a quarter of an hour, and each turn ended after a second or two with the model's first note and nothing else. The owner saw a thread that was "always waking", with a wall of identical quota notices, and asked for an invariant that catches it. Nothing in Fyr noticed: every transition was legal, and the thread's own state (Running, then not) was right each time.

1. **The shape of the loop.** The CLI starts a turn by itself (a background task finished, a quota notice arrived), and the adapter's settle window closes it before the model answers: the result the adapter writes for such a turn carries no measured duration. A turn the provider's quota refused looks the same to the thread. Either way the thread woke, spent a turn's tokens and ended with nothing the owner asked for — the burst here was 47 such turns in 15 minutes, most of them identical quota notices.

2. **What counts.** `KindRunStarted` is emitted only when the agent manager sees a stream message open a turn that no send opened (`turnStarted` refuses a live turn, and `send` emits the run itself), so it is exactly "the CLI woke itself". The run must then end with no measured duration, or with the quota class. A run behind a message, one the owner stopped, and one that ran to its own measured end are not this loop.

3. **The rule.** Three such runs within `InvSelfWakeWindow` (10 minutes) open the `thread-self-wake-churn` card, and the window then starts again, so a thread in the loop gets one card per three wakes rather than one per wake. Across five days of this host's events `KindRunStarted` is rare enough that the rule fires on the two threads the owner complained about and nowhere else; the ordinary background wake of a thread that does its work never reaches three in ten minutes.

4. **Not a fix.** The invariant reports; it does not stop the thread. The fix is in the adapter, which is the `Fix it` thread's work with this evidence.

Tests: `internal/engine/thread_invariants_test.go` (`TestThreadInvariantSelfWakeChurn`: two wakes are below the threshold, a third opens the card; runs with no self-start never count, and a quota-refused self-start counts with a measured duration).

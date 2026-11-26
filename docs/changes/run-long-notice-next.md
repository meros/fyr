# V0.run-long-notice-next: a long run's start notice never starts a turn

→ V0.run-long-notice (1), (3). *Amends V0.run-long-notice (1): the start notice is `Next`, not a wake notice.*

**What went wrong.** The start notice of a pressed run request that ran over 10 s was built in the `result` rule, whose urgency is `Wake`, so it started a turn in an agent that had ended its own. That turn had nothing to do: on one host, in a day, 21 of 33 start notices were the only notice of a run that ended within 45 s, and in each the agent called no tool and wrote one line ("X is running; I'll read the result when it ends"). The owner got a new agent message that said nothing. The Run card already showed the run in progress, and the result wakes the agent anyway.

(1) **The start notice is `Next`.** It is its own rule, `result.long` (`Urgency: Next`). It goes out with the next delivery: at a tool call in a live turn, or with whatever wakes the agent next. It never wakes an idle agent. An agent that waits on the run in a live turn still learns at its next tool call that the run is under way, so it does not ask again.

(2) **Coalescing is unchanged** (V0.run-long-notice (3)). The notice keeps the result's key `result:thread:<action>`. When the run ends while it is still queued, the result replaces it and wakes the agent once, and the agent reads one line: the result.

(3) The result notice and the other script actions are unchanged.

Tests: `internal/core/runrequest_test.go` (TestRunRequestLongNoticeWaitsForNext: no wake when idle, delivered at a tool call, the result wakes once with one line), `internal/core/feed_test.go` (TestNoticeRules row `result.long`).

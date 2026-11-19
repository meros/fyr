# V0.budget-restart: the heavy-job budget outlives a restart of fyrd, so a deploy no longer lets in jobs the host has no room for

→ decision 0029 S4, V0.163, V0.load-tuning. *Amends S4: the controller starts from its kept budget, not the full one.* (owner, 19 Nov, after a read of the day's load log: "Keep budget across restarts".)

(1) **The cause.** The budget controller (AIMD) lived in fyrd's memory and began every start at the full budget (1.0 CPU, 1.0 memory). On 19 Nov fyrd restarted five times for deploys. After three of them (09:53, 11:04, 12:34) the budget jumped from the learned 0.5 back to 1.0. Two jobs were let in on a host with 3–7 % memory headroom, and within a minute the memory guard froze one of them ("froze job … memory: 7 % free"). The budget then took about two minutes to shrink back to 0.5.

(2) **The kept budget.** fyrd writes the budget, why it last moved and when to `slot-budget.json` in its data directory. It writes when the budget moves, and at least every 5 minutes while it holds, so the file's time says when the budget was last true. On start the controller takes the kept budget back when it is younger than 30 minutes, bounded to 1/N–2 (the host's `slots` may have changed). An older, missing or unreadable file starts at the full budget, as before. The first `slots: budget` line after a restart says "kept across a restart: <why>".

Tests: `internal/slots/aimd_keep_test.go` (TestBudgetKeptAcrossARestart, TestBudgetKeptIsRewrittenWhileItHolds).

# V0.restart-why: a budget kept across a restart says so once in its reason, however many restarts it outlived

→ V0.budget-restart. Seen on home-laptop on 19 Nov after the load-review deploys: `/api/load` and `slots: budget` read `why="kept across a restart: kept across a restart: the host is calm …"`.

1. **The cause.** A fyrd that reads the kept budget prefixes its reason with "kept across a restart: ". While the budget holds it writes the budget again every 5 min (`AIMDWrite`) with that reason, so the next fyrd prefixed it once more.
2. **The fix.** The prefix is added once: a reason read back drops every prefix it already carries (a file an older fyrd wrote may hold several) before the one is added.

Tests: `internal/slots/aimd_keep_test.go` (TestBudgetKeptTwiceSaysItOnce).

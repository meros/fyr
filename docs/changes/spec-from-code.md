# V0.spec-from-code: SPEC 3 and 5.12 state the model and the agent tools as the code has them, and a test keeps the tool table equal to the registered tools

→ 3 (Records), 3.11, 3.15, 5.1, 5.11, 5.12, V0.one-tool-vocabulary, V0.old-routes-end, V0.remove-t3. *Changes only the spec and one test; no behaviour changes.*

The problem: section 3 said every change to a record goes through the one path of 5.1, while the front's own commands, the guard's runtime state, the load passes and the host settings change state without an Input. Section 5.12 had drifted from the tools: grouped rows, Input kinds that are not the ones the tools send (`decision.open` for `ask` and `run_request`, `thread.settleAsk` for `settle_request`, `pr.linked` for `pr_create`), and a test name that no longer exists. A few lines still described T3 adoption and the routes before `/api/h/<hostId>/` as current.

(1) **Section 3 names the exceptions to 5.1.** The front's `front.*` vocabulary and the owner's preferences and device data on the front; the guard's runtime state on the engine (admission queue and slots, freezes and thaws, leftover processes ended or kept); the load passes that rewrite stored records without an Input (`MigrateObserved`, `MigrateCodeOnly`, `MigrateDone`) until #456; and host settings in the host's key-value store (`host_setup` writes the agent's mark directly). The list that stood at the end of 5.12 moves here.

(2) **5.12 has one row per registered tool**, with its key arguments, the Input kind it sends (or `read`, or none with the reason) and what it does, checked against the tool schemas in `internal/engine`. `TestSpecListsTheTools` (`internal/engine/vocabulary_test.go`) parses the table's rows and fails when its names and the registered tools differ.

(3) **Removed things are no longer current.** 5.1's `makes` no longer names an adopted thread, 3.15's matching no longer runs on adoption, 3.11's presence call is `POST /api/front/presence {host, threadId, device}`, and 5.11's client reads go through `/api/h/<hostId>/`.

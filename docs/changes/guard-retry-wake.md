# V0.guard-retry-wake: the memory-recovered notice wakes the agent Fyr held

→ V0.guard-wake (E1).

The cause: a heavy command held at the gate while memory was short told the agent to run it again "when memory has recovered". The notice was marked `Next` (delivered with the next prompt), which overrides the slot rule's `Wake`. An agent that ended its turn to wait stayed asleep until the owner wrote to it.

The fix: the recovery notice is `Wake`.

Tests: the recovery step of the gate-hold test in `internal/engine/heavy_test.go` asserts the notice is `Wake`.

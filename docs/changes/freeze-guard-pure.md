# V0.freeze-guard-pure: the memory guard's decisions are pure functions, and the engine's tunables are in one file

No behaviour change. `freezeGuard` (internal/engine/freeze.go) kept its effects and takes its decisions from `guarddecide.go`: `memoryState` (what the signals say), `wakeFine`, `judgeFrozen` (keep, keep past the limit, or thaw, for one frozen job) and `thawWords` (the reason text). `judgeFrozen` asks for the rerun entry and the room check only when its rule needs them, as before. The engine's own clocks (`SlotIdle`, `HeavyCallStale`, `ThawAfterOK`, `FreezeMax`, `FreezeCeiling`, `WakeEvery`, `guardKeep`, ...) are in `tunables.go`; they are not `slots.Policy` fields because that would export new config for clocks only the engine has.

Tests: `internal/engine/guarddecide_test.go`.

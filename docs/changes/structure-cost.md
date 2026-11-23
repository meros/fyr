# V0.structure-cost: dead code out of core and the engine, one load pass, the change, request and press reducers beside what they serve, and an apply that marshals its thread once

→ V0.95 (5), V0.98 (7), V0.changes-code-only (4), V0.203, V0.thread-invariants. No behaviour an owner or an agent sees changes, but for (2)'s retired sweep.

(1) **Dead code.** `NeedsOwner`, `WorkGoing`, `WorkOver` and `WorkDone` (the clock-free forms, and core's only `time.Now`) had no callers; their `…At` forms are the API. Also gone: `Monogram`, `PhaseGuide` and `PhasesAfter` (with `phaseHelp`), `QuietNow`, `ActionSpec.IsRequest` and `engine.EffectTimeout`, each used only by its own test. `Compute` is a helper of core's tests now; code calls `ComputeWith`. The `noDecisions` Condition kind is never armed (the agent cannot name it, and "settle when done" reads `workDone` since V0.203): it is gone, and SPEC 7.2 and 7.6 say so.

(2) **One load pass.** The repairs of records stored by older builds were in five files (`DecodeThread` and three `migrate…` in `actionspec.go`, `codeonly.go`, `done.go`, `hostchange.go`, and the engine). They are in `core/migrate.go`: `DecodeThread` reads the stored shapes, `Migrate(t, records, now)` runs every pass on a thread and `MigrateRecord(c, now)` on a host record, and the engine's `loadRecords` calls them once and stores what changed. A "settle when done" Rule stored before V0.203 (`all(runEnded, noDecisions, idle)`) gets `all(workDone, idle)`; another Rule on `noDecisions` is dropped, since it could never hold. The sweep of V0.95 (5) (the leaked `acme/app#88` fixture and the `~N` loop records), which wrote threads outside a reduce on every load, is retired: it cleaned a one-off leak, and every host that loaded a build since has run it.

(3) **The kind table.** `kinds.go` keeps the table and the observer reducers. The change tools' reducers (`changeKind`, `reportChange`, `codeOnly`, `findChange`) are in `changekinds.go`, the change_request reducers in `ask.go`, offer and press beside `Holder` in `actionspec.go`.

(4) **The cost of an apply.** `stage` marshalled the thread before and after, both without their feed, and the view before and after; `persist` marshalled them again; and every apply checked that the thread survives a store and a load (`core.CheckSerialization`: a marshal and an unmarshal of the thread and the view, and the attention, the overdue Rule and the next check twice each). Now the engine keeps each thread's stored bytes (`storedJSON`, valid while `e.threads` and `e.views` hold the records they are of), `stage` marshals the reduce's thread and view once (the thread again only when it moves `UpdatedAt`), `persist` writes those bytes, and the serialization check runs on one apply in 64, on every thread at load, and on every apply under `go test`. The invariants' tick copied every thread on each tick; it keeps its copy of a record until `e.threads` points at another. The clone in `Reduce` and the one per Rule firing (for the rollback of a refused `then`) stay. One host, the same three-run sample each, `go test -bench` with `-benchtime=3000x`:

| Benchmark | Before | After |
|---|---|---|
| an apply that changes the thread | 634 µs, 254 kB, 1 263 allocs | 345 µs, 145 kB, 578 allocs |
| an apply that changes nothing | 241 µs, 119 kB, 460 allocs | 208 µs, 110 kB, 425 allocs |
| the invariants' tick over 50 threads | 338 µs, 154 kB, 613 allocs | 36 µs, 18 kB, 175 allocs |

Tests: `internal/core/migrate_test.go` (TestMigrateRetiredWhens), `internal/engine/apply_bench_test.go` (TestStoredJSONIsWhatIsStored and the three benchmarks). The test of the retired sweep (TestLoadDropsLoopRecords) is removed with it.

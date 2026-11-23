# V0.structure-split: freeze.go and wire.go split by concern, no behaviour change

Why: `engine/freeze.go` (931 lines) held the guard's loop, its thaw rules, its undeclared-work freezes and its log, and `cmd/fyrd/wire.go` (918 lines) held every startup step and a 60-field struct whose steps depend on call order.

(1) `engine/freeze.go` is `freeze.go` (the frozen job), `freezeguard.go` (the tick and the victim), `thaw.go` (what may thaw and how a frozen job is described), `undeclared.go` (flags and `freezeUndeclared`) and `guardlog.go` (`guardOnce`, `guardRecord`, `publishGuard`, `guardWhy`). More of `freezeGuard` is pure and in `guarddecide.go` with table tests: `stillHolding`, `wakesAlone`, `wakeDue`, `freezeGate`, `freezeRoom`.

(2) `cmd/fyrd/wire.go` keeps the `wiring` struct and its order; each step is in its own file (`wire_agents.go`, `wire_heavy.go`, `wire_guard.go`, `wire_deploy.go`, `wire_listen.go`) and its comment lists what it needs from the earlier steps and what it makes, with the order-dependent points. Fifteen fields nothing read are gone. A step still writes the struct rather than returning a typed result: the guard step alone reads two dozen things the heavy step makes, so a result type would be the struct again.

(3) `server`'s four optional `/state` fields use one generic `present(v, ok)` instead of four functions returning `any`.

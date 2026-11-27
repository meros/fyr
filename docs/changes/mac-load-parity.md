# V0.mac-load-parity: full load-management parity on macOS (services, turns, critical-stall holds, fast flagging, Docker stats, and invariants)

→ V0.guard-mechanism, V0.guard-tick, V0.guard-finish, V0.mac-swap-stall, decision 0029, decision 0032, decision 0036.

After the ground-up guard refactor (`internal/guard` + `guard.Mechanism` + `cgroup.ProcStops`/`ProcMechanism`), several load-management layers were still Linux/cgroup-only or returned no-ops when `tree == nil` on macOS:

(1) **Services on macOS (decision 0036 (D)).**
- `Services.Tick` (`internal/cgroup/services.go`) now scans each thread's process tree (`Trees`, `CLIs`, wired in `cmd/fyrd/wire_guard.go`) when there is no cgroup tree, using the same topmost-matching-subtree logic as Linux cgroups. Dev servers and dev stacks on macOS are now reserved in admission (`Services.Extras`), excluded from undeclared-work accounting (`Services.InCgroup`), and shown on thread rows.
- `ProcStops.Holds` (`internal/cgroup/procstop.go`) implements `cgroup.Control.Holds`, and `Services.HoldChecker(ctl)` wires `GuardService` through `ctl` instead of hardcoded `groups.Holds` (which always returned false without cgroups).
- `procTable.rootsWith` (`internal/cgroup/proctree.go`) and `Sampler.Roots` (`internal/cgroup/undeclared_sampler.go`) exclude roots whose subtree holds a service via `skipServices`, so `FreezeUndeclared` never `SIGSTOP`s a service on macOS.
- `agentShell` (`internal/cgroup/undeclared_proc.go`) recognizes direct `sh`/`bash`/`zsh -c`/`-lc` wrappers alongside Claude's shell-snapshot wrapper, so non-Claude CLIs (such as `agy`) get elder root detection and service-wrapper filtering too.
- `Docker.Memory` (`internal/cgroup/docker.go`) falls back to `docker stats --no-stream` (cached for `dockerEvery`, 10 s) when container cgroups are absent under `/sys/fs/cgroup` (macOS Docker Desktop, OrbStack, Colima).

(2) **Multi-part turns without cgroups (decision 0032 slice 2).**
- `ProcStops.Units` and `ProcStops.RotateUndeclared` (`internal/cgroup/procstop.go`, wired via `ProcMechanism` in `internal/cgroup/mechanism.go`) split multi-worker undeclared work into its heavy parts and rotate them via `SIGSTOP`/`SIGCONT` (stopping inactive parts and persisting their PIDs before resuming the active part). `ProcStops.KillJob` kills both paused parts and the active part when taking turns.

(3) **Critical-stall holds, fast flagging, and invariants without cgroups.**
- `memoryState` (`internal/guard/decide.go`) and `InvThawIntoCritical` (`internal/engine/invariants.go`) treat `Stall` as the agents' stall when `AgentsStall` is absent on non-cgroups providers (`sysctl`, `proc`), matching `AIMD.Step`: a frozen job on macOS stays frozen past `FreezeMax` (10 min) while the Mac thrashes critically instead of thawing back into swap thrash, up to `FreezeCeiling` (30 min). Linux `cgroups` keeps `AgentsStall` separate from host `Stall`.
- `Sampler.OnCritical` (`internal/cgroup/load.go`, wired in `cmd/fyrd/wire_deploy.go`) wakes `guardWake` when a host without cgroups enters `StallCritical` (and at most once per `TriggerFresh`), so `FlagNow` flags rapidly growing undeclared work immediately on macOS instead of waiting the 45 s detector window.
- `InvProbeSilent` (`internal/engine/invariants.go`) also checks `sysctl`, catching a silent slowdown probe on macOS.

(4) **Smaller fixes.**
- `cmd/fyrd/wire_heavy.go`: when a killed undeclared job's rerun matches `rerun.Match`, `text` is set from `class` so `stops.Seen`/`groups.Seen` can find and freeze/kill the rerun if memory runs short again.
- `ProcStops.stop`: dead PIDs whose `SIGSTOP` fails are pruned from `stopped-procs` instead of being left in KV.
- `cgroup.Cwds` (`internal/cgroup/worktrees.go`): `lsof` on macOS is bounded by `PSTimeout` (10 s).

Tests: `internal/cgroup/procmech_test.go` (`TestProcMechanismTakesTurnsWithoutCgroups`, `TestProcServicesAreProtectedWithoutCgroups`, `TestSamplerOnCriticalWakesGuardWithoutCgroups`), `internal/cgroup/proctree_test.go` (`TestProcStopsRealTurnsAndKill`), `internal/cgroup/services_test.go` (`TestSvc6DockerStatsFallbackWithoutHostCgroups`), `internal/guard/decide_test.go` (`TestMemoryState`), `internal/engine/invariants_test.go` (`TestInvariantThawIntoCriticalNotForGoneOrCeiling`, `TestInvariantProbeSilentOnSysctl`), plus the real-process and sysctl Darwin tests (`TestDarwinSamplerReadsSysctl`, `TestDarwinQueueReadsThisHost`, `TestDarwinSwapMeterReadsThisHost`, `TestV207CAStopSurvivesARestartAndTheNextStartResumes`, `TestProcStopsRealTurnsAndKill`) on macOS.

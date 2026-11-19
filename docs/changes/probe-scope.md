# V0.probe-scope: in the scope layout the slowdown probe runs in a scope of its own under the agents' slice, and a probe child that cannot start leaves the burst running in fyrd

→ decision 0029 section 0 (the probe); amends V0.agents-unit.

The cause, seen on the live host: since the switch to the scope layout every guard and budget log line says `slowdown=n/a` and `/api/load` has `slowdown: null`. The probe child was started in `agents/probe/`, which in the scope layout is a `mkdir` in `/sys/fs/cgroup/fyr.slice/fyr-agents.slice`. That slice is root's, so the `mkdir` failed with EACCES, `RunProbe` tried again every 30 s and never measured. Admission's slowdown gate, the CPU AIMD and the card's Speed row had no input.

(1) **The probe's scope.** In the scope layout fyrd starts `fyrd loadprobe` through `systemd-run --scope` as it starts a thread's CLI: unit `fyr-agent-probe-<pid>-<n>.scope` (a new `<n>` each start, a name the module's polkit rule allows) in `fyr-agents.slice`, at the default agent weight (`CPUWeight=100`). So it runs where a new job would run, in the agents' budget, not in `server/`. The unit layout keeps `agents/probe/`.

(2) **The fallback.** When the probe child cannot start, or ends without giving a value (systemd-run refused), the burst runs in fyrd for 30 s and the child is tried again; s is never absent for it. The first failure is logged once: `load: the probe child: <error>; the probe runs in fyrd until it starts`.

(3) **The probe is no thread.** A `fyr-agent-probe-…` scope (this one, and the layout's check at start) is not read as a thread named `probe-…`.

Tests: `internal/cgroup/probe_scope_test.go` (TestProbeScopeStartsThroughTheScopePrefix, TestProbeStartFailureFallsBackToTheBurst, TestProbeScopeIsNoThread), `internal/cgroup/layout_test.go` (TestAgentsUnitScopeNames).

# V0.thaw-at-start: at start fyrd thaws the work the last fyrd left frozen, and a released thread's undeclared/ goes with its job/

→ decision 0029 S5, decision 0032 slice 2, V0.164 section 7, V0.process-tree-hosts, V0.job-limits.

(1) **The cause.** When fyrd restarts (a self-deploy, a config reload, a crash) while the guard holds a thread's work frozen on a cgroup host, nothing thaws it. At start `cmd/fyrd/main.go` calls `ResumeStopped`, but that returns at once unless the host pauses with SIGSTOP (V0.process-tree-hosts). The new engine starts with no frozen state, so the thread shows as not paused; the undeclared detector seeds its idle level from the memory it reads now, frozen work included, so no episode starts and `EndUndeclared` never runs. A frozen `undeclared/` or `undeclared/u*/` stayed frozen with no end, and the agent's Bash hung. A frozen `job/` recovered only when its slot was released.

(2) **Thaw at start.** Before the guard's first tick, fyrd reads `cgroup.freeze` of `job/`, `undeclared/` and each `undeclared/u*/` under every thread cgroup the tree knows: the scopes and the legacy `fyr.service/agents/` in the scope layout, `agents/<thread>/` in the unit layout. Each one at 1 is set to 0, with one line: `guard: thawed undeclared/u123/ thread=t_… reason="left frozen by the last fyrd"`. The new engine starts with nothing frozen, as the cgroups now are.

(3) **Release removes `undeclared/`.** A thread's last Release removed its `job/` and `cli/` but left `undeclared/` and its units, so the thread's cgroup could never go. It now removes the units, then `undeclared/`, between `job/` and `cli/`; the kernel refuses one that still holds a process, as before.

Tests: `internal/cgroup/thawstart_test.go` (TestThawAtStartThawsWhatTheLastFyrdLeftFrozen, TestThawAtStartScopeAndLegacy, TestReleaseRemovesUndeclared).

# V0.rerun-classes: under the memory guard, a job of a class the owner marked rerunnable is killed and run again instead of held frozen

→ decision 0036 slice 6 ((F)), V0.151, V0.164 section 7, V0.209, V0.guard-wake. 

(1) **The owner's list.** Settings → Automation → Kill and rerun (desktop and phone, one component) lists the command classes the owner marked rerunnable on this host, each with Remove, and a field to add one (`make test`, `yarn lint`, `tsc`). Nothing is on it by default. Each row says what the guard did to that class in the last hour: "Not killed in the last hour", "Killed once in the last hour", or "Killed 3 times in the last hour; frozen instead until 14:41". The command is `host.rerunClasses` with `add` or `remove`, owner only. It is kept in the host's store, and `GET /api/load` shows it as `rerun.classes`. An entry matches a job when it equals the job's heavy class (a job record's "project: " prefix aside), or when its words stand alone in the job's command ("make test" in `cd web && make test 2>&1`, not in `make testdata`). A service (decision 0036 (D), `GuardService`) is never killed.

(2) **When a job is killed.** Under the memory guard, a job (admitted or undeclared) whose class is on the list is killed instead of frozen in two cases:
- it was frozen and could not wake within 2 min;
- it is the job the guard would freeze, and its class's learned wall time (p90) is under 5 min. Undeclared work has no learned wall time, so it is frozen first.

The kill is `cgroup.kill` on the job's `job/` or `undeclared/` cgroup, which takes every process under it, frozen or not. Before the kill fyrd opens a pidfd for each process, and after it calls `process_mrelease` on each, so the memory comes back at once instead of while the processes exit. On a host without cgroups, fyrd sends SIGKILL to each process it stopped, and to the whole group of one that leads its own process group. Log: `guard: killed job thread=… class="make test" why="frozen 2m0s and could not wake" procs=3 mem=… frozen_for=… kills_this_hour=1 …`. The guard's history adds a `kill` event, and the chart ends the frozen stretch there.

(3) **The agent is told, and the rerun is admitted again.** The thread gets: "Fyr killed your heavy job (`make test`) to free memory on this host: the owner marked it rerunnable. Its failure or exit 137 came from that, not from your change. Run the command again: Fyr admits it once memory has recovered and the job fits." The admission of a killed job goes back. Its rerun goes through the pool like a new job, even when its class is learned (V0.209 would just run it), so it waits until memory is no longer short and its learned need fits. A killed undeclared command that the heavy patterns do not match is admitted under its rerunnable entry. `guard: rerun admitted thread=… class=…`.

(4) **The learned peak rises.** After a kill, the class's learned memory peak is multiplied by 1.2 (as VPA does after an overrun), so the rerun is admitted where the larger peak fits.

(5) **At most 3 kills an hour.** A class killed 3 times within an hour is no longer killed until an hour after the first of those kills. It is frozen instead. The log says `guard: class="make test" killed 3 times in an hour: frozen instead of killed until 14:41`. The owner sees it in the guard's history ("Stopped killing make test for an hour: killed 3 times; it is frozen instead") and on the class's row.

Tests: `internal/engine/rerun_test.go`, `internal/cgroup/rerun_test.go`, `web/test/rerun.test.ts`.

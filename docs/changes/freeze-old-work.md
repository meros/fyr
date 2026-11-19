# V0.freeze-old-work: the undeclared-work freeze holds the long job that holds the memory, not the newest small process

→ V0.164 section 4.5 and 7, V0.process-tree-hosts, V0.201. (bug report, 19 Nov, #344: "Fyr paused your undeclared heavy work (`sleep 30`)" while a detached 4–8 GB Python build of the same thread ran on.)

(1) **The cause.** The roots of an episode were only the processes that started after its window began (its first over reading less 30–120 s, less 15 s). Every older process counted as a resident. A job whose memory grows slowly crosses the threshold long after it starts, so it was never a root. On 19 Nov the agent started a build with `setsid nohup timeout 3h …` at 10:31:19, as Fyr's hint says. Its memory crossed the threshold at about 10:57, so the episode began then. The only process newer than that was the waiter's `sleep 30`, forked every 30 s. At 11:02:11 that sleep was named and frozen (daemon.log: `cmd="sleep 30" mem=2.2 GB`), and the build ran on. The same log shows freezes of `pg_isready`, `curl …/health` and `git …` in other threads that held 2.5–3.7 GB, and "could not freeze …: no process to move".

(2) **Elders.** An episode over its memory threshold also takes as roots the older work whose subtree holds a quarter or more of the episode's memory over the idle level:
- a Bash shell the CLI started (the CLI's wrapper, `…/shell-snapshots/snapshot-…`), such as a long `run_in_background`;
- with cgroups, a process in the thread's cgroup whose parent is outside it and which is not the CLI: a job detached with `setsid` or `nohup … &` stays in the thread's cgroup.

A new process under an elder is not a root of its own. The CLI, its MCP servers and small older processes (a waiter) stay residents. The heaviest root names the command, so the notice names the build. The freeze holds the roots as before. fyrd now gives the CLIs' pids to the sampler on a cgroup host too. Without the CLI's pid, no detached process is taken, because it could be the CLI.

(3) **Limits.** Without cgroups (macOS), a detached job leaves the process tree under the CLI and is still not seen. The CLI's own timeout on a background Bash runs on while Fyr freezes it, and Fyr cannot stop that clock. The notice says so ("A timeout inside the job may fire after it resumes").

Tests: `internal/cgroup/undeclared_elders_test.go` (TestElderTheDetachedBuildIsTheRootNotTheWaitersSleep, TestElderAnOldBackgroundBashHoldsTheMemory, TestElderProcessTree).

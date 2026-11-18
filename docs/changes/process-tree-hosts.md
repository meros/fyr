# V0.process-tree-hosts: a host without cgroups (a Mac) sees each thread and each heavy job from its process tree, and pauses work under the memory guard with SIGSTOP

→ V0.150 (6), V0.164 (3), (7), (9), V0.151 (5), V0.142 (8), V0.195 (3), decision 0029 section 0 (c). *Amends V0.150 (6), V0.164 (9), V0.151 (5) and V0.195 (3).* (owner, 18 Nov: "Make sure everthing works properly (as good as possible) on mac as well.")

(1) **The reading.** Every read takes the process table once (`/proc`, on macOS `ps -A -o pid=,ppid=,rss=,etime=,time=,state=,args=`; zombies are left out). A thread is its CLI and the CLI's descendants: memory is the sum of RSS (shared pages count more than once), CPU time counts per process, so a child that ends takes nothing back; a process that starts and ends between two reads is not seen.

(2) **Shown.** `GET /api/load` has `threads` and `agents` from these figures, so the rows (desktop and phone, the same components) show CPU and memory; `cgroups` stays false and the tooltip's agents line ends "(process trees)".

(3) **Jobs.** A slot hold is measured from them as V0.142 (5) from a cgroup, without the stall (demand is the cores): the record, the learned class, the live use in admission, the give-back of an idle admission (V0.209: a job that runs is not given back) and the memory guard's growth. A hold that measured nothing (no CPU, no memory) teaches nothing, and a stored class without measures is forgotten at the start: a Mac had learned its classes at the 2 % floor, so it admitted every job beside every other.

(4) **Undeclared.** The roots of an episode are the processes under the CLI started in it whose parent is older; the command, the project in the wait reason and the suggestions follow as with cgroups.

(5) **Paused.** The guard's freeze is SIGSTOP and its thaw SIGCONT: for a held job the processes under the CLI whose command line holds its command, and their descendants; for undeclared work the roots and their descendants; children forked during the stop are looked for again; never the CLI. Its rules, notices and `paused` are V0.151's and V0.164's. The pids are stored (KV `stopped-procs`) before the first signal, and fyrd resumes them at a shutdown and at its next start.

(6) **Quiet wait.** V0.195 (3) reads the process tree's CPU time; what the agent detached has left that tree and does not count.

(7) **Not here:** turns among the parts of undeclared work (decision 0032 slice 2), CPU weights and per-thread memory limits, which need cgroups. Tests: `internal/cgroup/proctree_test.go` (real processes for the stop and the resume after a restart), `web/test/load.test.ts`.

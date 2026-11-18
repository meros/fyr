# V0.mac-load-signals: a Mac's slowdown and free memory follow what the machine really does

→ V0.150, V0.142 (2)–(4), V0.146 (2), V0.153, decision 0029 section 0 (c). *Amends V0.150.* (owner, 18 Nov: "Verifiera alla last-hanteringsmekanismer på mac", "fixa eventuella problem")

(1) **The slowdown.** On macOS the probe's burst does not feel a full machine: a thread that just slept runs ahead of the busy ones, and the burst fits in one time slice. Under 32 busy loops on 8 cores it read 1.01 while a job of 3 s ran 4.2× slower, at any nice level, so the slowdown gate, the CPU budget's cut and "the host thrashes" never acted. The probe's value is now raised to what the run queue says: (runnable threads using at least 2 % CPU + 1) ÷ cores, from `ps -A -M`. Measured under 0 to 32 loops it is within 3 % of the timed job's slowdown from 8 loops up, and 1.0 below.

(2) **The free memory.** `kern.memorystatus_level` stands still until memory is reclaimed: it read 85 % with 10 GB of 24 just allocated and 0.1 GB free. What is available is now the smaller of it and the page counts: free, speculative, purgeable, file cache and half of the programs' own pages. The kernel's level still decides near the limit.

(3) **Tests.** `internal/cgroup/host_darwin_test.go`. Run by hand on an 8-core, 24 GB Mac with real processes: the two signals above, a second job held back at 2.1× and 4.1×, thrashing at 4.1×, the CPU budget's cut, the process tree's cores and memory, undeclared work flagged and paused by SIGSTOP without the CLI or its other children, resumed by a thaw, at the episode's end and by a new fyrd after a crash.

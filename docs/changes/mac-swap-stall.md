# V0.mac-swap-stall: a Mac's memory stall is its swap traffic, and memory the kernel swaps out is not free

→ V0.mac-load-signals, V0.142 (2)–(4), decision 0029 section 0 (c), decision 0036 E2. *Amends V0.mac-load-signals (2).* (owner, 20 Nov: "Start testing the updated load management subsystems on mac, fix any problems")

(1) **What was wrong.** macOS has no PSI, so a Mac's stall was only the kernel's pressure level, and that level stays at warn (elevated) while the machine thrashes. On an 8-core, 24 GB Mac, a job that re-read 17 GB of data that does not compress swapped 200 to 300 MB/s each way for over two minutes. During that time fyrd read 29 % headroom against a 10 % reserve and an elevated stall. Memory was therefore never short: the guard paused nothing and admission held nothing back. The headroom counted half of the programs' own pages as room the compressor would make, but the kernel was swapping instead.

(2) **The stall.** fyrd now reads the kernel's swap counters (`vm.compressor.swapper.swapins_total` and `swapouts_total`, in pages) and turns them into rates over at least 5 s. Swap-ins and swap-outs both at 64 MB/s or more mean thrashing: what is read back pushes other pages out, so the stall is critical. Swap-outs at 16 MB/s or more, or swap-ins at 64 MB/s alone, make it elevated. The stall is the worse of this and the kernel's pressure level. When the Mac is calm, swap-ins measured 0 to 15 MB/s and swap-outs 0.

(3) **The headroom.** For one minute after a swap-out at the elevated rate, the programs' own pages count for nothing in the page counts, because the compressor has not made the room. The 17 GB job read 30 % free with them and 16 % without.

(4) **Measured after the change, on the same Mac and job.** The headroom fell from 61 % to 14 % at the first swap-out. The stall was critical within one 3 s sample of the thrashing starting, and it returned to elevated, then to ok, when the job ended.

(5) **What was checked on the Mac and found right.** The whole Go suite on `origin/main`: only path tests fail, because `/var` is a symlink to `/private/var`, and `TestFolderArgvRealDirenv` fails; none of these are in the load packages. A scratch fyrd's `/api/load` gave the sysctl provider, a capacity of 80 % of the RAM and a headroom that follows `memory_pressure`. The process-tree reader read a 400 MB job using one core as 413 MB and 0.97 cores. SIGSTOP paused a job's tree, without the CLI, down to 0 % CPU, and SIGCONT resumed it. Stopping the scratch fyrd left no process behind.

Tests: `internal/cgroup/host_darwin_test.go` (TestDarwinSwapTrafficIsTheStall, TestDarwinSwapMeterReadsRates, TestDarwinSwapMeterReadsThisHost).

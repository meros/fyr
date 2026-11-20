# V0.guard-swap: a full agents swap counts as short memory, and a freeze moves the frozen work's memory to swap

→ V0.164 section 7, V0.load-tuning, decision 0029 S5, decision 0032 slices 1 and 2. *Amends the memory reserve of admission and of the guard.* 

(1) **The cause.** On 19 Nov from 14:12 to 14:37 agents/ sat at its memory.high (12 GiB) with its swap cap (6 GiB) full. Nothing the agents held could move to swap, so the kernel reclaimed only their page cache (0.9 GiB left): every exec and import read from disk again, 2.4 GB/s, 80 % system CPU, load 98, memory PSI some 78 %. The guard said "memory short" only at 14:19 (12 % free) and froze one dev stack. A frozen process keeps its memory, so the freeze freed no RAM: memory stayed short until 14:39, after the owner killed two Python jobs. The host had 7–9 GiB of RAM and 49 GiB of swap free outside agents/.

(2) **Swap full.** The agents' swap is full when agents/memory.swap.current is 90 % or more of a set memory.swap.max. While it is, the memory reserve is twice the policy's (20 % instead of 10 %), for admission, the budget controller and the guard alike: a job waits with "memory: 12 % free (swap full)", and the guard counts memory short at 12 % free. `GET /api/load` has `agentsSwapFull`; the guard's log lines add `swap_full=true`.

(3) **A freeze frees memory.** After it freezes a job, undeclared work, or the units of a rotation that wait for their turn, Fyr writes the frozen cgroups' anonymous memory to their `memory.reclaim` with `swappiness=200` (anonymous memory only, so the page cache that running work shares stays; a kernel that refuses the key gets the plain amount). It asks for no more than the agents' swap cap leaves, less 10 % of it, or than the host's free swap when the agents have no cap. It runs in the background, once per thread every 30 s at most, and logs `cgroups: moved 3.1 GiB of t_… frozen work to swap`. The work reads it back when it is thawed. A host without cgroups (SIGSTOP) is unchanged.

(4) **Not in this change.** The guard still freezes one thread at a time on the host and gives each unit of a rotation a turn, also a small one. With (3) a freeze now returns the RAM, which is what one freeze lacked on 19 Nov; a second freeze is the next step if the logs show memory short with one thread frozen and moved out.

Host config (outside Fyr): the laptop's agents swap cap went from 6 to 10 GiB on the same day (nixos `dd29ed63`), still inside its 20 GiB zram.

Tests: `internal/cgroup/reclaim_test.go` (TestGuardSwapFullAtNinetyPercentOfTheCap, TestGuardSwapFullDoublesTheReserve, TestGuardSwapReclaimMovesFrozenWorkWithinTheSwapRoom, TestGuardSwapReclaimNeedsTheKernelsFile).

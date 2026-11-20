# V0.cache-reclaim: fyrd drops the agents' page cache before it reaches their memory.high, so no task is throttled for cache

→ V0.agents-budget-share (6), V0.guard-swap (memory.reclaim), V0.admission-tidy (the working set). *Answers V0.agents-budget-share (6), which found the agents' stall was the kernel reclaiming cache at memory.high and left it.*

(1) **Why.** The agents fill their memory.high with page cache (git, go and node builds read a lot). Past memory.high the kernel reclaims in the context of the task that charges the next page and throttles it, and PSI counts that as the agents' full stall. On 20 Nov 48 of the 74 "memory short" episodes came with the agents' working set at half their budget or less and the host's own stall calm; the slice had 525 000 `high` events, and 4.6 of its 7.2 GB were file cache. A larger slice fills with cache the same way (V0.agents-budget-share (6)).

(2) **The reclaim.** Each sampler tick (5 s), when the agents' memory.current is past 90 % of their memory.high (`CacheReclaimAt`), fyrd writes `<bytes> swappiness=0` to memory.reclaim for the amount down to 80 % (`CacheReclaimTo`): page cache only, never anonymous memory to swap. In the unit layout it writes agents/'s own file; in the scope layout the slice is systemd's, so it asks each thread's scope for a share in proportion to its inactive_file (a share under 16 MiB is skipped). One reclaim runs at a time, in the background. A line a minute at most sums them: `cgroups: dropped <n> GiB of the agents' page cache ahead of memory.high (<k> reclaims; now <cur> of <high>)`. A kernel that refuses `swappiness=` (before 6.12?) gets one line and no more reclaims; nothing else changes there.

(3) **Measured.** By hand on the same host, `1G swappiness=0` on one scope dropped 1 GB of cache in 0.7 s and left its anonymous memory as it was. A script doing (2) for 20 min dropped 1.6 GB in 1.8 s at its first tick (11 976 of 12 288 MiB) and then had nothing to do while it ran, because the slice stayed under 90 %.

(4) **What still stalls, and is meant to.** Anonymous memory past memory.high, cache read back as fast as it goes (V0.admission-tidy's refaults), and a job throttled by its own limit (V0.job-limits) still stall, and the guard still acts on them.

Tests: `internal/cgroup/cachereclaim_test.go` (TestCacheReclaimLeavesTheAgentsUnderNinetyPercentAlone, TestCacheReclaimAsksTheAgentsForCacheDownToEightyPercent, TestCacheReclaimSplitsOverTheScopesByTheirInactiveCache).

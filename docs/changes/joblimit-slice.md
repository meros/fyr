# V0.joblimit-slice: the job limits' swap valve reads the agents' slice in the scope layout, and a job of a class not learned yet keeps the limit it was given when it was moved

→ amends V0.job-limits (2) and (3); builds on V0.agents-unit.

(1) **The valve reads where the agents are.** fyrd read the agents' use and swap for the job limits from `fyr.service/agents/`, by hand. In the scope layout of V0.agents-unit the agents live in `fyr-agents.slice`, so it read nothing there: the agents' use was 0 and their swap never full, and the swap valve of V0.job-limits (3) (decision 0032 slice 3: memory.high with no swap room livelocks) never opened. It now reads the tree's agents cgroup, as the agents' capacity already did. No other place in `internal/cgroup` joins the root and `agents` by hand outside the unit layout's own setup.

(2) **An unknown class is sized once.** A job whose class is not learned yet got the agents' free memory, at most half their budget, recomputed on every sampler tick. The agents' use counts the job itself, so as the job grew its limit shrank below what it held: it was throttled the harder the more it grew, and a `guard: job limit` line was logged on every change. The limit is now computed once, when fyrd moves the job into `job/` or `undeclared/`, and kept. After that only the swap valve changes it (to `max` and back to the kept value). A learned class is sized as before. The log lines are unchanged.

Tests: `internal/cgroup/joblimit_test.go` (TestJL8TheValveReadsTheAgentsSliceInTheScopeLayout, TestJL9AnUnknownJobKeepsItsFirstLimitAsItGrows).

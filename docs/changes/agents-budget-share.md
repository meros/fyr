# V0.agents-budget-share: the agents' memory budget defaults to a share of the machine, the CPU cap to none, and fyrd states the budget it runs with

→ #198 (go public: no defaults sized for one machine), decision 0036 (accepted with this entry), V0.80, V0.agents-unit. *Amends the module's defaults of V0.80: `memoryHigh`/`agentsMemoryHigh` 13G, `memoryMax`/`agentsMemoryMax` 14G, `agentsMemorySwapMax` 16G, `agentsCpuCores` 4.*

(1) **Why.** The module's defaults were fixed sizes that suited one 30 GiB, 8-core laptop. On a 16 GiB machine 13G leaves the desktop almost nothing; on a 64 GiB machine it starves the agents.

(2) **Memory is a share.** `memoryHigh` and `agentsMemoryHigh` default to `40%`, `memoryMax` and `agentsMemoryMax` to `46%`, and `agentsMemorySwapMax` to `30%`. These are the shares `docs/HOST-SETUP.md` already recommended. systemd reads a percentage on the slice and the unit as a share of the RAM, or for the swap limit of the swap. In the unit layout (agentsUnit off) fyrd writes `agents/`'s files itself, and the kernel takes only bytes, so fyrd resolves the percentage from `/proc/meminfo` (`MemTotal`, `SwapTotal`), rounded down to a page. A percentage it cannot resolve leaves the file as it is and says so in `cgroups: budget not fully set`. A size such as `12G` is taken as it is, as before.

(3) **No CPU cap by default.** `agentsCpuCores` defaults to `null`: no `CPUQuota` on the slice and no `cpu.max` on `agents/`. The machine's core count is not known when the configuration is built, and the agents' low `cpu.weight` (100 against fyrd's 1000 and the desktop's default) already gives the rest of the machine the CPU first. A host that sets a number gets the cap as before.

(4) **fyrd states its budget.** The `cgroups: on` line at start ends with `agents' budget: memory.high 12.0 GiB (40% of 30.0 GiB RAM), memory.max …, memory.swap.max … (… of … swap), cpu.max 4.0 cores` (or `none`), read back from the agents' cgroup, whichever layout and whoever set it.

(5) **The module carries no notes about one machine.** The comments about a machine's QoS script and T3's launcher are gone.

(6) **What was measured and not changed.** On the owner's laptop, from the start of release `3be8b2f` (09:21) to 12:48 on 20 Nov: no invariant broke, oomd killed nothing, and every freeze thawed within about a minute. All 47 `memory short` episodes came from the PSI trigger on the agents' slice, and 6 of the 8 freezes were at 70–80 % headroom. The slice held 1.3 GB of anonymous memory and 9.5 GB of file cache at its `memory.high` (521 053 `high` events), so the stall was the kernel reclaiming cache. A rule "no freeze while the working set leaves half the budget" was tried and dropped: on the morning of 20 Nov the desktop froze at 64 % headroom (V0.freeze-fast-class-name), and that rule would have stopped the freeze that fixed it. A one-minute freeze of a small job is the cheaper error. A larger slice was not chosen either: it fills with cache in the same way.

Tests: `internal/cgroup/budget_test.go` (TestBudgetShareResolvesAgainstTheMachine, TestBudgetShareWithoutMeminfoLeavesTheFileAndSaysWhy, TestResolveShareKeepsBytesAndRejectsNonsense); `flake.nix` `checks.x86_64-linux.module` (the defaults on the slice and in `config.json`, and a host with `agentsCpuCores = 4`).

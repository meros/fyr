# 0029 Adaptive slots: admit heavy jobs by measured load and a learned cost, not by a fixed count

Date: 2026-11-17 · Status: **proposed; the owner answered Q1–Q4 on 17 Nov** (research, no code; the question was relayed by the coordinator thread t_d8b8040dae603057) · Would amend V0.86 (1) and (6), V0.99 (2), and build on V0.137 (the owner's queue order) and issue #174 (heavy jobs without a slot)

**The owner's words, 17 Nov:** "can we make slots adaptive somehow? Let's assume a job is normally 1 in cost, then if a specific job reliably is less taxing but long running than expected - lower the 'cost'. So it doesn't measure time, it measures system load (memory and cpu). What we want to avoid is system overload, but also not just wait wait wait."

## What would change for the owner, day to day

- The rail's pips become a **budget bar**: "1.4 / 2.0", filled by the cost of each running job. The ring's colour comes from CPU pressure and memory headroom, not from the load average.
- A cheap job (for example a `go test ./internal/x/...` that uses 0.3 cores) starts beside a full `make test` instead of queueing behind it.
- When the box is already overloaded by work outside the slots, a new job waits even if a pip is free. Today it would start, and everything would get slower.
- A slot held by a thread that is not running a job (it is writing the spec) costs almost nothing, so it no longer blocks others.
- Nothing is ever killed by Fyr. When a job turns out heavier than its learned cost, Fyr stops admitting new jobs and tells the thread. Under memory pressure the newest job is frozen until memory recovers (owner, Q3).
- Work that holds no slot is not counted in the budget (owner, Q2); #174's nudges handle it. The CPU pressure gate still feels it, because pressure is machine-level.

## Context

**Today (V0.86):** one pool per host of N slots (`slots`, default 2). Every kind (`test`, `build`, `e2e`) takes one slot. The queue is first come, first served, the self-deploy goes first, and V0.137 adds the owner's reorder. A job's cost is always 1, and the machine's state is never consulted: the pool admits whenever `held < N`.

**What the machine looked like.** This host has 4 cores, 30 GB RAM, 20 GB zram plus 34 GB disk swap, and `agents/` capped at `cpu.max` 4 cores, `memory.high` 13 GB and `memory.max` 14 GB. It was sampled read-only on 17 Nov, 08:47–09:17, every 10 s:
- `/proc/pressure/*`, `/proc/loadavg`, `procs_running`, `MemAvailable`, swap and `/proc/vmstat`;
- every cgroup under `fyr.service/`: `cpu.stat`, `memory.current`, `memory.peak`, `memory.swap.current` and the three pressure files;
- the slot pool, from the store's KV table every 30 s.

Next to the sampler, a probe in this thread's cgroup timed a fixed 20 ms CPU burst every 5 s. Its **stretch** (wall time ÷ CPU time) is how much slower a new job runs right now, and it is used below as ground truth. The scripts are in `research/adaptive-slots/` and run read-only: `sampler.py samples.jsonl 1800`, `probe.py probe.jsonl 1800`, then `analyze.py` and `auc.py` in the same folder; `jobs.py ~/.local/share/fyr/logs` gives the wall times.

| Signal (30 min, 180 samples) | p10 | p50 | p90 | max |
|---|---|---|---|---|
| CPU PSI some avg10 | 19 % | 89 % | 95 % | 98 % |
| load1 ÷ cores | 1.8 | 4.6 | 11.0 | 12.6 |
| run queue (`procs_running`) ÷ cores | 1.5 | 9.5 | 15.5 | 24.8 |
| memory PSI some / full avg10 | 0.01 / 0.00 % | 0.8 / 0.4 % | 9.7 / 4.2 % | 21.5 / 12.5 % |
| io PSI some avg10 | 0.2 % | 0.8 % | 3.8 % | 9.7 % |
| MemAvailable | 4.9 GB | 6.6 GB | 9.2 GB | 9.7 GB |
| swap used | 13.0 GB | 14.5 GB | 18.4 GB | 19.6 GB |
| cores used by Fyr's cgroups | 3.0 | **4.0** | 4.1 | 4.1 |
| probe stretch (a new job's slowdown) | 1.0× | 2.5× | 8.1× | 25× |

What the window showed:
- **Fyr alone filled the machine.** Agents used the whole `agents/cpu.max` (4.0 cores) more than half the time. A new job ran at under half speed 57 % of the time, and at under a quarter 33 % of the time.
- **Slots were not the limit. 73 % of agent CPU came from threads that held no slot.** At most one thread was ever queued. The heaviest work was:
  - a Norban e2e/screenshot thread: 1.0 cores on average, 7.9 GB, peak 9.6 GB, 3.7 GB swapped;
  - a Python data build (`build_admin_ny.py`): 4.7 GB plus 4.8 GB in swap;
  - a Node app at 4.2 GB.
  None of these held a slot most of the time. This is #174, measured.
- **Slots were held without a job.** One thread held a test slot for 18 min at 0.28 cores on average, while it edited the spec. The owner's "wait wait wait" in one picture: a pip is full, the job is not running, and the next job queues.
- **Real job costs vary widely.** E2e runs used 0.9–1.2 cores at 2 GB, with their own cgroup's CPU pressure at 59 %, so they would have used more if they could. One `make test` used 2.3 cores. An idle agent CLI uses 0.01–0.05 cores and 0.7–1 GB.
- **Wall time says little about cost.** In 129 thread logs:
  - `make test` (n = 310): p50 95 s, p90 304 s;
  - layout e2e (n = 155): p50 88 s, p90 254 s;
  - `yarn screenshot:diff` (n = 71): p50 180 s, p90 437 s.
  The same job runs about 3× longer under load. The owner is right that time is the wrong cost.

## 1. Measuring: which signal says "overloaded"

Each signal was scored by how well it separates "a new job runs at under half speed" (stretch > 2) from normal (ROC AUC; 0.5 is chance, 1.0 is perfect; n = 272):

| Signal | AUC stretch > 2 | AUC stretch > 4 | Verdict |
|---|---|---|---|
| CPU PSI some avg10 | **0.88** | **0.75** | Best single signal. Under 50 % the median stretch is ≤ 1.2×; above 80 % it is about 4×. It **saturates** above ~85 %, so it cannot tell 4× from 10×. |
| run queue ÷ cores (instant, `procs_running`) | 0.84 | 0.71 | Nearly as good, with no lag, and it keeps rising past PSI's ceiling. It is noisy, so smooth it over 10–30 s. GNU make's `-l` reads this on Linux for the same reason. |
| swap used | 0.81 | 0.71 | A proxy for how many big jobs are alive, not a cause. It is a level, not a rate (zram keeps idle pages cheaply). Not a control signal. |
| load1 ÷ cores | 0.76 | 0.65 | It lags by a minute. That is the known Ninja `-l` overshoot. V0.80's rail colour uses it; replace it. |
| io PSI some | 0.73 | 0.69 | Low here (p90 3.8 %); it rises with the same jobs. Watch only. |
| MemAvailable | 0.71 | 0.63 | Not a speed signal. It is the **safety** signal (see below). |
| memory PSI some / full | 0.65 / 0.62 | 0.60 / 0.58 | Memory was not the bottleneck in this window (full p90 4 %, peaks of 12–20 %). It is the onset sign of thrashing, which is where real harm starts. |
| swap-in rate | 0.56 | 0.56 | Bursty, peaks of 13 000 pages/s, poor predictor on its own. |

**Decision for the signals:**
- **CPU (speed):** CPU PSI some avg10 is the primary signal. Read it from `/proc/pressure/cpu` or, better, from `agents/cpu.pressure`, which leaves the owner's desktop out. Beside it goes a smoothed run queue per core for magnitude above PSI's ceiling.
- **Memory (safety):** `MemAvailable` minus the predicted peaks of starting jobs must stay above a reserve. Memory PSI **full** avg10 is a stop signal.
- **Not used:** load average, swap used, swap-in rate.

This matches prior art:
- Meta's senpai drives memory from PSI some.
- systemd-oomd kills on memory PSI full avg10 (default 60 % for 30 s) and swap above 90 %.
- Buck2 throttles on memory full above ~10 % over 5 s.
- Kubernetes evicts on `memory.available` < 100 Mi and only exposes PSI as metrics (KEP-4205, GA in 1.36).

**Per-job measurement from cgroup v2.** Each read below is a single file read; per-thread cgroups are already there (V0.80).

| What | File | Use |
|---|---|---|
| CPU work | `cpu.stat` usage_usec delta | Core-seconds, and cores = core-seconds ÷ wall time. |
| CPU starvation | the job's cgroup `cpu.pressure` some | Says whether the measured cores are what the job *got* or what it *wanted*. |
| Memory peak | `memory.peak` | Kernel 6.12+: open the file at the grant, write to reset it, read at the release. That gives the job's own peak on that fd (this host runs 7.3). |
| Memory now | `memory.current`, `memory.swap.current` | The running job's live size, for the guard. |
| Relative to the machine | cores from `cpu.max` of `agents/` (else `nproc`); RAM from `agents/memory.high` (else MemTotal) | Costs are fractions of what Fyr may use, not of the hardware. |

## 2. Identifying a job and learning its cost

**Job class** = project + normalised command, for example `fyr: make test`, `fyr: go test ./...`, `Norban: yarn screenshot:diff` or `Norban: playwright`. The thread is only for attribution: threads are short-lived, while a class recurs across them. The command comes from the gate hook (PreToolUse on Bash, `internal/agent/hooks.go`), which already sees every Bash command. The patterns are #174's host setting (one list for "heavy" and for "class"). The longest matching pattern names the class; an unmatched job in a slot falls back to `project: <kind>`.

**Cost unit.** Today's default pool is 2 slots, so **1.0 = half of what agents may use.** On this host that is 2 cores or 6.5 GB (`memory.high` ÷ 2). A job's cost is its dominant share, as in Dominant Resource Fairness (Ghodsi et al., NSDI 2011): the larger of the CPU and memory shares.

```
cpu_cost = demand_cores / (agent_cores / N0)        N0 = 2, the old slot count
mem_cost = (peak - baseline) / (agent_mem / N0)     baseline = the thread's memory.current at the grant
cost     = clamp(max(cpu_cost, mem_cost), 0.1, 2.0)
```

**CPU demand, not CPU got.** Under contention a job gets fewer cores than it wants and would look cheap. The measured value therefore becomes `demand ≈ got ÷ (1 − cgroup cpu.pressure some)`. The divisor is capped at 4× so a starved job doesn't explode. Runs whose cgroup pressure was above 80 % update the estimate at half weight.

**Learning.**
- Each class starts at the prior **1.0**. Each finished job updates two numbers separately:
  - **CPU:** an EMA of demand_cores with α = 0.3, so about the last 3–5 runs count.
  - **Memory:** a decaying max: `m = max(peak, 0.9·m)`. Memory is never averaged down quickly, because underestimating it is what crashes the machine. VPA does the same: p90 plus a 15 % margin, with a 1.2× bump after an OOM.
- A class needs 3 runs before its learned cost is used. Until then it is `max(learned, 1.0)` with no lowering, so the first few runs are as safe as today.
- Bounds are **0.1–2.0**.
- The table lives in the store's KV table (`slot-costs`) and survives restarts, like the pool.

**Attribution, in two stages.**
1. **Slice 1–3:** the thread's cgroup while the slot is held, minus the thread's idle baseline (its CLI is about 0.03 cores and 0.7–1 GB, measured just before the grant). Subagents, background jobs and an `&` are counted correctly, because they are in the same cgroup. Two weaknesses:
   - a thread that also does other heavy work during the hold makes the job look heavier;
   - a thread that holds the slot but runs nothing makes it look lighter.
   The second is today's 18-minute hold. It is caught by an "idle hold" rule: after 3 min under 0.2 cores with no matched command alive, the slot's cost drops to 0.1 and the thread is nudged to release (#174's last box).
2. **Later, optional:** exact per-job cgroups. The gate hook can already return `updatedInput`, so it can rewrite a matched command to `fyr job run --class 'make test' -- <cmd>`, which starts the command in `agents/<thread>/job-<n>/`. cgroup v2's no-internal-process rule means the thread's own cgroup must then split into `cli/` and `job-*/` leaves. That is a V0.80 change, so it is a separate slice.

## 3. Admission: a budget instead of a count

**Rule.** The pool holds a budget **B** in cost units, starting at **B = N0 = 2.0**: with every cost at its prior 1.0 it behaves exactly like today. The queue's head is admitted when **all** of these hold:

1. **CPU:** `Σ cost(running slotted jobs) + cost(head) ≤ B`. Work without a slot is not counted (owner, Q2; section 5);
2. **CPU pressure:** `agents/cpu.pressure` some avg10 < **P_cpu** (60 %), unless the head's cost ≤ 0.25 (cheap jobs backfill anyway; see 4);
3. **Memory headroom:** `MemAvailable − Σ (predicted peak − current) of running jobs − predicted peak(head) ≥ R_mem` (max(2 GB, 10 % of RAM));
4. **Memory pressure:** memory PSI full avg10 < 5 %.

Rules 3 and 4 are never waived. Rules 1 and 2 give way to the progress guarantee (section 4).

**Adapting B (AIMD, slow).** Every 30 s:
- if a job waits **and** CPU PSI some avg10 < 40 % **and** memory full < 1 % **and** no admission happened in the last 60 s: `B += 0.25`, up to `B_max = 2·N0`;
- if CPU PSI some avg10 > 85 % for 60 s **or** memory full > 5 %: `B = max(1.0, 0.75·B)`.

B is shown, never hidden. AIMD is the TCP and Netflix concurrency-limits pattern; the 60 s holds are there because PSI avg10 and the jobs themselves take time to ramp up. Bazel's load scheduling counts actions started in the last 5 s for the same reason.

**Options compared**

| Option | How it decides | Fit for Fyr |
|---|---|---|
| **Fixed N (today)** | count < N | Ignores both job size and outside load. In the window: 2 pips full while the machine ran at 4.0 cores with 73 % from unslotted work. |
| **GNU make `-l` / Ninja `-l`** | start only while the load is under L; always 1 if none run | Load average lags a minute (Ninja overshoots). Make on Linux reads the instant run queue instead. Good as a guard, too coarse alone. |
| **Credit budget (Bazel `--local_resources`, Kubernetes requests, Nomad)** | `used + requested ≤ capacity` from declared or learned sizes | Bazel and Kubernetes pack by **requests, not usage**; Bazel's default capacity is the cores and 2/3 of RAM, and it always admits one action (`--allow_one_action_on_resource_unavailable`, default on). This is rule 1 above, with costs learned instead of declared. |
| **PSI-driven AIMD (Buck2 resource control, senpai)** | concurrency up when idle, down under pressure | Buck2 suspends the newest action under memory pressure and never the oldest. This is how B adapts. Alone it reacts late, after the overload is felt. |
| **Kubernetes eviction, systemd-oomd** | kill or evict under pressure | Fyr does not kill an agent's job. oomd stays the machine's last line, outside Fyr. |
| **BuildKit `max-parallelism`** | a plain count | Same as today. |

**Recommendation:** credits from learned costs (rule 1) plus PSI and memory gates (rules 2–4), with a slow AIMD on B. The credits stop the box from being overcommitted before it is felt. PSI catches what the credits get wrong, including all the work outside Fyr's knowledge.

## 4. Not "wait wait wait"

- **Order.** The queue keeps the owner's order (V0.137) and the self-deploy's priority. The head is always the next candidate.
- **Progress guarantee.** If no slotted job runs, the head is admitted regardless of rules 1 and 2, as Bazel, Ninja and make do (always one) and Buck2 does (the oldest never suspended). Only the memory rules (3, 4) can hold it, and only while the memory guard is red. Then the thread is told why ("waiting: 2.1 GB free, the job needs ~6 GB").
- **Backfill.** A job behind the head may start when the head does not fit, under EASY backfilling (Slurm; Mu'alem & Feitelson 2001). The waiting job must fit the budget left over **and** either:
  - cost ≤ 0.25 (cheap: it barely touches the head's room), or
  - a learned p90 duration under the earliest predicted end of a running job, so it is gone before the head could have started.
  The learned durations come from the same class table.
- **Aging cap.** A head passed by backfill for 10 min, or 3 times, stops backfill until it starts. An expensive job (cost 2.0) can't starve behind a stream of cheap ones.
- **Idle holds.** A held slot with no matched command alive for 3 min and under 0.2 cores counts as 0.1, and its thread is nudged to release. This is the 18-minute hold above.
- **Heavier than predicted.** Nothing is killed. The running job's cost is its live measurement once that is higher than the prediction, so new admissions stop by themselves. The thread gets one notice ("your `make test` uses 3.1 cores, predicted 2.0; others wait") and the learned cost rises.
- **Memory guard.** When `MemAvailable` falls under `R_mem` **or** memory full avg10 > 10 % for 10 s:
  - no new admissions, the rail ring turns red, and the thread whose job grew most since its grant gets a notice;
  - `agents/memory.high` (already set) throttles before `memory.max` kills;
  - **freeze the newest slotted job** (owner, Q3; Buck2's `cgroup_freeze` strategy):
    - write `1` to its job cgroup's `cgroup.freeze`; the thread gets a notice ("your `make test` is paused: 1.2 GB free; it resumes when memory recovers");
    - the **oldest running job is never frozen** (progress guarantee), and at most one job is frozen at a time;
    - it thaws when `MemAvailable ≥ R_mem + 1 GB` **and** memory full avg10 < 2 % for 30 s, or after 10 min at most;
    - a freeze does not free memory (the frozen pages may be swapped out); it stops the job's growth and gives the CPU and I/O to the others so they finish and release theirs;
    - the frozen job's own timeouts (a test's deadline, a Playwright timeout) may fire after the thaw, and the notice says so;
    - **this needs S5's job cgroups.** Freezing the thread's cgroup would also freeze the agent CLI and its API stream. Until S5, the guard sends notices only.

## 5. Agents that never call slot_acquire (#174)

**Owner, Q2: ignore it in the budget.** Work without a slot is left to #174 (the prompt rule and the gate hook's nudge); the budget counts only slotted jobs. Consequences, stated so they are not surprises:
- The CPU pressure gate (rule 2) and the memory rules (3, 4) read machine-level signals, so they still feel unslotted work. A job can wait because of it, and its wait reason names the pressure, not a thread.
- V0.80 already shows each thread's CPU in its row; S1 adds nothing more for unslotted threads.
- In the window, unslotted work was about 1.3–1.6 units most of the time. Without #174 making that work take slots, the pressure gate will often be the binding rule.

The option that was not chosen, kept for the record: count a thread without a slot as load **U** when it uses more than 0.5 cores or 1.5 GB above its baseline over 30 s;
- with B = 2.0, only cheap jobs would have been admitted, and the rail would have said why: "1.5 of 2.0 used by work without a slot".

and add U to rule 1. It can be revisited from S1's data if #174 does not close the gap.

## 6. Slices

Each slice ships alone and is useful alone.

| Slice | What | The owner sees | Risk |
|---|---|---|---|
| **S1 Measure and show** (no behaviour change) | `GET /api/load` adds `agents/cpu.pressure`, memory PSI full, MemAvailable, the smoothed run queue, each thread's cgroup CPU pressure, `memory.peak`. Per slot hold, a record: class, cores, demand, peak, wall time. The rail ring's colour moves from load average to PSI and headroom. | The ring is honest. Desktop tooltip and phone thread row: "1.8 cores · no slot" or "make test · 2.1 cores · 3.2 GB". Settings → Automation lists the measured jobs. | Low. Wrong attribution only mislabels. |
| **S2 Budget admission** | Costs fixed at 1.0 except the idle-hold rule. Rules 1–4, the progress guarantee, the memory guard and its notices. B fixed at N. Pips become a bar. | Fewer jobs start while the box is under pressure. A queued thread says why it waits. | Too strict: the owner sees more waiting while pressure is high. That is the point, but the thresholds may need tuning; each is a host setting. |
| **S3 Learn costs, backfill** | The class table, the EMA and decaying max, the 3-run rule, EASY backfill with aging, and the "heavier than predicted" notice. | Cheap jobs run beside big ones. Settings lists each class's cost and how many runs it is based on, with **Reset**. | A bad learned cost: bounded 0.1–2.0, memory never learned down fast, and the memory guard holds regardless. |
| **S4 Adaptive B** | AIMD on B from PSI. | The bar's capacity moves (for example "2.5"), with the reason in the tooltip. | Oscillation: slow steps, 60 s holds, bounds 1.0–2·N. |
| **S5 Job cgroups and the freeze** | The gate hook wraps matched commands in `fyr job run`; the thread cgroup splits into `cli/` and `job-*/`. The memory guard freezes the newest job (Q3). | Exact per-job numbers. A paused job in its thread row ("paused: memory") on desktop and phone. | Changes V0.80's tree, and a wrapped command could change behaviour. Required by Q3's freeze; S2's guard sends notices only until then. |

Desktop and phone stay in sync: the phone has no rail ring (V0.99 (3)), but the thread row shows the same cost and wait reason as the desktop row (`web/src/row-ui.tsx`).

## Risks

- **The signal is shared with the owner's own work.** System PSI includes the browser and editor. Reading `agents/cpu.pressure` keeps admissions about Fyr's work, but the memory headroom is machine-wide on purpose.
- **Attribution by thread is coarse** (section 2). The idle-hold rule and S5 are the answers.
- **An overloaded box was the normal state in this window.** With the proposal, jobs would have waited more, not less, until #174 makes the unslotted work take slots. The win from S3 comes from cheap jobs and idle holds; the win against overload comes from S2.
- **The probe measured from one thread cgroup** with cpu.weight 100 (300 while the owner watches it, V0.85). Other threads saw similar or worse stretch. The AUC ranking is what matters, not the absolute values.
- **One 30-minute window on one host.** S1 records continuously, and the thresholds (P_cpu 60 %, R_mem) should be set from a week of S1's data before S2 is enabled.

## The owner's answers (17 Nov)

1. **CPU overload signal:** PSI some, with the run queue for the size above PSI's ceiling (recommended).
2. **Unslotted heavy work:** ignore it in the budget (the recommendation was to count it as U). Section 5 records the consequences.
3. **A job heavier than predicted, under memory pressure:** stop admissions, notify, **and freeze the newest job** (the recommendation was notices only). Section 4 gives the rules; S5 is now required for it.
4. **Backfill:** EASY backfill with an aging cap (recommended).

## Sources

- Linux PSI: https://docs.kernel.org/accounting/psi.html · cgroup v2 (`cpu.stat`, `memory.peak` reset per fd, `memory.high`): https://docs.kernel.org/admin-guide/cgroup-v2.html
- systemd-oomd: https://www.freedesktop.org/software/systemd/man/latest/oomd.conf.html, https://www.freedesktop.org/software/systemd/man/latest/systemd-oomd.service.html
- Meta TMO and senpai: https://engineering.fb.com/2022/06/20/data-infrastructure/transparent-memory-offloading-more-memory-at-a-fraction-of-the-cost-and-power/, https://github.com/facebookincubator/senpai/blob/main/senpai.py
- Kubernetes:
  - resource management: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
  - node-pressure eviction: https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/
  - KEP-4205 (PSI metrics): https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/4205-psi-metric
  - VPA flags: https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler/docs/flags.md
- Bazel: https://bazel.build/reference/command-line-reference (`--local_resources`), https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/actions/ResourceManager.java
- Buck2 resource control: https://github.com/facebook/buck2/blob/main/app/buck2_resource_control/src/scheduler.rs
- GNU make: https://www.gnu.org/software/make/manual/html_node/Job-Slots.html, https://www.gnu.org/software/make/manual/html_node/Parallel.html, https://github.com/mirror/make/blob/master/src/job.c · Ninja: https://github.com/ninja-build/ninja/blob/master/src/real_command_runner.cc
- Nomad: https://developer.hashicorp.com/nomad/docs/job-specification/resources, https://developer.hashicorp.com/nomad/api-docs/operator/scheduler
- BuildKit: https://github.com/moby/buildkit/blob/master/docs/buildkitd.toml.md
- Adaptive limits: TCP (RFC 5681) https://www.rfc-editor.org/rfc/rfc5681 · Netflix concurrency-limits https://github.com/Netflix/concurrency-limits · Envoy adaptive concurrency https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/adaptive_concurrency_filter
- Backfill: Slurm https://slurm.schedmd.com/sched_config.html · Mu'alem & Feitelson, IEEE TPDS 12(6), 2001 https://www.cse.huji.ac.il/~feit/papers/SP2backfil01TPDS.pdf
- Dominant Resource Fairness: Ghodsi et al., NSDI 2011

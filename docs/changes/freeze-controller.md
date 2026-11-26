# V0.freeze-controller: the load simulation gets the 26 Nov incident and a load ladder; three pressure-driven freeze rules were simulated and not taken

→ V0.load-sim, V0.sim-fixes, V0.freeze-cap-holds-memory.

Why: on a 30 GiB laptop the agents sat in a critical stall for minutes while the guard froze at most as many threads as it allowed. The owner asked for one feedback loop in place of the fixed limit, the 60 s wait and newest-first, simulated and researched.

(1) **Scenarios.** `incident-26nov` (one thread's four lint parts of 7.5 GB, a 5 GB smoke run, six small jobs, nine agents, swap cap 10 GB); `incident-retry` (the same with the desktop at 9 GB and a new lint run every 30 s, which is what the log showed); `ladder-2/4/6/8` (n threads, each a 3 GB undeclared job). The first model of the incident did not reproduce it (35 s of critical stall against 2110 s with no Fyr); the retry and the desktop did (6 of 37 jobs done, 31 threads lost to oomd). That is listed in the scenario's `expect` as a finding.

(2) **Tried, not taken.** Each was run against the whole table:
- the freeze limit gives way to pressure (all heavy threads may freeze, whole, when the unit is near oomd's limit or swap is full in a critical stall): `ladder-6` lost a thread to oomd that it kept before, and `incident-retry` did not improve (31 killed, critical stall 184 s against 189 s);
- the thread holding the most goes first, and the wait between freezes shrinks from 60 s to none as the unit's stall goes from 10 % to oomd's limit: `ladder-4` took 7 min (5 before) with 8 freezes (3), `ladder-6` and `ladder-8` lost threads to oomd.
Faster freezing flaps: a thaw pages the work back in and raises the stall again, so more freezes cost more than they save in this model. The model counts the pages the kernel reclaimed from a frozen cgroup as its pgscan, so oomd picks the frozen thread; how real oomd ranks it is not measured.

(3) **Built: the pool counts a thread's runs at once.** A thread held one admission whatever number of heavy commands it ran, so its lint run started every 30 s counted as one need (a learned command never waited either, once the thread held a slot). Now `Holder.Instances` and the open heavy calls per thread (`Engine.HeavyOpenCalls`, published without a lock like `HeavyOpen`) make admission count each run's need (`Admission.OpenCalls`), and the gate asks the pool for room for both before a second command starts beside one that runs (`Pool.GrowFor`), learned commands included: a concurrent run that finds no room waits and is told when it is admitted. A hold's need was the thread's class need; with several runs of mixed classes it is the hold's class times the number of runs.

Found with a per-minute trace of the simulation: the pool's "one job always runs" rule decided the thread as if it were alone, so a grow for a fifth run passed as a first; and the runs that go counted nothing against the headroom (their growth read 0.0 GB). A grow is now decided beside the runs open at that moment (not when it queued: they may have ended), with their growth. `HeavyDone` republishes the open count at every call that ends (a unit test found it only did at the last).

Simulated (`concurrent-4/8/16`; `-old` has the open calls counted as one need): the 20 earlier scenarios are unchanged.

| scenario | before | now |
|---|---|---|
| `concurrent-4` | 31 s critical stall, 1 freeze, 1.3 GB swap, 8 min | no stall, no freeze, no swap, 8 min |
| `concurrent-8` | 12 done, 1374 s critical stall, 28 min | 12 done, no stall, 15 min (serial 31) |
| `concurrent-16` | 7 of 20 done, 13 killed by oomd | 20 of 20 done, none killed, no stall, 28 min (serial 57) |
| `incident-retry-class` (the lint command a class) | 7 of 37 done, 30 killed | 37 of 37 done, none killed, 84 s critical stall, 42 min (serial 109) |

(4) **The learner taught the gate junk.** The learned list on a real host held `done`, `python3`, `python3 <<'EOF'`, `backend` and `find /`, and none of the lint runs. `Suggest` took a shell keyword (a `for` loop's `done`) and a bare interpreter running a heredoc as the command, and the gate held other commands as class `done`. `Trivial` now covers shell keywords and a bare interpreter (`python3`, `bash`, `node` alone or with `<<`, `-c`, `-e`), `Suggest` returns none for them, and a learned pattern that is trivial is dropped at load (it already was for the older trivial set). The lint runs stay out of reach for another reason: the suggestion is built from the process command line (`node …/yarn-3.5.1.cjs run lint:typecheck` under a worktree path), which the Bash text the gate matches (`yarn lint`) does not match, and each worktree makes a new pattern, so none reaches LearnAfter. Not fixed here.

(5) **Tried, not taken: a share gate for commands no class matches.** While a thread's undeclared work holds its share of the budget (budget over the threads with heavy work, at least two), a new command that is not plainly light (an allowlist of git, cat, ls, grep and the like) waits. With only that rule `incident-retry` finished 16 of 37 (6 before) and lost 21 threads to oomd (31), but its critical stall grew from 189 s to 2590 s: held commands retried in the same second all found the share free, because what a start holds shows in the reading after a while (7 lint runs at once from 21 held). Charging each start for 30 s took it back to 9 of 37 and 28 killed, since the retry arrives every 30 s. Neither was shipped: a rule that holds an agent's commands on a guess needs a better result than that.

(6) **What the simulation says is next.** What is left is the work no class matches: `incident-retry`, where an agent starts a heavy command Fyr cannot name, still loses threads to oomd in the model. Freezing does not stop it (the parts start whatever the guard holds, and a frozen part keeps its memory once the swap cap is full), and the share gate above did not either. What is not tried: learning the lint commands from the Bash text of the call instead of the process command line, so the existing class gate and the runs-at-once count (3) reach them; and a rerunnable class (decision 0036 (F)) killed instead of frozen.

Research (control laws that were considered): Senpai's integral controller with a small probe and a quadratic backoff; Buck2's 10 %/5 % hysteresis, youngest first, oldest never; lmkd's one kill at a time. The report is in the PR description.

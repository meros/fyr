# V0.gate-release-paced: after memory recovers the gate lets the commands it held run one thread at a time

→ V0.guard-wake (E1), V0.gate-hold-visible, V0.subagent-admission, V0.load-sim, decision 0036 (E1). Closes #736.

(1) **What happened.** Over 24 h on a laptop host the gate held 69 heavy commands, 26 of them within 10 min of the same thread's previous release. One thread was held 6 times in 23 min on one class, three others 4–5 times, each time after "[Fyr] Memory has recovered on this host: run the heavy command Fyr held again." Freezes of 1–13 min followed the reruns, and their peaks ran well above what their classes predicted (a smoke run at 43 % of the agents' memory against 16 %, a check at 44 against 16, a test at 47 against 24).

(2) **The cause.** On the first tick memory was no longer short, the guard asked `ClearGate` (`internal/guard/tick.go`), and the engine cleared every held thread and told all of them to run again at once (`internal/engine/freezeguard.go`). A held command is one the pool does not queue: a thread's next command beside an admission it holds, or a learned command, which takes a free share or simply runs. So the reruns met no admission that could say no: all of them started into the room one of them had, and memory was short again within a minute.

(3) **The rule.** The gate keeps its held threads in order and the guard lets them run one thread at a time:
- the first on the tick memory is fine by the wake rule's test (`wakeFine`: no elevated or critical stall, and the headroom at least the reserve + 5 % unless a wake cap holds), and no frozen work thawed on that tick;
- each next one `GateSettle` (60 s) after the one before it ran its command again, or `GateRerunWait` (2 min) after its release when its agent did not run it;
- in order of the project's priority (V0.project-priority), then the one that has waited longest. A released thread that is held again because memory went short keeps its place.
While threads still wait for their turn, a new heavy command that the gate would hold while memory is short (same rule, `GateHolds`) waits behind them. Its agent reads: "Heavy job (…): memory has just recovered on host-a, and Fyr lets the heavy commands it held run one at a time, so they do not fill it again (n ahead). It did not run. …", and the thread shows Waiting with "memory recovered on host-a; the held heavy commands run one at a time, and the … command waits its turn" (V0.gate-hold-visible). A released thread's next command runs. Each thread is still told once, with the same notice, when its turn comes: no new kind of notice and no more of them. A host where nothing was held is not touched: no thread waits longer while the host is calm.

(4) **Never interrupt the owner.** Nothing here reaches the owner's screen but the thread's Waiting reason, which updates in its row as before; nothing takes the focus or opens anything.

(5) **The simulation.** A new invariant, `rehold`, fails a run where the gate holds a thread's command while memory is short within 10 min of letting that thread's held command run. A new scenario, `recovery-herd`, models the incident: the desktop takes memory for 4 min while six threads start a learned check (2.5 GB learned, 3 GB real), so the gate holds all six, and each thread runs it twice more later on. The simulation can now name classes the gate takes as learned (`scenario.soft`).

| scenario | before | now |
|---|---|---|
| `recovery-herd` | 12 of 18 done, 5 threads killed by oomd, 86 min, 4597 s critical stall (no Fyr: 42 min, 2416 s), `rehold`, `oomd-kill`, `worse-than-none` | 18 of 18 done, none killed, 26 min, 0 s critical stall, no freeze |
| `incident-26nov` | 10 min, 5 freezes, 35 s critical | 9 min, 3 freezes, 38 s critical |
| `concurrent-4-old` | 19 min, 2 freezes | 20 min, 1 freeze |

No other row changed. `incident-retry-class` shows `rehold` before and after, and is listed as known: one thread starts a new lint run every 30 s into the stall its own runs make, and the gate never holds more than that one thread, so there is no herd to pace.

(6) **Tried, not taken: release nothing while work is frozen.** It made `incident-26nov` hold one frozen thread with no other heavy work running for 30 s (`max-frozen`): the held thread counts as holding an admission, so the frozen one could not wake alone, and the gate waited for it. The release now skips only a tick on which frozen work thawed.

Tests: `internal/guard/tick_test.go` (TestTickReleasesTheGateOneThreadAtATime, TestTickRetriesTheGateOnRecovery), `internal/engine/heavy_test.go` (TestGuardWakeTheGateHoldsANewCommandWhileMemoryIsShort), `internal/engine/sim_test.go` (TestSimLoad: `recovery-herd`, `rehold`).

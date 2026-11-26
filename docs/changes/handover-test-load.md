# V0.handover-test-load: the handover tests wait for the agent's turn, not 5 s

→ V0.trans-host-handover, V0.153 (a slow start under pressure), V0.fix-flakes; `internal/front/handover_test.go`.

`TestTransHostHandoverResumeAndInjectNotice` failed once in a local `make test` while the host was short on memory: "timed out waiting for first run on host-a" after 7.9 s.

**The cause: a wall-time budget on a real process start.** The handover tests run a real agent: the test binary itself, started as the fake CLI. Each wait for a turn used `eventually`, which gives up after 5 s. Alone, the first run takes 0.3 to 1 s; beside 8 busy loops, 50 of 50 runs passed and the slowest took 0.96 s. CPU load does not slow it much. Memory pressure does: starting a large binary while pages are swapped out can take seconds before it writes anything. Fyr itself allows a start a minute (`InitTimeout`) on a calm host, and more under pressure (V0.153). The test asked for 5 s, which the product never promised.

The host-a offline/online lines (`api/stream: EOF`) in the log are the front's stream to a host on the loaded host. They do not affect this wait, which reads host-a's engine directly. `sql: database is closed` comes from the agent's last writes after the test's cleanup closed the store. That is noise, as V0.fix-flakes found.

**The fix.** The four turn waits in the handover tests use `awaitRun`. It ends when the turn has ended with the expected result, and it fails at once when the thread records a `Failure`, so a start that fails still fails at once. Its bound, `runGuard`, is a hang guard set past the product's own start deadline (`InitTimeout` + 1 min, with `InitTimeout` set explicitly on these hosts). A slow start passes. A stuck start fails on the Failure the product records at `InitTimeout`. Only a turn the product never ends reaches the guard.

**Proof.** With every fake CLI start delayed by 6 s (`FYR_FAKE_INIT_DELAY_FILE`, which stands in for a start on a thrashing host), main's tests fail with the same "timed out waiting for first run on host-a". The tests in this change pass.

After: the package passes, and 250 runs of the five handover tests (`-count=50`) beside 8 busy loops pass.

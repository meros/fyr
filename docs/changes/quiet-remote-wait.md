# V0.quiet-remote-wait: a background command that visibly waits on a remote system is called quiet only after an hour, not after 5 min

→ V0.195 (3) and (4). Amends V0.195 (3): how long a quiet thread must stay quiet before its agent is woken.

**What was seen.** On 25 Nov the quiet-wait wake of V0.195 fired 10 times in 8 threads, and every one was a wait that was right: a deploy-status tool with `--wait` (6 times), a CI helper with `--wait`, `gh run watch`, `kubectl wait --for=condition=complete` on a job, and an `until gcloud … ; do sleep 60; done` loop on a rollout. Each wake turned a waiting thread back to working only for the agent to say it was still waiting. The wake already came once per task: the threads told twice ran two different commands.

**The rule the logs support** reads the command, not the network: a command waiting on a remote system uses no CPU on this host and often holds no socket between polls, so a measure of network I/O would miss the `sleep 60` loops. The command text named the remote wait in all 10 cases.

1. **A remote wait** is a background Bash whose command has a `--wait` flag, `gh run watch`, a `gh … --watch`, `kubectl … wait` or `kubectl … rollout status`, or a polling loop (`until`/`while` … `do` … `sleep`, as in V0.195 (5)) that calls a network client (`gh`, `gcloud`, `kubectl`, `curl`, `wget`, `aws`, `az`, `terraform`, `tofu`, `helm`, `ssh`). `core.RemoteWait` decides it when the task starts, from the Bash call's input; the task keeps it as `remoteWait`.
2. **Its quiet counts after `QuietRemoteAfter` (1 h)** instead of `QuietAfter` (5 min). The CLI ends a background Bash at its timeout (30 min unless the agent gives one, at most 2 h), so most remote waits end before Fyr says anything; one that is still quiet after an hour is woken about once, as before. Other background commands keep the 5 min count, so the 18 Nov stall (a local loop on a condition that could no longer turn true) is still caught.
3. **Once per task** stays (V0.195 (4)).

Tests: `internal/core/quietwait_test.go` (`TestRemoteWait`: the 25 Nov shapes and local loops), `internal/engine/quietwait_test.go` (`TestRemoteWaitIsToldMuchLater`, `TestLocalWaitIsNotARemoteWait`).

# V0.ci-superseded-checks: a cancelled run that a newer run of the same check replaced does not fail CI, and the runner's green `test` wins over a red `local/test` on the same head

→ amends V0.112 and V0.advisory-checks (which checks Summarize reads), V0.ci-self-hosted (5) (what "counts the same" means).

Why. Two cases closed merge asks on heads that were green:

- **A superseded cancelled run.** Marking a PR ready starts a workflow a second time. Its concurrency group cancels the first run, and GitHub keeps that run in the head's rollup next to the new one. Fyr read the cancelled run as a failed check. In one case a board-automation workflow was cancelled at 15:42:12 and succeeded at 15:42:35, and Fyr reported "CI failed" for it.
- **A red `local/test` behind a green runner `test`.** `scripts/ci-local.sh` posts `local/test` on the head. A run on a loaded host failed on load flakes, while the self-hosted runner's `test` passed on the same SHA. Both counted, so the red status held every merge ask on that head. Agents pushed empty commits to get a fresh head, or the owner merged on GitHub by hand.

(1) **A superseded run.** A check run whose conclusion is cancelled does not count when the same head has another run with the same name in the same workflow that started later. With no start time, any run of that check that was not cancelled supersedes it. A cancelled run that nothing replaced still fails, and so does the newest run when it is the one that was cancelled. A check of the same name in another workflow does not supersede it. The batched poll now reads each check run's `startedAt` and its workflow's name (`checkSuite.workflowRun.workflow.name`), as `gh pr view` already did.

(2) **A fallback pair.** A commit status `local/X` stands in for the check `X`, the same gate run on a host instead of the runner (`scripts/ci-local.sh` posts `local/test` for `pr.yml`'s `test`). One result on a head decides:
   - **The runner's `X` passed:** `local/X` does not count, whatever it says. A red local gate there is the host's load, not the head.
   - **`local/X` passed while `X` has not finished:** `X` does not count. This is the fallback case: the runner is down and its job stays queued. Before this change, the queued job held the ask even after the fallback passed.
   - **The runner's `X` failed:** it counts, even when `local/X` passed. A red runner check is fixed or re-run on the runner. A host's run does not outvote it.
   - **Otherwise:** both count as before. A red `local/X` while `X` still runs fails the head, and the ask opens again once `X` passes.

(3) **One rule everywhere.** `github.Gating` applies the advisory rule, (1) and (2). The poll and the merge ask (`fromView`, for `gh pr view` and the batched read alike), the detail's summary and the host's last look before it merges (`MergeBlock`) all read the checks through it. The PR detail still lists every check with its state.

(4) **`scripts/ci-local.sh` is unchanged.** It still posts failure when its gate fails, including a test that timed out under load. Under (2), that red status no longer matters once the runner's `test` is green. A host's gate that hides its own failures would let a real red result through when the runner is down.

Tests: `internal/github/superseded_test.go`.

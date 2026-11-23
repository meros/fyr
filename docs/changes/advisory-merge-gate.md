# V0.advisory-merge-gate: the host's last look before a merge leaves advisory checks out too, through one predicate

→ V0.advisory-checks, V0.43, V0.112. *Amends V0.advisory-checks (1): the rule holds wherever Fyr decides whether CI passed for a merge.*

Why: V0.advisory-checks left advisory checks out of the PR's checks that the poll, the merge ask and auto-merge read, but the merge itself read the PR again (`MergeChecked`) and `MergeBlock` looked at every check of that read: a queued advisory check (a job for a Mac that sleeps) still refused the merge with "CI has not passed on … yet", although `test` had passed and the ask had been shown.

(1) **One predicate.** `github.Counts(name)` is the rule (every check gates a merge but one whose name ends in `(advisory)`). The PR's checks (`fromView`: the poll, the batched poll, the merge ask's gate, Ready and auto-merge), the detail's summary and `MergeBlock` (its failing-check count, its running-check look and its "no check has reported" text) all read the checks through it. The deploy's read of main's check runs is not a merge gate and is unchanged.

(2) **Tests.** `MergeChecked` merges with `test` passed and an advisory check queued or failed; `MergeBlock` neither waits for a running advisory check nor counts a failed one, and a PR whose only check is advisory still waits for CI.

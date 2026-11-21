# V0.merge-refused-after-merged: a merge result that arrives after the PR was observed merged leaves no `mergeable` condition

→ decision 0018; fixes a `thread-change-invalid` violation (V0.thread-invariants): "non-OPEN PR (MERGED) still has mergeable condition".

(1) `mergeRefused` (V0.136) sets the `mergeable` condition only while the change's PR is open. A failed merge result (a timeout, a read error) can arrive after Fyr observed the PR merged; the condition then described a PR that no longer needs anyone.

(2) `host.restarted` drops checks, review, conflict and `mergeable` conditions from any change whose PR is not open, so a record saved by an older build stops tripping the load check.

Tests: `TestMergeRefusedAfterMergedLeavesNoCondition`, `TestRestartDropsConditionsOfNonOpenPR` in `internal/core/thread_invariants_test.go`.

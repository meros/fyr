# V0.thread-change-pr-twins: two changes never hold one PR, whoever made them

→ SPEC sections 3, 5, 6 and 7; extends V0.87 (2) (one work item, one card) and the `thread-change-invalid` invariant of V0.thread-invariants.

Evidence: `thread-change-invalid` fired at load with `duplicate PR URL <url> across changes`.

(1) **Cause.** V0.87 (2) folded only a change Fyr made for a PR (`pr:<repo>#<n>`) into another. Two changes keyed by the agent that both got the same PR (as `pr` evidence or `change_open`'s PR) were never folded, so a thread could store both, and the invariant fired on every load.

(2) **Fix.** `core.FoldSamePR` folds any changes that hold one PR URL into the first one not made for the PR; the others become aliases. It runs when a change is opened or joined (`joinPRWork`) and on load (`migrateDone`), so stored twins heal on the next start.

Tests: `internal/core/prtwins_test.go`.

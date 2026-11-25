# V0.merge-github-trouble: a merge GitHub answers with its own error is read again, taken as done when the PR merged, and tried again before it fails

→ V0.43, V0.97, V0.136, V0.205 (3).

(1) **The cause.** When Fyr ran a merge the owner had pressed, any error from `gh pr merge` was the merge's failure, at once. The agent heard "fix the cause, and ask again". On one PR GitHub answered `GraphQL: Something went wrong while executing your query` (a 500) twice in 12 s, and the PR itself was fine. On another, GitHub answered `502 Bad Gateway` but had merged the PR, and Fyr still told the agent to fix it and ask again.

(2) **Read again.** When GitHub's own trouble answers the merge, `MergeChecked` waits and reads the PR again. GitHub's own trouble means a 5xx, GraphQL's "Something went wrong", or a connection that failed or timed out (`github.GitHubTrouble`). If the PR is merged, the merge succeeded, and the owner and the agent hear that it merged. If it is still open on the same head and nothing blocks it, Fyr merges it again, bound to the same head (V0.97). A read that GitHub's trouble answers is read again the same way.

(3) **Bounded tries.** Fyr waits 5 s, 15 s and 30 s between tries: 4 tries over about 50 s, each one read and at most one merge. When GitHub fails every try, the merge fails with "GitHub failed, not the PR" (`core.GitHubFailed`), the number of tries and GitHub's last words. The agent's next step is then to check GitHub's status, wait a few minutes and ask again, not to fix the PR. A refusal is not GitHub's trouble, so it fails at once as before. Refusals are a conflict, a rule, any other 4xx, a rate limit or a failed login. "Base branch was modified" keeps its own retries (V0.205).

Tests: `internal/github/merge_trouble_test.go` (TestMergeGitHubTroubleMergedAnywayIsSuccess, TestMergeGitHubTroubleRetriesAndMerges, TestMergeGitHubTroubleGivesUpSayingGitHub, TestMergeGitHubTroubleOnlyForTrouble), `internal/core/merge_trouble_test.go` (TestMergeGitHubTroubleNextIsWait).

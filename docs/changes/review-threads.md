# V0.review-threads: a blocked merge ask names unresolved review threads, and asks for a review only when the rules require approvals

→ V0.136 (the merge ask waits for green CI; a BLOCKED head names its rule), V0.178 (the batched poll). *Amends V0.136*: a `pull_request` rule on the base no longer means "a review is required" by itself.

A PR was green and GitHub read it as BLOCKED with an empty review decision. The ruleset on its base had a `pull_request` rule that required 0 approvals and every review thread resolved, and one bot review thread was open. Fyr refused the merge ask twice with "a review is required by the rules of main … request a review, or ask the owner to approve it". The owner had opened the PR, so GitHub would not let them approve it anyway. The agent and the owner chased an approval nobody needed. Fyr read the rule's type and ignored its parameters, and GitHub's empty review decision already meant that no review was required.

(1) **The rule's parameters.** The rules read (`GET repos/{repo}/rules/branches/{base}`, still once per repo and base, cached 30 min) keeps the most approvals any `pull_request` rule requires (`required_approving_review_count`). It also keeps whether one of them requires review threads resolved (`required_review_thread_resolution`). They go on the PR as `approvals` and `threadResolution`, next to `rules`.

(2) **The unresolved threads, in the poll's own query.** The batched GraphQL read of V0.178 also asks for `reviewThreads(first: 100) { totalCount nodes { isResolved } }` and counts the unresolved ones. Threads past the first 100 count as unresolved. A repo with one watched PR is now read by the same query, not by `gh pr view`, which cannot read review threads: it is one GraphQL call either way, so the cost stays flat. A PR the query does not return is still read alone (V0.178). A read by `gh pr view` (the runner's read before a merge) leaves the count unknown (-1).

(3) **The reason, in this order:**
- REVIEW_REQUIRED, or a rule that requires approvals and no approval: "a review is required by the rules of <base>". The next step says to request a review from someone who can approve, and that the PR's author cannot approve their own PR. When the owner opened it, someone else approves it, or the owner merges it on GitHub with admin rights.
- Thread resolution required and threads unresolved: "N review threads are unresolved and the rules of <base> require every review thread resolved". The next step is to answer each thread (fix the code or reply), resolve it, and ask again.
- Otherwise the reasons of V0.136 follow as before. When the count was not read, the reason says that the rules require every review thread resolved, before the generic "the rules of <base> block it".

(4) GitHub's refusal of a merge for an unresolved conversation ("A conversation must be resolved …") gets the same next step: resolve the threads.

Tests: `TestBlockedMergeAskNamesReviewThreadsOrApproval` (internal/core: the #1008 shape first; several threads; threads resolved; REVIEW_REQUIRED; 1 approval required and none given; approved with a thread open; the refusal's words), `TestPollerReadsReviewThreadsAndRuleParameters` (internal/github: a repo's one PR is read by the batched query, which counts the threads; the rule's parameters are read; two calls in all).

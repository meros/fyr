# V0.review-thread-notice: unresolved review threads fail a PR's review condition, so the agent hears of them and a review watch fires

→ V0.review-threads (the poll counts unresolved threads), V0.95 (said once per head), V0.review-correctness (the review condition follows the review decision). *Amends V0.review-correctness (1)*: the review condition no longer follows the review decision alone.

A review bot left two review comments on a PR as COMMENTED reviews. GitHub's review decision stays empty for those, so Fyr dropped the review condition, and the agent's watch on `{type: "review", status: "fail"}` never fired. The poll already counted the unresolved threads, but the count was kept only when GitHub read the PR as BLOCKED, and only a merge ask on a repo whose rules require resolved threads used it. The PR sat until the owner noticed the comments.

(1) **The count on every read.** The poller keeps the unresolved thread count on every PR it reads in its batched query, not only on a BLOCKED one. No extra call: the query already asked for the threads. A read that cannot see threads (`gh pr view`, -1) keeps the count the last read saw.

(2) **The review condition**, in this order: CHANGES_REQUESTED is fail "Changes requested"; any unresolved review thread, whoever left it and whatever the decision, is fail "N unresolved review threads"; APPROVED is ok; REVIEW_REQUIRED is pending; otherwise there is none. When the threads are resolved the condition goes back to what the decision says.

(3) **The notice.** The fail is a `fact.blocks` notice that wakes the agent like a failed CI: "Review on PR #N: 2 unresolved review threads." Threads resolved and new ones left later on the same head are news again: the said state of a threads notice carries the time the condition failed, so V0.95 (2) does not drop it. A count that grows while the condition already fails says nothing new.

(4) **Merge.** Unresolved threads alone do not stop the owner's Merge or a merge ask; the base's rules do, as in V0.review-threads. The change's Address review offer names the count.

(5) **The watch tool** says which review states fill the review condition. `fyr_state` names the count on the PR line.

Tests: `TestReviewThreadsNotice` and `TestReviewThreadsFireWatch` (internal/core: a COMMENTED-only review with threads fails, notifies and wakes; one thread is singular; resolved clears and a new thread on the same head fires again; an unread count is kept; CHANGES_REQUESTED still fails and stops the merge; threads alone do not stop it; a review fail watch fires).

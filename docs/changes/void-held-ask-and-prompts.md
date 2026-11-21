# V0.void-held-ask-and-prompts: Void held merge asks close as void (not retryable gate failures), and the prompt tells agents not to arm CI watches before change_request or remove Fyr worktrees by hand

→ SPEC 0.5 V0.12, V0.136, V0.prompt-open-pr, decision 0024 §5.

(1) `gateMergeAsks` (`internal/core/mergeask.go`) previously closed a held merge ask that became void (`askVoid`) with `gate: true` and `next: "nothing; the work it was about is over"`, which `resultNotices` (`internal/core/feed.go`) formatted as `"Next: nothing; the work it was about is over; then ask again, and Fyr shows the ask when the PR can merge."`. It now emits `decision.closed` with `void: true` (matching `voidAsks`), so the agent hears `"is void: ... Do not ask again."`.

(2) `promptMerge` (`internal/agent/prompts.go`) now explicitly instructs agents not to arm a CI watch just to wait before calling `mcp__fyr__change_request {change, ask: "merge"}`, since `change_request` already waits for CI.

(3) `promptStartChange` (`internal/agent/prompts.go`) now tells agents not to run `git worktree remove` on Fyr-managed worktrees, since Fyr cleans them up once nothing is uncommitted or unpushed.

Tests: `internal/core/voidask_test.go` (`TestHeldMergeAskClosesAsVoidWhenPRMerges`); `internal/agent/prompts_open_pr_test.go` (`TestPromptOpenPRWhenReady`).

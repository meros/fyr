# V0.pr-create-tool: Fyr exposes mcp__fyr__pr_create so agents can open GitHub pull requests without hitting CLI permission classifiers

→ SPEC 0.5 V0.12, V0.59, V0.prompt-open-pr; decision 0035 (only `internal/github` runs `gh`).

Claude Code's `--permission-mode auto` classifier frequently refused `gh pr create` as an unrequested external write before Fyr's approval judge was consulted, forcing agents to fall back to manual Run cards (and failing when body files lived inside denied shell commands). Prefix allowlisting `Bash(gh pr create:*)` is brittle because agents work inside worktrees (`cd <worktree> && gh pr create`), Nix dev shells (`nix develop -c gh pr create`), or heredocs.

(1) `internal/github/create.go` adds `GhIn(dir)` and `CreatePR(ctx, gh, req)`, executing `gh pr create` directly via argv (no shell) with flag-injection checks and returning the canonical PR URL. `cmd/fyrd/main.go` meters and budgets it through `ghMeter` and `ghBudget` like all other `gh` calls.

(2) `internal/engine/change_tools.go` registers `pr_create` (covered by `mcp__fyr__*` in `AllowedTools` on every CLI adapter). It restricts `dir` to the thread's folder or one of its tracked change worktrees, defaults `dir` and `head` from `change` when provided, runs `OutsideDeps.CreatePR` outside `e.mu` (V0.182), immediately links the PR onto the thread's change record, and kicks `OnPR`.

(3) `internal/agent/prompts.go` (`promptOpenPR`, GitHub setups only) steers agents to use `mcp__fyr__pr_create`, while keeping `gh pr create` capture working. `web/src/approvals.ts` formats `mcp__fyr__pr_create` cleanly on desktop and phone.

Tests: `internal/github/create_test.go`; `internal/engine/pr_create_test.go`; `internal/agent/prompts_open_pr_test.go`; `web/test/approvals.test.ts`.

# V0.prompt-open-pr: The GitHub prompt tells agents to push, open a PR, and ask to merge when a code change is ready

→ SPEC 0.5 V0.12, V0.prompt-fits-setup, V0.settle-merge-fixes (1).

Previously `promptMerge` told agents how to request a merge once they pushed, and `promptNoGitHub` told agents on non-GitHub repositories to stop at a local branch, but nothing told agents on GitHub repositories to push and open the pull request themselves once a code change is ready. Cautious agents stopped at a local commit and offered a chip to push and open a PR, adding an unnecessary round-trip even though the PR and merge ask already serve as the owner's review gate.

(1) `promptMerge` (`internal/agent/prompts.go`) now explicitly instructs agents on GitHub setups to push the branch, open a pull request with `gh pr create`, and call `mcp__fyr__change_request {change, ask: "merge"}` right away instead of stopping at a local commit to ask or offer a chip (unless the owner asked not to push).

(2) Non-GitHub git setups still omit `promptMerge` and keep `promptNoGitHub`.

Test: `internal/agent/prompts_open_pr_test.go` (`TestPromptOpenPRWhenReady`).

# 0035 Fyr runs Claude Code and works with GitHub for now; the architecture keeps room for more

Date: 2026-11-19 · Status: accepted; amended by V0.agy-adapter: a second CLI adapter exists · Builds on 0015 (a provider is a CLI adapter), 0018 and 0024 (changes)

**Context.** Fyr runs every agent through the Claude Code CLI: the runner speaks its `stream-json` protocol (`internal/claude`), providers are Claude logins or Anthropic-compatible endpoints set through `ANTHROPIC_*`, and hooks, MCP config, permission modes, compaction and rate-limit parsing are Claude Code's. Changes, checks, merges, deploy watches and releases go through GitHub and the `gh` CLI (`internal/github`). Going public (#196) asked whether to put the agent CLI (#201) and the forge (#202) behind interfaces first.

**Decision.** For practical reasons Fyr supports one of each today: Claude Code as the agent CLI and GitHub as the forge, and the README says so. No second implementation and no plugin system is built now. But the code is built so a second one can be added later without a rewrite.

| Question | Decision |
|---|---|
| Agent CLI | Claude Code only. Other CLIs (Codex, Gemini CLI, OpenCode) cannot run a thread. Anthropic-compatible endpoints stay supported as providers (0015) |
| Forge | GitHub only, through `gh`. A project whose remote is not GitHub still runs threads; it has no change cards, CI state or merges |
| Seams | The agent CLI and the forge each sit behind one package boundary. Only the runner and `internal/claude` (with `internal/cli/claudecli` and provider detection) know Claude Code's protocol, flags, hooks and folders; only `internal/github` runs `gh` or calls GitHub's API |
| Core types | `internal/core`, the engine and the client speak Fyr's own terms (a session, a turn, a tool call, a usage reading; a change, a PR, checks, a merge), not Claude Code's message shapes or GitHub's JSON. A field named after one vendor stays at the edge |
| New code | New code follows the two rules above. Existing code that breaks them is fixed when it is touched, not in one sweep |

**Consequences.** No second implementation to keep alive and test, and no interface designed before a second user shows what it must hold. A new agent CLI or forge needs a new decision that names a real user and the features it must carry (sessions and resume, hooks, MCP tools, permission modes; PRs, checks, merge). A project on another forge showing its threads without errors is ordinary bug work, not part of this decision.

# 0035 Fyr runs Claude Code and works with GitHub; no agent or forge interface for now

Date: 2026-11-19 · Status: **accepted** (the owner chose "state it, no interface" for #201 and #202 on 19 Nov) · Builds on 0015 (a provider is a CLI adapter), 0018 and 0024 (changes)

**Context.** Fyr runs every agent through the Claude Code CLI: the runner speaks its `stream-json` protocol (`internal/claude`), providers are Claude logins or Anthropic-compatible endpoints set through `ANTHROPIC_*`, and hooks, MCP config, permission modes, compaction and rate-limit parsing are Claude Code's. Changes, checks, merges, deploy watches and releases go through GitHub and the `gh` CLI (`internal/github`). Going public (#196) asked whether to put the agent CLI (#201) and the forge (#202) behind interfaces first.

**Decision.** Neither. Fyr is a control room for Claude Code, and its change tracking needs GitHub. The README says both as requirements.

| Question | Decision |
|---|---|
| Agent CLI | Claude Code only. Other CLIs (Codex, Gemini CLI, OpenCode) cannot run a thread. Anthropic-compatible endpoints stay supported as providers (0015) |
| Forge | GitHub only, through `gh`. A project whose remote is not GitHub still runs threads; it has no change cards, CI state or merges |
| Interfaces | Not built now. No speculative abstraction in `internal/claude` or `internal/github` |

**Consequences.** No second implementation to keep alive and test. A new agent CLI or forge needs a new decision that names a real user and the features it must carry (sessions and resume, hooks, MCP tools, permission modes; PRs, checks, merge). A project on another forge showing its threads without errors is ordinary bug work, not part of this decision.

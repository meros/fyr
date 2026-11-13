# Competing agent workbenches (survey 2026-11-13)

Web research from changelogs, docs and third-party writing. "(unverified)" marks claims not confirmed.

| Tool | Attention | Snooze / archive | Hosts / architecture | GitHub | Licence |
|---|---|---|---|---|---|
| **Enjoy / JOY** (Mo Bitar, enjoy.dev) | One Inbox across projects, "Needs you" first; Active / Needs you / Working / Completed; stays Working while background work runs | Snooze to a time or until the agent finishes; the agent can snooze and wake itself; queue a conversation after another; concurrency cap per project | Several computers, window names the current one; browser client on the phone, E2E encrypted per the site; iOS app "coming soon" | Light: CI watchers, routine per PR | Closed, $14.99/month |
| **Conductor** | Backlog / in progress / in review / done; mark unread; PR state on rows | Archive after merge; no snooze | Mac only; iOS + QR remote control since Oct 2026 | Strong: checks tab, forward failing checks to Claude | Closed; bought by Meta 4 Nov 2026; its cloud announced to shut down 20 Nov |
| **Claude Code agent view** | Pinned / Ready for review / Needs input / Working / Completed; a small model writes row summaries; `claude agents --json` | Pin, stop, delete; idle sessions reaped after ~1 h | One machine; remote control from web/mobile | PR badge by state; web auto-fix of PRs | Closed |
| **Codex app** | (unverified) | Archive removes the worktree unless pinned | SSH hosts (alpha); phone paired per host by QR | Automations | Closed |
| **Paradev** (was Superset) | Status from hooks in the agent config; sounds, dock badge | Pin, bulk actions | Remote hosts (beta) via relay | PR state | MIT (one source) |
| **Paseo** | (not documented) | (not documented) | One daemon per machine (WebSocket + MCP); desktop, web, CLI, mobile clients over Tailscale/SSH/optional E2E relay; Hub for several daemons | PRs, diffs | Apache-2.0 or AGPL (sources disagree) |
| **Happy** | Push for approvals and completion | – | Self-hostable E2E relay, QR keys, several machines per phone | – | Open |
| **Vibe Kanban** | Kanban; In Review → Done on merge | Columns | Browser signs each action, host verifies | Review status | Open; development stopped |

Also: Nimbalyst (session Kanban + iOS), Omnara (dropped its CLI wrapper because following Claude Code changes cost too much), Terragon (shut down Feb 2026), Imbue mngr (agents across local/Docker/Modal/SSH), Cursor (push for finished / needs input / ready for review), Jules (CI fixer).

## Taken into Fyr

Condition-based snooze and queues (Enjoy) · agent can snooze itself (Enjoy) · PR state as an attention reason (agent view, Conductor) · small-model row summary (agent view) · one daemon per host (Paseo) · signed actions (Vibe Kanban) · push only for what needs you (Cursor, Happy) · archive cleans a worktree only without unpushed work (Codex) · use typed protocol messages, never parse terminal output (Omnara's lesson).

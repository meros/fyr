# How the owner used T3 Code (3–13 Nov 2026)

Read-only analysis of the local T3 Code v0.1.7 database (`statev2.sqlite`) and settings, 2026-11-13. Times are Europe/Stockholm. The event log was compacted before 12 Nov, so some lifecycle details cover only the last two days.

## Numbers

| Area | Measurement |
|---|---|
| Threads | 1,255 total: 196 started by the owner, 999 subagent child threads, 54 v1 imports, 6 forks. About 18 own threads a day |
| Where started | 148 desktop/web, 54 phone (27 %), over Tailscale |
| Projects | norban 942 (185 own), wayout 145, scratch 92, nixos 73, small others |
| Hosts | 1 environment; 0 peer links |
| Worktree mode | never used on a repo; agents create worktrees themselves |
| Runs | 3,052; per top-level thread median 7, p90 40, max 133 |
| Run duration | median 0.7 min, p90 12 min, p99 52 min |
| Concurrency | when anything runs: median 1, p90 4; daily peaks 6–8, max 13 |
| Time of day | peak 06–11, heavy until 16, tail to 22; weekends ~7 % |
| Approvals / plans | 0 / 0 (mode `auto` on 1,112 threads, full-access 141) |
| AskUserQuestion | 312 requests in 93 threads; 1–4 questions each (4 questions: 70); answer time median 1.0 min, p90 5.1 min, max 26 min |
| Failures | 3.3 % of runs; classes: Cloudflare/auth 8, API 12, MCP start-up timeout 7, missing workspace 6, auth 3, other 6; no usage-limit failures |
| Settle | 180 manual, 47 automatic (top-level); auto-settle on merge turned off by the owner |
| Create → settle | median 1.6 h, p75 6.4 h, p90 20 h |
| Snooze | 31 uses; presets 3 h, tomorrow 06:00, Monday 06:00 |
| Pin / archive / delete | 0 / 8 / 6 |
| Thread visits | ~21k in 11 days (~1,900 a day) |
| Native Claude subagents | 951 in 122 parent threads (avg 8, max 75 per parent) |
| T3 delegation | 52 `delegate_task`; 36 `t3_thread_send`; 35 thread list/read/search |
| Scheduled tasks | 3, all one-off, agent-created |
| PRs | 369 distinct in 11 days; 124 of 196 own threads link ≥ 1 PR; one PR per root thread almost always |
| PR watches | 160 watch, 63 unwatch |
| Models (runs) | Sonnet 5.5 1,790, Opus 5.5 1,118, DeepSeek Flash 90, Fable 5.2 53 |
| Agent instances | 3 Claude Code instances: main account, second account, DeepSeek endpoint |
| Heavy agent tools | browser preview ~1,300 calls in 30+ threads; background waits 518; monitors ~1,250; web search 1,620; skills 828 |
| Never or barely used | pin, plan mode, approvals, worktree mode, scheduled tasks, peers, guest share, PR stacks, review guides, secrets (5) |

## Kinds of work (own thread titles, ±5)

Product UI for norban (~70) · bug/incident/CI fixes (~22) · agent pipeline and dev-loop tooling (~22) · NixOS, desktop and T3 itself (~18) · screenshot-only starts (~16) · triage and coordination (~14) · life and errands (~13) · research and specs (~12) · hardware and firmware (~9).

## Consequences used in the spec

See `docs/SPEC.md` section 0.2 and `docs/FYR-V1.md` section 1.

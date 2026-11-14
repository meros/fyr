# Plan

Status lives in GitHub: https://github.com/meros/fyr/milestones. This file holds the milestone definitions and exit criteria only.

| Milestone | Scope | Exit criterion |
|---|---|---|
| **v0 first viable** | SPEC 0.5: one host, one desktop client on localhost; threads with the `claude` CLI, questions, interrupt, settle, snooze, archive; adopt open T3 threads; PR state | The owner runs a working day of Claude threads in Fyr with T3 closed, and every v0 test is green |
| M0 Skeleton | `docs/FYR-V1.md` 7 | a phone pairs over the tailnet and sees the inbox |
| M1 Conversation | `docs/FYR-V1.md` 7 | A7; a 3-hour session survives two host restarts |
| M2 Inbox | `docs/FYR-V1.md` 7 | A1, A3, A4, A6 |
| M3 GitHub | `docs/FYR-V1.md` 7 | A5, A9 |
| M4 Calm machine | `docs/FYR-V1.md` 7 | A2, A8, A10 |
| M5 Cut-over | `docs/FYR-V1.md` 7 | A1–A10 for one week |

v0 takes parts of M1, M2, M3 and M5, without the phone, the tailnet, the governor and push. M0 then replaces the v0 transport (decision 0002).

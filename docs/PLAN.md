# Plan

Status lives in GitHub: https://github.com/meros/fyr/milestones. This file holds the milestone definitions, their order and exit criteria only.

## Order

| # | Milestone | Scope | Exit criterion |
|---|---|---|---|
| 1 | **Solid core** | Consolidate the model before new features: one Actions component, the PR on its change, Thread as components, one reducer path with effects run by Fyr, Rules for every watch, one feed of notices to the agent (decision 0020), engine primitives, typed vocabularies, Go layering, one web command model for desktop and phone, the spec restated | No code reads `Thread.PR`; one action vocabulary; desktop and phone share one command model; spec sections 3 and 5 state the model as built; `make test` green |
| 2 | **Two machines** | meros-server as second host and phone front (decisions 0016 and 0021): dual installs by default, single roles still supported; each process keeps an other-hosts config, with tailnet autodetection offering candidates; the client routes every call through its front, never to a host; providers, models and login per host; a host unreachable for 30 days is forgotten by its fronts | The owner works a day across both machines from laptop and phone; every host-scoped screen acts on the chosen host; a host added by autodetection serves its threads after one press, and taking a host offline for a day changes nothing but its offline mark |
| 3 | **Projects and workspaces** | Archive a project; where worktrees land; the workspace line; agent tools to file a thread and make a worktree | A finished project archives with its threads; worktrees land as decided in #25 |
| 4 | **Conversation and live view** | Read events, the View-tab reply, background processes on a new message, the cost of a live view | No missed read events for a week; the live view cost is measured and the owner decided where it is on |
| 5 | **M4 Calm machine** | Governor, push batching, policy file, no-progress signal (`FYR-V1.md` 7) | A2, A8, A10; a forced memory hog is stopped and shown as failed |
| 6 | **M5 Cut-over** | One week only in Fyr, T3 off | A1–A10 for one week |
| 7 | **Mesh** | Hosts dial each other (decision 0021, point 8): facts and idempotent relayed commands, a host reachable through a third, a thread move designed as a record protocol with one writer at each step | A host two hops away is reached through a third; no record has two writers; a thread moved between hosts leaves one record and arrives as one |

## Why this order

The review on 2026-11-15 found that features were added one spec row at a time: three action vocabularies, the PR modelled twice, four settle fields, two keep-current mechanisms, and desktop and phone with separate command and form code. Every later milestone builds on those parts, so Solid core goes first (owner decision, 2026-11-15).

## Closed

- **v0 first viable** (SPEC 0.5): done 2026-11-14; the V0 rows after it shipped on `main` without a milestone.
- **M0–M3** (`FYR-V1.md` 7): closed 2026-11-15. v0.x delivered most of their scope; fronts and hosts (decision 0016) replaced M0's ConnectRPC and tsnet pairing.

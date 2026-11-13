# Fyr

A calm control room for Claude coding agents. Like a lighthouse (*fyr* in Swedish), it watches everything and shines only when you must act.

Fyr runs Claude Code sessions on your own machines and shows one list: what needs you on top, running work below. It is built for one person who runs many agents in parallel against GitHub, and it is meant to replace T3 Code for that use.

**Status:** design complete for v1, no product code yet. Next step: milestone M0 in `docs/FYR-V1.md`.

## Start here

| Read | For |
|---|---|
| [`docs/FYR-V1.md`](docs/FYR-V1.md) | What v1 is, the acceptance checklist, architecture, build plan, roadmap |
| [`docs/SPEC.md`](docs/SPEC.md) | The full spec (concepts, data model, Actions, Rules, Attention, modules, UX, technology) |
| [`docs/REVIEW-LOG.md`](docs/REVIEW-LOG.md) | Five adversarial review rounds and a consistency pass, with every resolution |
| [`design/screens-v1.html`](design/screens-v1.html) | The v1 screens: inbox after a break, the question card, phone answer and capture, bulk settle |
| [`design/`](design/README.md) | The Fyr design system (tokens, components, brand book), screen mocks and their generators |
| [`research/`](research/) | T3 Code feature map, the owner's T3 usage, competitors, coordinator agents, vision ideas |
| [`spikes/`](spikes/) | Proven assumptions: Go client for the `claude` control protocol; PSI and cgroup behaviour |

## Decisions so far

- Single host in v1; data model ready for a mesh of hosts (moves need a TLA+-checked protocol first).
- Go daemon, Go-native `claude` control client, SQLite event log, ConnectRPC, `tsnet`, TypeScript PWA, Nix flake with NixOS and home-manager modules.
- One Action registry for every surface; one pure `attention` function for every status.
- Async effects: merges and similar effects are requested by the agent, approved by you, run by Fyr, and reported back as context.
- Ready for several users per host (owner, visibility, per-user view state), but v1 has one user.


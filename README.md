# Fyr

A calm control room for Claude coding agents. Like a lighthouse (*fyr* in Swedish), it watches everything and shines only when you must act.

Fyr runs Claude Code sessions on your own machines and shows one list: what needs you on top, running work below. It is built for one person who runs many agents in parallel against GitHub, and it is meant to replace T3 Code for that use.

**Status:** v0 is in daily use (spec 0.5, V0.1–V0.71). Current milestone: `Solid core`, which consolidates the data model; the order is in [`docs/PLAN.md`](docs/PLAN.md). Status lives in [GitHub milestones](https://github.com/meros/fyr/milestones); how work flows is in [`docs/PROCESS.md`](docs/PROCESS.md).

## Start here

| Read | For |
|---|---|
| [`docs/FYR-V1.md`](docs/FYR-V1.md) | What v1 is, the acceptance checklist, architecture, build plan, roadmap |
| [`docs/SPEC.md`](docs/SPEC.md) | The full spec (concepts, data model, Actions, Rules, Attention, modules, UX, technology) |
| [`docs/PROCESS.md`](docs/PROCESS.md) | Spec → red test → code; where plan, status and decisions live |
| [`docs/decisions/`](docs/decisions/README.md) | Decision records |
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


## Run on a Mac

NixOS uses `nixosModules.fyr`. On a Mac, with Homebrew `go`, `git`, `gh`, `claude` and [fnm](https://github.com/Schniz/fnm) (Node LTS, `.nvmrc`):

    scripts/install-macos.sh

It builds this checkout, installs the launcher, a seed and the current build under `~/.local/share/fyr`, and starts two launchd agents: `com.github.meros.fyr` (Fyr on http://127.0.0.1:7777, log `~/Library/Logs/fyr.log`) and `com.github.meros.fyr.update`, which runs `scripts/update.sh` daily at 04:00: it builds and tests origin's default branch, links it as `next` and sends fyrd SIGHUP, so the launcher switches with its probation and rollback. It also makes `~/Applications/Fyr.app` (Spotlight, Dock) that opens Fyr in a Chrome app window. `scripts/install-macos.sh --uninstall` removes both agents and the app. Without Nix, deploys and updates build with the toolchain on the agent's PATH; the Makefile refuses a Node that is not an LTS.

# Fyr

A calm control room for Claude coding agents. Like a lighthouse (*fyr* in Swedish), it watches everything and shines only when you must act.

Fyr runs Claude Code sessions on your own machines and shows one list: what needs you on top, running work below. It is built for one person who runs many agents in parallel against GitHub, and it is meant to replace T3 Code for that use.

**Status:** v0 is in daily use (spec 0.5, V0.1–V0.71). Current milestone: `Solid core`, which consolidates the data model; the order is in [`docs/PLAN.md`](docs/PLAN.md). Status lives in [GitHub milestones](https://github.com/meros/fyr/milestones); how work flows is in [`docs/PROCESS.md`](docs/PROCESS.md).

## Requirements

- **Claude Code.** Fyr runs every agent through the Claude Code CLI, with a Claude login or an Anthropic-compatible endpoint. Other agent CLIs are not supported ([decision 0035](docs/decisions/0035-claude-code-and-github.md)).
- **GitHub, through `gh`.** Change cards, CI state, merges and updates need a GitHub remote and a `gh` login. A project on another forge still runs threads, without change tracking.

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

NixOS uses `nixosModules.fyr`. On a Mac, with Homebrew `git`, `gh` (logged in) and `claude`, and Xcode's command line tools:

    scripts/install-macos.sh

It builds nothing. It downloads the CI release that the tag `deployable` points at, checks its sums, installs the launcher, a seed and the current build under `~/.local/share/fyr`, and starts the launchd agent `com.github.meros.fyr` (Fyr on http://127.0.0.1:7777, log `~/Library/Logs/fyr.log`). It also makes `~/Applications/Fyr.app` (Spotlight, Dock), which opens Fyr in a Chrome app window. When Tailscale runs, it sets `hostListen` (`tailnet:7778`) and `wellKnownListen` (`127.0.0.1:7779`) in `~/.config/fyr/config.json` unless they are there, and points Tailscale Serve's port 7443 at the well-known port, so your other Fyr installs find this Mac and follow it. The macOS Tailscale app cannot proxy to Fyr's Unix socket, so that port answers `/.well-known/fyr` and nothing else; opening Fyr over the tailnet from a browser is not set up on a Mac. The agent starts through a small host program, `~/.local/share/fyr/host/fyr`, which stays the same across installs, so a permission you give Fyr in System Settings (App Management, Full Disk Access) survives reinstalls and updates. `scripts/install-macos.sh --uninstall` removes the agent and the app.

## Updates

Fyr updates itself only to CI-built releases (decision 0027). Every green push to main publishes signed `fyr` and `fyrd` binaries for Linux and macOS (amd64 and arm64) as the release `build-<sha12>`, and moves the tag `deployable` to it. Each host checks `deployable` every hour (config `updateEveryMinutes`). After it has stayed put for 10 minutes (it wakes for that, not at the next check), the host downloads the release, verifies its signature and sums, runs `fyrd selftest` on a copy of the database, and switches. The launcher checks `/healthz` after the switch and rolls back to `previous` if the build is not healthy or crashes within 10 minutes. Settings → Auto-update turns this off. The releases of a private repository are read with `GH_TOKEN` from `~/.config/fyr/github.env` (mode 0600; a fine-grained token that may only read the repository's contents), which systemd's `EnvironmentFile` and the Mac's launchd job both load; without it, the `gh` login is used. To stop a bad release on every host, move the tag back: `git push -f origin <sha>:refs/tags/deployable`.

To try a branch from inside Fyr, an agent asks `fyr.deploy {dir}`. It builds the checkout and switches to it once you press. Auto-update then pauses until a release contains that commit.

## License

MIT, see [LICENSE](LICENSE). The Geist fonts in `web` (from the `geist` package) and in `design/settings/fonts` are under the SIL Open Font License 1.1 ([OFL.txt](design/settings/fonts/OFL.txt)).

<p align="center">
  <img src="design/logo/lighthouse-512.png" alt="" width="96">
</p>

<h1 align="center">Fyr</h1>

<p align="center">
  <strong>A calm control room for coding agents.</strong><br>
  Run a dozen Claude Code or Antigravity agents on your own machines. Fyr tells you only when one needs you.
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#what-you-get">Features</a> ·
  <a href="#security-and-trust">Security</a> ·
  <a href="docs/INSTALL.md">Docs</a> ·
  <a href="docs/SPEC.md">Spec</a> ·
  <a href="LICENSE">MIT</a>
</p>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/hero-dark.png">
  <img alt="Fyr on the desktop with an agent's two-question card open and four threads in Your move, next to the phone app showing the same inbox" src="docs/images/hero-light.png">
</picture>

Running many coding agents at once is easy. Keeping track of them is not: terminal tabs, a question that waited 40 minutes for an answer, a red CI run nobody looked at, a merge you have to remember to press.

Fyr turns that around. It runs Claude Code and Antigravity (`agy`) sessions on your machines and keeps **one list**. Threads that need you (a question, a failure, red CI, a merge to approve) rise to **Your move**. Everything else stays in **Moving without you**, one live line each, until it is done. Start a thread at your desk and answer its question from your phone. (*Fyr* is Swedish for lighthouse: it watches everything and lights up only when you have to act.)

## What you get

- **One inbox for every agent.** Each conversation is a *thread*. Asking, failed, blocked and finished threads come to the top, ordered by what to do: *Answer*, *Approve*, *Unblock*. Snooze, settle, quiet hours and per-project priority keep the list short. Press `?` for the keyboard shortcuts.
- **Questions become cards.** When an agent asks, you get a card with its options. Answer with `1` `2` Enter on the desktop or two taps on the phone (skip a question, step back or cancel the card), and the answer goes straight back into the run. Questions from subagents arrive the same way.
- **Agents ask, you approve, Fyr acts.** An agent never merges or deploys on its own. It *requests*, and you approve when it suits you (and an optional per-project approval judge can auto-allow routine tool calls and escalate the rest):
  - A **merge ask** reaches you only once CI is green on the commit you will merge.
  - A **Run card** shows the exact command, root included, and runs it on that machine when you press.
  - A **deploy** of a branch builds and switches once you press.

  The result goes back to the agent as context.
  <p align="center"><picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/approve-dark.png">
    <img alt="A merge ask for pull request 4170 with Merge and Not now, and a Run card that asks to run udisksctl as root, with Run as root, Copy and Not now" src="docs/images/approve-light.png" width="720">
  </picture></p>
- **Agents stop reporting CI.** Fyr reads branches, multiple PRs per thread, checks and reviews from git and GitHub and shows them on the thread that owns them. It tells the agent when CI fails and can settle the thread when its PRs merge.
- **Agents get tools to work with you.** Over MCP an agent can ask you something, request a command or a merge, open a PR, open a change in its own git worktree, wait for a condition (CI green, a PR merged, a quota reset, a time), show you a page or a screenshot, and start or message other threads.
- **Your machine stays usable.** Heavy jobs (test suites, builds, e2e runs) are admitted by the CPU and memory they need, and the rest queue. A memory guard pauses runaway work before the kernel's OOM killer acts. On Linux every agent runs in its own cgroup.
- **Agents survive restarts.** The launcher holds the agent processes, so a crash, a restart or an update of the daemon does not interrupt a run.
- **Native desktop apps and a full phone client.** Run Fyr in a browser, in `Fyr.app` on macOS, or in `fyr-desktop` on Linux (window, tray icon and native desktop notifications). Your phone is an installable PWA with push notifications: answer, steer, approve, start a thread with a screenshot, and settle.
- **Several machines, one list.** Installs find each other on a LAN (mDNS) or over an optional tailnet and pair with an invite code. Every host's threads show in one inbox, a new thread picks which machine runs it, and you can hand a thread over to another provider or another host.
- **Your own agent subscriptions.** Fyr drives the `claude` CLI (Claude logins or Anthropic-compatible endpoints) and the Antigravity CLI (`agy`, Google accounts or Gemini endpoints). Several logins can sit side by side; each thread shows its plan's remaining window and waits cleanly when quota runs out.
- **Updates that roll back.** Fyr installs only signed CI releases (on the `stable` channel of version tags or `main`'s green builds). It tests each one on a copy of its database first and goes back to the previous build if the new one is not healthy.

## Status

Fyr is v0. Its author uses it every day, and it changes daily. Expect breaking changes, and do not expect database migrations between every pair of builds. It is built for **one person who runs many agents in parallel against GitHub**. The roadmap is in [`docs/PLAN.md`](docs/PLAN.md) and the current work in the [milestones](https://github.com/meros/fyr/milestones).

## Requirements

- **Linux or macOS.** NixOS has a module (and optional `fyr-desktop` native app); macOS has `Fyr.app` in a DMG (macOS 13+); other Linux (Debian, Ubuntu) has an install script.
- **[Claude Code](https://docs.claude.com/en/docs/claude-code)** (`claude`, with a Claude account or an Anthropic-compatible endpoint) and/or **Antigravity CLI** (`agy`, via ACP, with a Google login or Gemini endpoint). Both sit behind Fyr's CLI adapter seam ([decision 0035](docs/decisions/0035-claude-code-and-github.md)).
- **GitHub through [`gh`](https://cli.github.com/)**, logged in, for the agents' pull requests and change tracking. Projects on other forges still run threads, without it. Installing and updating Fyr need no `gh`.
- **Optional: [Tailscale](https://tailscale.com/)**, as one route to open Fyr from your phone away from home and to connect machines. Fyr also runs on a single machine or a plain LAN without it.

## Install

### NixOS

```nix
# flake.nix
inputs.fyr.url = "github:meros/fyr";

# in your NixOS configuration
imports = [ inputs.fyr.nixosModules.fyr ];
services.fyr = {
  enable = true;
  user = "you"; # agents run as this user, with its logins and repositories
  # desktop.enable = true; # optional: native Linux window, tray item and desktop notifications
};
```

Fyr serves http://127.0.0.1:7777. The module also sets up the agents' cgroups and the memory guard. [`docs/HOST-SETUP.md`](docs/HOST-SETUP.md) lists the host settings that help the guard.

### macOS

Download [`Fyr.dmg`](https://github.com/meros/fyr/releases/latest/download/Fyr.dmg) (macOS 13+, Apple Silicon and Intel), drag **Fyr** into **Applications** and open it.

> **First open (Gatekeeper):** `Fyr.app` is signed with Fyr's own certificate rather than an Apple Developer ID yet. A browser download is quarantined by macOS, so the first launch needs **right-click → Open** (or **System Settings → Privacy & Security → Open Anyway**).

`Fyr.app` registers its background job under **Login Items** (no admin password needed), opens Fyr in a native window with macOS notifications and a menu-bar icon, walks you through setup in **Settings → Setup**, and keeps both `fyrd` and `Fyr.app` updated automatically. To remove it, choose **Uninstall…** from the menu-bar icon.

For a terminal or developer install (no Gatekeeper prompt):

```sh
git clone https://github.com/meros/fyr && cd fyr
scripts/install-macos.sh
```

### Linux without Nix (Debian, Ubuntu)

You need systemd with cgroup v2, `git`, `curl` and `claude` (`gh` is for the agents' pull requests; the install needs no login):

```sh
sudo apt install git curl gh   # gh: see cli.github.com if your release lacks it
git clone https://github.com/meros/fyr && cd fyr
scripts/install-linux.sh --print   # shows every file and command, changes nothing
scripts/install-linux.sh
```

Run it as yourself, not as root. It downloads the current signed release into `~/.local/share/fyr` as you, then asks `sudo` for the system part, showing each command: the unit `fyr.service` (it runs as you), the agents' slice, a polkit rule and a small helper. Fyr serves http://127.0.0.1:7777 and updates itself. `scripts/install-linux.sh --uninstall` removes the unit and the root files. [`docs/INSTALL.md`](docs/INSTALL.md#linux-without-nix) has the details.

### From source

You need Go and Node. The dev shell has both: `nix develop`.

```sh
make build        # bin/fyr (the launcher) and bin/fyrd (the daemon, with the web client inside)
make install      # puts the build under ~/.local/share/fyr
bin/fyr           # starts Fyr on http://127.0.0.1:7777
```

[`docs/INSTALL.md`](docs/INSTALL.md) covers configuration, the phone, more machines and updates.

## First steps

1. Open http://127.0.0.1:7777 (or `Fyr.app` / `fyr-desktop`). **Settings → Setup** checks your machine and Fyr finds your existing `claude` and `agy` logins (**Settings → Providers**).
2. Press **New** (or `c`), pick a folder, and write what the agent should do. The folder becomes a project.
3. Go and do something else. When the agent asks, fails or finishes, its thread moves to **Your move**, and your desktop or phone is notified if you allowed it.
4. Answer, steer, or press the card's button. **Settle** a thread when you are done with it.

## Security and trust

Fyr is built for one owner who runs agents on their own machines. This is who can act, as built today, and what Fyr does not protect.

**The owner is your user account.** `fyrd` runs as you. On each machine, a process running as your user (or root) reaches Fyr's owner socket (`owner.sock`, mode 0600, checked by the caller's UID) and so acts as the owner there: that is how `fyr open`, `fyr pair` and `fyr device …` work. Fyr has no accounts or passwords. A Tailscale login counts only when you name it (`ownerLogin` in the config, or `fyr auth owner <login>`); Fyr never takes it from Tailscale by itself. ([0028](docs/decisions/0028-who-may-act.md), [V0.owner-without-tailnet](docs/changes/owner-without-tailnet.md))

**Every browser and phone is a signed-in device.** A device gets a session cookie only one of these ways:

- `fyr open` on the machine: a one-time link that signs this machine's browser in.
- `fyr device add`, or **Add a device** on a signed-in device: a QR code or link, single use, 10 minutes.
- An 8-character approval code that a device without a session shows. You type it on a device that is already signed in, or run `fyr device approve <code>`.
- With Tailscale Serve and your `ownerLogin`: a device on your tailnet login. A new one waits until you allow it on a device you already use.

A device that is not signed in gets the sign-in page and nothing else. Away from the machine itself Fyr serves no plain HTTP: a phone needs Tailscale Serve or a certificate you configure. **Settings → Devices** lists every device; Revoke ends one on every install, with the devices it added. A new install starts each way in **in observe mode**, so you are not locked out while you sign your devices in: for its first three clean days a browser on the machine itself (or on your Tailscale login through Serve) is still let in without a session, and counted. It then enforces by itself; `fyr auth enforce` does it now, and `fyr auth status` shows where it stands. The LAN HTTPS listener always enforces. ([0030](docs/decisions/0030-every-hop-names-who-acts.md) 3.4, 5)

**Installs trust keys, not networks.** Each install has its own Ed25519 key. Installs talk over mutual TLS pinned to those keys, and accept only the members of a signed roster. An install joins the roster by your invite code (`fyr pair`), by a member that vouches for it, or, as one optional shortcut, by being an untagged node of the same Tailscale login. Being on the same LAN, an mDNS answer or a name never counts. `fyr host remove` takes one out. ([0026](docs/decisions/0026-keys-not-networks.md), [0030](docs/decisions/0030-every-hop-names-who-acts.md) 3.6)

**Agents run as you.** An agent is the `claude` or `agy` CLI running as your user, with your files, your SSH keys, your `gh` login and your other credentials. Fyr asks it to request merges, root commands and Fyr deploys (`change_request`, `run_request`), and these run only when you press on the card; a merge ask reaches you only once CI is green, and a Fyr deploy is taken only from a thread on Fyr's own repository. Fyr also blocks `gh pr merge` and `sudo` in the agent's shell. These gates guard against an agent's mistakes. They are not a sandbox.

**Updates are signed.** The updater installs only releases signed with the release key, the built-in one or the one you set in `updateKey`, and tests each on a copy of the database first. The first download by the install scripts is checked against its sums over HTTPS. ([Updates](docs/INSTALL.md#updates))

**What Fyr does not protect:**

- An agent with a shell can do whatever your user can, including use `owner.sock`, read Fyr's data and push with your `gh`. Fyr's tokens limit only what it does through Fyr's own tools. Run agents only on machines and accounts you would hand them.
- Anyone with your user account or root on a machine is the owner there.
- An install that is paired and allowed to drive a host is the owner on that host. Remove a lost or stolen install from any other one.

## How it is built

`fyrd` is a Go daemon. It drives the `claude` CLI (control protocol) and the `agy` CLI (ACP) behind one adapter interface, keeps every thread as an event log in SQLite, reads git and GitHub, and serves a TypeScript web app with live updates. The browser, the desktop shells (`Fyr.app`, `fyr-desktop`) and the phone PWA share that app. A small launcher, `fyr`, keeps `fyrd` and the agents running across updates and rollbacks.

Each install is a *host* (it runs agents), a *front* (it serves the app and merges the threads of every host it knows), or both. Both is the default.

| Read | For |
|---|---|
| [`docs/INSTALL.md`](docs/INSTALL.md) | Configuration, the phone, more machines, updates |
| [`docs/HOST-SETUP.md`](docs/HOST-SETUP.md) | Host settings for the memory guard (cgroups, swap, oomd) |
| [`docs/SPEC.md`](docs/SPEC.md) | The full spec: concepts, data model, actions, attention, UX |
| [`docs/API-v0.md`](docs/API-v0.md) | The HTTP API between `fyrd` and the web client |
| [`docs/decisions/`](docs/decisions/README.md) | Architecture decision records |
| [`docs/changes/`](docs/changes/README.md) | The change log of the spec, one file per change |
| [`docs/PROCESS.md`](docs/PROCESS.md) | How Fyr is built: spec, red test, code |
| [`design/`](design/README.md) | The design system, brand book and screen mocks |

The screenshots in this README come from the client's mock mode. To make them again, run `npm run build && node scripts/readme-shots.mjs` in `web/`.

## Contributing

Issues are welcome. Read [`docs/PROCESS.md`](docs/PROCESS.md) and [`CLAUDE.md`](CLAUDE.md) first: every change in behaviour updates the spec and comes with a test.

Pull requests from forks are read, but CI does not run them on its own. The PR gate runs on a self-hosted runner that takes only branches in this repository, so an accepted change is re-run from a branch here.

## License

MIT, see [LICENSE](LICENSE). The Geist fonts in `web` (from the `geist` package) and in `design/settings/fonts` are under the SIL Open Font License 1.1 ([OFL.txt](design/settings/fonts/OFL.txt)).

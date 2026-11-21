# Installing and running Fyr

The [README](../README.md) has the short install for NixOS, macOS, Linux without Nix and from source. This page covers the rest: configuration, the phone, more machines and updates.

## Files

| Path | What |
|---|---|
| `~/.config/fyr/config.json` | The daemon's configuration. Fyr reads it again when it changes. Most settings are also in the app (Settings). |
| `~/.config/fyr/github.env` | Optional `GH_TOKEN=…` (mode 0600). A public release repo needs no token; a private fork does, and a token raises GitHub's API limit. Without it the `gh` login is used when `gh` is installed. |
| `~/.local/share/fyr/` | Data: the database, the builds (`current`, `previous`), logs and the owner socket. |
| `~/Library/Logs/fyr.log` | The log on macOS. On Linux: `journalctl -u fyr`. |
| `/etc/systemd/system/fyr.service`, `fyr-agents.slice`, `/etc/polkit-1/rules.d/50-fyr-agents.rules`, `/usr/local/lib/fyr/agents-pressure-chown` | Linux without Nix: what `scripts/install-linux.sh` writes as root ([below](#linux-without-nix)). |

On NixOS, `services.fyr` writes the configuration: every option of the module is documented in [`nix/module.nix`](../nix/module.nix).

## Configuration

The keys you are most likely to set in `config.json`:

| Key | Default | What |
|---|---|---|
| `permissionMode` | `"auto"` | Permission mode (`--permission-mode` / ACP mode) for every provider that sets none |
| `idleStopMinutes` | `10` | Stop an agent process after this long without a run; the thread resumes on the next message |
| `profiles` | found on the host | Providers: Claude logins (each its own `CLAUDE_CONFIG_DIR`), Anthropic-compatible endpoints, and Antigravity (`agy`) logins or Gemini endpoints. Easier from Settings → Providers |
| `scanRoots` | none | Extra folders the folder picker scans for git repositories |
| `slots` | `2` | The share of the host heavy jobs may use before they queue: 2 is half the host per job, as a starting point that Fyr then learns per job class |
| `role` | `"both"` | `host` (runs agents), `front` (serves the app for `hosts`), or `both` |
| `updateChannel` | `"main"` (a new install from the scripts: `"stable"`) | Which releases the host follows: `stable` (versions), `main` (every green commit) or a version such as `"v1.2.3"` to stay on it, read at start ([Channels and versions](#channels-and-versions)) |
| `updateEveryMinutes` | `60` | How often the host checks for a new release |
| `updateRepo` | `"meros/fyr"` | The GitHub repository releases come from (`owner/name`), read at start |
| `updateKey` | the built-in key | The base64 ed25519 public key every release must be signed with, read at start. A fork that signs its own releases sets it ([Updates](#updates)) |
| `agentPath` | none | Folders put first on the agents' `PATH` (`~/` is your home folder), read at start |
| `cgroups` | absent: off | The agents' cgroups and budget. The NixOS module and `install-linux.sh` set it; see [HOST-SETUP](HOST-SETUP.md) |

**The agents' `PATH`** is, in this order: `agentPath`, the `PATH` fyrd started with (the unit's), the `PATH` of your login shell (`$SHELL -l`, read once at start; a profile that takes more than 5 s is skipped and logged), then those of `~/.local/bin`, `~/.nix-profile/bin`, `/etc/profiles/per-user/<you>/bin`, `/run/wrappers/bin` and `/run/current-system/sw/bin` that exist. Each folder appears once, where it first comes. So a tool your login shell finds, the agents find; restart Fyr after you change your profile.

## macOS (`Fyr.app`)

The recommended macOS install (macOS 13+, Apple Silicon and Intel) is [`Fyr.dmg`](https://github.com/meros/fyr/releases/latest/download/Fyr.dmg) (decision 0037). Drag **Fyr** into **Applications** and open it.

- **First launch (Gatekeeper):** `Fyr.app` is signed with Fyr's own self-signed certificate (`Fyr Local Signing`) rather than an Apple Developer ID yet. Because browsers quarantine downloads, macOS blocks the first double-click; use **right-click → Open** or **System Settings → Privacy & Security → Open Anyway**.
- **Background agent & Login Items:** on launch `Fyr.app` registers its per-user `launchd` agent (`fyr.agent`) via `SMAppService` with no admin rights, and registers itself as a Login Item so macOS notifications arrive even with the window closed. If you previously used `scripts/install-macos.sh`, `Fyr.app` stops the script's job, removes its plist, and keeps your data in `~/.local/share/fyr`.
- **Setup wizard:** open **Settings → Setup** (or the **Set up** card at the top of the inbox) to install Command Line Tools, check Claude Code, download and sign in to `gh`, pick or clone a first project, and pair a phone.
- **Updates:** `fyrd` updates itself from signed CI releases as on Linux, and `Fyr.app` checks for newer signed app releases every 6 hours (or via **Check for Updates…** in the menu bar), accepting only builds signed with its pinned certificate. macOS permissions granted once survive updates because they are tied to the certificate.
- **Developer / script path:** `scripts/install-macos.sh` remains available for terminal or local-checkout installs, and `macos/build.sh --dmg` builds `Fyr.app` and `Fyr.dmg` locally.

## Linux without Nix

`scripts/install-linux.sh` is the NixOS module's stand-in on Debian, Ubuntu and other systemd Linux (V0.linux-without-nix). Run it as the user Fyr and its agents run as; it refuses root.

**What runs as you:** the download of the release of the install's channel ([below](#channels-and-versions); a new install: the newest stable version) (over HTTPS with `git` and `curl`, no `gh` login; checked against `SHA256SUMS`) into `~/.local/share/fyr/builds/<sha12>`, the `current` link, the seed (`seed/fyrd`) and the launcher (`launcher/fyr`); and the `cgroups` and `updateChannel` keys in `~/.config/fyr/config.json` (other keys are kept; with no `python3` and an existing file, it tells you what to add).

**What needs root**, run with `sudo`, each command printed first:

| File | What |
|---|---|
| `/etc/systemd/system/fyr.service` | A system unit with `User=` you: it starts at boot without a login. It runs `~/.local/share/fyr/launcher/fyr -data ~/.local/share/fyr -seed …/seed/fyrd`, reads `~/.config/fyr/github.env` if it is there, and sets no `PATH` (fyrd builds the agents' `PATH`, above). |
| `/etc/systemd/system/fyr-agents.slice` | The agents' budget (`MemoryHigh=40%`, `MemoryMax=46%`, `MemorySwapMax=30%`) and systemd-oomd's rule (80 %), the module's defaults. |
| `/etc/polkit-1/rules.d/50-fyr-agents.rules` | Lets you `start` and `set-property` units named `fyr-agent-….scope` and `fyr-deploy-….scope`, and `start`, `stop` and `restart` `fyr.service` (the Linux app's Pause and Resume), nothing else. |
| `/usr/local/lib/fyr/agents-pressure-chown` | Run as root before each start of the unit: hands you the slice's `memory.pressure`, for the guard's PSI trigger. |

Then `systemctl daemon-reload`, `enable` and `restart fyr.service`. These are the module's files (`nix/module.nix`); a change to one goes to the other.

**The layout follows polkit.** A polkit that reads JavaScript rules (0.106 and later: Debian 12, Ubuntu 24.04) gets the four files above: fyrd alone in `fyr.service`, each thread in a scope of `fyr-agents.slice` ([HOST-SETUP](HOST-SETUP.md)). With an older polkit (Ubuntu 22.04's 0.105) or none, the script writes the V0.80 unit instead: `Delegate=cpu memory`, fyrd splits it into `server/` and `agents/<thread>/`, and there is no slice or rule. Install `polkitd` 0.106 or later and run the script again to move to the slice.

**Run it again** whenever you like: it installs the current release of the channel, rewrites the root files and restarts the unit (which ends running agent turns). `--print` shows every file and command and changes nothing: no download, no `sudo`. `--uninstall` stops the unit and removes the root files; `~/.local/share/fyr` and `~/.config/fyr` stay.

**Updates** are the updater's ([below](#updates)). It needs no `gh` login for a public release repo.

**The phone over Tailscale:** run `tailscale serve --bg --https=7443 unix:$HOME/.local/share/fyr/serve.sock` once as root (the module's `tailnet.enable` does the same). The script prints it when Tailscale is up.

## The Linux app

On a Linux desktop Fyr can run as its own app instead of a browser tab (decision 0038): a window that stays signed in, a tray item (Open, Open in Browser, Pause or Resume, Show Log, Quit) and the desktop's own notifications, with the same settings as a phone's (Settings → Notifications in the window). On NixOS:

```nix
services.fyr.desktop.enable = true;
```

It installs `fyr-desktop`, starts it in the tray at login, and lets you pause and resume `fyr.service` from the tray without a password (`install-linux.sh`'s polkit rule does the same). GNOME shows tray items only with the AppIndicator extension; the window and the notifications work without it. Elsewhere, build it with `nix build .#fyr-desktop`, or with `CGO_ENABLED=1 go build ./cmd/fyr-desktop` and the GTK 3, WebKitGTK 4.1 and libayatana-appindicator development packages. fyrd's updates need nothing from the app: it reconnects after each one.

## The phone

The app is a PWA: open it in the phone's browser and add it to the home screen. Allow notifications in Settings → This device.

The phone has to reach your front. With **Tailscale**, set `services.fyr.tailnet.enable = true` on NixOS. Fyr is then served by Tailscale Serve on `https://<machine>.<tailnet>.ts.net:7443`, to your Tailscale login only (`ownerLogin` sets the login on a tagged node). On a **LAN**, serve the front over HTTPS with the `tls` keys of `config.json` and pair the phone: `fyr device add` prints a one-time link (valid for 10 minutes) to open on the phone.

On macOS the Tailscale app cannot proxy to Fyr's socket, so the macOS install publishes only the address that other installs use to find it (see below), not the app.

## More machines

Every install has its own key. Installs pair once and then trust each other's keys, on the tailnet or a LAN ([decision 0030](decisions/0030-every-hop-names-who-acts.md)):

- **Same Tailscale user:** set a `peerPort` (NixOS: `services.fyr.peerPort = 7779;`) and Tailscale Serve on both. They find and pair with each other.
- **Anything else:** run `fyr pair` on one install. It prints a code; run `fyr pair CODE -addr host:port` on the other.

`fyr host list` shows the paired installs, and `fyr host remove ID` forgets one. Each front shows the threads of every host it knows, and a new thread asks which host runs it.

## Updates

Fyr updates itself only to CI-built releases ([decision 0027](decisions/0027-one-updater.md)):

1. Every green push to `main` publishes signed `fyr` and `fyrd` binaries for Linux and macOS (amd64 and arm64) as the release `build-<sha12>`, and moves the tag `deployable` to it. A version tag `vX.Y.Z` publishes the release `vX.Y.Z` the same way ([below](#channels-and-versions)).
2. Each host checks its channel every `updateEveryMinutes`: `deployable` on `main`, the newest version on `stable`. Once that has stayed put for 10 minutes, the host downloads the release, verifies its signature and checksums, and runs `fyrd selftest` on a copy of the database.
3. It switches. The launcher checks `/healthz` and rolls back to `previous` if the build is unhealthy or crashes within 10 minutes.

Settings → Auto-update turns this off. Settings → Updates shows the version and the channel, the release repo and the fingerprint of the key releases are verified with.

### Channels and versions

A version is a release named `vX.Y.Z` (V0.versioned-releases); `vX.Y.Z-rc.N` is a prerelease of it. `fyrd --version` (and `fyr --version`, which also asks the running build) prints the version, or the build's commit on `main`; the status screen and Settings → Updates show it with the channel.

| `updateChannel` | Follows |
|---|---|
| `stable` | The newest version that is not a prerelease. What a new install from the scripts follows. |
| `main` | Every green commit of `main` (the `build-<sha12>` release `deployable` points at). The default when the key is missing, so an install from before channels stays where it was. |
| `v1.2.3` (any version, prereleases too) | That version only: the host stays on it until you change the key. |

**Install a version:** `scripts/install-linux.sh --version v1.2.3` (or `install-macos.sh`). Without `--channel`, the install is then pinned to it; `--version v1.2.3 --channel stable` installs it and follows stable from there. Add `--print` to see what it would do. On a host that is already installed you can also set the key and press Update now.

**Switch channels:** set `"updateChannel"` in `~/.config/fyr/config.json` and restart Fyr (`systemctl restart fyr`, or `launchctl kickstart -k gui/$(id -u)/app.fyr.fyrd` on a Mac), or run the install script again with `--channel stable` or `--channel main`. A host that moves from `main` to `stable` goes to the newest version at its next check, even though that is older than the main build it ran (as when `deployable` is moved back); the version's smoke test runs on a copy of your data first, and if it cannot open it the host stays where it is and says so in Settings → Updates. The NixOS module has no option for the key yet: a NixOS host follows `main`.

**Cut a version** (the repository's owner): tag a commit of `main` and push the tag, `git tag v1.2.3 <sha> && git push origin v1.2.3`, or run `gh workflow run test.yml -f version=v1.2.3 -f commit=<sha>`. CI runs the gate on that commit, builds it with the version in the binaries, signs it and publishes the release `v1.2.3` with the commits since the previous version as its notes; a `-rc.N` version is published as a prerelease.

**No `gh` needed** (V0.release-without-gh). The installs and the updater read a public release repo over plain HTTPS: the tag with `git ls-remote`, a release's files from their download URLs (`https://github.com/<repo>/releases/download/build-<sha12>/…`), and the compares (how far behind, the changelog) from GitHub's REST API without a login. GitHub allows 60 such calls an hour per address without a login; the updater spends a few per new release, asks again with the answer's ETag, and when GitHub says the limit is spent waits until its reset without asking. A token (`GH_TOKEN` or `GITHUB_TOKEN` in `github.env`, else `gh auth token` when `gh` is installed) is sent to the API only: it raises the limit and reads a private fork's releases. The first install checks the sums over HTTPS; the running fyrd checks every later release's signature.

**A fork** that publishes its own releases sets `updateRepo` to its repository and `updateKey` to the public half of its own key (`go run ./cmd/fyrrelease keygen` makes a pair; the private half is `FYR_RELEASE_KEY` in `.github/workflows/test.yml`). `go run ./cmd/fyrrelease verify DIR KEY` checks a downloaded release against it. The install scripts take the repo from `FYR_REPO`.

To stop a bad release on every host, move the tag back: `git push -f origin <sha>:refs/tags/deployable`.

To try a branch, an agent asks Fyr to deploy its checkout (`fyr.deploy {dir}`). Fyr builds it and switches once you press; auto-update then waits until a release contains that commit.

## Uninstall

- **NixOS:** remove `services.fyr`, then remove `~/.local/share/fyr` if you do not want the data.
- **macOS:** choose **Uninstall…** from the Fyr menu-bar icon (unregisters the background agent and Login Item, turns off Fyr's Tailscale Serve if configured, optionally deletes `~/.local/share/fyr` and `~/Library/Logs/fyr.log`, and moves `Fyr.app` to the Trash). Dragging `Fyr.app` to the Trash also cleans up the background agent on its next start. For a script install, run `scripts/install-macos.sh --uninstall`.
- **Linux without Nix:** `scripts/install-linux.sh --uninstall`, then remove `~/.local/share/fyr` and `~/.config/fyr` if you do not want the data.
- **From source:** stop `fyr`, then remove `~/.local/share/fyr` and `~/.config/fyr`.

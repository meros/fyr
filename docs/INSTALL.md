# Installing and running Fyr

The [README](../README.md) has the short install for NixOS, macOS, Linux without Nix and from source. This page covers the rest: configuration, the phone, more machines and updates.

## Files

| Path | What |
|---|---|
| `~/.config/fyr/config.json` | The daemon's configuration. Fyr reads it again when it changes. Most settings are also in the app (Settings). |
| `~/.config/fyr/github.env` | Optional `GH_TOKEN=…` (mode 0600) for reading releases from a private fork; without it the `gh` login is used. |
| `~/.local/share/fyr/` | Data: the database, the builds (`current`, `previous`), logs and the owner socket. |
| `~/Library/Logs/fyr.log` | The log on macOS. On Linux: `journalctl -u fyr`. |
| `/etc/systemd/system/fyr.service`, `fyr-agents.slice`, `/etc/polkit-1/rules.d/50-fyr-agents.rules`, `/usr/local/lib/fyr/agents-pressure-chown` | Linux without Nix: what `scripts/install-linux.sh` writes as root ([below](#linux-without-nix)). |

On NixOS, `services.fyr` writes the configuration: every option of the module is documented in [`nix/module.nix`](../nix/module.nix).

## Configuration

The keys you are most likely to set in `config.json`:

| Key | Default | What |
|---|---|---|
| `permissionMode` | `"auto"` | `claude --permission-mode` for every provider that sets none |
| `idleStopMinutes` | `10` | Stop an agent process after this long without a run; the thread resumes on the next message |
| `profiles` | found on the host | Providers: Claude logins (each its own `CLAUDE_CONFIG_DIR`) and Anthropic-compatible endpoints. Easier from Settings → Providers |
| `scanRoots` | none | Extra folders the folder picker scans for git repositories |
| `slots` | `2` | The share of the host heavy jobs may use before they queue: 2 is half the host per job, as a starting point that Fyr then learns per job class |
| `role` | `"both"` | `host` (runs agents), `front` (serves the app for `hosts`), or `both` |
| `updateEveryMinutes` | `60` | How often the host checks for a new release |
| `agentPath` | none | Folders put first on the agents' `PATH` (`~/` is your home folder), read at start |
| `cgroups` | absent: off | The agents' cgroups and budget. The NixOS module and `install-linux.sh` set it; see [HOST-SETUP](HOST-SETUP.md) |

**The agents' `PATH`** is, in this order: `agentPath`, the `PATH` fyrd started with (the unit's), the `PATH` of your login shell (`$SHELL -l`, read once at start; a profile that takes more than 5 s is skipped and logged), then those of `~/.local/bin`, `~/.nix-profile/bin`, `/etc/profiles/per-user/<you>/bin`, `/run/wrappers/bin` and `/run/current-system/sw/bin` that exist. Each folder appears once, where it first comes. So a tool your login shell finds, the agents find; restart Fyr after you change your profile.

## Linux without Nix

`scripts/install-linux.sh` is the NixOS module's stand-in on Debian, Ubuntu and other systemd Linux (V0.linux-without-nix). Run it as the user Fyr and its agents run as; it refuses root.

**What runs as you:** the download of the release that the tag `deployable` points at (with your `gh` login, checked against `SHA256SUMS`) into `~/.local/share/fyr/builds/<sha12>`, the `current` link, the seed (`seed/fyrd`) and the launcher (`launcher/fyr`); and the `cgroups` keys in `~/.config/fyr/config.json` (other keys are kept; with no `python3` and an existing file, it tells you what to add).

**What needs root**, run with `sudo`, each command printed first:

| File | What |
|---|---|
| `/etc/systemd/system/fyr.service` | A system unit with `User=` you: it starts at boot without a login. It runs `~/.local/share/fyr/launcher/fyr -data ~/.local/share/fyr -seed …/seed/fyrd`, reads `~/.config/fyr/github.env` if it is there, and sets no `PATH` (fyrd builds the agents' `PATH`, above). |
| `/etc/systemd/system/fyr-agents.slice` | The agents' budget (`MemoryHigh=40%`, `MemoryMax=46%`, `MemorySwapMax=30%`) and systemd-oomd's rule (80 %), the module's defaults. |
| `/etc/polkit-1/rules.d/50-fyr-agents.rules` | Lets you `start` and `set-property` units named `fyr-agent-….scope` and `fyr-deploy-….scope`, and `start`, `stop` and `restart` `fyr.service` (the Linux app's Pause and Resume), nothing else. |
| `/usr/local/lib/fyr/agents-pressure-chown` | Run as root before each start of the unit: hands you the slice's `memory.pressure`, for the guard's PSI trigger. |

Then `systemctl daemon-reload`, `enable` and `restart fyr.service`. These are the module's files (`nix/module.nix`); a change to one goes to the other.

**The layout follows polkit.** A polkit that reads JavaScript rules (0.106 and later: Debian 12, Ubuntu 24.04) gets the four files above: fyrd alone in `fyr.service`, each thread in a scope of `fyr-agents.slice` ([HOST-SETUP](HOST-SETUP.md)). With an older polkit (Ubuntu 22.04's 0.105) or none, the script writes the V0.80 unit instead: `Delegate=cpu memory`, fyrd splits it into `server/` and `agents/<thread>/`, and there is no slice or rule. Install `polkitd` 0.106 or later and run the script again to move to the slice.

**Run it again** whenever you like: it installs the current release, rewrites the root files and restarts the unit (which ends running agent turns). `--print` shows every file and command and changes nothing: no download, no `sudo`. `--uninstall` stops the unit and removes the root files; `~/.local/share/fyr` and `~/.config/fyr` stay.

**Updates** are the updater's ([below](#updates)). It reads releases with `gh` until #200, so keep `gh` logged in (or put `GH_TOKEN` in `github.env`).

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

1. Every green push to `main` publishes signed `fyr` and `fyrd` binaries for Linux and macOS (amd64 and arm64) as the release `build-<sha12>`, and moves the tag `deployable` to it.
2. Each host checks `deployable` every `updateEveryMinutes`. Once the tag has stayed put for 10 minutes, the host downloads the release, verifies its signature and checksums, and runs `fyrd selftest` on a copy of the database.
3. It switches. The launcher checks `/healthz` and rolls back to `previous` if the build is unhealthy or crashes within 10 minutes.

Settings → Auto-update turns this off. A fork that publishes its own releases sets `updateRepo` to its repository, signs them with its own key (`FYR_RELEASE_KEY` in `.github/workflows/test.yml`) and builds with that key's public half in `internal/release`.

To stop a bad release on every host, move the tag back: `git push -f origin <sha>:refs/tags/deployable`.

To try a branch, an agent asks Fyr to deploy its checkout (`fyr.deploy {dir}`). Fyr builds it and switches once you press; auto-update then waits until a release contains that commit.

## Uninstall

- **NixOS:** remove `services.fyr`, then remove `~/.local/share/fyr` if you do not want the data.
- **macOS:** `scripts/install-macos.sh --uninstall`.
- **Linux without Nix:** `scripts/install-linux.sh --uninstall`, then remove `~/.local/share/fyr` and `~/.config/fyr` if you do not want the data.
- **From source:** stop `fyr`, then remove `~/.local/share/fyr` and `~/.config/fyr`.

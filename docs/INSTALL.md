# Installing and running Fyr

The [README](../README.md) has the short install for NixOS, macOS and from source. This page covers the rest: configuration, the phone, more machines and updates.

## Files

| Path | What |
|---|---|
| `~/.config/fyr/config.json` | The daemon's configuration. Fyr reads it again when it changes. Most settings are also in the app (Settings). |
| `~/.config/fyr/github.env` | Optional `GH_TOKEN=…` (mode 0600) for reading releases from a private fork; without it the `gh` login is used. |
| `~/.local/share/fyr/` | Data: the database, the builds (`current`, `previous`), logs and the owner socket. |
| `~/Library/Logs/fyr.log` | The log on macOS. On Linux: `journalctl -u fyr`. |

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
- **From source:** stop `fyr`, then remove `~/.local/share/fyr` and `~/.config/fyr`.

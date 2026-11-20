# 0037 The Mac app: Fyr.app in a DMG, signed with Fyr's own certificate, registered with SMAppService, a native window, and a setup wizard on both fronts

Date: 2026-11-20 · Status: **accepted** (#434; owner decisions 20 Nov) · Builds on 0007 (the module's Mac stand-in), 0026 and 0030 (pairing), 0027 (one updater), 0028 (who may act) · Depends on #200 (the release source and launchd label are config, releases read without `gh`) and the versioned releases of `PLAN.md` v1.0

**Context.** v1.0's exit criterion (`PLAN.md`) asks that a stranger on a fresh macOS account go from `Fyr.dmg` to a merged PR without a terminal. Today `scripts/install-macos.sh` needs a checkout, Xcode's command line tools, Homebrew's `git`, `gh` and `claude` with `gh` logged in, and `python3`. It signs ad hoc, so macOS ties each permission to a binary's hash, and a small C host program that never changes holds the permissions in its place. A new install starts on an empty inbox with no provider, no project and no phone.

**Decision.**

| Question | Decision |
|---|---|
| Who it is for | The public: anonymous downloads of public releases, no token for the first install or for updates (#200) |
| Package | A DMG with `Fyr.app`; no `.pkg`, no admin password, no root |
| Bundle | `Contents/MacOS/Fyr` (menu-bar shell and window), `Contents/MacOS/fyr-host` (launchd's first process, replacing `<data>/host/fyr`), `Contents/Resources/fyrd` and `fyr` (universal seed binaries), `Contents/Library/LaunchAgents/<label>.plist` |
| Registration | `SMAppService.agent` (macOS 13+): a per-user agent, no admin rights, listed under Login Items. The hand-written LaunchAgent plist is the fallback |
| Data | Stays in `~/.local/share/fyr`; the self-update into `builds/` is unchanged |
| Signing | One self-signed certificate, "Fyr Local Signing", whose private key is a CI secret. `fyr`, `fyrd` and `fyr-host` are signed with `rcodesign` on the Linux runner, the app with `codesign` on the Mac runner. The updater also checks that a new build is signed by that certificate. A Developer ID and notarization replace it later (permissions are asked for once more then) |
| Gatekeeper | A browser download carries quarantine, so the first launch needs right-click → Open or Privacy & Security → Open Anyway; the download page and README say so. A one-line `curl` install (no quarantine) is the path for terminal users |
| Build | The owner's Mac is a self-hosted runner labelled `macos`. `mac.yml` builds the shell and DMG when `macos/` changes or a release needs a new DMG, and uploads them to the release. A DMG that lags a release while the Mac sleeps is harmless. `release.sh` makes universal binaries (`llvm-lipo` in the flake) and keeps the per-arch ones for self-update |
| Download | `releases/latest/download/Fyr.dmg` |
| The shell's job | Registers the agent; menu bar (Open, Pause, Quit, Uninstall…); the window; native notifications; runs `xcode-select --install`. Everything else is in the web wizard, which serves desktop and phone |
| Window | WKWebView on `http://fyr.localhost:PORT`, the origin `fyr open` uses, signed in by a one-time link from the owner socket. An ATS exception for `localhost` and its subdomains allows plain HTTP there. External links open in the default browser; ⌘W, ⌘Q, ⌘,; the size is restored; a dock badge counts Your move; files drop into the composer; no service worker |
| Notifications | WKWebView has no Web Push. The shell follows fyrd's stream on the owner socket and posts `UNUserNotificationCenter` notifications under the push attention rules; a click opens the thread. The window registers as a device of kind `native-mac`, which Web Push skips |
| Shell updates | fyrd sees a newer app version in `manifest.json` and offers "Update Fyr.app" |
| Setup wizard | Steps worked out from the host's real state, never a stored "done" flag: one read, `host.setup`, returns each check (ok, missing or fixable, with a reason), and each fix is an ordinary command. Steps: this Mac (macOS 13+, command line tools, disk), Claude (CLI, provider, login), GitHub (`gh` and its scopes; download into `<data>/tools`, device-code login in the page), first project (scan `~/git`, `~/code`, `~/src`, `~/Developer`, or clone), phone (Tailscale Serve set up by fyrd, or LAN TLS; a QR code), other installs (pair, 0026). A skipped step stays as a "Set up · 3 of 6" card atop the inbox and in Settings → Setup. It ends on a ready-made first thread. Desktop and phone in the same PR |
| Permissions | Asked for only when an agent runs into them, never up front |
| Moving script installs | The app stops the script's launchd job, removes its plist and old `~/Applications/Fyr.app`, keeps the data. macOS asks once more for each permission |
| Uninstall | Menu → Uninstall… unregisters the agent, turns Tailscale Serve off and asks whether to keep the data. An app moved to the Trash is noticed by the next fyrd start, which cleans up |

**Spike (20 Nov, macOS 26.4.1, Swift 6.3).** A bundle signed with a fresh self-signed code-signing certificate, untrusted by the system:

| Question | Result |
|---|---|
| Does `codesign` sign with an untrusted self-signed identity? | Yes, by its SHA-1 hash, once its keychain is on the user's search list (`--keychain` alone finds nothing). CI adds a temporary keychain to the list and removes it after |
| Do permissions survive a rebuild? | Two builds with different binaries get the same designated requirement, `identifier "<id>" and certificate leaf = H"<cert sha1>"`, where ad hoc gives `cdhash H"…"`. TCC keys grants by the designated requirement, so a grant survives every build signed with the certificate and `fyr-host` can change freely. A grant surviving a real update is still checked by hand in the clean-VM run |
| Does SMAppService accept the app? | Yes. `register()` went straight to `enabled` (no approval step), launchd ran the bundle's helper through `BundleProgram`, a bundle replaced in place with another build kept the registration and ran the new helper, and `unregister()` removed the job |
| What does unregister leave? | A Background Task Management record "disabled, allowed" for the app and its agent; it goes when the app is deleted. The first `register()` shows macOS's "Background item added" notice |
| Gatekeeper | `spctl` rejects the app (`origin=Fyr Local Signing`), as expected without a Developer ID |
| Does `fyr.localhost` load in WKWebView? | Name resolution works; App Transport Security blocks plain HTTP to it even with `NSAllowsLocalNetworking`. An `NSExceptionDomains` entry for `localhost` with subdomains and insecure loads fixes it. `127.0.0.1` needs no exception |

**Order of work (a PR each).** (1) this decision; (2) the wizard and `host.setup` against the script install, Tailscale Serve moving into fyrd; (3) release: public URLs (#200), universal binaries, signing, the certificate check in the updater; (4) the Mac runner, `mac.yml` and the shell: registration, menu bar, window, moving script installs, the DMG; (5) native notifications and the `native-mac` device kind; (6) an install test on a clean VM (tart on the Mac runner): mount, copy, launch, `/healthz`, the wizard, uninstall, nothing left; then the README. `install-macos.sh` stays as a developer path. Later: Developer ID, notarization, a Homebrew cask.

**Consequences.** A stranger installs without a terminal; a permission given once survives auto-updates and shell updates; the C host-program workaround goes. The cost is a Mac runner the owner keeps awake for DMG builds, a signing secret in CI, and a first launch that needs Open Anyway until a Developer ID. The wizard's `host.setup` also serves the Linux install (#199), since its checks are the host's state, not the Mac's.

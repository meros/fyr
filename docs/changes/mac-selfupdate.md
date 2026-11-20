# V0.mac-selfupdate: Fyr.app updates itself from the latest app release, accepting only a build signed with its own certificate

→ decision 0037 ("Shell updates"), which had fyrd offer an update; this makes it automatic. Builds on V0.mac-ci.

Why: fyrd updates itself (decision 0027), but the app around it (the shell, `fyr-host`, the launcher and the seed fyrd) stayed as installed. A Mac install should keep itself current as a whole.

1. **The check.** An app signed with a certificate checks the repo's latest release a minute after it starts and every 6 hours, and from the menu (Check for Updates…). It reads GitHub's API without a login (the repo is public). The latest release is `app-<sha12>`, which mac.yml publishes (V0.mac-ci). A tag that is not this build's version is an update.
2. **The trust.** `build.sh` writes the signing certificate's SHA-1 into Info.plist (`FyrSigner`). The app downloads `Fyr.app.zip`, unpacks it beside itself, and swaps it in only when `codesign --verify --strict --deep -R '=identifier "<id>" and certificate root = H"<FyrSigner>"'` passes and the new app's version is the tag's. A different certificate, an ad hoc build or a changed file fails it. An app built ad hoc pins nothing and does not update itself.
3. **The swap.** `FileManager.replaceItemAt` replaces the bundle with one rename on its volume. The app then starts the new build (`--updated`, which keeps the window as it was) and quits. fyrd, the launcher and the agents run on: a running program keeps its old files, and launchd starts the new `fyr-host` at the agent's next start. The agent's registration survives, as the spike found for a certificate-signed bundle replaced in place.
4. **The agents' PATH** no longer carries fnm's per-shell folders (`fnm_multishells`), which fnm removes later. `fyr-host` puts fnm's default Node (`~/.local/share/fnm/aliases/default/bin`) in their place when it exists.

An install from before this change pins no certificate and is replaced by hand once.

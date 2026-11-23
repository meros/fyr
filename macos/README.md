# Fyr.app

The Mac app (decision 0037, `docs/changes/mac-shell.md`): a menu-bar shell and window in Swift, `fyr-host` (the agent's first process) in C, and the current release's `fyr` and `fyrd`.

```sh
macos/build.sh            # build/Fyr.app, signed ad hoc, with the deployable release
macos/build.sh --dmg      # and build/Fyr.dmg
```

Needs Xcode's command line tools (Swift 5.9+). An ad hoc build that replaces a registered one in place is killed by launchd; run `Fyr.app/Contents/MacOS/Fyr --agent unregister` and then `--agent register`, or sign with a certificate (`FYR_SIGN_IDENTITY`, see `build.sh`).

CI builds the official app on the Mac runner when `macos/` changes on main (`.github/workflows/mac.yml`, `docs/changes/mac-ci.md`): `https://github.com/meros/fyr/releases/latest/download/Fyr.dmg`. A PR that changes `macos/` also compiles it there when the Mac is up (`.github/workflows/mac-pr.yml`, `docs/changes/ci-gates.md`); that check is advisory and never holds a merge, so the build on main stays the gate. To build it by hand with the same certificate (the owner's copy):

```sh
FYR_SIGN_P12=~/.config/fyr/signing/fyr-local-signing.p12 FYR_SIGN_P12_PASSWORD="$(cat ~/.config/fyr/signing/p12.password)" macos/build.sh --dmg
```

A test install beside the real one:

```sh
FYR_BUNDLE_ID=fyr.app.test FYR_LABEL=fyr.agent.test FYR_DATA='~/.local/share/fyr-test' \
FYR_LOG='~/Library/Logs/fyr-test.log' FYR_REPLACES= \
FYR_FLAGS='-listen 127.0.0.1:7787 -config ~/.config/fyr-test/config.json' macos/build.sh
```

# Fyr.app

The Mac app (decision 0037, `docs/changes/mac-shell.md`): a menu-bar shell and window in Swift, `fyr-host` (the agent's first process) in C, and the current release's `fyr` and `fyrd`.

```sh
macos/build.sh            # build/Fyr.app, signed ad hoc, with the deployable release
macos/build.sh --dmg      # and build/Fyr.dmg
```

Needs Xcode's command line tools (Swift 5.9+). An ad hoc build that replaces a registered one in place is killed by launchd; run `Fyr.app/Contents/MacOS/Fyr --agent unregister` and then `--agent register`, or sign with a certificate (`FYR_SIGN_IDENTITY`, see `build.sh`).

A test install beside the real one:

```sh
FYR_BUNDLE_ID=fyr.app.test FYR_LABEL=fyr.agent.test FYR_DATA='~/.local/share/fyr-test' \
FYR_LOG='~/Library/Logs/fyr-test.log' FYR_REPLACES= \
FYR_FLAGS='-listen 127.0.0.1:7787 -config ~/.config/fyr-test/config.json' macos/build.sh
```

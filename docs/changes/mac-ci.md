# V0.mac-ci: CI builds Fyr.app and Fyr.dmg on the owner's Mac when the app's sources change, signed with Fyr's own certificate

→ decision 0037 ("Signing", "Build", "Download"); builds on V0.mac-shell.

Why: Fyr.app must come signed with one certificate, so a permission given once survives each new build, and from CI, so a stranger downloads a build nobody made by hand.

1. **"Fyr Local Signing"** is a self-signed code-signing certificate (RSA 3072, 20 years). Its `.p12` and password are the repo secrets `FYR_MAC_SIGN_P12` (base64) and `FYR_MAC_SIGN_PASSWORD`. The owner's copy stays in `~/.config/fyr/signing` (mode 0700), for builds by hand.
2. **`macos/build.sh` signs from a `.p12`** (`FYR_SIGN_P12`, `FYR_SIGN_P12_PASSWORD`). It imports the file into a keychain made for the build, puts that keychain on the user's search list only while signing, and deletes it after. `FYR_VERSION` names the build.
3. **`.github/workflows/mac.yml`** runs on the self-hosted runner labelled `macos` (the owner's Mac) on a push to main that changes `macos/` or the workflow, and by hand. It never runs for a PR, so no fork's code reaches the Mac. It builds Fyr.app and Fyr.dmg, checks that the app is signed with the certificate, and publishes `Fyr.dmg`, `Fyr.app.zip` and `SHA256SUMS` as the release `app-<sha12>`, GitHub's latest: `releases/latest/download/Fyr.dmg`. The newest three app releases are kept. The `build-<sha>` releases stay `--latest=false`, so they never take that link.
4. **A change to fyrd alone builds no new app.** The app carries fyrd only as a seed; an installed app updates fyrd itself (decision 0027).

Not yet: an installed app learning that a newer app release exists (decision 0037, "Shell updates").

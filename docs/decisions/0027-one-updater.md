# 0027 Auto-update installs only CI-built, signed releases; a local build is an explicit developer switch

Date: 2026-11-16 · Status: accepted (#144) · Amends V0.13 (2), V0.111 and #141's `scripts/update.sh` · Spec row V0.126

**Context.** On 16 Nov Fyr updated itself about 30 times on one laptop, and the update path showed its seams:
- It built the coordinator agent's working checkout, so it once built an older commit than main. The coordinator then checked every binary's `vcs.revision` with `strings`, and ran `git pull` before every ask.
- It ran the full `make test` locally (about 4 min). That run failed once under load, and the notice said only "exit status 2".
- A second updater, `scripts/update.sh`, took no lock and reported nothing.
- builds/ reached 7.3 GB.
- CI on main cancelled itself, so many merged commits had no result.
- A build that crashed after its 60 s probation sent the launcher into a crash loop instead of a rollback.

The full list is in #157.

**Decision.**

1. **What an auto-update installs.** Only a release CI published. On a green push to main, the `release` job cross-compiles `fyr` and `fyrd` with the flake's toolchain for linux and darwin, amd64 and arm64. This is cheap: CGO is off and the client is embedded. The job writes `manifest.json` and `SHA256SUMS`, signs the sums with ed25519 (repo secret `FYR_RELEASE_KEY`; the public key is in `internal/release`), publishes `build-<sha12>`, and moves the tag `deployable` forward. A host never builds to update and needs no toolchain.
2. **What runs on the host before a switch.** The host verifies the signature and every checksum. Then it runs `fyrd selftest` on a snapshot of the database: this checks the schema, a read, and the embedded client. CI already ran the full suite on that commit.
3. **Trigger.** Each host reads `deployable` every minute through `gh api`, which needs no extra token. When the tag moves, a quiet period starts (10 min, `updateQuietMinutes`). When it ends, the host installs the newest release once. An agent's `fyr.deploy` ask without `dir` means "install the newest release now", and Auto-update approves it. Moving `deployable` back by hand is followed on every host, which is how to stop a bad release.
4. **Hosts.** Each host updates on its own, with no canary. A release that rolled back on a host is not installed there again until a newer one comes.
5. **Switch and health.** Every switch goes through the same path, whether release or local build:
   - snapshot the database, then link `next` atomically and exit 75;
   - the launcher asks `/healthz` after ready;
   - a red health check, or a crash within 10 min, rolls back to `previous`;
   - `probation-ok` tells fyrd that the switch held;
   - old builds and snapshots are pruned.
6. **Local builds.** `fyr.deploy {dir}` builds a checkout to try it: it is how Fyr is developed from inside Fyr.
   - It always waits for the owner's press, because the code is unreviewed.
   - It runs the smoke test, or the full suite when the ask says `tests: true`, and keeps its build and test logs.
   - Once it runs, auto-update pauses until a release contains its commit.
7. **Releases always restart.** The no-restart switch for client-only changes (V0.81) stays for local builds only.

**Consequences.**
- `scripts/update.sh` and the launchd update job are gone.
- `scripts/install-macos.sh` downloads a release instead of building, so a Mac needs only git, gh and claude.
- The Makefile is a developer's and CI's tool on Linux under Nix; no host runs it to update.
- The launcher binary changes only with a NixOS rebuild or a Mac reinstall, and fyrd works with an older launcher, which just skips the health check.
- Rotating the key needs a new public key in a build that each host installs by hand.

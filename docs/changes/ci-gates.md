# V0.ci-gates: a PR runs main's gate, the Linux desktop shell is built on every PR and on main, and a PR that changes macos/ compiles Fyr.app

→ V0.ci-self-hosted, V0.mac-ci, decision 0038. *Amends V0.ci-self-hosted: a PR's gate is `make test-ci` and the Nix packages, not `make test`.*

Why: no gate compiled the Linux GTK shell. `cmd/fyr-desktop`'s window, tray and offline page are `linux && cgo`, and the Makefile runs Go with `CGO_ENABLED=0`, so `go vet` and `go test` saw only its stub; main built only `.#fyr` and a PR no Nix package at all. A Swift error in `macos/` showed up only in the release run on main. main's gate was `make test`'s parts written out by hand in `test.yml`, while the Makefile said CI ran `make test FYR_LAYOUT_TIMING=1`, which would have failed (make exports a command-line variable, and a Go test refuses any `FYR_` variable). `scripts/ci-local.sh` said PRs start no workflow.

(1) **One gate target.** `make test-ci` builds `web/dist`, runs go vet and go test side by side, then the typecheck, the web unit tests and the layout test with its timing budgets on, then the auth e2e. The budgets measure wall time, so the web side never runs beside the Go side. `pr.yml`, `test.yml` and `scripts/ci-local.sh` all run it. The Makefile unexports `FYR_LAYOUT_TIMING`, so `make test FYR_LAYOUT_TIMING=1` works too: the value reaches the layout test through its recipe and no Go test sees it.

(2) **The Nix packages on every PR.** `pr.yml`, `test.yml` and `scripts/ci-local.sh` run `nix build .#fyr .#fyr-desktop` (about two minutes on the runner): the Go tests in the sandbox, the Go and npm dependency hashes, and the only compile of the cgo GTK shell. A break that only the Nix build catches no longer reaches main first.

(3) **Fyr.app on a PR.** `.github/workflows/mac-pr.yml` runs `swift build -c release` for both architectures on the self-hosted Mac runner when a PR changes `macos/` (or the Mac workflows). It signs nothing, reads no secret and publishes nothing. The lock against forks is `pr.yml`'s: the repository setting that holds every outside contributor's run for approval, which the owner does not give on these runners, and a job `if` that skips a fork's run. While the Mac sleeps that check, and the PR's merge ask, wait for it. A PR that does not touch `macos/` never starts it.

(4) **Text.** `scripts/ci-local.sh` says it is the fallback for when the runner is down; `docs/PROCESS.md`, `macos/README.md` and the Makefile's timing comment say what CI runs.

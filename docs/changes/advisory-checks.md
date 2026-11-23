# V0.advisory-checks: a check whose name ends in "(advisory)" never holds or fails a merge ask

→ V0.112, V0.136, V0.ci-gates. *Amends V0.112: Summarize reads a PR's checks less its advisory ones.*

Why: a job for a self-hosted runner that is offline (a Mac that sleeps) stays queued, and GitHub ends no queued job by itself: `timeout-minutes` and `continue-on-error` start counting only once a runner takes it. Fyr read every check in a PR's rollup, so one such job kept the merge ask waiting until the machine woke. Counting only the checks branch protection requires was the other way, but a repository without required checks (Fyr's own) would then still count every check, and a cancelled job reads as a failure.

(1) **The rule.** A check run or commit status whose name ends in `(advisory)` (case and trailing space ignored) is left out of the PR's checks: not pending while it waits or runs, not failed when it fails or is cancelled, and not counted in "n of m checks done". Every other check counts as before; a PR whose only checks are advisory is read as a PR with no check reported yet.

(2) **Shown.** The PR's detail still lists an advisory check with its state, so a failure is visible; only the merge gate and the card's CI state ignore it.

(3) **First use.** The PR-time Fyr.app build (`.github/workflows/mac-pr.yml`, V0.ci-gates) names its job `swift build (advisory)`: it runs when the Mac is up and never gates a merge; the build on main (`mac.yml`) stays the gate for the app.

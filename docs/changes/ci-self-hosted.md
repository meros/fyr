# V0.ci-self-hosted: a PR's gate runs on a self-hosted GitHub Actions runner on meros-server; the local gate stays as the fallback

→ amends #405 (PRs started no Actions run; scripts/ci-local.sh posted `local/test`). The owner, 20 Nov: "Can we add a gh runner to it? For these types of jobs?"

1. **Why.** Self-hosted minutes cost nothing on a private repo, so PRs can run on GitHub again. The local gate on the laptop competed with the owner's work and with agents (load 40–70 on 20 Nov; gates at nice 19 starved the layout test into timeouts). meros-server ran the whole `make test` in 140 s idle, against 133–329 s on the laptop.
2. **The runner** (nixos-config, `modules/nixos/services/github-runner-fyr.nix`): one runner for `meros/fyr`, labels `self-hosted, meros-server, nix`, a dynamic user, one job at a time, CPUWeight 50 and MemoryHigh 5 GB / MemoryMax 6 GB so the box's other services keep their share; nix, git, make and Chromium as `google-chrome` on its PATH.
3. **The workflow** `.github/workflows/pr.yml`: on `pull_request`, `runs-on: [self-hosted, meros-server]`, `make test` and gitleaks over the PR's commits, the same as `scripts/ci-local.sh`; a newer push cancels the older run. Fyr's merge ask waits for its check as for any check.
4. **Unchanged.** The hourly `test.yml` still tests main on GitHub's runners with the layout timing budgets and publishes the release. `scripts/ci-local.sh` is the fallback when the runner is down; its `local/test` counts the same.
5. **Safety.** A self-hosted runner runs whatever a workflow says; this repo is private and only the owner opens PRs. It must never serve a public repo or fork PRs.

# V0.public-repo: the repository is public; fork PRs stay off the self-hosted runner, and the tree holds no private names, quotes or build output

→ amends V0.ci-self-hosted (6). The repository goes from private to public.

1. **CI.** `.github/workflows/pr.yml` runs on the self-hosted runner, and a PR runs the workflow from its own head, so a fork could change it. The lock is the repository's Actions setting "Require approval for all external contributors": no fork PR runs until the owner approves it, and the owner approves none on that runner. The job also has `if: github.event.pull_request.head.repo.full_name == github.repository`, so an approved fork run skips it. `test.yml` runs only on pushes to main and by hand, as before.
2. **Private names.** The names of another project, its repos and the owner's machines that had come back into tests and docs since #196 are neutral again (`acme`, `garden`, `home-laptop`, `home-server`). The runner's label `meros-server` stays: it is the runner's real label.
3. **No quotes.** The owner's quoted words and the dated "(owner, …)" notes are gone from the spec, the change log, the decisions and the design notes; references to issues, decisions and V0 rows in them are kept. `docs/changes/README.md` and `CLAUDE.md` say so for new entries.
4. **Removed.** `research/` (the owner's T3 Code usage and the competitor notes) and the research issue template; the committed `fyrd` binary and the `result` symlink, now in `.gitignore`.
5. **Not changed.** Git history, past PRs, issues and releases are public as they are.

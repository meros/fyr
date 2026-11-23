# How Fyr is built

Fyr runs as a software project with three layers. Each layer has one home.

| Layer | Home | Changes by |
|---|---|---|
| **Spec**: what Fyr does | `docs/SPEC.md` (versioned, section numbers are stable IDs) | a PR labelled `type:spec` |
| **Decisions**: why it is that way | `docs/decisions/NNNN-title.md`, one file per decision | a PR; the discussion happens in a `type:decision` issue |
| **Plan and status**: what is done, what is next | GitHub milestones and issues | issue state; `docs/PLAN.md` only holds milestone definitions and exit criteria |

Status is never written by hand in a markdown file. A milestone's progress bar and its open issues are the status.

## The change loop

Every change in behaviour follows the same order:

1. **Spec.** Change the spec section, or add one, and add the change as its own file in `docs/changes/` (never a row in the table of SPEC 0.5, which is closed). The PR says which section changed and why. A change that needs a choice between options first gets a decision record.
2. **Red.** Write a test that names the spec ID and fails against the current code. Commit it (the branch's gate may show it red).
3. **Green.** Change the code until the test passes. Do not change the test to make it pass, unless the spec changed again.
4. **Close.** The PR closes the issue (`Closes #N`) and lists the spec IDs it covers.

A bug is a place where the code breaks the spec. The fix starts at step 2. If the spec itself was wrong, the fix starts at step 1.

## Traceability

- Tests name the spec ID they prove in the test name or in a comment on the first line: `// SPEC 6.2: failed is never hidden by a visit`.
- `scripts/spec-coverage.sh` lists each spec ID referenced by a test, so a section without tests is visible.
- Issues carry the spec section in the form field "Spec", and a label `area:*`.

## Issue types (labels)

| Label | For | Form |
|---|---|---|
| `type:work` | a slice of the plan with acceptance criteria | Work item |
| `type:bug` | the code breaks the spec | Bug |
| `type:spec` | the spec must change (new behaviour, a gap, a contradiction) | Spec change |
| `type:decision` | a choice that needs options and a recorded reason | Decision |
| `type:research` | a question to answer with sources, ends in a note under `docs/` | Research |

Areas: `area:core`, `area:agent`, `area:store`, `area:server`, `area:web`, `area:t3`, `area:github`, `area:nix`, `area:design`, `area:android`.

Milestones and their order are in `docs/PLAN.md`.

## Tests and checks

```sh
nix develop            # Go, Node, gh, sqlite
make test              # go vet, go test, web typecheck
make e2e               # daemon + fake claude + API flow
make test-ci           # CI's gate: make test's parts with the layout timing budgets on
scripts/ci-local.sh    # a PR's gate when the runner is down: make test-ci, nix build, gitleaks; posts local/test
```

Every push to main runs one job on the self-hosted Linux runner (`.github/workflows/test.yml`: gitleaks, the
tests, the nix build, the release); `gh workflow run test.yml` starts it by hand. A PR runs `.github/workflows/pr.yml` on the same runner (V0.ci-self-hosted; a fork's PR never runs there, and it has the Nix store and Chrome), and Fyr's merge waits for
its check; when the runner is down, `scripts/ci-local.sh` on the host posts the status `local/test`, which counts the same. A PR runs what main runs (V0.ci-gates): `make test-ci` and `nix build .#fyr .#fyr-desktop`, the only build of the cgo GTK shell. A PR that changes `macos/` also compiles Fyr.app on the Mac runner when it is up (`.github/workflows/mac-pr.yml`); that check is advisory (V0.advisory-checks) and never holds a merge.

`CGO_ENABLED=0` everywhere: the SQLite driver is pure Go, and NixOS has no global C compiler.

The fake agent (`internal/claude/fake`) speaks the real control protocol. Test binaries run it as a child process, so tests cover the real client code, the process boundary and the protocol, without the network or a Claude account. A small number of tests run the real `claude` CLI; they are tagged `live` and do not run in CI.

## Commits and PRs

Plain text, no tool attribution. The PR body starts with "In short", names the spec sections and the decision records, and separates what ran from what did not.

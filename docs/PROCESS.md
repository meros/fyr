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
2. **Red.** Write a test that names the spec ID and fails against the current code. Commit it (CI may show it red on the branch).
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
| `type:research` | a question to answer with sources, ends in a `research/` file | Research |

Areas: `area:core`, `area:agent`, `area:store`, `area:server`, `area:web`, `area:t3`, `area:github`, `area:nix`, `area:design`, `area:android`.

Milestones and their order are in `docs/PLAN.md`; after `M5 Cut-over` come `v1.1` and later from `docs/FYR-V1.md` section 8.

## Tests and checks

```sh
nix develop            # Go, Node, gh, sqlite
make test              # go vet, go test, web typecheck
make e2e               # daemon + fake claude + API flow
```

`CGO_ENABLED=0` everywhere: the SQLite driver is pure Go, and NixOS has no global C compiler.

The fake agent (`internal/claude/fake`) speaks the real control protocol. Test binaries run it as a child process, so tests cover the real client code, the process boundary and the protocol, without the network or a Claude account. A small number of tests run the real `claude` CLI; they are tagged `live` and do not run in CI.

## Commits and PRs

Plain text, no tool attribution. The PR body starts with "In short", names the spec sections and the decision records, and separates what ran from what did not.

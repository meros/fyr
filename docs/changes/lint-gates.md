# V0.lint-gates: the gate checks the rules of hooks, every hook's dependency list and gofmt's layout

→ V0.ci-gates. *Amends V0.ci-gates (1): `make test` and `make test-ci` also run `go-fmt` and `web-lint`.*

Why: nothing checked the web client's hooks or the Go layout. A hook whose dependency list leaves something out on purpose looked the same as one that forgot it, so a refactor could not tell which ones it might change, and about forty Go files had drifted from gofmt.

(1) **The linter.** `web/.oxlintrc.json` turns on two rules only: `react/rules-of-hooks` and `react/exhaustive-deps`. Everything else is off. `npm run lint` runs oxlint with `--deny-warnings` over `web/src` and `web/test`. ESLint's TypeScript parser supports TypeScript below 6.1, and the client is on TypeScript 7, so the rules run in oxlint, which parses TSX itself and reads `eslint-disable` comments. oxlint reports a missing dependency where the hook body uses it, not at the dependency list. So a hook that leaves something out on purpose is wrapped in `/* eslint-disable react-hooks/exhaustive-deps -- <reason> */` … `/* eslint-enable … */`, and a one-line hook takes `// eslint-disable-next-line … -- <reason>`. The reason states why the list is partial: a mount-only effect, a clock tick that re-evaluates `Date.now()`, a function that is new on each render and reads only setters and refs, or a key that must not follow every update.

(2) **The hooks.** 76 hooks were reported. None had a list that made it act wrongly.
- 21 hooks are now complete. The value they left out never changes, so adding it changes no behaviour: the module-wide `api`, `useState` setters, `useRef` objects, and the conversation cache from `useMemo(…, [])`. Three hooks were rewritten so their list is complete without a reason:
  - the deploy line's "first seen" uses a functional update;
  - the invite's expiry timer depends on the expiry it reads;
  - the view pane names `hasView` instead of `!!v` in its list.
- 55 hooks are deliberately partial and carry their reason.
- The three `rules-of-hooks` reports were local helpers named `use`. They are renamed `usage` (the mock load) and `read` (the DTO contract test).

(3) **gofmt.** `make go-fmt` fails, and names the files, when `gofmt -l` finds any tracked Go file it would change. The files it found are formatted in this change.

(4) **Wired in.** `make test` runs `go-fmt` and `web-lint` beside the other phases. `make test-ci` runs `go-fmt` with go vet and go test, and `web-lint` with the typecheck. `make test-go` and `make test-web` take them too. So the PR gate (`pr.yml`), main's gate (`test.yml`) and `scripts/ci-local.sh` all run them. The flake's `npmDepsHash` follows the new dev dependency.

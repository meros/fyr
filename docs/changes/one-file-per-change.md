# V0.one-file-per-change: each change to v0 is its own file, named instead of numbered

→ SPEC 0.5, `docs/PROCESS.md`. (owner, 18 Nov: "Almost every pr conflicts due to version. Fix this problem.")

(1) **Why.** Every PR added a row at the end of the table in SPEC 0.5, so any two open PRs conflicted on that line, and each had taken its number from the `main` it started on: V0.199 and V0.207 were each given twice.

(2) **The rule.** The table is closed at V0.211. A new change is a file in `docs/changes/`, and its name is its ID (`docs/changes/README.md`). A PR title needs no version.

(3) **Held by a test.** `docs/changes_test.go` fails on a new row in the table, and on an entry whose first line does not name its file.

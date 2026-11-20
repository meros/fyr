# Change log of v0, after V0.213

One file per change, so that two PRs never touch the same line and never have to agree on a number.

- **The name is the ID.** `docs/changes/quiet-merge.md` is `V0.quiet-merge`. Choose a short lowercase name for what changes (letters, digits, `-`). Tests, code comments and later entries cite it like a numbered row: `// V0.quiet-merge: ...`. No number is chosen, so none can be taken twice.
- **The first line is** `# V0.quiet-merge: the change, in one sentence`. The body is what a row of the table in `../SPEC.md` 0.5 held: `→` and what it amends, why (the problem, in your own words: the repository is public, so no quotes of the owner and no names of private projects, machines or people), the numbered points.
- **The order is the merge order:** `git log --reverse --diff-filter=A --format='%as %s' -- docs/changes/`.
- **An entry is not removed**, and a later entry amends an earlier one, as in the table. V0.1 to V0.213 stay in that table, which is closed.

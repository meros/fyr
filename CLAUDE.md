# Fyr: rules for agents

- **Desktop and phone stay in sync.** A change to one front (layout, content, a control) goes to the other in the same PR. A difference is allowed only for a stated reason (touch size, no keyboard, no hover), written next to the code that makes it. Share the component where you can (`web/src/row-ui.tsx` is the inbox row of both).
- Another session may work in the main checkout: work in a worktree of your own (`git worktree add .claude/worktrees/<name> -b <branch> origin/main`).
- **A change to the spec's change log is a new file, `docs/changes/<name>.md`, cited as `V0.<name>`** (`docs/changes/README.md`). Do not add a row to the table in `docs/SPEC.md` 0.5 and do not pick a `V0.<number>`: that line is what made every PR conflict. A PR title needs no version.

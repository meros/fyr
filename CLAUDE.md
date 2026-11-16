# Fyr: rules for agents

- **Desktop and phone stay in sync.** A change to one front (layout, content, a control) goes to the other in the same PR. A difference is allowed only for a stated reason (touch size, no keyboard, no hover), written next to the code that makes it. Share the component where you can (`web/src/row-ui.tsx` is the inbox row of both).
- Another session may work in the main checkout: work in a worktree of your own (`git worktree add .claude/worktrees/<name> -b <branch> origin/main`).

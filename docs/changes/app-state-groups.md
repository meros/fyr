# V0.app-state-groups: the app shell's state is returned in groups, and each app hook names the groups it reads

→ V0.web-tidy-split (the app-*.tsx split).

(1) **The cause.** `useAppState` returned about a hundred flat keys, every app hook took a `Pick<ReturnType<typeof useAppState>, ...>` of twenty to forty of them, and `app.tsx` merged ten results into one object and destructured about 130 names from it. What a hook read was a list of key names, not a dependency a reader could follow.

(2) **The change.** `useAppState` returns seven groups: `data` (the store and its views, host lookups, the clock), `cmds`, `open` (what is open and marked), `inbox` (what the list shows), `route`, `ui` (dialogs, key help, theme) and `device` (phone or desktop, push, install). Each other hook takes `Pick<AppState, "group" | ...>` and, when it reads the result of an earlier hook, a second argument that names those results (`{ actions, history }`). Each hook's header says what it reads and returns, and where the desktop and the phone differ and why. `app.tsx` calls the hooks in the same order as before and destructures, per source, only the names its render uses.

(3) **No change in behaviour.** The bodies of the hooks are the same; only where each name comes from changed. The hooks are called in the same order, and the effect dependencies are the same. Not done: a context layer (the groups are passed down by the one component that owns them), and splitting the `useAppState` body (its state is read across the groups, so a split would only move the same hooks).

# 0008 A project is a topic; the folder is a hint

Date: 2026-11-14 · Status: accepted · Spec: 0.5 V0.23, V0.27

**Context.** V0.23 matched every thread to a project by its folder and made an auto project when nothing matched. On the first live day this made a project for each T3 scratch folder and one for the home folder, which then held every thread below it. This thread runs in a scratch folder and is about Fyr, so folder matching filed it wrong. Projects were seeded from T3 at every start, and a project could only filter and colour threads.

**Decision.** The owner chose four things.

| Question | Decision |
|---|---|
| No project matches a new thread's folder | Fyr makes no project. The thread has "No project". When the folder is in a git repository, the thread offers "Make project from <repo>" (one tap). Auto projects end. |
| What decides a thread's project | The topic. The folder match only proposes the first project. The owner or the agent can file the thread again; the agent gets a Fyr tool `set_project` for this. A filed thread is never moved by matching. |
| Who owns the project list | Fyr. T3 projects are imported once (the existing seeded projects stay). The start no longer seeds from T3; an "Import from T3" action stays for later. |
| Extra powers of a project | All four: agent instructions (text added to the system prompt of every thread in the project), a project page with the open PRs of its repo sources, project actions (named one-tap actions such as Deploy for Fyr, each through the effect path, 5.10), and archive (hide a finished project and its threads from the inbox and pickers; reversible). |

**Consequences.** The `auto` flag and the empty-project clean-up become migration code only: existing auto projects with threads become normal projects, empty ones go. Unfiled threads are normal, so the inbox and the filters treat "No project" as a value. Agent instructions make a project useful without git. Project actions generalise `fyr.deploy`; the effect path keeps the owner's approval for each one.

## Addendum (same day): one model for every kind of place

The owner asked for one data model and no special cases. Scratch folders, plain folders, local git repositories, GitHub repositories, several hosts and later kinds (an SSH host, a devcontainer) are all the same three objects.

```
Project   { id, name, color, icon, instructions, actions[], sources[]: Ref, archived }
Workspace { id, host, path, provider, refs[]: Ref, branch?, base? }   // where one thread runs
Thread    { …, workspace: Workspace.id, projectId?, projectManual }
Ref       { kind, key }   // "folder", "host-a:/home/owner/nixos" · "repo", "github.com/meros/fyr"
```

A **provider** is the only place where a kind of place has its own code:

```
interface Provider {
  kind: string                                        // scratch, folder, clone, worktree, …
  identify(host, path) -> refs[]                      // what this place is: its folder ref, its origin remote
  prepare(project, host, opts) -> Workspace           // make or find a place to run a thread for this project
  covers(source Ref, ref Ref) -> bool                 // per ref kind: folder = path prefix on the same host, repo = equal key
}
```

- **Matching is one function**: the project whose sources cover any ref of the workspace, the most specific cover first (the longest folder, then a repo). A scratch workspace has no refs, so it matches nothing. A project is never made by matching (auto projects end), so the home folder needs no rule: it matches only when the owner gave a project that folder.
- **Starting a thread** asks the providers in order of the project's sources to `prepare` a workspace on the chosen host: a folder source gives that folder, a repo source a known clone (or a new clone), no source a fresh scratch folder.
- **Several hosts** are only the `host` field: a repo ref is host-independent, so one project holds the same repository on every host without setup.
- **Worktrees** are a provider too: `prepare` with `opts.isolated` makes a sibling folder `<clone>--<slug>` with branch `fyr/<slug>`. The owner chose **prompt only** for shared clones: the system prompt tells the agent its workspace and which other live threads use the same clone, and a Fyr tool `worktree_new` lets the agent move to its own worktree. Fyr does not make a worktree by itself and does not block writes.

The engine stores workspaces as their own table; a thread points at one. Existing threads get a workspace from their `cwd` at start (identify), so no thread data is rewritten by hand.

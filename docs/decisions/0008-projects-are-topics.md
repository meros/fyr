# 0008 A project is a topic; the folder is a hint

Date: 2026-11-14 · Status: accepted (owner, 2026-11-14) · Spec: 0.5 V0.23, V0.27

**Context.** V0.23 matched every thread to a project by its folder and made an auto project when nothing matched. On the first live day this made a project for each T3 scratch folder and one for the home folder, which then held every thread below it. This thread runs in a scratch folder and is about Fyr, so folder matching filed it wrong. Projects were seeded from T3 at every start, and a project could only filter and colour threads.

**Decision.** The owner chose four things.

| Question | Decision |
|---|---|
| No project matches a new thread's folder | Fyr makes no project. The thread has "No project". When the folder is in a git repository, the thread offers "Make project from <repo>" (one tap). Auto projects end. |
| What decides a thread's project | The topic. The folder match only proposes the first project. The owner or the agent can file the thread again; the agent gets a Fyr tool `set_project` for this. A filed thread is never moved by matching. |
| Who owns the project list | Fyr. T3 projects are imported once (the existing seeded projects stay). The start no longer seeds from T3; an "Import from T3" action stays for later. |
| Extra powers of a project | All four: agent instructions (text added to the system prompt of every thread in the project), a project page with the open PRs of its repo sources, project actions (named one-tap actions such as Deploy for Fyr, each through the effect path, 5.10), and archive (hide a finished project and its threads from the inbox and pickers; reversible). |

**Consequences.** The `auto` flag and the empty-project clean-up become migration code only: existing auto projects with threads become normal projects, empty ones go. Unfiled threads are normal, so the inbox and the filters treat "No project" as a value. Agent instructions make a project useful without git. Project actions generalise `fyr.deploy`; the effect path keeps the owner's approval for each one.

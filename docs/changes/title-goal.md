# V0.title-goal: a regenerated title says the goal of the work, not the step the agent is at

→ V0.108, V0.69. *Amends V0.108: the title call no longer asks "what the thread works on now".* 

(1) **The title is the goal.** The title call (`internal/agent/title.go`) is asked for a title like an issue's: the goal of the work and the thing it is about ("Fix the header that overlaps the menu on phones"). The goal comes from what the owner asked for; when the owner changed the goal later, the newest goal counts.

(2) **Not the current step.** The call is told not to name the step the agent is at (running tests, waiting for CI, rebasing, opening a pull request, answering a review): the title must still be right when that step is over.

(3) **The agent's own rename.** The description of `thread_rename` asks the agent for the same kind of title when it writes one itself.

(4) **Unchanged.** What the call reads (the first 2 000 characters and the newest 10 000, V0.108), who may replace whose title, and both fronts: the change is in the host's prompt only.

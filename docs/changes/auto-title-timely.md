# V0.auto-title-timely: the start title comes 45 s into the first run, and the goal title never keeps the first message's line

→ V0.auto-title. *Amends V0.auto-title (1) and (2).*

Why: a day of threads after V0.auto-title showed two gaps. (a) The start title waited for the end of the first run, and a first run often takes 5 to 30 minutes. Until then the thread kept the first line of its first message, and twice the owner pressed Regenerate before the first run ended. (b) When the first change appeared before the start title (agents open their change early, so this is common), the goal call read the first line as "The thread's current title" and was told to keep it word for word. It did: a thread started with a screenshot and "order snoozed is earliest deadline first" kept that line after its goal title.

(1) **Start title early.** The start title is asked on the agent's first message at least `StartTitleAfter` (45 s) into the first run, or at the run's end if that comes sooner. 45 s is long enough for the agent to have read the request and said what it found. Still once per thread.

(2) **No placeholder to keep.** The goal call names the current title to keep only when it is not the first message's line (`core.TitleFrom` of the first message). That line was not made by a model, so the goal call writes a title from the conversation and the change titles instead. This also covers a start title that failed or is still running when the goal call starts.

Unchanged: who may replace whose title, the prompt, the cost (still at most two calls per thread), and both fronts.

Tests: `TestAutoTitleEarlyInTheFirstRun` (core), `TestGoalTitleDoesNotKeepThePlaceholder` (engine).

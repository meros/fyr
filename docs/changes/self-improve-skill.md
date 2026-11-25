# V0.self-improve-skill: the repository carries a skill that improves Fyr from how it was used, from measuring to dispatching fixes and checking that they worked

→ docs/PROCESS.md (the change loop). It adds no behaviour to fyrd.

(1) **The problem.** On a busy day the owner could not tell whether the fog came from bugs in Fyr, from an overloaded host, or from threads that had grown too big. A manual analysis of the day's threads found causes in all three, but it was slow and partly wrong in its details. It also left the fixes in one large thread, which is the kind of thread it had just criticised.

(2) **The skill.** `.claude/skills/fyr-improve/SKILL.md` runs the loop in one Fyr thread:
- measure;
- read the threads behind the leads;
- group by root cause;
- confirm each cause in code and in the spec;
- file an issue and start one focused thread per cause, each with one PR;
- follow the PRs to merge and live;
- measure again.

It judges every finding by three goals in order: the owner's control is never reduced, the owner's clarity, and the throughput of the threads. A finding that would trade control for speed becomes a question to the owner, not a fix.

(3) **The signals.** `.claude/skills/fyr-improve/signals.py` reads the host's `fyr.db` read-only and prints a scorecard and the leads for a window:
- notices by template;
- notices that only woke an agent for a short turn;
- the owner asking a thread what goes on;
- unasked-question nudges;
- merge refusals and merge asks that were not shown;
- failed effects;
- holds and freezes;
- run failures;
- threads with many PRs or repos;
- titles to judge;
- PRs merged and their lead time;
- how long decisions waited.

Its output holds private data, so it stays in the session's scratchpad. Issues, PRs and these notes get anonymised evidence.

(4) **Not done here.** Fyr does not run the skill by itself. The owner starts it, or a run with the argument `daily` arms a Fyr watch for the next day.

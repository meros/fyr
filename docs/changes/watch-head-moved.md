# V0.watch-head-moved: an agent can watch for a push to its PR's head

→ SPEC 7.1, 7.2, V0.checks-follow-head. Closes #725.

Why: an agent waiting on an outside bot that pushes to its PR could only wait for checks to fail or for a quiet timer. The bot's push was heard late, from the timer, and the owner had to ask whether the agent was still working. `changeMoved` cannot stand in: it also moves on checks, reviews, conditions and evidence, and it is not the agent's to arm.

(1) **The kind.** `headMoved {change}` is anchored: arming stores the change's head (the PR's head as Fyr last read it, else the record's newest revision or workspace head). It holds when the head Fyr reads differs from the stored one, whoever pushed. Like every agent watch it fires once and is removed. Checks, reviews and other facts of the same head never make it true.

(2) **Refused.** Arming is refused when no head of the change is known yet (nothing to compare with, so the first read would look like a move) and when the change is no longer in flight.

(3) **No new reads.** It reads the head the PR poller already brings in; Fyr makes no extra GitHub call for it.

(4) **Shown.** The wait strip reads "waits for a push to <change>"; a watch approval reads "When <change> gets a new push". It runs in the background and never takes the owner's focus.

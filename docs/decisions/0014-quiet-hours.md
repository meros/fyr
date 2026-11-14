# 0014 Quiet hours per project

Date: 2026-11-14 · Status: accepted (owner, walk-through) · Spec: 0.5 V0.46

**Context.** The owner asked: "Add per project quiet hours! Hide threads by default and dont notify". Some projects (work, for example) should not reach the owner in the evening and at night, while the others still do. Their example window: "17-06", 17:00 to 06:00.

**Decision.** The owner chose in a walk-through:

| Question | Decision |
|---|---|
| What quiet means | The project's threads are hidden from the inbox (sections, counts, the "Needs you" count) and send no Web Push. |
| When the window ends | **Back silently.** No catch-up notification; the threads simply reappear as they are, with whatever state they reached. |
| Reaching a quiet project's threads during the window | **Only via the project filter.** There is no "Quiet" row or section in the inbox. Search still finds them. |
| The window | `from`–`to` in the host's local time zone, crossing midnight when `to` is not after `from` (17:00–06:00), on every day or on chosen days (the day a window starts on). |

**How it works.** The host decides: it computes `quietUntil` per thread and per project, so every client agrees and the push path needs no client. The client hides threads that carry a `quietUntil` in the future, and sets one timer to the nearest `quietUntil` so the threads reappear on time without polling. The host's tick sends the changed threads when a window starts or ends; that stream message does not count as an attention change, so it never pushes.

**How the words read in the UI.** "Quiet hours" on the project screen; "Threads hide and send no notifications from 17:00 to 06:00."; the filter chip says "Quiet until 06:00".

**Consequences.** A question asked during the window waits, unseen, until the morning or until the owner opens the project through the filter. The live view, decision cards and message delivery of an open thread are unchanged. Days are the start day of a window, so a weekday-only 17:00–06:00 still covers Friday night into Saturday morning, and Monday 00:00–06:00 is quiet only when Sunday is chosen.

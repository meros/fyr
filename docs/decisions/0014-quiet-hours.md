# 0014 Quiet hours per project

Date: 2026-11-14 · Status: accepted (owner, walk-through) · Spec: 0.5 V0.46

**Context.** Some projects (work, for example) should not reach the owner in the evening and at night, while the others still do. Their example window: "17-06", 17:00 to 06:00.

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

## Addendum: several windows (2026-11-14)

**Context.** The owner, looking at the project screen with its one row "17:00–06:00": "add option to have more quiet hours, like all weekend. Maybe multiple rows? Not sure... Or per day?"

**Decision.**

| Question | Decision |
|---|---|
| Shape | **Several rows.** `quiet` is a list of windows `[{from, to, days?}]`, at most 14. The project is quiet while any window holds. Empty or null is off. Not a per-day table: one row per idea ("weeknights", "the weekend") reads better and needs fewer taps. |
| Old data | The single object stored before this addendum reads as a list of one and is written back as a list. `project.update` also accepts the object. |
| Adding rows | "+ Add quiet hours" adds 17:00–06:00 every day. Two one-tap presets: **Weeknights 17:00–06:00** (starts Sun–Fri) and **All weekend** (Sat–Sun, all day). A preset's chip steps aside while the list holds that window. |
| All day | A toggle per row. It is stored as `from` equal to `to` (`00:00`–`00:00`), the meaning that already existed, so the host needs no new field; turning it off restores 17:00–06:00. |
| Times | 24-hour hour and minute pickers in every locale; the native time field shows AM/PM in some. |
| The line | All rows in plain words: "Threads hide and send no notifications weeknights 17:00–06:00 and all weekend." A lone every-day window keeps "from 17:00 to 06:00". While quiet: "Quiet until Mon 06:00." |
| Joining | `quietUntil` is the end of the whole quiet stretch: windows that overlap it or follow on without a gap join, whichever row they come from, at most a week ahead. |

**Why weeknights start Sunday to Friday.** Days are the start day of a window. With Monday to Friday starts and an all-day weekend, the quiet stops at Monday 00:00 and the project can notify between 00:00 and 06:00 on Monday. Starting the weeknights on Sunday too makes the two presets together run from Friday 17:00 to Monday 06:00, which is what "weeknights and all weekend" means. On its own the preset leaves only Saturday evening open, which the weekend preset covers. The owner can turn Sunday off on the row.

**Consequences.** Every client and the push path read the list through the same host computation; nothing else changed for the inbox, the filter or push. A client cached from before the addendum cannot show the list on the project screen until it reloads; hiding and push are unaffected, because the host decides.

## Addendum: one summary push at the end (2026-11-23)

"Back silently" is replaced: the owner kept the summary push. When a project's window ends, one push says what was held back and still needs the owner ("Acme" · "1 asking, 2 done"); nothing when all of it was seen or settled meanwhile. See V0.push-summary-batch.

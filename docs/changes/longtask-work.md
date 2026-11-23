# V0.longtask-work: the V0.38 main-thread guard counts work, not wall time

Cause of the flake: the layout test's V0.38 check budgeted long tasks (no task over 100 ms, under 250 ms in all) scaled by a calibration factor. A busy host deschedules the page's thread, so any task reads long however little work it does; at a load average near 28 one run in 7276 checks missed the budget, on plain main too (under 8 busy loops the same page showed single tasks of 200 ms with the same code).

The check now counts the work units the property is made of, which do not move with the load (V0.38 (5)):
- store flushes in the burst never exceed the animation frames drawn in the same window (stream messages are applied once per frame); an unbatched client gives 600 flushes in 66 frames and fails;
- markdown parses stay within the 300 messages that arrived.
The long tasks seen are printed as information only. `web/src/markdown.ts` `workCount` keeps the two counters on `globalThis.__fyrWork`. The section no longer needs the timed, calibrated slot.

Evidence: 10 runs of the section beside 8 busy loops, 0 failures; with the frame batching removed the check fails. The parse bound is loose (a memo removed raised parses from 33 to 82, under the bound of 330); it guards only against a parse per message per render.

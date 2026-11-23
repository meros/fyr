# V0.longtask-work: the V0.38 main-thread guard counts work, not wall time

Cause of the flake: the layout test's V0.38 check budgeted long tasks (no task over 100 ms, under 250 ms in all) scaled by a calibration factor. A busy host deschedules the page's thread, so any task reads long however little work it does; at a load average near 28 one run in 7276 checks missed the budget, on plain main too (under 8 busy loops the same page showed single tasks of 200 ms with the same code).

The check now counts the work units the property is made of, which do not move with the load (V0.38 (5)):
- store flushes in the burst never exceed the animation frames drawn in the same window (stream messages are applied once per frame); an unbatched client gives 600 flushes in 66 frames and fails;
- markdown parses stay within 1.25 times the flushes plus 2. With the memo, parses equal flushes in every run (a flush parses what it brought once); without it they double. The bound is derived from the flushes, so it does not depend on the burst size.
The long tasks seen are printed as information only. `web/src/markdown.ts` `workCount` keeps the two counters on `globalThis.__fyrWork`. The section no longer needs the timed, calibrated slot.

Evidence: 10 runs of the section beside 8 busy loops, 0 failures; with the frame batching removed it fails (600 flushes in 66 frames); with the markdown memo removed it fails 10 of 10 (for example 74 parses over 37 flushes, bound 49). The full layout e2e passes (7278 checks).

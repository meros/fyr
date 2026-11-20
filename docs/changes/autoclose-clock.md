# V0.autoclose-clock: the auto-close clock empties when the section closes, not halfway

→ V0.196, V0.48. 

(1) **The cause.** The clock in the header of an opened Snoozed or Settled section is a CSS animation whose duration was the time left, computed on every render. The list re-renders on each store update, and a new `animation-duration` on a running animation rescales the time already gone: one second after a re-render at 1 s the pie showed 2/9 gone, and at 5 s it was empty while the section still had 5 s to go. Clicking back into the window to see why restarted the section clicked in, so only the other one folded.

(2) **The fix.** `HideClock` (one component, desktop and phone) reads the time left once per deadline; a restart is a new deadline and a new clock. The close timer was right and is unchanged: headless runs with the window blurred, frozen, and a pointer resting over the list all folded both sections at 10 s.

*Test:* `web/test/layout.e2e.mjs` "V0.196 auto-close" desktop and phone: 3 s after opening, another section folds and opens (a re-render), and each clock's time left (its animation's end time less its current time) is within 0.8 s of the real deadline. Before the fix it showed 3.9 s left of 6.9 s.

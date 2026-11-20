# V0.finish-debounce: a thread that stops working waits a few seconds before the list shows it finished

→ V0.open-row-only, V0.207, V0.199. *Amends V0.open-row-only (1) ("no timer runs"): one timer runs, for this hold only.* 

The cause: the host shows a run's end at once, so a run that ends and starts again a second later (a queued message, a Fyr line that wakes the agent) jumps from Moving without you to Finished, unread and back.

(1) **The hold.** A thread whose section goes from Moving without you to Finished (unread or seen) stays in Moving, with the row it had there, for 5 seconds (`FINISH_HOLD_MS`). If it works again within them, it never moved. After them it goes to Finished as before.

(2) **Nothing else waits.** A move to Your move, Snoozed or Settled, and back to Moving, is at once, and so is every move that does not leave Moving for Finished. The counts follow the list. The thread view and push (which already waits, `notify.Delay`) read the live state.

(3) **Both fronts.** One implementation (`steadyThreads` in `web/src/still-list.ts`), applied to the threads before the sections are made, so the desktop list and the phone list hold alike.

# V0.day-chart: the status screen shows the day so far, as four numbers and a chart of the open changes

→ V0.status-home, V0.98. *Adds a block to V0.status-home (3), between Moving without you and Since you looked.* 

(1) **The numbers.** Today: changes **finished** (merged or the owner's Done, with how many merged), changes **opened**, changes **open now** (with the peak when it was higher), and **threads started**. A thread archived is left out, as on the board; a change shown in several threads counts once (`allChanges` in `board.ts`).

(2) **The chart.** The open changes from local midnight to now, as a step line over a light area: a change is open from its `createdAt` until its `done.at` (V0.98), so one opened before today and still open at midnight starts the line above zero. Each merge is a green dot on the axis at its time; the line ends in a dot at now. The axis says 0 and the peak, and the hour every 1, 2, 3 or 6 hours. One series, so no legend: the block's title names it.

(3) **Hover.** On the desktop a pointer over the chart shows a line and "14:20 · 3 open"; near a merge's dot it names the merge ("Merged #318 …"), and a click opens its thread. On the phone a touch does the same (no hover); a vertical swipe still scrolls.

(4) **Both fronts.** One component (`Today` in `home-ui.tsx`, the pure part `homeDay` in `home.ts`). The phone's chart is 96 px high instead of 120 and its numbers sit two by two (a 360 px screen); the axis shows at most four hours.

(5) **A quiet day.** With nothing open since midnight and no thread started, the numbers show (all 0) and the chart does not.

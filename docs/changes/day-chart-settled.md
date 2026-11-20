# V0.day-chart-settled: a change of a settled thread is not open on the day chart

→ V0.day-chart, V0.78. *Changes V0.day-chart (1) and (2).* (owner, 20 Nov, on the chart showing 20 open: "Never count settled threads as open...")

(1) **Open ends at the settle.** A change is open from its `createdAt` until its `done.at` or its thread's `settledAt`, whichever is first. A thread settled before midnight puts none of its changes on the line; one settled today steps the line down at the settle.

(2) **Not finished.** A change put away by a settle is not counted as finished: finished stays merged or the owner's Done.

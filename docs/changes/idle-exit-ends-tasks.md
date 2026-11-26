# V0.idle-exit-ends-tasks: the timeline says which background tasks ended with a lost agent process, also when no run was going on

→ V0.33, V0.133.

An idle agent process (its turn over, one background command running) was killed by a signal during a memory stall. Fyr saw the exit: the read loop ended, the runner's process was collected, and `process.lost` stopped the running task, so the thread was no longer Waiting on it. But `process.lost` wrote an event only when it ended a run. With no run going on, the task changed to stopped without a line in the timeline. Later, someone reading the event log saw nothing between the end of the turn and the owner's next message, and the task's own "ended" line came only from the next process. That read as "Fyr never noticed the exit" (#724), and the timeline did not show that the work had stopped.

(1) **Each task `process.lost` stops gets a line.** For each task it stops, the reducer appends a `task` event (`kind: ended`, `status: stopped`, summary "ended with the agent process"), during a run or not, before the run's `run.ended` when it ends one. The rest is unchanged: the tasks read stopped, a failure is recorded only when a run or a decision died with the process, and Fyr starts nothing by itself. The next process's CLI tells the agent its tasks did not finish.

(2) **Test.** `TestIdleProcessKilledEndsItsBackgroundTasks` ends a turn on a background command, SIGKILLs the idle CLI (started directly and through the runner), and checks that the task reads stopped, that the thread is not Waiting, and that the timeline has the line.

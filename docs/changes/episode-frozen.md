# V0.episode-frozen: frozen undeclared work keeps its episode open until the guard thaws it or its processes are gone

→ V0.164 (the episode's end), decision 0032 slice 1 (at most 8 freezes per episode), V0.oomd-guard (4), V0.guard-swap.

(1) **The cause.** The detector ended an episode after the thread had been calm (cores and working set under half their thresholds) for the window W (45 s). Frozen work uses no CPU, and after a freeze `ReclaimFrozen` (V0.guard-swap) pushes its anonymous memory to swap, so the working set (`memory.current` less `inactive_file`) falls to the idle level: a frozen thread is calm. W after the freeze the episode ended, `OnEnd` → `EndUndeclared` thawed the work, and the guard logged the thaw as "episode or hold ended" whatever the stall. The work swapped back in, was flagged again with a new `Since`, so the guard's count for the episode started at 1 again: the cap of 8 and the rule of no timed thaw into a critical stall never applied. The loop was freeze, swap out, thaw, swap in, freeze, about every 2 min, with a pause and a resume notice to the agent each time. `MarkPaused` only marked the episode's record.

(2) **Frozen calm is not calm.** `MarkPaused` (called where the guard freezes the work or makes its parts take turns) now also marks the episode frozen. A frozen episode is not ended by calm.

(3) **What ends it.** The guard's thaw (`UndeclaredThaw`) now calls the detector's new `MarkThawed`: the episode is no longer frozen and its calm counts again from the thaw, so it ends after W of calm once the work runs. A frozen episode whose work holds no process any more (`UndeclaredEmpty`, read by the sampler) ends as before, on W of calm. A thread whose CLI ended, or that is admitted, ends its episode as before.

Tests: `internal/cgroup/undeclared_test.go` (TestEpisodeFrozenCalmDoesNotEnd, TestEpisodeFrozenEndsAWindowAfterTheThaw, TestEpisodeFrozenEndsWhenTheWorkIsGone).

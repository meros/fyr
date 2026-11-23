# V0.handover-filed: a thread handed over from another host is filed by its reduce, and the load pass files through the restart reduce

→ V0.review-correctness (2), V0.trans-host-handover, V0.55.

(1) **The handover sends its project through the Input.** V0.review-correctness made `core.Filed`, applied by `core.ReduceIn`, the one filing rule, but the trans-host handover (`PrepareHandIn`) still set the new thread's project by hand before its `thread.handedIn` Input. Now `core.Adopted` carries `Project`, the project the sender chose (empty for none); the handed-in reduce files the thread in it by hand, and `ReduceIn` files it with `core.Filed` like every other thread: in the chosen project while it exists, by its folder when none was chosen. A project on the received thread itself is not read.

(2) **The load pass no longer writes threads past the reduce.** `Load` filed every stored thread with `core.Filed` and wrote it directly. The `host.restarted` Input that `Load` applies to every thread right after already runs `ReduceIn` with the host's projects, so it files each thread and stores the change in its own transaction, checked by the thread invariants. The direct write is gone.

The other writes of a thread's project are inside the reducer: the owner's filing commands (`internal/core/actions.go`) and `project.matched` (`internal/core/project.go`).

Tests: `internal/engine/transhost_test.go` (TestHandInFiledByTheReduce), `internal/core/project_test.go` (TestHandedInFiledFromTheInput); `internal/engine/projects_test.go` (TestMatchAtLoad) covers the load pass.

# V0.engine-split-engine-go: engine.go is split by concern, no behaviour change

`internal/engine/engine.go` held the Engine struct, the command path, the apply pipeline, the awaited effects, the DTO builders and the quota-reset block in one 1500-line file. Whole declarations move, in the same package, nothing is renamed or newly exported:

- `quotaresets.go`: resolveQuotaReset, SyncQuotaResets, the saved quota-reset state and publishQuotaResets.
- `awaited.go`: the `awaited` method.
- `dto.go`: dto, seen, build, threadRootPrompt, Snapshot.
- `submit.go`: Command, CommandAs, submit, await, rejected, replay.
- `apply.go`: Apply and the reduce, stage, persist, commit, publish path.

# V0.usage-jobs: Fyr's own model calls and agy's turns reach the usage ledger

→ V0.usage-ledger, decision 0041 (phase 2), SPEC 3.18. A system job (title, search, question, approval judge) ran `claude -p --output-format text`, which prints no usage, and agy's per-turn usage reached no reader, so neither was counted.

(1) **Print hands over its result.** `cli.Launch.Result`, when set, hears a print run's result message, in the shape the adapter's `UsageReader` reads. claude's print run now writes `--output-format json`, which is the same result message a stream ends a turn with (proved against claude 2.1.3xx: `uuid`, `session_id`, `usage`, `modelUsage`); Print returns its `result`, an errored one as an error, and output that is not a result message as it is. agy's print run hands over the result its adapter writes.

(2) **System jobs** (`agent.printOn`) record the call with source `job:<job>`, the provider and model the job ran on, and the thread it served: a title and a question under their thread, the approval judge under the thread it judged for, a search under none. A print run is one turn of a session that is never resumed, so it keeps no baseline. `SystemPrintFor` is `SystemPrint` for a job that serves a thread.

(3) **A project's setup run** is a thread: its turns are counted with source `job:setup` (`Engine.ThreadSource`, by the project's `setup.thread`). A handover's brief is a turn of the thread with trigger `handover`, counted since V0.usage-ledger.

(4) **agy.** Proved against recorded protocol logs: the answer to `session/prompt` carries `usage {inputTokens, outputTokens, cachedReadTokens, cachedWriteTokens, thoughtTokens, totalTokens}` for that turn only (a later turn can be smaller than an earlier one), and `totalTokens` is input + cached read + output, so thinking is part of output, as with claude. `agycli.Adapter.Usage` reads it as a `PerTurn` report. It names no model: the meter gives its tokens to the thread's model (`unknown` when the thread has none), and counts them all as the main part. agy says no cache lifetime: its cache writes count as the 5-minute kind.

Tests: `internal/cli/claudecli/usage_test.go` (TestPrintReadsTheJSONResult), `internal/cli/claudecli/claudecli_test.go` (TestPrintArgs), `internal/cli/agycli/usage_test.go`, `internal/agent/usage_jobs_test.go` (TestSystemJobUsageReachesTheLedger, TestSetupThreadIsAJob), `internal/usage/usage_test.go` (TestRecordUnnamedModel), `internal/engine/usage_test.go` (TestThreadSourceOfASetupRun).

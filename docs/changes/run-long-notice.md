# V0.run-long-notice: a Run card that runs long tells the agent once when it starts and again when it ends; a short one tells it once

→ V0.149 (6). *Amends V0.149 (6): a pressed run request can give the agent two notices, not one.* (owner, 20 Nov: "Run cards - when pressing run on a long running job the agent should be notified, then notify again when it's done. If it's a very short job then only one notification".)

(1) **The start notice.** When the owner presses Run (or Run as root) on a run request, fyrd starts a timer of `LongRunAfter` (10 s). If the script still runs when it fires, fyrd applies the observed input `action.long {on, action}`; its reduce logs the event `action.long` and the agent gets a wake notice: "The owner pressed Run on your run request "<why>"; it has run for over 10 s and still runs. Fyr sends you the exit code and the output when it ends; end your turn or do other work meanwhile." So an agent that ended its turn knows the job is under way, and one that waits on it does not ask again.

(2) **A short run says nothing more.** The timer stops when the script ends, before its result is reduced, and `action.long` on a run that is no longer running logs nothing. A run that ends within 10 s gives the agent only its result, as before.

(3) **One line while queued.** The start notice has the result's coalesce key (`result:thread:<action>`): when the agent has not taken it yet (busy in a turn) and the run ends, the result replaces it, so the agent reads one line, the result.

(4) Only run requests (V0.149) get the start notice; other script actions of a change, the thread or the view keep one notice, their result. The conversation log does not show `action.long`: the card already shows the run in progress. `run_request`'s description says it.

Tests: `internal/core/runrequest_test.go` (TestRunRequestLongNotice), `internal/engine/runrequest_test.go` (TestRunRequestLongRunTellsStart).

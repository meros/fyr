# V0.arch-cleanup: slots.go and auth.go are split by concern, JSON request and response handling is one helper, and the runner waits on signals

No behaviour change except where named.

(1) **Files only.** `internal/engine/slots.go` is split into `slots.go` (the queue and its sweeps), `freeze.go` (the memory guard), `heavygate.go` (the gate hook) and `slots_fmt.go` (wording). `internal/front/auth.go` is split into `authmw.go`, `authapi.go`, `ownerroutes.go` and `pushsubs.go`.

(2) **One JSON helper.** `internal/httpguard` has `WriteJSON` (logs an encode failure), `ReadJSON` (bounded body, returns the error for a caller that picks its own answer), `DecodeLoose` (all fields optional: an empty body is fine, any other failure is logged instead of dropped), `DecodeJSON` (answers 400, or 413 over the cap) and `NewClient` (a client with a 30 s timeout). The request decodes in `front`, `server` and `peerwire` and the owner-socket answers in `peerwire` use them; each site keeps its own cap. `provider/check.go` no longer falls back to a client without a timeout.

(3) **The runner waits on signals.** `Server.Close` waits on a per-process `done` channel and one grace timer instead of polling every 20 ms; `Client.ListWait` retries on a ticker and `ListWaitContext` stops with its context. `Server.allowed` keeps its short retry, because the launcher's pid note has no signal to wait on.

(4) `Urgency.rank` is a switch, not a map built per call.

Tests: `internal/httpguard/json_test.go`, `internal/runner/runner_test.go` (TestListWaitContextCancel; the runner tests ran with `-count=50` under load).

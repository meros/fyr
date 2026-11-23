# V0.review-structure: one local child, one ack and write core, acp.go split by concern, shared adapter helpers

→ V0.90, V0.review-hangs, V0.review-robust. No behaviour change.

Why: the same review found the claude and agy adapters carrying byte copies of the same code, a 1757-line `acp.go`, and the cross-host handover asking "is this claude?" where it meant "can this adapter move a session?".

(1) **One way to start a CLI.** `runner.Start(rc, spawn, wrap)` wraps the command line (V0.agents-unit) and starts it in the runner (V0.90) or, without one, as fyrd's own child (`runner.StartLocal`, the former `child.go` of both `internal/claude` and `agycli`, which used the runner's own `tail`).

(2) **One ack and write core.** `runner.Link` writes and logs JSON lines and keeps the runner's ack state (`Handled`, `Note`, `Ack`, `Opened`, `Answered`); `claude.Client` and agy's session embed it. `runner.SeqKey` is the sequence key (`claude.SeqKey` names it).

(3) **acp.go by concern.** `acp.go` (types, start), `acp_rpc.go` (JSON-RPC and the read loop), `acp_init.go` (handshake, model and effort), `acp_turn.go` (the queue, the worker, the end of a turn), `acp_tools.go` (tool calls and background tools), `acp_perm.go` (permission requests). The three ways a turn ends (its prompt answered, an unsolicited turn settled, a pre-restart turn answered on a reattached pipe) build a `turnEnd` and call one `closeTurn`, with the cancelled check in `turnEnd.cancelled`.

(4) **SessionArchiver.** `cli.SessionArchiver` (`ExportSession`, `ImportSession`) is an optional adapter interface like `Printer`; the claude adapter implements it and the handover asks for it instead of comparing the CLI id. `cli.ErrSessionTooLarge` is the one error. A CLI without it gets "Session transfer is not supported for <cli> sessions".

(5) **Helpers.** `fsutil.CopyTree` (was `copyTree` in both `sessions.go`), `cli.Endpoint.Env` (the sorted model variables both `Env`s appended), `cli.ErrText` (the 300-rune error tail), `cli.LastErrorLine` (the login line worth showing), and `cli.LoginEnv`, which drops the adapter's `CredentialVars` and the browser variables with `launchenv.Without`.

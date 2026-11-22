# V0.json-helpers: one JSON reply, one answer to a bad body

Follows V0.arch-cleanup. `api.WriteJSON` was already the one JSON reply of the host and the front; the copy added to `httpguard` is gone and `peerwire` uses `api.WriteJSON`. `api.WriteJSON` now logs an encode failure instead of dropping it. `api.DecodeBody(w, r, &v, limit)` is the one answer to an unreadable body: `{"ok":false,"error":"bad JSON: ..."}` with 400, or 413 over the cap. Six handlers in `front` and `server` that each wrote their own text (`bad JSON`, `bad JSON: <err>`) use it, so their bodies agree. `httpguard` keeps `ReadJSON` and `DecodeLoose` (all fields optional), and `NewClient`.

Kept as they are, on purpose: the handlers that answer a bad or empty body through their own field check (`device is required`, `endpoint is required`), and the proxy paths that pass a host's body through unchanged (`front/settings.go`, `server/deadline.go`); `mcp` keeps its own JSON-RPC error answer.

Tests: `internal/api/decodebody_test.go`.

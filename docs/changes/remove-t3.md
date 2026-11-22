# V0.remove-t3: Remove T3 import and adoption

→ 0.4, 3.1, 5.1, 5.10, V0.9, decision 0004.

Fyr has run standalone since cut-over, so the T3 Code bridge is dead weight across every layer.

1. **Server, engine and CLI.** Removed `internal/t3`, `internal/engine/t3.go`, `GET /api/t3/candidates` (now 404), the `t3.adopt` host command and effect, `thread.adopted`, `observer:t3`, `fyrd import-t3`, the `-t3` and `-list` flags, the T3 settings profile fallback, and the `~/.t3/scratch` default scratch root.
2. **Client.** Removed the Adopt from T3 dialog, the desktop inbox button, the phone More sheet item, `T3Candidate`, `t3Candidates`, and the mock adopt handler.
3. **Data shape.** Removed `source` and `t3Id` from `core.Thread` and `ThreadDTO`. Adopted threads keep their history and Claude session; a stored thread JSON carrying `"t3Id"` and `"source": "t3"` loads without error and saves without them.
4. **Docs and Nix.** Closed decision 0004, marked V0.9 superseded, dropped the 0.4 cut-over row and T3 entries from `docs/SPEC.md` and `docs/API-v0.md`, and updated `nix/module.nix`.

# V0.run-card-rerun: a succeeded Run card requires confirmation before running again, and action events record the acting device

→ V0.149, V0.68, V0.60, decision 0030 (3.3). A finished Run card kept its Run again button in the first slot and fired `action.run` on a single click or tap, so a stray press after a layout shift could re-run an already succeeded command. Meanwhile, `action` events recorded `"by": "owner"` without the acting device or front, unlike `command` events.

(1) **Succeeded Run card (`web/src/runcard.ts`, `web/src/runcard-ui.tsx`).** Once a run request has succeeded, `RunCard` moves `Run again` out of the first button slot (`Copy`, `Close`, quiet `Run again`) and routes its press through Fyr's confirmation dialog (desktop) / bottom sheet (phone, V0.68), showing the command and that it already succeeded. Fresh and failed cards keep one-press Run / Run again. Desktop and phone share `RunCard` and `ConfirmView`.

(2) **Action event actor (`internal/core/kinds.go`, `internal/core/runrequest.go`).** `action.run` and `action.dismiss` record `actor` (e.g. `device:<id>`) and `front` on their `action` events whenever the input carries an actor, matching `command` events.

(3) **Tests.** `internal/core/runrequest_test.go`, `internal/engine/runrequest_test.go`, `web/test/runcard.test.ts`, and `web/test/layout.e2e.mjs` (desktop and phone).

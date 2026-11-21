# V0.agy-open-tool-settle: an `agy` reactive turn does not settle while one of its tools is still open

→ amends V0.agy-reactive-wakeup (1), V0.agy-reattach-queue.

The settle window added in V0.agy-reactive-wakeup ends a turn that `agy` woke on its own (a background command finished, a quota notice arrived) when 1.5 s pass after a `usage_update` with buffered text. It ignored the turn's tools. The live sessions do not emit that text at the end of a step: they emit it as the note before they wait on a command, and the `usage_update` follows while the command is still open — `"Running the full suite now."`, `usage_update`, then nothing for five minutes.

Replayed against today's streams, the shipped logic fired a synthetic result while a command was open in five threads; the worst was at 10:18:54, where a screenshot command was announced, the model wrote "Capturing screenshots", its `usage_update` came in, and 1.5 s later the turn was declared finished while the command ran on for another hour. The command was then promoted to background work and the thread sat on a finished label.

1. **No settle over an open tool (`internal/cli/agycli`).** Both arming paths — the `usage_update` one, and the fallback that covers a turn ending without it (a quota notice) — skip a turn that has a tool which has not completed. `openToolLocked` reads the turn's own `emittedTools`/`finishedTools`, the maps the adapter already keeps for background promotion.
2. **The tool's end is the settle point (`internal/cli/agycli`).** When the last open tool of an unsolicited turn completes, the settle is armed there instead. The turn may then end normally (the model's `usage_update` follows), or its text with no `usage_update` at all is taken by the fallback — both were losing the window when the arm sat on the earlier event.

A turn that is *prompted* keeps its own rule: it ends on its JSON-RPC response, and the settle never fired for one (the prompt worker is busy in `requestOn`, and `finishUnsolicitedTurn` refuses a `turnRunning` session). This is why the earlier "stuck in Running" reports could not be reproduced on the current build: those threads ran a build from before V0.agy-reactive-wakeup existed, and an adopted process of that build has no settle at all — the next process gets one.

The test drives the exact order the live sessions use (tool opens, note, `usage_update`) and fails if the turn settles before the command ends, and passes only when the tool's completion is what arms the window.

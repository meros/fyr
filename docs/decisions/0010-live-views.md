# 0010 Live views: a page plus a patched state, kept current by a Stop hook

Date: 2026-11-14 · Status: accepted (owner, 2026-11-15, shipped in V0.37, V0.52, V0.62) · Spec: 0.5 V0.37

**Context.** The owner reads a thread to find out where its work stands: which steps are done, what blocks, what comes next. The agent knows this and could show it. The research handover asked for a per-thread view the agent keeps current, pinned above the conversation, so the thread does not have to be read and interpreted. `research/stop-hook-spike.md` showed that a Stop hook registered through `initialize` makes the CLI run one more turn when the host blocks, on new and resumed sessions, so Fyr can ask for the update at the end of a run instead of hoping the agent remembers.

**Options.**

1. The agent republishes a whole HTML page with `show` whenever something changes. Every update costs the page's tokens and lands as a new card in the log; nothing is pinned.
2. Structured blocks (a schema of steps, blockers, links) rendered by the client. Calm and cheap, but the schema decides what a view can be before anyone has used one.
3. A page the agent writes rarely, plus a small JSON state it patches often. The page reads the state; the host sends the state on load and on every change.

**Decision.** Option 3, with these parts.

| Part | Decision |
|---|---|
| Page | Self-contained HTML (or a fragment wrapped in the Fyr theme), at most 512 kB, stored once by SHA-256 in the attachment store and served like an HTML preview (`sandbox allow-scripts`, no origin, `connect-src 'none'`). The host puts a small bridge script first in `<head>`. |
| State | A JSON object, at most 16 kB, changed with JSON Merge Patch (RFC 7396: keys merge, `null` deletes, arrays are replaced whole). `headline` (a string) is the one-line status and also the inbox row's line. The rest is free-form. |
| Tools | `view_publish {html \| path, state}`, `view_state {patch}`, `view_unchanged {reason}`. Every thread lists them (V0.107: the CLI reads the tool list once per process, so a view turned on during a run must already be listed); a call fails while the view is off. |
| Events | `view.published {pageId, state}` and `view.state {state}` carry the full resulting state, so folding the log is "take the last one". `view.unchanged {reason}` records the check. |
| Messages | MCP Apps names over `postMessage`: `ui/notifications/state` (host to page), `ui/message` (a press, page to host), `ui/notifications/size-changed` and `ui/notifications/initialized` (page to host). The host accepts a message only when `event.source` is the view's own iframe. |
| Presses | A press becomes `thread.send {text, from: "view"}`. The agent reads it as `[From the live view] <text>`; the log shows it as a compact chip. A press never runs an Action; privileged steps still go through `effect_request`. |
| Stop hook | Registered next to the PreToolUse gate on every `initialize`. It blocks once per run, counted by fyrd, when the thread has the flag, the process started with the view tools, no `view_*` call happened since the run started, `stop_hook_active` is false and no background task runs. The reason says what to update and never dictates a reply. |
| Flag | A thread field `liveView` (not "mode": plan, ask and auto use that word), set by `thread.create {view: true}` and `thread.setView {on}`. The prompt block and the tool list are fixed when the process starts, so `setView` ends an idle process at once and a running one after its run. |
| DTO | `liveView: bool` and `live: {pageId, state, updatedAt, checkedAt, headline}`. The handover named the field `view`, but the thread DTO already uses `view` for the owner's per-thread watermark and snooze (`core.View`), so the view is `live`. |

**Caveats from the spike, handled.** The forced turn's text would replace the run's summary: when the forced turn calls only `view_*` tools, the run ends with the summary it had before the block. The CLI echoes the block reason as a user message with `isSynthetic: true`: it is dropped and never logged. The `stop-hook-error` system notification after a block is ignored. The `view_*` calls are hidden in the log like other tools with their own card (V0.21).

**Deferred.** Structured blocks, live GitHub bindings in the page, schema validation of the state, multi-file pages and page templates.

**Consequences.** One forced turn per run at most, so the cost is bounded by the spike's numbers (about $0.001 to $0.003 a forced turn on haiku in a small session, more with a large prompt cache write). A thread without the flag sees no change except one extra hook round trip at the end of each turn. The client's mock serves the host's bridge verbatim (a Go test keeps the copies equal), so the layout test exercises the real bridge. What a good view looks like is open: the prompt block suggests `headline`, `steps`, `blockers` and `next`, and real use should decide whether a shared shape (option 2) is worth adding later.

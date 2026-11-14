# Stop hook spike (live views)

Date: 2026-11-14. CLI: Claude Code 2.1.322, model haiku (`claude-haiku-5-5`).
Driver: a copy of `internal/claude/client.go` plus a 120-line `main.go` in
`/tmp/claude-1000/viewspike/` (throwaway). Five real runs, about $0.05 in total.

## In short

A Stop hook registered through the stream-json `initialize` request works, on a
new session and on a `--resume`d one. When the host answers the callback with
`{"decision":"block","reason":"..."}`, the agent runs one more turn with the
reason as its input, then the hook fires again with `stop_hook_active: true`.
Fyr can register it next to its existing PreToolUse gate; no `--settings`
command hook is needed, so the fallback was not tested.

## Answers

| # | Question | Answer |
|---|----------|--------|
| 1 | `initialize` accepts `{"Stop":[{"hookCallbackIds":["view"]}]}` and the CLI calls back at end of turn | Yes |
| 2 | A block reply makes the agent continue one more turn | Yes |
| 3 | Same on a session resumed with `--resume <id>` | Yes |
| 4 | `stop_hook_active` in the hook input, true on the second stop | Yes, `false` on the first stop and `true` on every stop after a block (checked over 3 consecutive blocks) |
| 5 | Cost of one forced extra turn on haiku | About $0.001 to $0.003 per extra turn in a trivial session; see Costs |

## What worked, exact JSON

Initialize (same shape as the PreToolUse gate in `internal/agent/agent.go`,
no `matcher` because Stop has no tool):

```json
{"type":"control_request","request_id":"fyr_1","request":{"subtype":"initialize","hooks":{"Stop":[{"hookCallbackIds":["view"]}]}}}
```

The callback the CLI sends after the assistant's final message (trimmed):

```json
{"type":"control_request","request_id":"5c436248-...","request":{"subtype":"hook_callback","callback_id":"view","input":{
  "session_id":"3d2e2ac7-...","transcript_path":"/home/meros/.claude/projects/-tmp-claude-1000-viewspike-work/3d2e2ac7-....jsonl",
  "cwd":"/tmp/claude-1000/viewspike/work","prompt_id":"fa01a578-...","permission_mode":"default","effort":{"level":"medium"},
  "hook_event_name":"Stop","stop_hook_active":false,
  "last_assistant_message":"Hi. I'm running in `/tmp/claude-1000/viewspike/work`, ...",
  "background_tasks":[],"session_crons":[],"scratchpad_dir":"..."}}}
```

Block reply. It is the Stop hook's JSON output placed as the `response` payload,
which is what `Client.handle` already does with whatever `OnHook` returns:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"5c436248-...","response":{"decision":"block","reason":"Update the view before you stop. Reply with exactly: VIEW UPDATED"}}}
```

Allow reply (the existing "no opinion" default):

```json
{"type":"control_response","response":{"subtype":"success","request_id":"6a47e190-...","response":{}}}
```

## What the stream shows after a block

In order, after the block reply:

1. A synthetic user message carrying the reason:
   ```json
   {"type":"user","isSynthetic":true,"message":{"role":"user","content":[{"type":"text","text":"Stop hook feedback:\nUpdate the view before you stop. Reply with exactly: VIEW UPDATED"}]},"parent_tool_use_id":null,"session_id":"3d2e2ac7-..."}
   ```
2. A system notification, labelled as an error although nothing failed:
   ```json
   {"type":"system","subtype":"notification","key":"stop-hook-error","priority":"immediate","text":"Stop hook error occurred · ctrl+o to see"}
   ```
3. A new assistant turn (thinking, then text `VIEW UPDATED`).
4. A second `hook_callback` for `view` with `"stop_hook_active":true` and
   `last_assistant_message` set to the new text.
5. After the allow reply, one `result` for the whole run: `num_turns: 2`,
   `stop_reason: "end_turn"`, `result: "VIEW UPDATED"`.

Only one `result` arrives per user message, however many times the host blocks,
so Fyr's run stays live through the forced turns. The CLI did not cap blocking:
3 blocks in a row gave 4 callbacks and `num_turns: 5`. The host must stop
blocking on its own, for example when `stop_hook_active` is true.

On the resumed session (`--resume 3d2e2ac7-...`, prompt "Say bye.") the same
initialize registration fired on the first stop with `stop_hook_active:false`,
the block produced the same synthetic message and extra turn, and the second
callback had `stop_hook_active:true`. Registration is per process, not stored
in the session, so Fyr must send it on every `initialize`, which it already does.

## Costs (`total_cost_usd`, haiku)

| Run | Turns | Cost | Cache read | Cache write |
|-----|-------|------|-----------|-------------|
| New session, no block (baseline) | 1 | $0.0040 | 33 066 | 17 644 |
| New session, 1 block | 2 | $0.0109 | 50 649 | 50 959 |
| Resumed session, 1 block | 2 | $0.0121 | 153 437 | 51 922 |
| New session, 3 blocks | 4 | $0.0108 | 223 781 | 40 325 |

The cost of a run is dominated by the cache write of the ~50k-token system
prompt (this machine's user settings, skills and plugins), which varies with
the cache state between runs. The marginal cost of one forced turn is one cache
read of the context (~50k tokens) plus a few hundred output tokens: the 3-block
run cost the same as the 1-block run because it wrote less cache. In a real Fyr
thread the extra turn costs one cache read of that thread's whole context plus
whatever the view update itself produces (tool calls, output), on the thread's
own model. On a long Opus thread that is not negligible, so block only when the
view is actually stale.

## Caveats for Fyr

- **The forced turn replaces the run's final answer.** `result.result` and
  `last_assistant_message` become the forced turn's text. Fyr's thread summary
  (digest of the last assistant message) would show "VIEW UPDATED" instead of
  the real answer. Either keep the summary from before the block, or word the
  reason so the agent ends with its real answer again.
- **The synthetic user message reaches Fyr's `case "user"`** and would be
  rendered as a user turn. Filter or label messages with `isSynthetic: true`.
- **The `stop-hook-error` notification** is a TUI string; Fyr should ignore
  that key rather than show an error.
- **The model may refuse a scripted reply.** In the 3-block run haiku would not
  write "VIEW UPDATED" because it had no view to update, and said so each time.
  The reason text should say what to update and how (e.g. call `mcp__fyr__show`),
  not dictate the reply.
- User-level settings hooks (4 SessionStart hooks here) still run alongside the
  initialize hooks, because Fyr does not pass `--setting-sources`.

## Not tested

- The `--settings '{"hooks":{"Stop":[...command...]}}'` fallback, because the
  initialize route worked on both new and resumed sessions.
- Stop during a run with tool calls, subagents, or an interrupt: whether an
  `interrupt` while the forced turn runs ends cleanly, and whether `SubagentStop`
  needs the same treatment.
- Other models than haiku.

## Recommendation

Register the view hook in the same `Initialize` call as the gate:

```go
c.Initialize(ctx, claude.Msg{
    "PreToolUse": []claude.Msg{{"matcher": "Bash", "hookCallbackIds": []string{"gate"}}},
    "Stop":       []claude.Msg{{"hookCallbackIds": []string{"view"}}},
})
```

and branch on the callback id in `OnHook` (today it ignores the id). For `view`:
return `{"decision":"block","reason":...}` only when the thread has a live view
that the turn made stale and `stop_hook_active` is false; otherwise return nil.
That caps the cost at one extra turn per user message.

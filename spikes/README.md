# Spikes (2026-11-13, NixOS, 32 GB RAM, 8 cores)

## Protocol: driving the `claude` CLI from Go (`protocol/`)

`claude` CLI 2.1.325, Go client in `protocol/go/` (469 non-blank LOC; `client.go` is the control client, ~200 LOC). Run with `nix shell nixpkgs#go --command go run .` from `protocol/go/`. Cost of the whole spike: about $0.06.

Spawn flags (from the official TypeScript Agent SDK 0.3.284): `--output-format stream-json --verbose --input-format stream-json --permission-prompt-tool stdio --model <m> --permission-mode default`. Envelopes: `{"type":"control_request","request_id","request":{subtype,…}}` and `{"type":"control_response","response":{"subtype":"success"|"error","request_id","response"}}`.

| Assumption | Result |
|---|---|
| Multi-turn session over stdin stream-json | proven (2 turns, one process) |
| `initialize` registers a PreToolUse hook; `hook_callback` before Bash; deny and allow respected | proven; a deny reaches the model as "PreToolUse:Bash hook error: <reason>"; a hook allow skips `can_use_tool` |
| `can_use_tool` for AskUserQuestion answered with `updatedInput.answers` | proven; answers keyed by question text |
| `interrupt`, `set_model` live | proven (interrupt ack 4 ms) |
| SIGKILL + `--resume <id>` keeps context | proven; 1.18 s end to end; `set_model` does not survive resume |
| RAM per session | ~174 MB at spawn, 210–250 MB idle, +3–5 MB per turn; `get_usage` jumps it to ~410 MB |
| Protocol drift over 7 SDK versions (May–Nov 2026) | needed subtypes never removed or renamed; only optional fields added; CLI also sends types outside the SDK union (`keep_alive`, `transcript_mirror`, …) → ignore unknown types |

Not tested live: `stop_task`, `mcp_set_servers`, `rewind_files`. The analysis scripts in `protocol/analysis/` diff SDK tarballs (download them yourself into empty folders; never run code from them).

## Resources: PSI, cgroups, RAM (`resources/`)

| Assumption | Result |
|---|---|
| PSI readable (`/proc/pressure/*`) | proven |
| `systemd-run --user --scope` with memory/CPU limits | works only with `XDG_RUNTIME_DIR` set; controllers delegated |
| `MemoryHigh` kills a runaway | **no**: it throttles into a near-livelock; with zram swap `MemoryMax` never fires → the governor must stop stalled scopes itself |
| OOM kill at `MemoryMax` with `MemorySwapMax=0` | not proven (sandbox blocked); run `XDG_RUNTIME_DIR=/run/user/1000 timeout 60 systemd-run --user --scope -p MemoryMax=300M -p MemorySwapMax=0 -- python3 hog.py 25 16` and expect `oom_kill 1` |
| Measuring memory | RSS understated the working set ~4× (zram); use cgroup `memory.current + memory.swap.current` |
| One `claude -p` process | ~311 MB max RSS, ~3–4 s local start-up overhead |

Suggested governor defaults and PSI thresholds are in `docs/SPEC.md` section 8.

## MCP binding and steering (issue #13 step 1, 2026-11-14, claude 2.1.325, haiku)

`spikes/mcp/main.go` runs Fyr's MCP server (`internal/mcp`) over HTTP with a bearer token and the real CLI.

| Check | Result |
|---|---|
| S1 `--mcp-config` with `type: http` and an `Authorization` header connects | yes: `system/init` lists `fyr` as `connected`, source `dynamic`; the user's own MCP servers still load |
| S2 `--allowedTools mcp__fyr__*` skips the permission request | yes: 0 `can_use_tool` requests (mode `auto`) |
| Natural use: "Merge <PR> with a squash merge", with the Fyr prompt block | the model loaded the deferred tool with ToolSearch and called `mcp__fyr__effect_request {action: pr.merge, args: {pr, method: squash}}`; it did not try Bash |
| Told to run `gh pr merge`, with the Fyr prompt block | the model refused and offered the Fyr tool; the hook never fired |
| S3 the same, without the prompt block | the model ran Bash; the PreToolUse hook denied it; the deny reason reached the model word for word |

Consequence: the prompt block and the tool description do the steering; the hook is the backstop. Fyr tools are deferred in the CLI, so the first use costs one ToolSearch call.

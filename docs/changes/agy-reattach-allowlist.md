# V0.agy-reattach-allowlist: an adopted `agy` process keeps the MCP allowlist it started with

→ amends V0.agy-adapter, V0.agy-reattach-queue (1), V0.90, V0.129.

A session's allowlist is decided when Fyr launches the CLI: with Fyr's MCP server in play, `Launch.AllowedTools` is `mcp__fyr__*`, and the `agy` adapter answers every `session/request_permission` for one of those calls itself (`handleServerRequest`, step 3). That list is Fyr's state, not part of the ACP handshake, and the runner's note (`procMeta`) did not carry it either. So when `fyrd` restarted and adopted the kept process, `ReattachSession` built the session with no allowlist at all, and from that moment every `mcp__fyr__*` call the process made became an approval card on the thread.

That is what the owner met today: in one thread, eleven `mcp__fyr__*` permission requests before a restart were answered in under 20 ms (the adapter's auto-allow) and every one after it waited on the owner — 3 s to 119 s each, one 8 minutes — for calls like `mcp__fyr__fyr_state` and `mcp__fyr__change_list`. The same restart also dropped `MCPServers`, so the translator fell back to guessing an eager `mcp_<server>_<tool>` name with a `Cut` on the first underscore, and dropped the timer policy.

1. **The note remembers it (`internal/agent`).** `procMeta` gains `MCP bool`, written at launch as `l.MCPConfigFile != ""` — the field that both puts Fyr's server in the process and its allowlist on `Launch`. An older fyrd's note simply reads false, so an old process is adopted as before.
2. **The state carries it back (`internal/cli`, `internal/agent`, `internal/cli/agycli`).** `cli.ReattachState` gains `AllowedTools`, `DisallowedTools` and `MCPServers`. `attach` fills them from the note (the allowlist, Fyr's server name, and `TimerTools` unless the process was started with the CLI's own timers), and `Adapter.ReattachSession` hands them to `acpSpec`, where the auto-allow and the eager-name translation already read them. A process started without Fyr's server gets none of this: it never had them.
3. **One name, one place (`internal/agent`).** The server's name and its allowlist are `fyrMCPName` and `fyrMCPTools`, used by the launch, the note and the reattach, so they cannot drift apart again.

The test replays both adoptions through a runner process and fails if the allowlist, server name or timer policy is missing from `ReattachState`.

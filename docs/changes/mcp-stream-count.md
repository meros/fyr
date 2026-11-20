# V0.mcp-stream-count: an MCP stream counts as open before its first event is sent, not after

→ V0.tool-reload. Found as a flake on home-server (20 Nov): `internal/server` TestMCPStreamThroughTheMainPort failed with "the open stream does not count".

1. **The cause, a product race.** `stream` wrote and flushed `notifications/tools/list_changed`, then counted the stream. A client that had read the event could still be reported as not connected (`Current` false), which is what fyrd asks to decide whether an agent's CLI will hear a tools change.
2. **The fix.** The stream is counted (and uncounted on return) before the first byte is written.

Tests: `internal/mcp/stream_count_test.go` (TestStreamCountsBeforeItsFirstEvent: the count is read at the moment of the first write; failed before the fix). TestMCPStreamThroughTheMainPort 50/50.

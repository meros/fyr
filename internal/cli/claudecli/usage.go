package claudecli

import "github.com/meros/fyr/internal/cli"

// V0.usage-ledger (decision 0041). Proved against recorded protocol logs:
// result.modelUsage counts every model of the session (the main agent, its
// subagents and the CLI's own small-model calls) and keeps counting across a
// process that resumes the session; result.usage is the main agent's
// requests of this turn only.
var _ cli.UsageReader = Adapter{}

// Usage reads a result's usage. A result without modelUsage (an older CLI)
// reports nothing.
func (Adapter) Usage(m cli.Msg) (cli.UsageReport, bool) {
	mu, _ := m["modelUsage"].(map[string]any)
	if len(mu) == 0 {
		return cli.UsageReport{}, false
	}
	r := cli.UsageReport{Scope: cli.Cumulative, Models: map[string]cli.TokenCount{}}
	r.ID, _ = m["uuid"].(string)
	r.Session, _ = m["session_id"].(string)
	for model, x := range mu {
		v, _ := x.(map[string]any)
		if v == nil {
			continue
		}
		c := cli.TokenCount{Input: cli.Num(v, "inputTokens"), Output: cli.Num(v, "outputTokens"),
			CacheRead: cli.Num(v, "cacheReadInputTokens"), Thinking: cli.Num(v, "thinkingTokens"),
			WebSearch: cli.Num(v, "webSearchRequests")}
		c.CacheWrite5m, c.CacheWrite1h = cli.Num(v, "cacheCreationEphemeral5mInputTokens"), cli.Num(v, "cacheCreationEphemeral1hInputTokens")
		if all := cli.Num(v, "cacheCreationInputTokens"); c.CacheWrite5m+c.CacheWrite1h < all {
			c.CacheWrite5m += all - c.CacheWrite5m - c.CacheWrite1h // an older CLI: no split, the API's default TTL
		}
		r.Models[model] = c
	}
	if u, _ := m["usage"].(map[string]any); u != nil {
		c := mainUsage(u)
		r.Main = &c
	}
	return r, true
}

// mainUsage reads the API's usage shape (result.usage, message.usage).
func mainUsage(u map[string]any) cli.TokenCount {
	c := cli.TokenCount{Input: cli.Num(u, "input_tokens"), Output: cli.Num(u, "output_tokens"), CacheRead: cli.Num(u, "cache_read_input_tokens")}
	if cc, _ := u["cache_creation"].(map[string]any); cc != nil {
		c.CacheWrite5m, c.CacheWrite1h = cli.Num(cc, "ephemeral_5m_input_tokens"), cli.Num(cc, "ephemeral_1h_input_tokens")
	}
	if all := cli.Num(u, "cache_creation_input_tokens"); c.CacheWrite5m+c.CacheWrite1h < all {
		c.CacheWrite5m += all - c.CacheWrite5m - c.CacheWrite1h
	}
	if d, _ := u["output_tokens_details"].(map[string]any); d != nil {
		c.Thinking = cli.Num(d, "thinking_tokens")
	}
	if s, _ := u["server_tool_use"].(map[string]any); s != nil {
		c.WebSearch, c.WebFetch = cli.Num(s, "web_search_requests"), cli.Num(s, "web_fetch_requests")
	}
	return c
}

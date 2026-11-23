package agycli

import "github.com/meros/fyr/internal/cli"

// V0.usage-jobs (decision 0041). Proved against recorded protocol logs: the
// answer to session/prompt carries usage {inputTokens, outputTokens,
// cachedReadTokens, cachedWriteTokens, thoughtTokens, totalTokens} for that
// turn only (a later turn can be smaller than an earlier one), and
// totalTokens = input + cached read + output, so thinking is part of
// output, as with claude. The result the adapter writes carries it as
// usage. It names no model: the report's one model is "", which the meter
// gives the thread's.
var _ cli.UsageReader = Adapter{}

// Usage reads the usage of a result this adapter wrote.
func (Adapter) Usage(m cli.Msg) (cli.UsageReport, bool) {
	u, _ := m["usage"].(map[string]any)
	if u == nil {
		return cli.UsageReport{}, false
	}
	c := cli.TokenCount{Input: cli.Num(u, "inputTokens"), Output: cli.Num(u, "outputTokens"),
		CacheRead: cli.Num(u, "cachedReadTokens"), CacheWrite5m: cli.Num(u, "cachedWriteTokens"), Thinking: cli.Num(u, "thoughtTokens")}
	if c.Zero() {
		return cli.UsageReport{}, false
	}
	r := cli.UsageReport{Scope: cli.PerTurn, Models: map[string]cli.TokenCount{"": c}}
	r.ID, _ = m["uuid"].(string)
	r.Session, _ = m["session_id"].(string)
	return r, true
}

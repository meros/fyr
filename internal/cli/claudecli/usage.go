package claudecli

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/proc"
)

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

// ProbeArgs are a calibration run's flags (V0.usage-overhead): a print run
// with the CLI's own tools and system prompt, no settings files and no MCP
// server but the launch's, plus what Fyr adds to a thread (the appended
// prompt, its MCP config and allowed tools, the CLI tools it takes away).
func ProbeArgs(l cli.Launch) []string {
	args := []string{"-p", "--output-format", "json", "--no-session-persistence", "--setting-sources", "", "--strict-mcp-config"}
	if l.Model != "" {
		args = append(args, "--model", l.Model)
	}
	if l.MCPConfigFile != "" {
		args = append(args, "--mcp-config", l.MCPConfigFile)
	}
	if len(l.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(l.AllowedTools, ","))
	}
	if len(l.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(l.DisallowedTools, ","))
	}
	if l.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", l.SystemPrompt)
	}
	return args
}

var _ cli.Prober = Adapter{}

// Probe runs ProbeArgs once with prompt on stdin and returns the result.
func (a Adapter) Probe(ctx context.Context, l cli.Launch, prompt string) (cli.Msg, error) {
	env, err := a.Env(l)
	if err != nil {
		return nil, err
	}
	bin := l.Binary
	if bin == "" {
		bin = "claude"
	}
	cmd := proc.Command(ctx, bin, ProbeArgs(l)...)
	cmd.Dir = l.Dir
	cmd.Env = claude.Env(env...)
	cmd.Stdin = strings.NewReader(prompt)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	runErr := cmd.Run()
	res, ok := printResult(out.Bytes())
	if !ok {
		msg := strings.TrimSpace(errb.String())
		if runErr != nil {
			return nil, fmt.Errorf("claude: %s", cli.ErrText(msg, runErr.Error()))
		}
		return nil, fmt.Errorf("claude: the probe printed no result")
	}
	return res, nil
}

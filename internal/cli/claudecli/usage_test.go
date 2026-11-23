package claudecli

import (
	"encoding/json"
	"testing"

	"github.com/meros/fyr/internal/cli"
)

// V0.usage-ledger: a result in the shape claude 2.1.3xx writes (the numbers
// are made up).
const resultFixture = `{"type":"result","subtype":"success","uuid":"u-1","session_id":"s-1","num_turns":2,
 "usage":{"input_tokens":6,"cache_creation_input_tokens":1659,"cache_read_input_tokens":157490,"output_tokens":220,
  "output_tokens_details":{"thinking_tokens":12},"server_tool_use":{"web_search_requests":1,"web_fetch_requests":2},
  "cache_creation":{"ephemeral_1h_input_tokens":1659,"ephemeral_5m_input_tokens":0}},
 "modelUsage":{
  "claude-small":{"inputTokens":2851,"outputTokens":25,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"thinkingTokens":0},
  "claude-big":{"inputTokens":50,"outputTokens":7954,"cacheReadInputTokens":1615374,"cacheCreationInputTokens":50995,"webSearchRequests":1,
   "thinkingTokens":1601,"cacheCreationEphemeral5mInputTokens":0,"cacheCreationEphemeral1hInputTokens":50995}}}`

func TestUsageReadsResult(t *testing.T) {
	var m cli.Msg
	if err := json.Unmarshal([]byte(resultFixture), &m); err != nil {
		t.Fatal(err)
	}
	r, ok := Adapter{}.Usage(m)
	if !ok {
		t.Fatal("no report")
	}
	if r.ID != "u-1" || r.Session != "s-1" || r.Scope != cli.Cumulative {
		t.Errorf("report %+v", r)
	}
	big := cli.TokenCount{Input: 50, Output: 7954, CacheRead: 1615374, CacheWrite1h: 50995, Thinking: 1601, WebSearch: 1}
	if r.Models["claude-big"] != big {
		t.Errorf("big %+v", r.Models["claude-big"])
	}
	if r.Models["claude-small"] != (cli.TokenCount{Input: 2851, Output: 25}) {
		t.Errorf("small %+v", r.Models["claude-small"])
	}
	main := cli.TokenCount{Input: 6, Output: 220, CacheRead: 157490, CacheWrite1h: 1659, Thinking: 12, WebSearch: 1, WebFetch: 2}
	if r.Main == nil || *r.Main != main {
		t.Errorf("main %+v", r.Main)
	}
}

// An older CLI writes cacheCreationInputTokens with no split: it counts as
// the 5-minute kind, the API's default.
func TestUsageWithoutSplit(t *testing.T) {
	m := cli.Msg{"modelUsage": map[string]any{"x": map[string]any{"cacheCreationInputTokens": float64(70)}}}
	r, ok := Adapter{}.Usage(m)
	if !ok || r.Models["x"].CacheWrite5m != 70 || r.Main != nil {
		t.Fatalf("%v %+v", ok, r)
	}
	if _, ok := (Adapter{}).Usage(cli.Msg{"type": "result"}); ok {
		t.Fatal("a result without modelUsage gave a report")
	}
}

package agycli

import (
	"testing"

	"github.com/meros/fyr/internal/cli"
)

// V0.usage-jobs: the usage of agy's answer to session/prompt, as the result
// carries it (the numbers are made up).
func TestUsageReadsResult(t *testing.T) {
	m := cli.Msg{"type": "result", "usage": map[string]any{"inputTokens": float64(70), "outputTokens": float64(29),
		"cachedReadTokens": float64(1500), "cachedWriteTokens": float64(4), "thoughtTokens": float64(15), "totalTokens": float64(1599)}}
	r, ok := Adapter{}.Usage(m)
	if !ok || r.Scope != cli.PerTurn || r.Main != nil {
		t.Fatalf("%v %+v", ok, r)
	}
	want := cli.TokenCount{Input: 70, Output: 29, CacheRead: 1500, CacheWrite5m: 4, Thinking: 15}
	if r.Models[""] != want || len(r.Models) != 1 {
		t.Fatalf("models %+v", r.Models)
	}
	for _, m := range []cli.Msg{{"type": "result"}, {"type": "result", "usage": map[string]any{"inputTokens": float64(0)}}} {
		if _, ok := (Adapter{}).Usage(m); ok {
			t.Errorf("a report from %v", m)
		}
	}
}

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
)

func req(in, cr, cw int64) map[string]any {
	return map[string]any{"input_tokens": float64(in), "cache_read_input_tokens": float64(cr), "cache_creation_input_tokens": float64(cw), "output_tokens": float64(1)}
}

func part(rows []store.OverheadRow, p, k string) int64 {
	for _, r := range rows {
		if r.Part == p && r.Kind == k {
			return r.Tokens
		}
	}
	return 0
}

// V0.usage-overhead: the context's growth between two requests is split by
// Fyr's share of the bytes added; what is resident is read again by each
// later request; a compaction empties it; a request counts once.
func TestOverheadDeltaSplit(t *testing.T) {
	o := newOverhead()
	o.request("a", req(10, 0, 990), false)   // the first request: 1000, cold
	o.add(400, true)                         // a Fyr tool call and its result
	o.request("b", req(2, 1000, 398), false) // 1400: Fyr's 400
	o.request("b", req(2, 1000, 398), false) // the same request on its next block
	o.add(300, false)
	o.add(100, true)
	o.request("c", req(2, 1400, 398), false) // 1800: 100 of the 400 are Fyr's
	o.request("d", req(2, 400, 98), true)    // 500: a compaction; the Stop hook's forced request
	rows, st := o.take("m")
	if w := part(rows, "resident", "cacheWrite"); w != 500 {
		t.Errorf("written %d, want 400 + 100", w)
	}
	if r := part(rows, "resident", "cacheRead"); r != 400 {
		t.Errorf("re-read %d, want the 400 once (c), none after the compaction", r)
	}
	if part(rows, "forced", "cacheRead") != 400 || part(rows, "forced", "input") != 2 || part(rows, "forced", "output") != 1 {
		t.Errorf("forced %+v", rows)
	}
	if st != nil {
		t.Errorf("no static key: no static facts, %+v", st)
	}
	o.staticKey = "s_1"
	o.request("e", req(1, 600, 0), false)
	_, st = o.take("m")
	if st == nil || st.Requests != 1 || st.Cold != 0 || st.Key != "s_1" {
		t.Fatalf("static %+v", st)
	}
}

// Through the stream: a Fyr tool call and its result between two requests
// are Fyr's; the turn's overhead lands beside its report.
func TestOverheadThroughTheStream(t *testing.T) {
	m, st, _ := usageManager(t)
	s := &session{thread: "t_1", proc: "p", digest: newDigest(), live: true, model: "claude-big", out: map[string]string{}}
	s.ov = newOverhead()
	s.ov.staticKey = "s_k"
	asst := func(id string, u map[string]any, blocks ...any) map[string]any {
		return map[string]any{"type": "assistant", "message": map[string]any{"id": id, "role": "assistant", "usage": u, "content": blocks}}
	}
	m.handle(s, asst("m1", req(5, 0, 1000), map[string]any{"type": "tool_use", "id": "tu1", "name": "mcp__fyr__change_list", "input": map[string]any{}}))
	m.handle(s, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "1 changes."}}}})
	m.handle(s, asst("m2", req(1, 1005, 200), map[string]any{"type": "text", "text": "done"}))
	m.handle(s, usageResult("r-1", 3, 1, 3, 10))
	o, err := st.UsageOverhead(store.UsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if o.Parts["resident"]["cacheWrite"] != 201 || o.Uncalibrated != 2 { // 1206 − 1005, all of it Fyr's
		t.Fatalf("overhead %+v", o)
	}
}

// A calibration runs the probe without and with Fyr's prompt and MCP server
// and keeps the difference; both runs are counted as calibration.
func TestCalibrate(t *testing.T) {
	m, st, _ := usageManager(t)
	m.cfg.MCPURL, m.cfg.RunDir = "http://127.0.0.1:1/mcp", t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
cat >/dev/null
case "$*" in
*--append-system-prompt*) n=31000;;
*) n=24000;;
esac
printf '{"type":"result","is_error":false,"num_turns":1,"uuid":"u-%s","result":"OK","usage":{"input_tokens":3,"output_tokens":1,"cache_creation_input_tokens":%s},"modelUsage":{"m":{"inputTokens":3,"outputTokens":1,"cacheCreationInputTokens":%s}}}' $n $n $n
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	n, err := m.calibrate(context.Background(), "m", "FYR PROMPT", cliProfile{p: provider.Profile{ID: "acme", Binary: bin, Auth: cli.Auth{Kind: cli.CLILogin}}, disallowed: []string{"CronCreate"}})
	if err != nil || n != 7000 {
		t.Fatalf("calibrate %d %v", n, err)
	}
	if got := rollup(t, st, "source"); got["calibration|"].CacheWrite5m != 55000 {
		t.Fatalf("calibration rows %+v", got)
	}
	m.mu.Lock()
	left := len(m.tokens)
	m.mu.Unlock()
	if left != 0 {
		t.Errorf("the calibration token outlived the run")
	}
}

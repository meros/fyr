package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/usage"
)

// V0.usage-overhead (decision 0041, phase 4): Fyr's overhead inside an
// agent's turns, beside the measured ledger.
//
//   - static: Fyr's appended prompt and its MCP tools are in every request.
//     The turn keeps its main-agent requests and how many found the cache
//     cold; a calibration measures what the key adds to one request.
//   - resident: Fyr's tool calls, their results and its notices stay in
//     the context and are read again by each later request. Between two
//     requests the context grows by the blocks added since; the growth is
//     split by the bytes of Fyr's blocks among them (exact when only Fyr's
//     were added, an estimate when others were too). What is resident is
//     counted again on each request until a compaction empties it.
//   - forced: the requests of a turn the Stop hook forced to keep the view.
//     Their input side is exact; their output is the count at the request's
//     start, a lower bound (the stream says no more).

// ovRequest is one main-agent request's input side.
type ovRequest struct{ input, cacheRead, cacheWrite, output int64 }

func (r ovRequest) ctx() int64 { return r.input + r.cacheRead + r.cacheWrite }

// overhead is a session's tracker. Its turn sums reset at each result; the
// resident tokens last as long as the context does.
type overhead struct {
	seen         map[string]bool // request ids of this process
	prev         int64           // the last request's context
	newAll       int64           // bytes added to the context since then
	newFyr       int64           // of them, Fyr's
	resident     float64         // Fyr's tokens in the context now
	fyrTools     map[string]bool // ids of Fyr's tool calls
	requests     int64
	cold         int64
	written      float64 // Fyr's tokens that entered the context this turn
	reread       float64 // Fyr's resident tokens read again this turn
	forced       ovRequest
	staticKey    string
	staticPrompt string
}

func newOverhead() *overhead { return &overhead{seen: map[string]bool{}, fyrTools: map[string]bool{}} }

func blockBytes(b map[string]any) int64 {
	j, _ := json.Marshal(b)
	return int64(len(j))
}

// add counts blocks that enter the context; fyr marks them Fyr's.
func (o *overhead) add(n int64, fyr bool) {
	o.newAll += n
	if fyr {
		o.newFyr += n
	}
}

// request takes a main-agent request the first time its id shows.
func (o *overhead) request(id string, u map[string]any, forced bool) {
	if id == "" || o.seen[id] || u == nil {
		return
	}
	o.seen[id] = true
	r := ovRequest{input: cli.Num(u, "input_tokens"), cacheRead: cli.Num(u, "cache_read_input_tokens"),
		cacheWrite: cli.Num(u, "cache_creation_input_tokens"), output: cli.Num(u, "output_tokens")}
	c := r.ctx()
	before := o.resident
	switch {
	case o.prev > 0 && c < o.prev*7/10:
		before, o.resident = 0, 0 // a compaction: the context starts again
	case o.prev > 0 && c > o.prev && o.newAll > 0:
		share := float64(c-o.prev) * float64(o.newFyr) / float64(o.newAll)
		o.resident += share
		o.written += share
	case o.prev == 0 && o.newAll > 0 && o.newFyr > 0:
		// The first request of a process: what Fyr sent before it is part
		// of a context whose growth no earlier request shows. Count Fyr's
		// bytes at the context's own tokens per byte, unknown here, so
		// leave it out: the static part covers the prompt.
	}
	o.reread += before
	o.newAll, o.newFyr = 0, 0
	o.prev = c
	o.requests++
	if r.cacheRead < c/5 {
		o.cold++
	}
	if forced {
		o.forced.input += r.input
		o.forced.cacheRead += r.cacheRead
		o.forced.cacheWrite += r.cacheWrite
		o.forced.output += r.output
	}
}

// take returns the turn's overhead and starts the next turn's sums.
func (o *overhead) take(model string) ([]store.OverheadRow, *store.StaticFacts) {
	const est = "delta-split: context growth between requests, by Fyr's share of the added bytes"
	const forced = "the Stop hook's forced requests: input side exact, output at the request's start"
	rows := []store.OverheadRow{
		{Part: "resident", Kind: "cacheWrite", Tokens: int64(o.written + 0.5), Method: est},
		{Part: "resident", Kind: "cacheRead", Tokens: int64(o.reread + 0.5), Method: est},
		{Part: "forced", Kind: "input", Tokens: o.forced.input, Method: forced},
		{Part: "forced", Kind: "cacheRead", Tokens: o.forced.cacheRead, Method: forced},
		{Part: "forced", Kind: "cacheWrite", Tokens: o.forced.cacheWrite, Method: forced},
		{Part: "forced", Kind: "output", Tokens: o.forced.output, Method: forced},
	}
	var st *store.StaticFacts
	if o.staticKey != "" && o.requests > 0 {
		st = &store.StaticFacts{Key: o.staticKey, Model: model, Requests: o.requests, Cold: o.cold}
	}
	o.requests, o.cold, o.written, o.reread, o.forced = 0, 0, 0, 0, ovRequest{}
	return rows, st
}

// staticKey names what Fyr adds to every request of a process: its
// appended prompt, its MCP tools (their hash) and the CLI tools it takes
// away. Two processes with one key cost the same per request.
func staticKey(adapter, prompt, tools string, disallowed []string) string {
	h := sha256.Sum256([]byte(adapter + "\x00" + prompt + "\x00" + tools + "\x00" + strings.Join(disallowed, ",")))
	return "s_" + hex.EncodeToString(h[:8])
}

// ovMessage feeds the tracker a main-agent stream message (V0.usage-overhead).
func (m *Manager) ovMessage(s *session, msg claude.Msg) {
	if m.cfg.Meter == nil || fromSubagent(msg) {
		return
	}
	inner, _ := msg["message"].(map[string]any)
	blocks, _ := inner["content"].([]any)
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ov == nil {
		s.ov = newOverhead()
	}
	o := s.ov
	if msg["type"] == "assistant" {
		id, _ := inner["id"].(string)
		u, _ := inner["usage"].(map[string]any)
		o.request(id, u, s.inForced)
	}
	for _, x := range blocks {
		b, _ := x.(map[string]any)
		if b == nil || b["type"] == "thinking" {
			continue
		}
		fyr := false
		switch b["type"] {
		case "tool_use":
			name, _ := b["name"].(string)
			if strings.HasPrefix(name, "mcp__"+fyrMCPName+"__") {
				id, _ := b["id"].(string)
				o.fyrTools[id] = true
				fyr = true
			}
		case "tool_result":
			id, _ := b["tool_use_id"].(string)
			fyr = o.fyrTools[id]
		}
		o.add(blockBytes(b), fyr)
	}
}

// ovInject counts text Fyr put in the context outside a tool: its notices
// in a message or a hook's context, the Stop hook's reason. all is the
// whole text it rode in, fyr the bytes of it that are Fyr's.
func (m *Manager) ovInject(s *session, all, fyr int) {
	if m.cfg.Meter == nil || s == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ov == nil {
		s.ov = newOverhead()
	}
	s.ov.add(int64(all), false)
	s.ov.add(int64(fyr), true)
	s.ov.newAll -= int64(fyr) // counted once in all
}

// ovResult writes the turn's overhead beside its report, and starts the
// calibration of a static key that has none.
func (m *Manager) ovResult(s *session, report, model string, p cliProfile) {
	m.mu.Lock()
	o := s.ov
	var rows []store.OverheadRow
	var st *store.StaticFacts
	if o != nil {
		rows, st = o.take(model)
	}
	m.mu.Unlock()
	if o == nil {
		return
	}
	if err := m.cfg.Meter.RecordOverhead(report, rows, st); err != nil {
		log.Printf("usage: overhead of %s: %v", s.thread, err)
	}
	if st != nil {
		m.calibrateOnce(st.Key, model, o.staticPrompt, p)
	}
}

// cliProfile is what a calibration runs on: the thread's provider and the
// CLI tools its launch took away.
type cliProfile struct {
	p          provider.Profile
	disallowed []string
}

// calibrating keeps one calibration per key and model at a time.
var calibrating sync.Map

// calibrateOnce measures a static key on a model, once: two print runs of
// the same CLI with a one-word prompt, one as Fyr starts a thread (its
// prompt, its MCP server, the CLI tools it takes away) and one without; the
// difference of their first request's context is what Fyr adds to each
// request. Both runs are recorded as calibration, which is overhead too.
func (m *Manager) calibrateOnce(key, model, prompt string, cp cliProfile) {
	if key == "" || model == "" || m.cfg.Meter == nil || m.cfg.MCPURL == "" {
		return
	}
	if _, ok, err := m.cfg.Meter.Calibration(key, model); ok || err != nil {
		return
	}
	k := key + "|" + model
	if _, busy := calibrating.LoadOrStore(k, true); busy {
		return
	}
	go func() {
		defer calibrating.Delete(k)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		n, err := m.calibrate(ctx, model, prompt, cp)
		if err != nil {
			log.Printf("usage: calibration of %s on %s: %v", key, model, err)
			return
		}
		if err := m.cfg.Meter.PutCalibration(key, model, n); err != nil {
			log.Printf("usage: calibration of %s on %s: %v", key, model, err)
		}
	}()
}

// writeCalibrationMCP is the MCP config of a calibration run: a token that
// names no thread, valid only while the run lasts.
func (m *Manager) writeCalibrationMCP() (path, tok string, err error) {
	tok = randHex(24)
	cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{fyrMCPName: map[string]any{
		"type": "http", "url": m.cfg.MCPURL, "headers": map[string]string{"Authorization": "Bearer " + tok}}}})
	path, err = writeMCPConfig(m.cfg.RunDir, cfg)
	if err != nil {
		return "", "", err
	}
	m.mu.Lock()
	m.tokens[tok] = ""
	m.mu.Unlock()
	return path, tok, nil
}

func (m *Manager) dropToken(tok string) {
	m.mu.Lock()
	delete(m.tokens, tok)
	m.mu.Unlock()
}

// calibrate runs the two probes and returns what Fyr adds to a request.
func (m *Manager) calibrate(ctx context.Context, model, prompt string, cp cliProfile) (int64, error) {
	p := cp.p
	ad, err := m.adapter(p)
	if err != nil {
		return 0, err
	}
	pr, ok := ad.(cli.Prober)
	ur, ok2 := ad.(cli.UsageReader)
	if !ok || !ok2 {
		return 0, fmt.Errorf("the %s CLI cannot measure its context", ad.ID())
	}
	base := cli.Launch{Binary: p.Binary, Dir: os.TempDir(), Auth: authOf(p), Endpoint: p.Endpoint, Env: p.Env, Model: model}
	if p.Key != nil {
		if k, err := p.Key(); err == nil {
			base.Key = k
		}
	}
	path, tok, err := m.writeCalibrationMCP()
	if err != nil {
		return 0, err
	}
	defer os.Remove(path)
	defer m.dropToken(tok)
	fyr := base
	fyr.SystemPrompt, fyr.MCPConfigFile, fyr.AllowedTools, fyr.DisallowedTools = prompt, path, []string{fyrMCPTools}, cp.disallowed
	const ask = "Reply with the single word OK. Use no tool."
	var ctxs [2]int64
	for i, l := range []cli.Launch{base, fyr} {
		res, err := pr.Probe(ctx, l, ask)
		if err != nil {
			return 0, err
		}
		r, ok := ur.Usage(res)
		if !ok || r.Main == nil {
			return 0, fmt.Errorf("the probe reported no usage")
		}
		if n := cli.Num(res, "num_turns"); n > 1 {
			return 0, fmt.Errorf("the probe took %d turns, not one", n)
		}
		r.Scope = cli.PerTurn
		id := r.ID
		if id == "" {
			id = newUUID()
		}
		m.record("cal:"+id, usage.Dims{Source: usage.SourceCalibration, Provider: p.ID, Adapter: ad.ID(), MainModel: model}, r)
		ctxs[i] = r.Main.Input + r.Main.CacheRead + r.Main.CacheWrite5m + r.Main.CacheWrite1h
	}
	return ctxs[1] - ctxs[0], nil
}

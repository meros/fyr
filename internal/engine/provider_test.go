package engine

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/secrets"
	"github.com/meros/fyr/internal/store"
)

// providerHarness runs the fake CLI under provider-made profiles (V0.50).
func providerHarness(t *testing.T, profiles map[string]provider.Profile) *harness {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(st)
	e.Load()
	h := &harness{t: t, e: e, r: &runner{}, dir: dir}
	var m *agent.Manager
	srv := mcp.New(func(tok string) (string, bool) { return m.ThreadForToken(tok) })
	e.Tools(srv)
	hs := httptest.NewServer(srv)
	m = agent.New(e, agent.Config{Profile: func(id string) provider.Profile { return profiles[id] }, IdleStop: time.Minute, MCPURL: hs.URL + "/mcp"})
	e.Agents, e.Effects, h.m = m, h.r, m
	t.Cleanup(func() { m.StopAll(); hs.Close(); st.Close() })
	return h
}

// SPEC 0.5 V0.50 (2)(6): an endpoint provider starts the CLI binary with
// the endpoint, its key from the secret store and no effort for a model
// without one; a Claude account passes the thread's effort; an endpoint
// without a key does not start.
func TestProviderLaunch(t *testing.T) {
	fake := []string{"FYR_FAKE_CLAUDE=1"}
	sec := secrets.Files(filepath.Join(t.TempDir(), "secrets"))
	sec.Put("ds.key", "sk-test-0123456789-zq")
	key := func(name string) func() (string, error) { return func() (string, error) { return sec.Get(name) } }
	opus := cli.Model{ID: "claude-opus-5-5", Efforts: []string{"low", "high", "max"}}
	ps := map[string]provider.Profile{
		"ds": {ID: "ds", Name: "DeepSeek", Binary: os.Args[0], Env: fake, Auth: cli.Auth{Kind: cli.APIKey, Secret: "ds.key"},
			Endpoint: cli.Endpoint{BaseURL: "https://api.deepseek.test/anthropic"}, Key: key("ds.key"),
			ModelCaps: []cli.Model{{ID: "deepseek-chat"}}, DefaultEffort: "high"},
		"acct": {ID: "acct", Name: "Claude", Binary: os.Args[0], Env: fake, Auth: cli.Auth{Kind: cli.CLILogin},
			ModelCaps: []cli.Model{opus}, DefaultEffort: "low"},
		"nokey": {ID: "nokey", Name: "Router", Binary: os.Args[0], Env: fake, Auth: cli.Auth{Kind: cli.APIKey, Secret: "r.key"},
			Endpoint: cli.Endpoint{BaseURL: "https://openrouter.test/api"}, Key: key("r.key")},
	}
	h := providerHarness(t, ps)
	run := func(profile, model, effort string) core.Thread {
		args := core.Args{"cwd": h.dir, "text": "launch", "profile": profile, "model": model}
		if effort != "" {
			args["effort"] = effort
		}
		r := h.cmd("thread.create", "", args)
		if !r.OK {
			t.Fatalf("create: %+v", r)
		}
		return h.wait(r.ThreadID, "run", func(t core.Thread, a core.Attention) bool {
			return (!t.Running && t.Summary != "") || a.State == core.Failed
		})
	}
	if s := run("ds", "deepseek-chat", "max").Summary; s != "launch: base=https://api.deepseek.test/anthropic token=set-zq effort= model=deepseek-chat" {
		t.Fatal(s)
	}
	if s := run("acct", "claude-opus-5-5", "max").Summary; s != "launch: base= token=unset effort=max model=claude-opus-5-5" {
		t.Fatal(s)
	}
	if s := run("acct", "claude-opus-5-5", "").Summary; !strings.Contains(s, "effort=low ") {
		t.Fatal("default effort: " + s)
	}
	th := run("nokey", "x", "")
	if th.Failure == nil || !strings.Contains(th.Failure.Message, "needs a key") {
		t.Fatalf("no key: %+v", th.Failure)
	}
}

// thread.setModel takes an effort alone; the thread keeps it.
func TestSetEffort(t *testing.T) {
	h := providerHarness(t, map[string]provider.Profile{"acct": {ID: "acct", Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"},
		ModelCaps: []cli.Model{{ID: "m", Efforts: []string{"low", "high"}}}}})
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "launch", "profile": "acct", "model": "m"})
	h.wait(r.ThreadID, "run", func(t core.Thread, _ core.Attention) bool { return !t.Running && t.Summary != "" })
	if rep := h.cmd("thread.setModel", r.ThreadID, core.Args{"effort": "high"}); !rep.OK {
		t.Fatal(rep.Error)
	}
	time.Sleep(300 * time.Millisecond) // the effect runs after the reply
	if rep := h.cmd("thread.send", r.ThreadID, core.Args{"text": "launch"}); !rep.OK {
		t.Fatal(rep.Error)
	}
	th := h.wait(r.ThreadID, "second run", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && strings.Contains(t.Summary, "effort=high")
	})
	if th.Effort != "high" || th.Model != "m" {
		t.Fatalf("%+v", th)
	}
}

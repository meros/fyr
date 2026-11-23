package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

// fakeClaude is a `claude` that reads its prompt and prints out.
func fakeClaude(t *testing.T, out string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\ncat >/dev/null\ncat <<'EOF'\n"+out+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// V0.usage-jobs: a system job's print run reaches the ledger as Fyr's own
// call, filed under the thread it served.
func TestSystemJobUsageReachesTheLedger(t *testing.T) {
	m, st, _ := usageManager(t)
	bin := fakeClaude(t, `{"type":"result","subtype":"success","is_error":false,"result":"A title","uuid":"j-1","session_id":"x",
"usage":{"input_tokens":2,"output_tokens":4,"cache_creation_input_tokens":845,"cache_creation":{"ephemeral_5m_input_tokens":845}},
"modelUsage":{"claude-small":{"inputTokens":2,"outputTokens":4,"cacheCreationInputTokens":845,"cacheCreationEphemeral5mInputTokens":845}}}`)
	a := SystemAgent{Profile: provider.Profile{ID: "acme", Binary: bin, Auth: cli.Auth{Kind: cli.CLILogin}}, Model: "claude-small"}
	out, err := m.printOn(context.Background(), core.JobTitle, "t_1", a, "SYS", "the conversation")
	if err != nil || out != "A title" {
		t.Fatalf("print %q %v", out, err)
	}
	got := rollup(t, st, "source", "thread", "project", "provider", "account", "model", "part", "trigger")
	want := cli.TokenCount{Input: 2, Output: 4, CacheWrite5m: 845}
	if got["job:title|t_1|p_1|acme|a_acme|claude-small|main||"] != want || len(got) != 1 {
		t.Fatalf("ledger %+v", got)
	}
	// A job that serves no thread (a search) is filed under none.
	a.Profile.Binary = fakeClaude(t, `{"type":"result","is_error":false,"result":"t_1","uuid":"j-2","modelUsage":{"claude-small":{"inputTokens":9,"outputTokens":1}}}`)
	if _, err := m.printOn(context.Background(), core.JobSearch, "", a, "SYS", "q"); err != nil {
		t.Fatal(err)
	}
	if got := rollup(t, st, "source", "thread"); got["job:search||"] != (cli.TokenCount{Input: 9, Output: 1}) {
		t.Fatalf("search %+v", got)
	}
}

// A project's setup run is a thread whose turns are Fyr's job.
func TestSetupThreadIsAJob(t *testing.T) {
	m, st, _ := usageManager(t)
	m.cfg.ThreadSource = func(id string) string {
		if id == "t_1" {
			return "job:setup"
		}
		return ""
	}
	s := &session{thread: "t_1", proc: "p", digest: newDigest(), live: true, model: "claude-big"}
	noteTrigger(s, "m", "owner", true)
	m.handle(s, usageResult("r-1", 3, 1, 3, 10))
	if got := rollup(t, st, "source"); len(got) != 1 || got["job:setup|"].Output != 4 {
		t.Fatalf("ledger %+v", got)
	}
}

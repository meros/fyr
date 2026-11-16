package claudecli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
)

// What claude 2.1.322 answers to the stream-json initialize request (16 Nov
// 2026, trimmed): aliases resolve to a full id; "default" is an alias too.
const initAnswer = `{"type":"control_response","response":{"subtype":"success","request_id":"fyr_models","response":{"models":[
{"value":"default","resolvedModel":"claude-opus-5-5","displayName":"Default (recommended)","supportedEffortLevels":["low","medium","high","xhigh","max"]},
{"value":"opus","resolvedModel":"claude-opus-5-5","displayName":"Opus 5.5","supportedEffortLevels":["low","medium","high","xhigh","max"]},
{"value":"haiku","resolvedModel":"claude-haiku-5-5","displayName":"Haiku 5.5"},
{"value":"claude-opus-4-6","resolvedModel":"claude-opus-4-6","displayName":"Opus 4.6","supportedEffortLevels":["low","medium","high","max"]},
{"value":"claude-haiku-4-5-20251001","displayName":"Haiku 4.5"}]}}}`

// V0.120 (1): a Claude login's catalog is what its CLI reports, each full
// id once, named by its own entry rather than the "default" alias.
func TestParseInitModels(t *testing.T) {
	ms, ok := parseInitModels([]byte(strings.ReplaceAll(initAnswer, "\n", "")))
	if !ok {
		t.Fatal("not parsed")
	}
	want := []cli.Model{
		{ID: "claude-opus-5-5", Name: "Opus 5.5", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{ID: "claude-haiku-5-5", Name: "Haiku 5.5"},
		{ID: "claude-opus-4-6", Name: "Opus 4.6", Efforts: []string{"low", "medium", "high", "max"}},
		{ID: "claude-haiku-4-5-20251001", Name: "Haiku 4.5"},
	}
	if len(ms) != len(want) {
		t.Fatalf("%+v", ms)
	}
	for i := range want {
		if ms[i].ID != want[i].ID || ms[i].Name != want[i].Name || strings.Join(ms[i].Efforts, ",") != strings.Join(want[i].Efforts, ",") {
			t.Fatalf("%d: %+v", i, ms[i])
		}
	}
}

// ListModels starts the CLI with no prompt, no tools, no settings and no
// saved session, sends initialize, and reads the answer; Fyr's own list is
// added.
func TestListModelsFromCLI(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\nread line\necho '{\"type\":\"system\",\"subtype\":\"hook_started\"}'\necho '" +
		strings.ReplaceAll(initAnswer, "\n", "") + "'\nsleep 10\n"
	os.WriteFile(bin, []byte(script), 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	ms, err := Adapter{}.ListModels(ctx, cli.Launch{Binary: bin, Dir: dir, Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("waited for the process to end")
	}
	ids := map[string]bool{}
	for _, m := range ms {
		ids[m.ID] = true
	}
	for _, id := range []string{"claude-opus-4-6", "claude-haiku-4-5-20251001", "claude-fable-5-2", "claude-sonnet-5-5"} {
		if !ids[id] {
			t.Fatalf("missing %s in %+v", id, ms)
		}
	}
	args, _ := os.ReadFile(argsFile)
	for _, want := range []string{"--input-format stream-json", "--setting-sources", "--no-session-persistence", "--strict-mcp-config"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("args %q lack %s", args, want)
		}
	}
}

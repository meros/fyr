package claudecli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	lst, err := Adapter{}.ListModels(ctx, cli.Launch{Binary: bin, Dir: dir, Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil {
		t.Fatal(err)
	}
	ms := lst.Models
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

// A fake claude that answers each initialize request with only the aliases
// until its cache is warm, the way claude 2.1.322 does on a config folder
// whose .claude.json has no additionalModelOptionsCache yet (found live 16
// Nov: CLAUDE_CONFIG_DIR=~/.claude answered default, opus, fable, sonnet,
// haiku; the next start answered 14 models). warmAfter is the request number
// (counted across processes in a file) from which it answers in full; 0 never.
func coldCLI(t *testing.T, warmAfter int) (bin string, starts func() int) {
	t.Helper()
	dir := t.TempDir()
	count, procs := filepath.Join(dir, "count"), filepath.Join(dir, "procs")
	aliases := `{"type":"control_response","response":{"subtype":"success","request_id":"ID","response":{"models":[` +
		`{"value":"default","resolvedModel":"claude-opus-5-5","displayName":"Default (recommended)"},{"value":"opus","resolvedModel":"claude-opus-5-5","displayName":"Opus 5.5"},` +
		`{"value":"fable","resolvedModel":"claude-fable-5-2","displayName":"Fable 5.2"},{"value":"sonnet","resolvedModel":"claude-sonnet-5-5","displayName":"Sonnet 5.5"},` +
		`{"value":"haiku","resolvedModel":"claude-haiku-5-5","displayName":"Haiku 5.5"}]}}}`
	full := strings.Replace(aliases, `]}}}`, `,{"value":"claude-opus-4-6","resolvedModel":"claude-opus-4-6","displayName":"Opus 4.6"},{"value":"claude-sonnet-4-6","resolvedModel":"claude-sonnet-4-6","displayName":"Sonnet 4.6"}]}}}`, 1)
	script := "#!/bin/sh\necho x >> " + procs + "\n" +
		"while read line; do\n" +
		"  echo x >> " + count + "\n" +
		"  n=$(wc -l < " + count + ")\n" +
		"  id=$(echo \"$line\" | sed 's/.*\"request_id\":\"\\([^\"]*\\)\".*/\\1/')\n" +
		"  if [ " + strconv.Itoa(warmAfter) + " -gt 0 ] && [ $n -ge " + strconv.Itoa(warmAfter) + " ]; then echo '" + full + "' | sed \"s/\\\"ID\\\"/\\\"$id\\\"/\"; else echo '" + aliases + "' | sed \"s/\\\"ID\\\"/\\\"$id\\\"/\"; fi\n" +
		"done\n"
	bin = filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte(script), 0o755)
	return bin, func() int { b, _ := os.ReadFile(procs); return strings.Count(string(b), "x") }
}

func ids(ms []cli.Model) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func quickPolls(t *testing.T) {
	p, w := listPoll, listWait
	listPoll, listWait = 20*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { listPoll, listWait = p, w })
}

// V0.120, found live: a cold answer (aliases only) is not the list. ListModels
// keeps the CLI running and asks again while its own fetch lands.
func TestListModelsWaitsForTheFullList(t *testing.T) {
	quickPolls(t)
	bin, starts := coldCLI(t, 3)
	l, err := Adapter{}.ListModels(context.Background(), cli.Launch{Binary: bin, Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids(l.Models), "claude-opus-4-6") || l.Note != "" || starts() != 1 {
		t.Fatalf("%+v starts %d", l, starts())
	}
	if !slices.Contains(l.Reported, "claude-sonnet-4-6") || slices.Contains(l.Reported, "default") {
		t.Fatalf("reported %v", l.Reported)
	}
}

// When the process never warms up, a second start reads the cache its first
// one wrote.
func TestListModelsAsksAgainInANewProcess(t *testing.T) {
	quickPolls(t)
	bin, starts := coldCLI(t, 0)   // the first start answers aliases to every request
	bin2, starts2 := coldCLI(t, 1) // the next start reads a warm cache
	l, err := Adapter{}.ListModels(context.Background(), cli.Launch{Binary: twoStarts(t, bin, bin2), Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids(l.Models), "claude-opus-4-6") || l.Note != "" || starts() != 1 || starts2() != 1 {
		t.Fatalf("%+v starts %d+%d", l, starts(), starts2())
	}
}

// twoStarts is a claude that runs a the first time and b after.
func twoStarts(t *testing.T, a, b string) string {
	dir := t.TempDir()
	flag := filepath.Join(dir, "started")
	bin := filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\nif [ -e "+flag+" ]; then exec "+b+" \"$@\"; fi\ntouch "+flag+"\nexec "+a+" \"$@\"\n"), 0o755)
	return bin
}

// When the CLI never lists more than its aliases, the answer is Fyr's known
// list and says so: never a quiet fallback.
func TestListModelsSaysWhenOnlyAliases(t *testing.T) {
	quickPolls(t)
	bin, starts := coldCLI(t, 0)
	l, err := Adapter{}.ListModels(context.Background(), cli.Launch{Binary: bin, Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.Note, "Fyr's known list") || starts() != 2 || len(l.Models) != len(Models) {
		t.Fatalf("%+v starts %d", l, starts())
	}
	if strings.Join(l.Reported, ",") != "claude-opus-5-5,claude-fable-5-2,claude-sonnet-5-5,claude-haiku-5-5" {
		t.Fatalf("reported %v", l.Reported)
	}
}

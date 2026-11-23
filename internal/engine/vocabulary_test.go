// V0.one-tool-vocabulary: every Fyr tool an agent is told to call exists, and
// a removed tool is named nowhere an agent reads, but answers with its
// replacement when an agent that read the old list calls it.
package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

var fyrTool = regexp.MustCompile(`mcp__fyr__([a-z_]+)`)

// TestNamedToolsExist fails when a prompt, the MCP instructions, a gate
// reason, a [Fyr] notice or a tool description names mcp__fyr__X and X is
// not a registered tool. The texts are read where an agent reads them, and
// from the Go sources, where the notices and the reasons are written.
func TestNamedToolsExist(t *testing.T) {
	e, _, _ := bare(t)
	srv := mcp.New(func(string) (string, bool) { return "", false })
	e.Tools(srv)

	texts := map[string]string{
		"the prompt":               agent.FyrPrompt,
		"the bare prompt":          agent.Prompt(agent.Setup{}),
		"the MCP instructions":     mcp.Instructions,
		"the changes paragraph":    agent.ChangesPrompt,
		"the live view prompt":     agent.ViewPrompt,
		"change_request":           ChangeRequestDesc,
		"the settle_request words": SettleRequestDesc,
	}
	for i, r := range core.GateRules {
		texts["gate rule "+string(rune('0'+i))] = r.Reason
	}
	for n, d := range srv.Descriptions() {
		texts["the tool "+n] = d
	}
	for _, root := range []string{"..", "../../cmd"} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			texts[p] = string(b)
			return nil
		})
	}
	if len(texts) < 50 {
		t.Fatalf("read only %d texts", len(texts))
	}
	for where, text := range texts {
		for _, m := range fyrTool.FindAllStringSubmatch(text, -1) {
			name := m[1]
			if strings.HasSuffix(name, "_") { // a prefix, as mcp__fyr__view_
				continue
			}
			if !srv.Has(name) {
				t.Errorf("%s names mcp__fyr__%s, which is not a tool", where, name)
			}
		}
	}
}

// TestRetiredToolsNamedNowhere: no text an agent reads names a removed tool,
// none is listed, and a call of one says what to call instead.
func TestRetiredToolsNamedNowhere(t *testing.T) {
	e, _, _ := bare(t)
	srv := mcp.New(func(string) (string, bool) { return "", false })
	e.Tools(srv)
	texts := []string{agent.FyrPrompt, agent.Prompt(agent.Setup{}), mcp.Instructions}
	for _, r := range core.GateRules {
		texts = append(texts, r.Reason)
	}
	for _, d := range srv.Descriptions() {
		texts = append(texts, d)
	}
	a := bareThread(t, e)
	for name, instead := range Retired {
		if srv.Has(name) {
			t.Errorf("%s is still a tool", name)
		}
		for _, x := range texts {
			if strings.Contains(x, name) {
				t.Errorf("%s is named in %.120q", name, x)
			}
		}
		out, isErr := callAs(t, e, a, name, map[string]any{})
		if !isErr || out != "Fyr removed the tool "+name+": "+instead {
			t.Errorf("%s answers %v %q", name, isErr, out)
		}
	}
}

// A retry of watch after a lost reply gets the watch it armed.
func TestWatchRetryAnswersTheArmedWatch(t *testing.T) {
	e, _, _ := bare(t)
	a := bareThread(t, e)
	args := map[string]any{"when": map[string]any{"kind": "at", "at": "2099-01-01T00:00:00Z"}, "then": "notify", "note": "check CI"}
	first, isErr := callAs(t, e, a, "watch", args)
	if isErr || !strings.HasPrefix(first, "Armed the watch w_") {
		t.Fatalf("%v %q", isErr, first)
	}
	again, isErr := callAs(t, e, a, "watch", args)
	if isErr || again != "Already armed: "+strings.TrimPrefix(first, "Armed ") {
		th, _ := e.Thread(a)
		t.Fatalf("%v %q after %q: %+v", isErr, again, first, th.Rules)
	}
	if th, _ := e.Thread(a); len(th.Rules) != 1 {
		t.Fatalf("rules %+v", th.Rules)
	}
}

// specToolRow is a row of the table in SPEC 5.12: its first cell starts
// with the tool's name.
var specToolRow = regexp.MustCompile("^\\| `([a-z_]+)[ `]")

// TestSpecListsTheTools (V0.spec-from-code): SPEC 5.12 has one row per
// registered tool, no more and no fewer, so the table cannot drift from the
// tools again.
func TestSpecListsTheTools(t *testing.T) {
	e, _, _ := bare(t)
	srv := mcp.New(func(string) (string, bool) { return "", false })
	e.Tools(srv)
	var registered []string
	for n := range srv.Descriptions() {
		registered = append(registered, n)
	}
	slices.Sort(registered)

	b, err := os.ReadFile("../../docs/SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	spec := string(b)
	start := strings.Index(spec, "\n### 5.12 ")
	if start < 0 {
		t.Fatal("SPEC.md has no section 5.12")
	}
	section := spec[start+1:]
	if end := strings.Index(section, "\n### "); end >= 0 {
		section = section[:end]
	}
	var listed []string
	for _, line := range strings.Split(section, "\n") {
		if m := specToolRow.FindStringSubmatch(line); m != nil {
			if slices.Contains(listed, m[1]) {
				t.Errorf("SPEC 5.12 lists %s twice", m[1])
			}
			listed = append(listed, m[1])
		}
	}
	slices.Sort(listed)
	for _, n := range registered {
		if !slices.Contains(listed, n) {
			t.Errorf("the tool %s is registered and has no row in SPEC 5.12", n)
		}
	}
	for _, n := range listed {
		if !slices.Contains(registered, n) {
			t.Errorf("SPEC 5.12 has a row for %s, which is not a registered tool", n)
		}
	}
	if len(registered) < 20 {
		t.Fatalf("only %d tools registered", len(registered))
	}
}

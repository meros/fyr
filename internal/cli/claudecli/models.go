package claudecli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/proc"
)

// ListArgs start the CLI for its initialize answer only (V0.120): stream
// JSON in and out, no tools, no MCP servers, no settings files (so none of
// the owner's hooks run) and no saved session. No prompt is ever sent.
func ListArgs() []string {
	return []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--setting-sources", ""}
}

// ListModels asks the CLI which models its login offers (V0.120). The
// installed claude (2.1.322, checked 16 Nov 2026) has no models command and
// no flag for it; its stream-json initialize answer lists models[] with
// value, resolvedModel, displayName and supportedEffortLevels. The answer
// is that list plus Fyr's own (Models), each id once.
func (a Adapter) ListModels(ctx context.Context, l cli.Launch) ([]cli.Model, error) {
	env, err := a.Env(l)
	if err != nil {
		return nil, err
	}
	bin := l.Binary
	if bin == "" {
		bin = "claude"
	}
	ctx, cancel := context.WithCancel(ctx)
	ok := false
	defer func() {
		if !ok {
			cancel()
		}
	}()
	cmd := proc.Command(ctx, bin, ListArgs()...)
	cmd.Dir = l.Dir
	if cmd.Dir == "" {
		cmd.Dir = os.TempDir()
	}
	cmd.Env = append(append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT=sdk-go"), env...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude: %v", err)
	}
	ok = true
	defer func() { cancel(); cmd.Wait() }() // ends the process once the answer is read
	fmt.Fprintln(in, `{"type":"control_request","request_id":"fyr_models","request":{"subtype":"initialize"}}`)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if ms, ok := parseInitModels(sc.Bytes()); ok {
			in.Close()
			return mergeModels(ms, Models), nil
		}
	}
	in.Close()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("claude did not list its models in time")
	}
	return nil, fmt.Errorf("claude ended without listing its models")
}

// parseInitModels reads one stream line; ok is false when it is not the
// initialize answer.
func parseInitModels(line []byte) ([]cli.Model, bool) {
	var m struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Response  struct {
				Models []struct {
					Value         string   `json:"value"`
					ResolvedModel string   `json:"resolvedModel"`
					DisplayName   string   `json:"displayName"`
					Efforts       []string `json:"supportedEffortLevels"`
				} `json:"models"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &m) != nil || m.Type != "control_response" || m.Response.RequestID != "fyr_models" || m.Response.Subtype != "success" {
		return nil, false
	}
	out := []cli.Model{}
	at := map[string]int{}
	for _, x := range m.Response.Response.Models {
		id := x.ResolvedModel
		if id == "" {
			id = x.Value
		}
		if id == "" || id == "default" {
			continue
		}
		name := x.DisplayName
		if x.Value == "default" {
			name = "" // "Default (recommended)" names the alias, not the model
		}
		if i, ok := at[id]; ok {
			if out[i].Name == "" {
				out[i].Name = name
			}
			continue
		}
		at[id] = len(out)
		out = append(out, cli.Model{ID: id, Name: name, Efforts: x.Efforts})
	}
	return out, true
}

// mergeModels is a then the ids of b it lacks.
func mergeModels(a, b []cli.Model) []cli.Model {
	seen := map[string]bool{}
	for _, m := range a {
		seen[strings.TrimSpace(m.ID)] = true
	}
	for _, m := range b {
		if !seen[m.ID] {
			a = append(a, m)
		}
	}
	return a
}

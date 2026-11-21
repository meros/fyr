package claudecli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/proc"
)

// ListArgs start the CLI for its initialize answer only (V0.120): stream
// JSON in and out, no tools, no MCP servers, no settings files (so none of
// the owner's hooks run) and no saved session. No prompt is ever sent.
func ListArgs() []string {
	return []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--tools", "", "--strict-mcp-config", "--no-session-persistence", "--setting-sources", ""}
}

// Listing is what a login's CLI reported: the catalog to keep (its list plus
// Fyr's own), the ids the CLI itself named, and a note when the answer is
// only Fyr's known list.
type Listing = cli.Listing

// NoteKnownList says that the CLI named only its short aliases.
const NoteKnownList = "Claude answered with its short names only (its own list was not ready), so this is Fyr's known list. Refresh in a minute."

// How long one start waits for a full answer, asking again every listPoll
// (vars for tests).
var (
	listPoll = time.Second
	listWait = 12 * time.Second
)

// ListModels asks the CLI which models its login offers (V0.120). The
// installed claude (2.1.322, checked 16 Nov 2026) has no models command and
// no flag for it; its stream-json initialize answer lists models[] with
// value, resolvedModel, displayName and supportedEffortLevels.
//
// That answer comes from a cache in the config folder's .claude.json
// (additionalModelOptionsCache), which the CLI refreshes in the background
// after it starts. With a cold cache (found live 16 Nov: Fyr sets
// CLAUDE_CONFIG_DIR, whose .claude.json had none) it names only its aliases
// (default, opus, fable, sonnet, haiku). So ListModels keeps the CLI running
// and asks again while its fetch lands; failing that it starts it once more
// (the cache is on disk by then); failing that the answer is Fyr's known
// list with a Note, never a quiet fallback.
func (a Adapter) ListModels(ctx context.Context, l cli.Launch) (Listing, error) {
	ms, full, err := a.ask(ctx, l, listWait)
	if err != nil {
		return Listing{}, err
	}
	if !full {
		if ms2, full2, err2 := a.ask(ctx, l, 0); err2 == nil && full2 {
			ms, full = ms2, true
		}
	}
	out := Listing{Models: mergeModels(append([]cli.Model(nil), ms...), Models)}
	for _, m := range ms {
		out.Reported = append(out.Reported, m.ID)
	}
	if !full {
		out.Note = NoteKnownList
	}
	return out, nil
}

// ask starts the CLI once and sends initialize; while the answer names only
// aliases it asks again every listPoll for up to wait.
func (a Adapter) ask(ctx context.Context, l cli.Launch, wait time.Duration) ([]cli.Model, bool, error) {
	env, err := a.Env(l)
	if err != nil {
		return nil, false, err
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
	cmd.Env = append(append(launchenv.Without(os.Environ(), cli.CredentialVars), "CLAUDE_CODE_ENTRYPOINT=sdk-go"), env...) // #167, V0.agent-env
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, false, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("claude: %v", err)
	}
	ok = true
	defer func() { in.Close(); cancel(); cmd.Wait() }() // ends the process once the answer is read
	type answer struct {
		ms   []cli.Model
		full bool
	}
	answers := make(chan answer, 4)
	go func() {
		defer close(answers)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			if ms, full, ok := parseInit(sc.Bytes()); ok {
				select {
				case answers <- answer{ms, full}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	n := 0
	send := func() {
		n++
		fmt.Fprintf(in, `{"type":"control_request","request_id":"fyr_models_%d","request":{"subtype":"initialize"}}`+"\n", n)
	}
	send()
	deadline := time.Now().Add(wait)
	var last *answer
	for {
		select {
		case x, open := <-answers:
			if !open {
				if last != nil {
					return last.ms, last.full, nil
				}
				if ctx.Err() != nil {
					return nil, false, fmt.Errorf("claude did not list its models in time")
				}
				return nil, false, fmt.Errorf("claude ended without listing its models")
			}
			last = &x
			if x.full || !time.Now().Before(deadline) {
				return x.ms, x.full, nil
			}
			select {
			case <-time.After(listPoll):
			case <-ctx.Done():
				return x.ms, x.full, nil
			}
			send()
		case <-ctx.Done():
			if last != nil {
				return last.ms, last.full, nil
			}
			return nil, false, fmt.Errorf("claude did not list its models in time")
		}
	}
}

// parseInitModels reads one stream line; ok is false when it is not the
// initialize answer.
func parseInitModels(line []byte) ([]cli.Model, bool) {
	ms, _, ok := parseInit(line)
	return ms, ok
}

// parseInit also says whether the answer is full: it names a model by its
// own id, not only the aliases (an alias has no version number).
func parseInit(line []byte) ([]cli.Model, bool, bool) {
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
	if json.Unmarshal(line, &m) != nil || m.Type != "control_response" || !strings.HasPrefix(m.Response.RequestID, "fyr_models") || m.Response.Subtype != "success" {
		return nil, false, false
	}
	full := false
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
		if strings.ContainsAny(x.Value, "0123456789") {
			full = true
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
	return out, full, true
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

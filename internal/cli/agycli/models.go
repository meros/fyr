package agycli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/meros/fyr/internal/cli"
)

// ListModels asks `agy acp --stdio` for the models available to the launch's
// auth (Google login or Gemini API key). It runs `initialize` + `session/new`
// (no prompt is sent), extracts grouped models and per-model effort levels from
// `configOptions` and `models.availableModels`, and immediately closes the
// session.
func (a Adapter) ListModels(ctx context.Context, l cli.Launch) (cli.Listing, error) {
	l.MCPConfigFile = ""
	l.AllowedTools = nil
	l.Resume = false
	l.Session = ""
	l.Model = ""
	l.Effort = ""
	l.SystemPrompt = ""
	l.Runner = nil
	if l.Dir == "" {
		l.Dir = os.TempDir()
	}
	sess, err := a.Start(l, cli.Handlers{})
	if err != nil {
		return cli.Listing{}, err
	}
	acpSess := sess.(*acpSession)
	defer func() {
		acpSess.Kill()
		<-acpSess.Done()
	}()
	ans, err := acpSess.Initialize(ctx, nil)
	if err != nil {
		if ctx.Err() != nil {
			return cli.Listing{}, fmt.Errorf("agy did not list its models in time")
		}
		return cli.Listing{}, fmt.Errorf("agy: %v", err)
	}
	inner, _ := ans["response"].(cli.Msg)
	ms, _ := inner["models"].([]cli.Model)
	if len(ms) == 0 {
		return cli.Listing{}, fmt.Errorf("agy ended without listing its models")
	}
	reported := make([]string, 0, len(ms))
	for _, m := range ms {
		reported = append(reported, m.ID)
	}
	return cli.Listing{Models: ms, Reported: reported}, nil
}

// ParseACPModels builds Fyr's grouped []cli.Model from an ACP `session/new`
// result (`configOptions` + `models.availableModels`).
//
// Each base model in `configOptions[id="model"]` gets its display name and the
// exact effort suffixes (`low`, `medium`, `high`, `xhigh`, `max`) present for
// it in `models.availableModels`. If `configOptions` is absent, it groups
// `models.availableModels` directly by stripping trailing effort suffixes.
func ParseACPModels(res cli.Msg) []cli.Model {
	avail := parseAvailableModels(res)
	cfgModels := parseConfigModelOptions(res)

	effortOrder := []string{"low", "medium", "high", "xhigh", "max"}
	if len(cfgModels) > 0 {
		effortsByBase := map[string]map[string]bool{}
		for _, a := range avail {
			base, eff := splitModelEffort(a.id)
			if eff != "" {
				if effortsByBase[base] == nil {
					effortsByBase[base] = map[string]bool{}
				}
				effortsByBase[base][eff] = true
			}
		}
		out := make([]cli.Model, 0, len(cfgModels))
		seen := map[string]bool{}
		for _, m := range cfgModels {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			if set := effortsByBase[m.ID]; len(set) > 0 {
				var es []string
				for _, e := range effortOrder {
					if set[e] {
						es = append(es, e)
					}
				}
				m.Efforts = es
			}
			out = append(out, m)
		}
		return out
	}

	if len(avail) == 0 {
		return nil
	}
	// Fallback when a server only sends `models.availableModels`.
	var order []string
	names := map[string]string{}
	effs := map[string]map[string]bool{}
	rawCount := map[string]int{}
	for _, a := range avail {
		base, eff := splitModelEffort(a.id)
		rawCount[base]++
		if _, ok := names[base]; !ok {
			order = append(order, base)
			names[base] = stripEffortParen(a.name)
		}
		if eff != "" {
			if effs[base] == nil {
				effs[base] = map[string]bool{}
			}
			effs[base][eff] = true
		}
	}
	out := make([]cli.Model, 0, len(order))
	for _, base := range order {
		// If a model only appears once with an effort-like suffix (e.g.
		// gpt-oss-120b-medium), keep its exact slug so set_model works directly.
		if rawCount[base] == 1 && len(effs[base]) == 1 {
			for _, a := range avail {
				b, _ := splitModelEffort(a.id)
				if b == base {
					out = append(out, cli.Model{ID: a.id, Name: a.name})
					break
				}
			}
			continue
		}
		var es []string
		for _, e := range effortOrder {
			if effs[base][e] {
				es = append(es, e)
			}
		}
		out = append(out, cli.Model{ID: base, Name: names[base], Efforts: es})
	}
	return out
}

type availModel struct{ id, name string }

func parseAvailableModels(res cli.Msg) []availModel {
	modelsObj, _ := res["models"].(map[string]any)
	raw, _ := modelsObj["availableModels"].([]any)
	var out []availModel
	for _, x := range raw {
		m, _ := x.(map[string]any)
		id, _ := m["modelId"].(string)
		name, _ := m["name"].(string)
		if id != "" {
			out = append(out, availModel{id: id, name: name})
		}
	}
	return out
}

func parseConfigModelOptions(res cli.Msg) []cli.Model {
	opts, _ := res["configOptions"].([]any)
	for _, x := range opts {
		o, _ := x.(map[string]any)
		id, _ := o["id"].(string)
		cat, _ := o["category"].(string)
		if id != "model" && cat != "model" {
			continue
		}
		rawOpts, _ := o["options"].([]any)
		out := make([]cli.Model, 0, len(rawOpts))
		for _, ro := range rawOpts {
			m, _ := ro.(map[string]any)
			val, _ := m["value"].(string)
			name, _ := m["name"].(string)
			if val != "" {
				out = append(out, cli.Model{ID: val, Name: name})
			}
		}
		return out
	}
	return nil
}

func splitModelEffort(id string) (base, effort string) {
	for _, e := range []string{"xhigh", "high", "medium", "low", "max"} {
		suf := "-" + e
		if strings.HasSuffix(id, suf) && len(id) > len(suf) {
			return strings.TrimSuffix(id, suf), e
		}
	}
	return id, ""
}

func stripEffortParen(name string) string {
	for _, suf := range []string{" (Low)", " (Medium)", " (High)", " (XHigh)", " (Max)", " (Extra)"} {
		if strings.HasSuffix(name, suf) {
			return strings.TrimSuffix(name, suf)
		}
	}
	return name
}

// ParseModelsOutput parses the plain text output of `agy models` into grouped
// models with per-model effort levels.
func ParseModelsOutput(text string) []cli.Model {
	var avail []availModel
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i := strings.LastIndex(line, "\r"); i >= 0 {
			line = strings.TrimSpace(line[i+1:])
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		slug := fields[0]
		if !strings.Contains(slug, "-") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, slug))
		avail = append(avail, availModel{id: slug, name: name})
	}
	raw := make([]any, 0, len(avail))
	for _, a := range avail {
		raw = append(raw, map[string]any{"modelId": a.id, "name": a.name})
	}
	return ParseACPModels(cli.Msg{"models": map[string]any{"availableModels": raw}})
}

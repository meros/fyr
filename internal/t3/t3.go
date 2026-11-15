// Package t3 reads T3 Code's state read-only so Fyr can adopt its open
// threads by their Claude session id (SPEC 0.5 V0.9, decision 0004) and
// reuse its provider instances as profiles (V0.11).
package t3

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/core"
	_ "modernc.org/sqlite"
)

// Candidate is an open top-level T3 thread that Fyr can adopt.
type Candidate struct {
	T3ID          string    `json:"t3Id"`
	Title         string    `json:"title"`
	Cwd           string    `json:"cwd"`
	Profile       string    `json:"profile"`
	Model         string    `json:"model"`
	SessionID     string    `json:"sessionId"`
	PRURL         string    `json:"prUrl,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
	HasTranscript bool      `json:"hasTranscript"`
	SnoozedUntil  string    `json:"snoozedUntil,omitempty"`
}

func open(db string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+db+"?mode=ro&_pragma=busy_timeout(5000)")
}

// Candidates lists open top-level threads with a Claude session, newest first.
func Candidates(db, home string) ([]Candidate, error) {
	c, err := open(db)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	rows, err := c.Query(`
SELECT t.thread_id, t.title, t.updated_at, t.payload_json, COALESCE(p.workspace_root, ''), COALESCE(pt.payload_json, '{}'),
  COALESCE((SELECT url FROM projection_thread_pull_requests r WHERE r.thread_id = t.thread_id ORDER BY linked_at DESC LIMIT 1), '')
FROM orchestration_v2_projection_threads t
LEFT JOIN projection_projects p ON p.project_id = t.project_id
LEFT JOIN orchestration_v2_projection_provider_threads pt ON pt.provider_thread_id = t.active_provider_thread_id
WHERE t.deleted_at IS NULL AND t.archived_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("read T3 threads: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id, title, updated, payload, root, ptPayload, pr string
		if err := rows.Scan(&id, &title, &updated, &payload, &root, &ptPayload, &pr); err != nil {
			return nil, err
		}
		var p struct {
			ProviderInstanceID string `json:"providerInstanceId"`
			ModelSelection     struct {
				Model string `json:"model"`
			} `json:"modelSelection"`
			WorktreePath string  `json:"worktreePath"`
			SettledAt    *string `json:"settledAt"`
			DeletedAt    *string `json:"deletedAt"`
			SnoozedUntil *string `json:"snoozedUntil"`
			Lineage      struct {
				ParentThreadID *string `json:"parentThreadId"`
			} `json:"lineage"`
		}
		json.Unmarshal([]byte(payload), &p)
		var pt struct {
			NativeThreadRef struct {
				NativeID string `json:"nativeId"`
			} `json:"nativeThreadRef"`
		}
		json.Unmarshal([]byte(ptPayload), &pt)
		if p.Lineage.ParentThreadID != nil || p.SettledAt != nil || p.DeletedAt != nil || pt.NativeThreadRef.NativeID == "" {
			continue
		}
		cwd := p.WorktreePath
		if cwd == "" {
			cwd = root
		}
		up, _ := time.Parse(time.RFC3339Nano, updated)
		cand := Candidate{T3ID: id, Title: title, Cwd: cwd, Profile: p.ProviderInstanceID, Model: p.ModelSelection.Model,
			SessionID: pt.NativeThreadRef.NativeID, PRURL: pr, UpdatedAt: up}
		if p.SnoozedUntil != nil {
			cand.SnoozedUntil = *p.SnoozedUntil
		}
		cand.HasTranscript = TranscriptPath(home, cand.SessionID) != ""
		out = append(out, cand)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, rows.Err()
}

// TranscriptPath finds <session>.jsonl in the projects of any ~/.claude*
// config folder, or "" (claudecli.SessionFile is the one lookup).
func TranscriptPath(home, session string) string {
	dirs, _ := filepath.Glob(filepath.Join(home, ".claude*"))
	for _, d := range dirs {
		if p := claudecli.SessionFile(d, "", session); p != "" {
			return p
		}
	}
	return ""
}

// History converts the last limit lines of a Claude transcript into Fyr
// timeline events: a user's text becomes "user", everything else "msg".
func History(home, session string, limit int) ([]core.Event, []time.Time, error) {
	path := TranscriptPath(home, session)
	if path == "" {
		return nil, nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	type line struct {
		ev core.Event
		at time.Time
	}
	var all []line
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Type        string         `json:"type"`
			IsMeta      bool           `json:"isMeta"`
			IsSidechain bool           `json:"isSidechain"`
			Message     map[string]any `json:"message"`
			Timestamp   string         `json:"timestamp"`
			Parent      any            `json:"parent_tool_use_id"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.IsMeta || l.IsSidechain || l.Message == nil {
			continue
		}
		if l.Type != "user" && l.Type != "assistant" {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
		if s, ok := l.Message["content"].(string); ok && l.Type == "user" {
			all = append(all, line{core.Event{Type: "user", Data: map[string]any{"text": s, "images": 0}}, at})
			continue
		}
		all = append(all, line{core.Event{Type: "msg", Data: map[string]any{"type": l.Type, "parent_tool_use_id": l.Parent, "message": l.Message}}, at})
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	evs := make([]core.Event, len(all))
	ats := make([]time.Time, len(all))
	for i, l := range all {
		evs[i], ats[i] = l.ev, l.at
	}
	return evs, ats, sc.Err()
}

// Profiles turns T3's claude provider instances into Fyr profiles. The
// built-in instance claudeAgent (the plain `claude` binary) is always first.
func Profiles(settings string) ([]agent.Profile, error) {
	b, err := os.ReadFile(settings)
	if err != nil {
		return nil, err
	}
	var s struct {
		ProviderInstances map[string]struct {
			Driver      string `json:"driver"`
			DisplayName string `json:"displayName"`
			AccentColor string `json:"accentColor"`
			Enabled     *bool  `json:"enabled"`
			Config      struct {
				BinaryPath   string `json:"binaryPath"`
				CustomModels []struct {
					Slug string `json:"slug"`
				} `json:"customModels"`
			} `json:"config"`
		} `json:"providerInstances"`
		Prefs map[string]struct {
			Hidden []string `json:"hiddenModels"`
			Order  []string `json:"modelOrder"`
		} `json:"providerModelPreferences"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	models := func(id string, custom []string) []string {
		hidden := map[string]bool{}
		for _, h := range s.Prefs[id].Hidden {
			hidden[h] = true
		}
		var out []string
		for _, m := range append(append([]string{}, s.Prefs[id].Order...), custom...) {
			if !hidden[m] {
				out = append(out, m)
			}
		}
		if len(out) == 0 && id == "claudeAgent" {
			out = DefaultModels
		}
		return out
	}
	out := []agent.Profile{{ID: "claudeAgent", Name: "Claude", Binary: "claude", Color: "#d97757", Models: models("claudeAgent", nil)}}
	ids := make([]string, 0, len(s.ProviderInstances))
	for id := range s.ProviderInstances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		pi := s.ProviderInstances[id]
		if pi.Driver != "claudeAgent" || (pi.Enabled != nil && !*pi.Enabled) || id == "claudeAgent" {
			continue
		}
		var custom []string
		for _, c := range pi.Config.CustomModels {
			custom = append(custom, c.Slug)
		}
		bin := pi.Config.BinaryPath
		if bin == "" {
			bin = "claude"
		}
		name := pi.DisplayName
		if name == "" {
			name = id
		}
		out = append(out, agent.Profile{ID: id, Name: name, Binary: bin, Color: pi.AccentColor, Models: models(id, custom)})
	}
	for i := range out {
		if len(out[i].Models) > 0 {
			out[i].DefaultModel = out[i].Models[0]
		}
	}
	return out, nil
}

// DefaultModels is the model list when nothing else names one.
var DefaultModels = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-2", "claude-haiku-5-5"}

// Projects reads T3's projects as Fyr project seeds (SPEC 0.5 V0.23): the
// root as the folder, the title, and a lucide icon as a line
// icon. T3's "No project" (its scratch root) becomes "Scratch".
func Projects(db string) ([]core.Project, error) {
	c, err := open(db)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	rows, err := c.Query(`SELECT title, workspace_root, COALESCE(project_icon_json, '') FROM projection_projects WHERE deleted_at IS NULL AND archived_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Project
	for rows.Next() {
		var title, root, icon string
		if err := rows.Scan(&title, &root, &icon); err != nil {
			return nil, err
		}
		p := core.Project{Name: title, Folder: root}
		var ic struct{ Kind, Name, Color string }
		if json.Unmarshal([]byte(icon), &ic) == nil && ic.Kind == "lucide" && ic.Name != "" {
			p.Icon = core.Icon{Kind: "line", Value: ic.Name}
			if ic.Color == "gray" {
				p.Color = "slate"
			}
		}
		if title == "No project" {
			p.Name = "Scratch"
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

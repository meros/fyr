package engine

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
)

// handoverHarness has fake profiles with their own config dirs (V0.45):
// a and b differ, s shares a's, x cannot resume, dead cannot start.
type handoverHarness struct {
	*harness
	dirA, dirB string
	out        string // the handover folder
}

func newHandoverHarness(t *testing.T) *handoverHarness {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work.dir")
	os.MkdirAll(work, 0o700)
	hh := &handoverHarness{harness: &harness{t: t, e: e, r: &runner{}, dir: work}, dirA: filepath.Join(dir, "cfgA"), dirB: filepath.Join(dir, "cfgB"), out: filepath.Join(dir, "handover")}
	fake := []string{"FYR_FAKE_CLAUDE=1"}
	profiles := map[string]provider.Profile{
		"a":    {ID: "a", Name: "Claude", Binary: os.Args[0], Env: fake, ConfigDir: hh.dirA, DefaultModel: "model-a"},
		"b":    {ID: "b", Name: "Claude (acme)", Binary: os.Args[0], Env: fake, ConfigDir: hh.dirB, Models: []string{"model-b", "model-b2"}, DefaultModel: "model-b"},
		"s":    {ID: "s", Name: "Same dir", Binary: os.Args[0], Env: append([]string{"CLAUDE_CONFIG_DIR=" + hh.dirA}, fake...)},
		"x":    {ID: "x", Name: "No resume", Binary: os.Args[0], Env: append([]string{"FYR_FAKE_NORESUME=1"}, fake...), ConfigDir: hh.dirB},
		"dead": {ID: "dead", Name: "Dead", Binary: filepath.Join(dir, "missing-binary")},
	}
	var m *agent.Manager
	srv := mcp.New(func(tok string) (string, bool) { return m.ThreadForToken(tok) })
	e.Tools(srv)
	hs := httptest.NewServer(srv)
	m = agent.New(e, agent.Config{
		Profile:     func(id string) provider.Profile { return profiles[id] },
		IdleStop:    time.Minute,
		MCPURL:      hs.URL + "/mcp",
		HandoverDir: hh.out,
	})
	e.Agents, e.Effects, hh.m = m, hh.r, m
	e.ProfileExists = func(id string) bool { _, ok := profiles[id]; return ok }
	guardDeps(t, e)
	t.Cleanup(func() { m.StopAll(); hs.Close(); st.Close() })
	return hh
}

// start makes a thread on profile a with one finished run.
func (h *handoverHarness) start(text string) core.Thread {
	h.t.Helper()
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": text, "profile": "a"})
	if !r.OK {
		h.t.Fatalf("create: %+v", r)
	}
	return h.wait(r.ThreadID, "first run", func(t core.Thread, _ core.Attention) bool { return !t.Running && t.Summary != "" })
}

func (h *handoverHarness) handover(id, profile string) handoverEv {
	h.t.Helper()
	if r := h.cmd("thread.handover", id, core.Args{"profile": profile}); !r.OK {
		h.t.Fatalf("handover: %+v", r)
	}
	h.wait(id, "handover done", func(t core.Thread, _ core.Attention) bool { return t.Handover == nil })
	for _, ev := range h.events(id) {
		if ev.Type == "handover" {
			var d handoverEv
			json.Unmarshal(ev.Data, &d)
			return d
		}
	}
	b, _ := json.Marshal(h.events(id))
	h.t.Fatalf("no handover event: %s", b)
	return handoverEv{}
}

// handoverTexts waits for n messages Fyr sent as the handover (the request
// to the source, the target's first message) and returns every user
// message as from|text. The handover ends (Handover nil) before the
// target's first message is in the log (V0.fix-flakes).
func (h *handoverHarness) handoverTexts(id string, n int) []string {
	h.t.Helper()
	var users []string
	eventually(h.t, fmt.Sprintf("%d handover messages", n), func() bool {
		users = users[:0]
		got := 0
		for _, e := range h.events(id) {
			if e.Type == "user" {
				var d struct{ Text, From string }
				json.Unmarshal(e.Data, &d)
				users = append(users, d.From+"|"+d.Text)
				if d.From == "handover" {
					got++
				}
			}
		}
		return got >= n
	})
	return users
}

type handoverEv struct {
	From, To, Model, Rung, Path, Note string
}

func (h *handoverHarness) transcript(cfg, sid string) string {
	m, _ := filepath.Glob(filepath.Join(cfg, "projects", "*", sid+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// Rung 1: the transcript goes into the target's config dir and the target
// resumes the same session there.
func TestHandoverResumeCopiesTranscript(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("remember the word tulip")
	sid := th.SessionID
	if h.transcript(h.dirA, sid) == "" {
		t.Fatal("the fake wrote no transcript in a's config dir")
	}
	if got := h.m.HandoverPlan(th.ID, "b"); got.Rung != "resume" {
		t.Fatalf("plan %+v", got)
	}
	ev := h.handover(th.ID, "b")
	if ev.Rung != "resume" || ev.From != "a" || ev.To != "b" || ev.Note != "" {
		t.Fatalf("event %+v", ev)
	}
	got, _ := h.e.Thread(th.ID)
	if got.Profile != "b" || got.Model != "model-b" || got.SessionID != sid {
		t.Fatalf("thread profile %q model %q session %q (was %q)", got.Profile, got.Model, got.SessionID, sid)
	}
	p := h.transcript(h.dirB, sid)
	if p == "" {
		t.Fatal("transcript not copied to b's config dir")
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSuffix(p, ".jsonl"), "tool-results", "fake.txt")); err != nil {
		t.Fatalf("session folder not copied: %v", err)
	}
	h.cmd("thread.send", th.ID, core.Args{"text": "recall"})
	got = h.wait(th.ID, "recall", func(t core.Thread, _ core.Attention) bool { return strings.HasPrefix(t.Summary, "recall:") })
	if got.Summary != "recall: remember the word tulip" || got.SessionID != sid {
		t.Fatalf("summary %q session %q", got.Summary, got.SessionID)
	}
}

// Rung 1 with one config dir: nothing to copy, the same session resumes.
func TestHandoverSameConfigDirResumes(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("hello")
	ev := h.handover(th.ID, "s")
	got, _ := h.e.Thread(th.ID)
	if ev.Rung != "resume" || got.Profile != "s" || got.SessionID != th.SessionID {
		t.Fatalf("event %+v thread %+v", ev, got)
	}
	if got.Model != "" {
		t.Fatalf("a profile without a default model leaves the model to the CLI, got %q", got.Model)
	}
}

// Rung 1 fails at resume → rung 2: the new agent reads the old conversation.
func TestHandoverResumeErrorFallsBackToAdopt(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("remember the word tulip")
	ev := h.handover(th.ID, "x")
	if ev.Rung != "adopt" || !strings.Contains(ev.Note, "No conversation found") {
		t.Fatalf("event %+v", ev)
	}
	b, err := os.ReadFile(ev.Path)
	if err != nil || !strings.Contains(string(b), "remember the word tulip") || !strings.Contains(string(b), "echo: remember the word tulip") {
		t.Fatalf("export %q: %s", ev.Path, b)
	}
	got := h.wait(th.ID, "adopt turn", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && strings.HasPrefix(t.Summary, "echo: [Fyr] Handover:")
	})
	if !hasEvent(h.events(th.ID), "user", ev.Path) {
		t.Fatal("the first message does not name the export")
	}
	if got.Profile != "x" || got.SessionID == th.SessionID || got.SessionID == "" {
		t.Fatalf("thread %+v (old session %s)", got, th.SessionID)
	}
}

// No transcript at all → rung 2, and the plan says so before.
func TestHandoverWithoutTranscriptAdopts(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("hello")
	os.RemoveAll(filepath.Join(h.dirA, "projects"))
	if got := h.m.HandoverPlan(th.ID, "b"); got.Rung != "adopt" || got.Note == "" {
		t.Fatalf("plan %+v", got)
	}
	ev := h.handover(th.ID, "b")
	if ev.Rung != "adopt" || !strings.Contains(ev.Note, "transcript") {
		t.Fatalf("event %+v", ev)
	}
}

// Rungs 1 and 2 fail → rung 3: one turn on the source writes the handover.
func TestHandoverBriefWhenExportFails(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("hello")
	os.RemoveAll(filepath.Join(h.dirA, "projects"))
	os.WriteFile(h.out, []byte("not a folder"), 0o600) // the export cannot be written
	ev := h.handover(th.ID, "b")
	if ev.Rung != "brief" || !strings.Contains(ev.Note, "transcript") {
		t.Fatalf("event %+v", ev)
	}
	users := h.handoverTexts(th.ID, 2)
	// hello, the request to the source, the start of the target
	if len(users) != 3 || !strings.HasPrefix(users[1], "handover|") || !strings.HasPrefix(users[2], "handover|") {
		t.Fatalf("users %q", users)
	}
	// The source's reply (the fake echoes) reaches the target inline.
	got := h.wait(th.ID, "target turn", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && t.Profile == "b" && strings.Contains(t.Summary, "echo:")
	})
	if got.SessionID == th.SessionID {
		t.Fatal("the target kept the old session")
	}
	if !strings.Contains(users[2], "echo: [Fyr] Handover:") {
		t.Fatalf("target message %q", users[2])
	}
}

// A target that cannot start leaves the thread where it was.
func TestHandoverTargetCannotStart(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("hello")
	if r := h.cmd("thread.handover", th.ID, core.Args{"profile": "dead"}); !r.OK {
		t.Fatalf("%+v", r)
	}
	got := h.wait(th.ID, "handover ended", func(t core.Thread, _ core.Attention) bool { return t.Handover == nil })
	if got.Profile != "a" || got.SessionID != th.SessionID {
		t.Fatalf("thread %+v", got)
	}
	if !hasEvent(h.events(th.ID), "effect.error", "handover") || hasEvent(h.events(th.ID), "handover", "") {
		t.Fatal("want effect.error and no handover event")
	}
	// The thread still works on its own profile.
	h.cmd("thread.send", th.ID, core.Args{"text": "still here"})
	h.wait(th.ID, "reply", func(t core.Thread, _ core.Attention) bool { return t.Summary == "echo: still here" })
}

func TestHandoverRefused(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("hello")
	if r := h.cmd("thread.handover", th.ID, core.Args{"profile": "nope"}); r.Status != 400 {
		t.Fatalf("unknown profile: %+v", r)
	}
	if r := h.cmd("thread.handover", th.ID, core.Args{"profile": "a"}); r.Status != 400 {
		t.Fatalf("own profile: %+v", r)
	}
	h.cmd("thread.send", th.ID, core.Args{"text": "hang"})
	h.wait(th.ID, "running", func(t core.Thread, _ core.Attention) bool { return t.Running })
	if r := h.cmd("thread.handover", th.ID, core.Args{"profile": "b"}); r.Status != 409 {
		t.Fatalf("while running: %+v", r)
	}
	h.cmd("thread.interrupt", th.ID, nil)
}

// A host restart in the middle ends the handover; the thread stays usable.
func TestHandoverClearedOnRestart(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	coretest.Patch(h.e, id, func(t *core.Thread, now time.Time) []core.Event {
		t.Handover = &core.Handover{To: "b", At: now}
		return nil
	})
	h.m.StopAll()
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	got, _ := h2.e.Thread(id)
	if got.Handover != nil || !hasEvent(h2.events(id), "effect.error", "handover") {
		t.Fatalf("thread %+v", got)
	}
}

// V0.handover-attachments: the conversation file lists the owner's
// attachments with their stored paths, so the new agent can Read them.
func TestConversationListsAttachments(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.Attach = h.att
	png, _ := h.att.Save("shot.png", "image/png", strings.NewReader("\x89PNG fake"))
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "see the screenshot", "attachments": []any{png.ID}})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	h.wait(r.ThreadID, "done", state(core.Done))
	conv, err := h.e.Conversation(r.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conv, "Attached: shot.png (image/png) at "+png.Path) {
		t.Fatalf("attachment path missing from the conversation:\n%s", conv)
	}
}

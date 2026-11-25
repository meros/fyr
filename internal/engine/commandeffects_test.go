package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/stream"
)

// effectAttention is what a row shows of a thread's attention: the part the
// web mock sets by hand for each command (V0.mock-contract).
type effectAttention struct {
	State   core.State   `json:"state"`
	Section core.Section `json:"section"`
	Group   core.Group   `json:"group,omitempty"`
	Word    string       `json:"word,omitempty"`
}

// commandEffect is one owner command on one thread as a real host does it.
type commandEffect struct {
	Name   string     `json:"name"`
	Action string     `json:"action"`
	Args   core.Args  `json:"args,omitempty"`
	Thread api.Thread `json:"thread"` // the thread before the command, as the host sends it
	// Projects: the host's projects before the command.
	Projects []api.Project   `json:"projects"`
	Before   effectAttention `json:"before"`
	// After: what the command's own change sent (its first upsert).
	After effectAttention `json:"after"`
	// Path, for a command that starts work (an agent's turn, a script):
	// each attention its upserts showed until the work ended, a repeat
	// left out; the last is where it rests.
	Path []effectAttention `json:"path,omitempty"`
}

func attentionOf(a core.Attention) effectAttention {
	return effectAttention{State: a.State, Section: a.Section, Group: a.Group, Word: a.Word}
}

// stableEffects makes the fixture the same on every run: ids, digests, session
// ids, times (in order; a far-off snooze stays), revs and the temporary folder.
func stableEffects(b []byte, dir string) []byte {
	s := strings.ReplaceAll(string(b), dir, "/home/owner/src/app")
	// Times keep their order (the mock reads activityAt against seenAt),
	// a second apart.
	timeRe := regexp.MustCompile(`"20[0-8]\d-\d\d-\d\dT[^"]*"`)
	var times []time.Time
	for _, m := range timeRe.FindAllString(s, -1) {
		if at, err := time.Parse(time.RFC3339Nano, strings.Trim(m, `"`)); err == nil {
			times = append(times, at)
		}
	}
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	times = slices.CompactFunc(times, time.Time.Equal)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s = timeRe.ReplaceAllStringFunc(s, func(m string) string {
		at, err := time.Parse(time.RFC3339Nano, strings.Trim(m, `"`))
		if err != nil {
			return m
		}
		i, _ := slices.BinarySearchFunc(times, at, func(a, b time.Time) int { return a.Compare(b) })
		return `"` + base.Add(time.Duration(i)*time.Second).Format(time.RFC3339) + `"`
	})
	s = regexp.MustCompile(`"(rev|seq|runEnds|durationMs)": \d+`).ReplaceAllString(s, `"$1": 1`)
	s = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`).ReplaceAllString(s, "00000000-0000-0000-0000-000000000000")
	digests := map[string]string{}
	s = regexp.MustCompile(`"digest": "[0-9a-f]+"`).ReplaceAllStringFunc(s, func(d string) string {
		if _, ok := digests[d]; !ok {
			digests[d] = fmt.Sprintf(`"digest": "digest-%d"`, len(digests)+1)
		}
		return digests[d]
	})
	ids := map[string]string{}
	s = regexp.MustCompile(`\b([tpdwm])_[0-9a-f]{8,}\b`).ReplaceAllStringFunc(s, func(id string) string {
		if _, ok := ids[id]; !ok {
			ids[id] = fmt.Sprintf("%s_%d", id[:1], len(ids)+1)
		}
		return ids[id]
	})
	return []byte(s)
}

// SPEC 0.5 V0.mock-contract (#457): the web mock is a second fyrd for the
// layout test, and it sets each command's attention by hand. This test runs
// the owner's commands on a real engine with the fake agent and writes, for
// each, the thread as the host sent it before, its attention then, what the
// command's own upsert showed and, for a command that starts work, every
// attention until that work ended, to web/test/fixtures/command-effects.json.
// The web test mock-contract.test.ts runs the same commands on the mock,
// seeded with the same thread, and asserts the same.
//
// The attentions come from the stream's upserts, not from reading the
// thread after the reply: an answer shows Seen until the agent takes it, and
// how long that is depends on the machine.
func TestCommandEffectsFixture(t *testing.T) {
	h := newHarness(t, time.Minute)
	var out []commandEffect
	// run sends one command and records it. work, when set, is what the
	// command starts: the test waits for it to end (between runs once the
	// reply is in) and records the path until then.
	run := func(id, name, action string, args core.Args, between func(), work func(core.Thread, core.Attention) bool) {
		t.Helper()
		before, projects := dtoOf(h.e, id), h.e.Projects()
		th, _ := h.e.Thread(id)
		ends := th.RunEnds
		ch := h.e.Hub.Subscribe()
		r := h.cmdID("fx-"+name, action, id, args)
		if !r.OK {
			t.Fatalf("%s: %+v", name, r)
		}
		if between != nil {
			between()
		}
		if work != nil {
			h.wait(id, name+": the work it started ended", func(t core.Thread, a core.Attention) bool { return t.RunEnds > ends && work(t, a) })
		}
		// The stream is behind the engine: apply commits a change and
		// publish sends its upsert after, without the lock, so the wait above
		// can see the work over before its last upsert is sent. Read up to
		// the upsert of the thread as it rests now, and nothing after it (a
		// clock tick), so the path ends at the same point on every run.
		want := dtoOf(h.e, id).Rev
		var frames []api.Thread
		timeout := time.After(8 * time.Second)
		for caught := false; !caught; {
			select {
			case f, ok := <-ch:
				if !ok {
					t.Fatalf("%s: the stream dropped the subscription", name)
				}
				var d api.Thread
				if f.Event != stream.FrameThread || json.Unmarshal(f.Data, &d) != nil || d.ID != id {
					continue
				}
				caught = d.Rev >= want
				// As a client reads them (#42): by rev, and none older than
				// the thread before the command (a late upsert of the turn
				// before).
				if d.Rev > before.Rev && d.Rev <= want {
					frames = append(frames, d)
				}
			case <-timeout:
				t.Fatalf("%s: no upsert of the thread at rev %d", name, want)
			}
		}
		h.e.Hub.Unsubscribe(ch)
		slices.SortStableFunc(frames, func(a, b api.Thread) int { return int(a.Rev - b.Rev) })
		var seen []effectAttention
		for _, d := range frames {
			if a := attentionOf(d.Attention); len(seen) == 0 || seen[len(seen)-1] != a {
				seen = append(seen, a)
			}
		}
		if len(seen) == 0 {
			t.Fatalf("%s: no upsert of the thread", name)
		}
		ce := commandEffect{Name: name, Action: action, Args: args, Thread: before, Projects: projects, Before: attentionOf(before.Attention), After: seen[0]}
		if work != nil {
			ce.Path = seen
		} else if last := attentionOf(dtoOf(h.e, id).Attention); len(seen) > 1 || seen[0] != last {
			t.Fatalf("%s: a mark moved on by itself: %+v, now %+v", name, seen, last)
		}
		out = append(out, ce)
	}
	idle := func(t core.Thread, a core.Attention) bool {
		for _, r := range t.Runs {
			if r.Running {
				return false
			}
		}
		return !t.Running && t.Starting == nil && a.State != core.Working
	}

	// One finished thread through the owner's marks, each from the last.
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	for _, c := range []struct {
		name, action string
		args         core.Args
	}{
		{"mark seen", "thread.markSeen", nil},
		{"mark unseen", "thread.markUnseen", nil},
		{"pin", "thread.pin", nil},
		{"unpin", "thread.unpin", nil},
		{"settle", "thread.settle", nil},
		{"unsettle", "thread.unsettle", nil},
		{"snooze", "thread.snooze", core.Args{"until": "2099-01-01T00:00:00Z"}},
		{"unsnooze", "thread.unsnooze", nil},
		{"archive", "thread.archive", nil},
		{"unarchive", "thread.unarchive", nil},
		{"pin then settle", "thread.pin", nil},
		{"settle a pinned thread", "thread.settle", nil},
		{"unsettle again", "thread.unsettle", nil},
	} {
		run(id, c.name, c.action, c.args, nil, nil)
	}
	r := h.cmd("project.create", "", core.Args{"name": "Acme"})
	if !r.OK {
		t.Fatalf("project: %+v", r)
	}
	pid := r.Result.(map[string]any)["id"].(string)
	run(id, "set project", "thread.setProject", core.Args{"projectId": pid}, nil, nil)
	run(id, "project archive", "project.archive", core.Args{"id": pid}, nil, nil)
	run(id, "project unarchive", "project.unarchive", core.Args{"id": pid}, nil, nil)

	// The same marks on a thread the owner has not read: snooze and settle
	// read it, archive does not, so only Unarchive brings it back unread.
	for _, m := range []string{"snooze", "archive", "settle"} {
		u := h.create("unread " + m)
		h.wait(u, "done", state(core.Done))
		var args core.Args
		if m == "snooze" {
			args = core.Args{"until": "2099-01-01T00:00:00Z"}
		}
		run(u, m+" an unread thread", "thread."+m, args, nil, nil)
		run(u, "un"+m+" a thread that was unread", "thread.un"+m, nil, nil, nil)
	}

	// A question the agent asked on a card; the answer starts its turn.
	q := h.create("x")
	h.wait(q, "done", state(core.Done))
	if out, isErr := callAs(t, h.e, q, "ask", map[string]any{"questions": []any{
		map[string]any{"question": "Which database?", "options": []any{map[string]any{"label": "Postgres"}, map[string]any{"label": "SQLite"}}},
	}}); isErr {
		t.Fatalf("ask: %s", out)
	}
	th, _ := h.e.Thread(q)
	run(q, "answer", "thread.answer", core.Args{"decisionId": th.Decisions[0].ID, "answers": map[string]any{"Which database?": "SQLite"}}, nil, idle)

	// A Run card the agent asked for; the owner's Run starts its script,
	// and its end tells the agent.
	c := h.create("x")
	h.wait(c, "done", state(core.Done))
	if out, isErr := callAs(t, h.e, c, "run_request", map[string]any{"command": "echo hi", "why": "Say hi"}); isErr {
		t.Fatalf("run_request: %s", out)
	}
	release := make(chan struct{})
	h.e.LiveScripts = func(ctx context.Context, dir string, a []string, out func(string)) (string, int, error) {
		<-release
		return "hi\n", 0, nil
	}
	th, _ = h.e.Thread(c)
	o := findOffer(th.Offers, "run-1")
	run(c, "run a Run card", "action.run", core.Args{"on": "thread", "action": o.ID, "digest": o.Digest}, func() { close(release) }, idle)

	b, err := json.MarshalIndent(map[string]any{"effects": out}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("..", "..", "web", "test", "fixtures", "command-effects.json"), append(stableEffects(b, h.dir), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

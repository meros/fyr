package front

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/claude/fake"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

func TestMain(m *testing.M) {
	if os.Getenv("FYR_FAKE_CLAUDE") == "1" {
		fake.Main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type handoverHost struct {
	id, name string
	work     string
	cfgDir   string
	hoDir    string
	eng      *engine.Engine
	mgr      *agent.Manager
	srv      *server.Server
	hs       *httptest.Server
}

func newHandoverHost(t *testing.T, name, id string) *handoverHost {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st)
	if err := eng.Load(); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work-"+name)
	cfgDir := filepath.Join(dir, "cfg-"+name)
	hoDir := filepath.Join(dir, "handover-"+name)
	os.MkdirAll(work, 0o700)
	os.MkdirAll(cfgDir, 0o700)
	envFake := []string{"FYR_FAKE_CLAUDE=1"}
	profiles := map[string]provider.Profile{
		"claude":   {ID: "claude", Name: "Claude", Binary: os.Args[0], Env: envFake, ConfigDir: cfgDir, Models: []string{"opus"}, DefaultModel: "opus"},
		"noresume": {ID: "noresume", Name: "NoResume", Binary: os.Args[0], Env: append([]string{"FYR_FAKE_NORESUME=1"}, envFake...), ConfigDir: cfgDir, Models: []string{"sonnet"}, DefaultModel: "sonnet"},
		"dead":     {ID: "dead", Name: "Dead", Binary: filepath.Join(dir, "missing-cli"), ConfigDir: cfgDir},
	}
	var mgr *agent.Manager
	mcpSrv := mcp.New(func(tok string) (string, bool) { return mgr.ThreadForToken(tok) })
	eng.Tools(mcpSrv)
	mcpHS := httptest.NewServer(mcpSrv)
	mgr = agent.New(eng, agent.Config{
		Profile:     func(pid string) provider.Profile { return profiles[pid] },
		IdleStop:    time.Minute,
		MCPURL:      mcpHS.URL + "/mcp",
		HandoverDir: hoDir,
	})
	eng.Agents = mgr
	eng.ProfileExists = func(pid string) bool { _, ok := profiles[pid]; return ok }
	srv := &server.Server{
		ID:             id,
		Host:           name,
		Engine:         eng,
		BuildID:        "test-build",
		Profiles:       []provider.Profile{profiles["claude"], profiles["noresume"], profiles["dead"]},
		HandoverPlan:   mgr.HandoverPlan,
		HandoverExport: mgr.HandoverExport,
		HandoverImport: mgr.HandoverIn,
	}
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		mgr.StopAll()
		mcpHS.Close()
		st.Close()
	})
	return &handoverHost{id: id, name: name, work: work, cfgDir: cfgDir, hoDir: hoDir, eng: eng, mgr: mgr, srv: srv, hs: hs}
}

func (h *handoverHost) createThread(t *testing.T, cwd, text string) core.Thread {
	t.Helper()
	if cwd == "" {
		cwd = h.work
	}
	r := h.eng.Command(apiCmd("mk-"+text, "thread.create", "", core.Args{"cwd": cwd, "text": text, "profile": "claude"}))
	if !r.OK {
		t.Fatalf("create thread on %s: %+v", h.name, r)
	}
	var got core.Thread
	eventually(t, "first run on "+h.name, func() bool {
		th, ok := h.eng.Thread(r.ThreadID)
		if !ok || th.Running || th.Summary == "" {
			return false
		}
		got = th
		return true
	})
	return got
}

func apiCmd(cid, action, tid string, args core.Args) (out struct {
	CommandID string    `json:"commandId"`
	Action    string    `json:"action"`
	ThreadID  string    `json:"threadId,omitempty"`
	Args      core.Args `json:"args,omitempty"`
}) {
	out.CommandID, out.Action, out.ThreadID, out.Args = cid, action, tid, args
	return
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// SPEC V0.trans-host-handover, decision 0039: Rung 1 transfers the session
// archive across hosts with different cwds, resumes on the target, copies
// history, settles source with link, and injects host-move + git limitations
// into the target agent's next turn.
func TestTransHostHandoverResumeAndInjectNotice(t *testing.T) {
	a := newHandoverHost(t, "host-a", "HOST-A")
	b := newHandoverHost(t, "host-b", "HOST-B")
	// Set up a real git repo on host-a with uncommitted & unpushed changes.
	runGitTest(t, a.work, "init", "-b", "main")
	os.WriteFile(filepath.Join(a.work, "a.txt"), []byte("v1\n"), 0o600)
	runGitTest(t, a.work, "add", ".")
	runGitTest(t, a.work, "commit", "-m", "init")
	runGitTest(t, a.work, "checkout", "-b", "feat/handover")
	os.WriteFile(filepath.Join(a.work, "a.txt"), []byte("v2\n"), 0o600)
	runGitTest(t, a.work, "commit", "-am", "unpushed")
	os.WriteFile(filepath.Join(a.work, "dirty.txt"), []byte("wip\n"), 0o600)

	f := newFront(t, HostConfig{Name: a.name, URL: a.hs.URL}, HostConfig{Name: b.name, URL: b.hs.URL})
	online(t, f.URL, 2)

	src := a.createThread(t, a.work, "remember the word orchid")

	// Plan reports resume, cli=claude, and dirty/unpushed git state without modifying git.
	var plan agent.Plan
	getDoc(t, f.URL+"/api/h/HOST-A/threads/"+src.ID+"/handover", &plan)
	if plan.Rung != agent.RungResume || plan.CLI != "claude" || plan.Git == nil || plan.Git.Branch != "feat/handover" || plan.Git.Dirty != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	code, rep := post(t, f.URL+"/api/front/command", fmt.Sprintf(
		`{"commandId":"ho-1","action":"front.threadHandover","args":{"fromHost":"HOST-A","threadId":%q,"toHost":"HOST-B","profile":"claude","cwd":%q}}`,
		src.ID, b.work,
	))
	if code != 200 || rep["ok"] != true {
		t.Fatalf("handover failed: %d %+v", code, rep)
	}
	res, _ := rep["result"].(map[string]any)
	if res["rung"] != "resume" {
		t.Fatalf("expected rung resume, got %+v", res)
	}
	targetRef, _ := res["threadId"].(string)
	targetID := strings.TrimPrefix(targetRef, "HOST-B/")
	if targetID == "" || targetID == targetRef {
		t.Fatalf("expected HOST-B/<id>, got %q", targetRef)
	}

	// Source thread is settled and linked forward; dirty file stayed untouched on host-a.
	srcAfter, _ := a.eng.Thread(src.ID)
	if srcAfter.Settle.At == nil || srcAfter.HandedOverTo == nil || srcAfter.HandedOverTo.HostID != "HOST-B" || srcAfter.HandedOverTo.ThreadID != targetID {
		t.Fatalf("source after handover: %+v", srcAfter)
	}
	if _, err := os.Stat(filepath.Join(a.work, "dirty.txt")); err != nil {
		t.Fatalf("dirty file must stay untouched on host-a: %v", err)
	}

	// Target thread has copied history, linked back, same resumed session id, and a queued Next host-move notice.
	dst, ok := b.eng.Thread(targetID)
	if !ok || dst.HandedOverFrom == nil || dst.HandedOverFrom.HostID != "HOST-A" || dst.HandedOverFrom.ThreadID != src.ID {
		t.Fatalf("target thread: ok=%v %+v", ok, dst)
	}
	if dst.SessionID != src.SessionID {
		t.Fatalf("expected resumed session %q, got %q", src.SessionID, dst.SessionID)
	}
	if len(dst.Feed.Queue) != 1 || dst.Feed.Queue[0].Urgency != core.Next || !strings.Contains(dst.Feed.Queue[0].What, "host-a") || !strings.Contains(dst.Feed.Queue[0].What, "1 uncommitted file") {
		t.Fatalf("expected host-move notice queued on target: %+v", dst.Feed.Queue)
	}
	dstEvs, _ := b.eng.Events(targetID, 0, 200)
	sawCopiedUser, sawHandover := false, false
	for _, ev := range dstEvs {
		if ev.Type == "user" && strings.Contains(string(ev.Data), "remember the word orchid") {
			sawCopiedUser = true
		}
		if ev.Type == "handover" && strings.Contains(string(ev.Data), `"fromHost":"host-a"`) {
			sawHandover = true
		}
	}
	if !sawCopiedUser || !sawHandover {
		t.Fatalf("target events missing copied history (%v) or handover divider (%v)", sawCopiedUser, sawHandover)
	}

	// Sending the next message on target delivers the queued host-move notice AND recalls prior resumed context!
	code, _ = post(t, f.URL+"/api/h/HOST-B/command", fmt.Sprintf(`{"commandId":"send-b","action":"thread.send","threadId":%q,"args":{"text":"recall"}}`, targetID))
	if code != 200 {
		t.Fatalf("send on target: %d", code)
	}
	eventually(t, "target responds with injected notice", func() bool {
		th, _ := b.eng.Thread(targetID)
		return !th.Running && strings.Contains(th.Summary, "Host handover:")
	})
}

// SPEC V0.trans-host-handover, decision 0039: Rung 2 (adopt) when target
// rejects resume; host-move notice is included in the adopt prompt.
func TestTransHostHandoverFallbackToAdopt(t *testing.T) {
	a := newHandoverHost(t, "host-a", "HOST-A")
	b := newHandoverHost(t, "host-b", "HOST-B")
	f := newFront(t, HostConfig{Name: a.name, URL: a.hs.URL}, HostConfig{Name: b.name, URL: b.hs.URL})
	online(t, f.URL, 2)

	src := a.createThread(t, a.work, "adopt me across hosts")
	code, rep := post(t, f.URL+"/api/front/command", fmt.Sprintf(
		`{"commandId":"ho-adopt","action":"front.threadHandover","args":{"fromHost":"HOST-A","threadId":%q,"toHost":"HOST-B","profile":"noresume","cwd":%q}}`,
		src.ID, b.work,
	))
	if code != 200 || rep["ok"] != true {
		t.Fatalf("adopt handover failed: %d %+v", code, rep)
	}
	res := rep["result"].(map[string]any)
	if res["rung"] != "adopt" {
		t.Fatalf("want rung adopt, got %+v", res)
	}
	targetID := strings.TrimPrefix(res["threadId"].(string), "HOST-B/")
	eventually(t, "target adopt turn includes host-move info", func() bool {
		th, _ := b.eng.Thread(targetID)
		return !th.Running && strings.Contains(th.Summary, "Host handover:") && strings.Contains(th.Summary, "host-a")
	})
}

// SPEC V0.trans-host-handover, decision 0039: if target fails to start, source
// stays active and unsettled and no orphan thread remains on target.
func TestTransHostHandoverFailureLeavesSourceUntouched(t *testing.T) {
	a := newHandoverHost(t, "host-a", "HOST-A")
	b := newHandoverHost(t, "host-b", "HOST-B")
	f := newFront(t, HostConfig{Name: a.name, URL: a.hs.URL}, HostConfig{Name: b.name, URL: b.hs.URL})
	online(t, f.URL, 2)

	src := a.createThread(t, a.work, "do not lose me")
	// Remove transcript so rung 1 skips and rung 2 tries to start "dead" -> fails.
	os.RemoveAll(filepath.Join(a.cfgDir, "projects"))
	code, rep := post(t, f.URL+"/api/front/command", fmt.Sprintf(
		`{"commandId":"ho-fail","action":"front.threadHandover","args":{"fromHost":"HOST-A","threadId":%q,"toHost":"HOST-B","profile":"dead","cwd":%q}}`,
		src.ID, b.work,
	))
	if code == 200 || rep["ok"] == true {
		t.Fatalf("expected failure for dead profile, got %d %+v", code, rep)
	}
	srcAfter, _ := a.eng.Thread(src.ID)
	if srcAfter.Settle.At != nil || srcAfter.HandedOverTo != nil {
		t.Fatalf("source must remain unsettled on target failure: %+v", srcAfter)
	}
	if got := b.eng.Snapshot(); len(got) != 0 {
		t.Fatalf("target must roll back orphan thread, got %d threads", len(got))
	}
}

// SPEC V0.trans-host-handover, decision 0039: Rung 3 (brief) when neither
// transcript nor conversation export is available.
func TestTransHostHandoverFallbackToBrief(t *testing.T) {
	a := newHandoverHost(t, "host-a", "HOST-A")
	b := newHandoverHost(t, "host-b", "HOST-B")
	f := newFront(t, HostConfig{Name: a.name, URL: a.hs.URL}, HostConfig{Name: b.name, URL: b.hs.URL})
	online(t, f.URL, 2)

	src := a.createThread(t, a.work, "brief across hosts")
	os.RemoveAll(filepath.Join(a.cfgDir, "projects"))
	// Make target handover dir a file so rung 2 cannot write its export, triggering needBrief -> rung 3!
	os.WriteFile(b.hoDir, []byte("not a dir"), 0o600)

	code, rep := post(t, f.URL+"/api/front/command", fmt.Sprintf(
		`{"commandId":"ho-brief","action":"front.threadHandover","args":{"fromHost":"HOST-A","threadId":%q,"toHost":"HOST-B","profile":"claude","cwd":%q}}`,
		src.ID, b.work,
	))
	if code != 200 || rep["ok"] != true {
		t.Fatalf("brief handover failed: %d %+v", code, rep)
	}
	res := rep["result"].(map[string]any)
	if res["rung"] != "brief" {
		t.Fatalf("want rung brief, got %+v", res)
	}
	targetID := strings.TrimPrefix(res["threadId"].(string), "HOST-B/")
	eventually(t, "target brief turn runs", func() bool {
		th, _ := b.eng.Thread(targetID)
		return !th.Running && strings.Contains(th.Summary, "Host handover:")
	})
}

// SPEC V0.trans-host-handover, decision 0039: worktree creation for a pushed
// branch when useWorktree=true on target host.
func TestTransHostHandoverCreatesWorktreeForPushedBranch(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "origin.git")
	os.MkdirAll(bare, 0o700)
	runGitTest(t, bare, "init", "--bare", "-b", "main")

	a := newHandoverHost(t, "host-a", "HOST-A")
	b := newHandoverHost(t, "host-b", "HOST-B")
	runGitTest(t, a.work, "clone", bare, ".")
	os.WriteFile(filepath.Join(a.work, "f.txt"), []byte("main\n"), 0o600)
	runGitTest(t, a.work, "add", ".")
	runGitTest(t, a.work, "commit", "-m", "init")
	runGitTest(t, a.work, "push", "-u", "origin", "main")
	runGitTest(t, a.work, "checkout", "-b", "feat/worktree-move")
	os.WriteFile(filepath.Join(a.work, "f.txt"), []byte("feat\n"), 0o600)
	runGitTest(t, a.work, "commit", "-am", "feat")
	runGitTest(t, a.work, "push", "-u", "origin", "feat/worktree-move")

	runGitTest(t, b.work, "clone", bare, ".")

	f := newFront(t, HostConfig{Name: a.name, URL: a.hs.URL}, HostConfig{Name: b.name, URL: b.hs.URL})
	online(t, f.URL, 2)

	src := a.createThread(t, a.work, "move with worktree")
	code, rep := post(t, f.URL+"/api/front/command", fmt.Sprintf(
		`{"commandId":"ho-wt","action":"front.threadHandover","args":{"fromHost":"HOST-A","threadId":%q,"toHost":"HOST-B","profile":"claude","cwd":%q,"useWorktree":true}}`,
		src.ID, b.work,
	))
	if code != 200 || rep["ok"] != true {
		t.Fatalf("worktree handover failed: %d %+v", code, rep)
	}
	res := rep["result"].(map[string]any)
	gotCwd, _ := res["cwd"].(string)
	if !strings.Contains(gotCwd, filepath.Join(".claude", "worktrees", "feat-worktree-move")) {
		t.Fatalf("expected target worktree path, got %q (res=%+v)", gotCwd, res)
	}
	if branch := runGitTest(t, gotCwd, "branch", "--show-current"); branch != "feat/worktree-move" {
		t.Fatalf("target worktree branch %q, want feat/worktree-move", branch)
	}
	// Ensure HTTP method guard rejects non-POST on export/import.
	resp, err := http.Get(a.hs.URL + "/api/threads/" + src.ID + "/handover/export")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("GET on export must not succeed, got %d", resp.StatusCode)
	}
}

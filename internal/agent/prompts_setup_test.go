package agent

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

// V0.prompt-fits-setup: the prompt leaves out what does not apply to the
// setup the agent starts in.
func TestPromptFitsSetup(t *testing.T) {
	merge := `mcp__fyr__change_request {change, ask: "merge"}`
	limit := "GitHub's API limit"
	openPR := "When you open a PR some other way than gh pr create"
	pkexec := "(Fyr runs it through pkexec)"
	cases := []struct {
		name      string
		s         Setup
		want, not []string
	}{
		{"full", FullSetup, []string{merge, limit, openPR, pkexec, "change_open {title, branch, workspace: true"}, []string{promptNoGitHub, promptNoGit, promptRootNoPkexec, "macOS"}},
		{"git without a GitHub remote", Setup{Git: true, OS: "linux", Pkexec: true},
			[]string{promptNoGitHub, pkexec, "change_open {title, branch, workspace: true"}, []string{merge, limit, openPR, "never run gh pr merge"}},
		{"no git, gh signed in", Setup{OS: "linux", Pkexec: true}, []string{promptNoGit, merge, limit}, []string{promptNoGitHub}},
		{"no git, gh not signed in", Setup{GH: "gh is not signed in on this host", OS: "linux", Pkexec: true}, []string{promptNoGit}, []string{merge, limit, openPR}},
		{"GitHub repo, gh not signed in", Setup{Git: true, GitHub: true, GH: "gh is not signed in on this host", OS: "linux", Pkexec: true}, []string{merge, limit}, nil},
		{"darwin", Setup{Git: true, GitHub: true, OS: "darwin"}, []string{promptDarwin, promptRootNoPkexec}, []string{pkexec}},
		{"linux without pkexec", Setup{Git: true, GitHub: true, OS: "linux"}, []string{promptRootNoPkexec, "pass root: true instead of sudo"}, []string{pkexec, "macOS", "This host runs"}},
		{"other OS", Setup{Git: true, GitHub: true, OS: "freebsd"}, []string{"This host runs freebsd, not Linux"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Prompt(c.s)
			for _, w := range c.want {
				if !strings.Contains(p, w) {
					t.Errorf("lacks %q", w)
				}
			}
			for _, w := range c.not {
				if strings.Contains(p, w) {
					t.Errorf("has %q", w)
				}
			}
			for _, l := range strings.Split(p, "\n")[1:] {
				if !strings.HasPrefix(l, "- ") {
					t.Errorf("a line that is not a rule: %q", l)
				}
			}
		})
	}
}

// V0.prompt-fits-setup: gh's answer is the reason, and a timeout says
// nothing.
func TestGHWhy(t *testing.T) {
	if w := ghWhy(nil, false); w != "" {
		t.Errorf("signed in: %q", w)
	}
	if w := ghWhy(&exec.Error{Name: "gh", Err: exec.ErrNotFound}, false); w != "gh is not installed on this host" {
		t.Errorf("missing: %q", w)
	}
	if w := ghWhy(errors.New("exit status 1"), false); w != "gh is not signed in on this host" {
		t.Errorf("signed out: %q", w)
	}
	if w := ghWhy(errors.New("killed"), true); w != "" {
		t.Errorf("timeout: %q", w)
	}
}

// V0.prompt-fits-setup: git and a GitHub remote are read from the folder.
func TestGitRemotes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	run := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	plain := t.TempDir()
	if g, h := gitRemotes(plain); g || h {
		t.Errorf("a plain folder: git %v github %v", g, h)
	}
	local := t.TempDir()
	run(local, "init", "-q")
	run(local, "remote", "add", "origin", "git@gitlab.com:o/r.git")
	if g, h := gitRemotes(local); !g || h {
		t.Errorf("a GitLab repo: git %v github %v", g, h)
	}
	run(local, "remote", "add", "up", "https://github.com/o/r")
	if g, h := gitRemotes(local); !g || !h {
		t.Errorf("a repo with a GitHub remote: git %v github %v", g, h)
	}
}

// setupHost records the setup notices a start sends.
type setupHost struct {
	feedHost
	mu2   sync.Mutex
	lines []string
}

func (h *setupHost) Apply(in core.Input) (any, error) {
	if in.Kind == core.KindSetup {
		h.mu2.Lock()
		h.lines = append(h.lines, in.Args.(string))
		h.mu2.Unlock()
		return nil, nil
	}
	return h.feedHost.Apply(in)
}

func (h *setupHost) said() []string {
	h.mu2.Lock()
	defer h.mu2.Unlock()
	return append([]string(nil), h.lines...)
}

// V0.prompt-fits-setup: an agent in a GitHub repository on a host where gh
// is not signed in is told once, in its feed; elsewhere it is not.
func TestStartTellsGHNotSignedIn(t *testing.T) {
	for _, c := range []struct {
		s    Setup
		told bool
	}{
		{Setup{Git: true, GitHub: true, GH: "gh is not signed in on this host", OS: "linux"}, true},
		{Setup{Git: true, GH: "gh is not signed in on this host", OS: "linux"}, false},
		{Setup{Git: true, GitHub: true, OS: "linux"}, false},
	} {
		h := &setupHost{feedHost: feedHost{cwd: t.TempDir()}}
		m := New(h, Config{
			Profile: func(string) provider.Profile {
				return provider.Profile{ID: "fake", Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"}}
			},
			MCPURL: "http://127.0.0.1:1/mcp",
			Setup:  func(string) Setup { return c.s },
		})
		if err := m.Do("t1", core.Effect{Kind: core.EffectSend, Text: "hi"}); err != nil {
			t.Fatal(err)
		}
		if !waitFor(8*time.Second, func() bool { s, _, _ := h.state(); return s != "" }) {
			t.Fatal("the run did not end")
		}
		if c.told {
			waitFor(5*time.Second, func() bool { return len(h.said()) > 0 }) // sent beside the start
		}
		m.StopAll()
		lines := h.said()
		if c.told && (len(lines) != 1 || !strings.HasPrefix(lines[0], "gh is not signed in on this host, so Fyr cannot read")) {
			t.Errorf("%+v: told %q", c.s, lines)
		}
		if !c.told && len(lines) != 0 {
			t.Errorf("%+v: told %q", c.s, lines)
		}
	}
}

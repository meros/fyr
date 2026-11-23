package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/gitdiff"
	"github.com/meros/fyr/internal/proc"
)

// The setup an agent starts in (V0.prompt-fits-setup): read when its
// process starts, so the prompt fits this host and this folder.

// HostSetup reads the setup of the folder dir on this host: git and its
// remotes, gh's sign-in (cached for a few minutes, it is the same for every
// thread), the OS and pkexec.
func HostSetup(dir string) Setup {
	s := Setup{OS: runtime.GOOS, Pkexec: proc.PkexecInstalled(), GH: ghProblem()}
	s.Git, s.GitHub = gitRemotes(dir)
	s.Zone = ZoneLabel(time.Now())
	return s
}

// ZoneLabel names the host's local time zone at t (V0.agent-local-time):
// "Europe/Stockholm (CET, UTC+01:00)". The IANA name is time.Local's when it
// has one, else the TZ variable's, else the target of /etc/localtime; with
// none of them the abbreviation and offset stand alone.
func ZoneLabel(t time.Time) string {
	abbr, off := t.In(time.Local).Zone()
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	detail := fmt.Sprintf("%s, UTC%s%02d:%02d", abbr, sign, off/3600, off%3600/60)
	name := time.Local.String()
	if name == "Local" || name == "" {
		name = os.Getenv("TZ")
		if name == "" {
			if l, err := os.Readlink("/etc/localtime"); err == nil {
				if _, after, ok := strings.Cut(l, "zoneinfo/"); ok {
					name = after
				}
			}
		}
	}
	if name == "" || name == "Local" {
		return detail
	}
	return name + " (" + detail + ")"
}

// gitRemotes reports whether dir is in a git repository, and whether one
// of its remotes is on github.com.
func gitRemotes(dir string) (git, github bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := gitdiff.Output(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return false, false
	}
	out, _ := gitdiff.Output(ctx, dir, "remote", "-v")
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && strings.HasPrefix(NormalizeRemote(f[1]), "github.com/") {
			return true, true
		}
	}
	return true, false
}

var ghAuth struct {
	sync.Mutex
	at  time.Time
	why string
}

// ghProblem is why gh cannot reach GitHub on this host, or "": `gh auth
// token` reads the stored sign-in without a call to GitHub.
func ghProblem() string {
	ghAuth.Lock()
	defer ghAuth.Unlock()
	if !ghAuth.at.IsZero() && time.Since(ghAuth.at) < 5*time.Minute {
		return ghAuth.why
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Run()
	ghAuth.why, ghAuth.at = ghWhy(err, ctx.Err() != nil), time.Now()
	return ghAuth.why
}

// ghWhy turns the result of `gh auth token` into the reason, or "". A
// timeout says nothing: gh may well be signed in.
func ghWhy(err error, timedOut bool) string {
	switch {
	case err == nil || timedOut:
		return ""
	case errors.Is(err, exec.ErrNotFound):
		return "gh is not installed on this host"
	}
	return "gh is not signed in on this host"
}

// GHNotice is the [Fyr] line an agent in a GitHub repository gets when gh
// cannot reach GitHub here (V0.prompt-fits-setup).
func GHNotice(why string) string {
	return why + ", so Fyr cannot read your pull requests or CI, and cannot merge them; gh calls of yours fail too. Ask the owner to sign in with gh auth login on this host (mcp__fyr__ask), and go on with what needs no GitHub."
}

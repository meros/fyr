package engine

// Run requests (SPEC V0.149): run_request, the live runner that streams a
// script's output into its run, Stop, and root through pkexec.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/proc"
)

// LiveScriptRunner runs a script and calls out with its output so far as
// it grows; tests replace it.
type LiveScriptRunner func(ctx context.Context, dir string, argv []string, out func(string)) (string, int, error)

func (e *Engine) runRequestTool(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "run_request",
		Description: "Ask the owner to run a command: Fyr shows a Run card in this thread (and in Needs you) with the full command, this host, the folder and why. The owner presses Run (or Run as root), Copy or Not now; Fyr runs exactly the command shown with bash -euo pipefail -c on this host, streams the output into the card, and sends you the exit code and the output's tail as a [Fyr] message. Use it whenever you would otherwise write \"run this\" or a `! cmd` block. Put several commands in one command (one script). Never write sudo: pass root: true and Fyr runs the whole script as root through pkexec, whose password prompt shows on this host's screen.",
		Schema: obj(map[string]any{
			"command":    str("The shell text to run, exactly as the owner sees it. Several lines are one script; it stops at the first error."),
			"why":        str("One line the owner reads before pressing Run: what it does and why you need it."),
			"root":       map[string]any{"type": "boolean", "description": "Run the whole script as root through pkexec (instead of sudo)."},
			"cwd":        str("The folder to run in; default this thread's folder."),
			"timeoutSec": map[string]any{"type": "integer", "description": "The time limit, default 600, at most 3600."},
			"id":         str("Optional: the same id again replaces that request."),
			"optional":   map[string]any{"type": "boolean", "description": "Nothing waits on it: a chip under your newest message instead of a card in the owner's dock and Your move, gone when the next turn starts if it did not run."},
			"alongside":  map[string]any{"type": "boolean", "description": "Keep your earlier cards that the owner has not run yet. Without it this card replaces them, so the owner never runs a command you have moved past. Set it only when the owner must run both."},
		}, "command", "why"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			req := core.RunRequestArgs{ID: sArg(a, "id"), Command: sArg(a, "command"), Why: sArg(a, "why"), Cwd: sArg(a, "cwd")}
			req.Root, _ = a["root"].(bool)
			req.Optional, _ = a["optional"].(bool)
			req.Alongside, _ = a["alongside"].(bool)
			if n, ok := a["timeoutSec"].(float64); ok {
				req.TimeoutSec = int(n)
			}
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindRunRequest, Thread: th, Args: req})
			if err != nil {
				return "", err
			}
			// V0.run-card-replaces: say which earlier cards this one replaced.
			replaced := ""
			if rep, ok := out.(core.OfferReply); ok && len(rep.Replaced) > 0 {
				qs := make([]string, len(rep.Replaced))
				for i, w := range rep.Replaced {
					qs[i] = fmt.Sprintf("%q", w)
				}
				replaced = fmt.Sprintf(" It replaced your earlier cards that the owner had not run (%s): they are gone, so put everything the owner must run now in this one.", strings.Join(qs, ", "))
			}
			how := "Run"
			if req.Root {
				how = "Run as root"
				if why := e.rootPrompt(); why != "" {
					how += fmt.Sprintf(" (it cannot run as root here now: %s; the card tells the owner to copy it)", why)
				}
			}
			if req.Optional {
				return fmt.Sprintf("The owner sees it as a chip under your newest message, with %s, until the next turn starts. If they run it, the exit code and the output arrive as a message starting with [Fyr].", how), nil
			}
			return fmt.Sprintf("The owner sees a Run card with %s. When they run it, the exit code and the output arrive as a message starting with [Fyr] (a run that lasts over 10 s also tells you once when it started); if they choose Not now, you hear that too.%s End your turn or do other work meanwhile.", how, replaced), nil
		}})
}

// runScriptLive runs argv in dir and calls out with the whole output so
// far, at most once a second and once at the end.
func runScriptLive(ctx context.Context, dir string, argv []string, out func(string)) (string, int, error) {
	cmd := proc.Command(ctx, argv[0], argv[1:]...) // #39: the limit stops what the script started too
	cmd.Dir = dir
	var mu sync.Mutex
	var b bytes.Buffer
	w := lockedWriter{mu: &mu, b: &b}
	cmd.Stdout, cmd.Stderr = w, w
	snapshot := func() string { mu.Lock(); defer mu.Unlock(); return b.String() }
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		last := ""
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if s := snapshot(); s != last && out != nil {
					last = s
					out(s)
				}
			}
		}
	}()
	err := cmd.Run()
	close(done)
	exit := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		exit = ee.ExitCode()
	} else if err != nil {
		exit = -1
		fmt.Fprintf(w, "\n%v", err)
	}
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(w, "\nstopped after the time limit")
	}
	s := snapshot()
	if out != nil {
		out(s)
	}
	return s, exit, err
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// rootArgv runs argv as root through pkexec, in dir, with this host's PATH
// (pkexec starts from a minimal environment and its own folder).
func rootArgv(dir string, argv []string, env, path string) []string {
	return append([]string{"pkexec", env, "--chdir=" + dir, "PATH=" + path}, argv...)
}

// pressKey names one running press.
func pressKey(thread, on, action string) string { return thread + "\x00" + on + "\x00" + action }

type pressRun struct {
	cancel  context.CancelFunc
	stopped bool
}

// runPress runs a script press: root through pkexec, its output streamed
// into the run, stoppable by action.stop.
func (e *Engine) runPress(threadID string, ef core.Effect) (string, int, error) {
	limit := time.Duration(ef.Script.TimeoutSec) * time.Second
	if limit == 0 {
		limit = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	argv := ef.Script.Argv
	if !ef.Script.Root && e.FolderEnv != nil { // V0.run-folder-env
		argv = e.FolderEnv(ef.Script.Dir, argv)
	}
	if ef.Script.Root {
		env, err := exec.LookPath("env")
		if err != nil {
			env = "/usr/bin/env"
		}
		argv = rootArgv(ef.Script.Dir, argv, env, os.Getenv("PATH"))
	}
	var r *pressRun
	if ef.Press != nil {
		r = &pressRun{cancel: cancel}
		k := pressKey(threadID, ef.Press.On, ef.Press.Action.ID)
		e.pressMu.Lock()
		if e.presses == nil {
			e.presses = map[string]*pressRun{}
		}
		e.presses[k] = r
		e.pressMu.Unlock()
		defer func() {
			e.pressMu.Lock()
			if e.presses[k] == r {
				delete(e.presses, k)
			}
			e.pressMu.Unlock()
		}()
	}
	if ef.Press != nil && ef.Press.Action.Request { // V0.run-long-notice
		after := e.LongRunAfter
		if after == 0 {
			after = core.LongRunAfter
		}
		t := time.AfterFunc(after, func() {
			e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindActionLong, Thread: threadID,
				Args: core.ActionLongArgs{On: ef.Press.On, Action: ef.Press.Action.ID}})
		})
		defer t.Stop()
	}
	var out string
	var exit int
	var err error
	if e.Scripts != nil {
		out, exit, err = e.Scripts(ctx, ef.Script.Dir, argv)
	} else {
		live := e.LiveScripts
		if live == nil {
			live = runScriptLive
		}
		var feed func(string)
		if ef.Press != nil {
			feed = func(s string) {
				e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindActionOutput, Thread: threadID,
					Args: core.ActionOutputArgs{On: ef.Press.On, Action: ef.Press.Action.ID, Output: s}})
			}
		}
		out, exit, err = live(ctx, ef.Script.Dir, argv, feed)
	}
	if r != nil {
		e.pressMu.Lock()
		stopped := r.stopped
		e.pressMu.Unlock()
		if stopped {
			out = strings.TrimRight(out, "\n") + "\n" + core.StoppedText
			if err == nil {
				err = errors.New(core.StoppedText)
			}
		}
	}
	return out, exit, err
}

// stopPress stops a running press (action.stop).
func (e *Engine) stopPress(threadID string, p *core.Press) {
	e.pressMu.Lock()
	defer e.pressMu.Unlock()
	if r := e.presses[pressKey(threadID, p.On, p.Action.ID)]; r != nil {
		r.stopped = true
		r.cancel()
	}
}

// rootPrompt is why a root request cannot ask for the owner's password on
// this host, or "" when it can (V0.149 (4)). Read at most every 30 s.
func (e *Engine) rootPrompt() string {
	if e.RootPrompt != nil {
		return e.RootPrompt()
	}
	e.pressMu.Lock()
	defer e.pressMu.Unlock()
	if time.Since(e.rootAt) < 30*time.Second && !e.rootAt.IsZero() {
		return e.rootWhy
	}
	e.rootWhy, e.rootAt = rootPromptFrom(myProcessNames(), proc.PkexecInstalled()), time.Now()
	return e.rootWhy
}

// rootPromptFrom decides from the command lines of the owner's processes
// (the program's base name, then its arguments) and whether pkexec is
// installed. An agent is a process whose command line names polkit and an
// agent ("polkit-gnome-authentication-agent-1", "swaypplet polkit-agent"),
// or lxpolkit; polkitd and the helper that runs during a prompt are not.
func rootPromptFrom(cmdlines []string, pkexec bool) string {
	if !pkexec {
		return "pkexec is not installed"
	}
	for _, c := range cmdlines {
		c = strings.ToLower(c)
		prog, _, _ := strings.Cut(c, " ")
		if strings.HasPrefix(prog, "polkit-agent-helper") || prog == "polkitd" {
			continue
		}
		if (strings.Contains(c, "polkit") && (strings.Contains(c, "agent") || strings.Contains(c, "auth"))) || prog == "lxpolkit" {
			return ""
		}
	}
	return "no polkit authentication agent runs in your session"
}

// myProcessNames are the command lines of this user's processes, from
// /proc (Linux), the program as its base name; none elsewhere.
func myProcessNames() []string {
	uid := os.Getuid()
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	var out []string
	for _, d := range dirs {
		st, err := os.Stat(d)
		if err != nil || !ownedBy(st, uid) {
			continue
		}
		b, err := os.ReadFile(d + "/cmdline")
		if err != nil || len(b) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		args[0] = filepath.Base(args[0])
		out = append(out, strings.Join(args, " "))
	}
	return out
}

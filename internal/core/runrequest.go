package core

// Run requests (SPEC V0.149): a command the agent wants the owner to run is
// a thread action with the exact shell text, why and root, shown as a Run
// card. The owner's press runs it (action.run), Stop stops it
// (action.stop), Not now removes it (action.dismiss); its output streams
// into its run (action.output). An agent message that asks the owner to
// type a command gets one notice that names the tool.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	KindRunRequest    = "run.request"
	KindActionStop    = "action.stop"
	KindActionDismiss = "action.dismiss"
	KindActionOutput  = "action.output"

	// EffectScriptStop stops the running script of a press.
	EffectScriptStop EffectKind = "script.stop"

	maxCommand = 8000
	maxWhy     = 200
	// TailForAgent is how much of a request's output the agent's notice
	// carries.
	TailForAgent = 1500
	// StoppedText ends the output of a run the owner stopped.
	StoppedText = "stopped by the owner"
)

// RunRequestArgs are run_request's.
type RunRequestArgs struct {
	ID         string `json:"id,omitempty"`
	Command    string `json:"command"`
	Why        string `json:"why"`
	Root       bool   `json:"root,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

// ActionOutputArgs are action.output's: the output so far of a running
// press.
type ActionOutputArgs struct {
	On     string `json:"on"`
	Action string `json:"action"`
	Output string `json:"output"`
}

// RunArgv is how a request's command runs: the shell text exactly, with
// errors stopping it.
func RunArgv(command string) []string {
	return []string{"bash", "-euo", "pipefail", "-c", command}
}

var sudoWord = regexp.MustCompile(`(^|[\s;&|(]|\$\()sudo(\s|$)`)

func checkRequest(a RunRequestArgs) error {
	cmd := strings.TrimSpace(a.Command)
	switch {
	case cmd == "":
		return errors.New("a run request needs a command: the shell text the owner runs")
	case len(a.Command) > maxCommand:
		return fmt.Errorf("the command is longer than %d characters; put it in a script file and run that", maxCommand)
	case strings.TrimSpace(a.Why) == "":
		return errors.New("a run request needs why: one line the owner reads before pressing Run")
	case len(a.Why) > maxWhy:
		return fmt.Errorf("why is longer than %d characters; one line", maxWhy)
	case sudoWord.MatchString(a.Command) && a.Root:
		return errors.New("Drop sudo: with root: true Fyr runs the whole script as root through pkexec")
	case sudoWord.MatchString(a.Command):
		return errors.New("sudo cannot ask for a password in Fyr. Drop sudo and pass root: true: Fyr runs the whole script as root through pkexec, after the owner's press")
	case a.TimeoutSec < 0 || a.TimeoutSec > maxScriptSec:
		return fmt.Errorf("timeoutSec must be 0 to %d", maxScriptSec)
	}
	return nil
}

// IsRequest: the action is a run request.
func (a ActionSpec) IsRequest() bool { return a.Request }

func reduceRunRequest(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[RunRequestArgs](in)
	if err != nil {
		return Result{}, err
	}
	if err := checkRequest(a); err != nil {
		return Result{}, err
	}
	t := r.T
	h, _ := HolderOf(t, OnThread)
	id := strings.TrimSpace(a.ID)
	if id == "" {
		n := 1
		for {
			id = fmt.Sprintf("run-%d", n)
			if h.Find(id) == nil && lastRunOf(h.Runs, id, OnThread) == nil {
				break
			}
			n++
		}
	}
	cwd := a.Cwd
	if cwd == "" {
		cwd = t.Cwd
	}
	label := "Run"
	if a.Root {
		label = "Run as root"
	}
	spec := ActionSpec{ID: id, Label: label, Run: RunScript, Argv: RunArgv(a.Command), Cwd: cwd, TimeoutSec: a.TimeoutSec,
		Request: true, Command: a.Command, Why: strings.TrimSpace(a.Why), Root: a.Root}
	if err := h.declare(spec, true); err != nil {
		return Result{}, err
	}
	t.ActivityAt = now
	return Result{Reply: OfferReply{Label: label, Where: OnThread}}, nil
}

// PendingRequest is the thread's oldest run request that never ran, or
// nil: it puts the thread in Needs you (V0.149 (5)).
func PendingRequest(t *Thread) *ActionSpec {
	for i := range t.Offers {
		a := &t.Offers[i]
		if a.Request && lastRunOf(t.Runs, a.ID, OnThread) == nil {
			return a
		}
	}
	return nil
}

func reduceStop(r Rec, in Input, now time.Time) (Result, error) {
	args := mapArgs(in)
	h, err := HolderOf(r.T, args.Str("on"))
	if err != nil {
		return Result{}, &Rejection{Code: RejectNotFound, Reason: err.Error()}
	}
	act := h.Find(args.Str("action"))
	if act == nil {
		return Result{}, reject(RejectNotFound, "no action %s", args.Str("action"))
	}
	if act.Run != RunScript {
		return Result{}, reject(RejectUnavailable, "Only a script can be stopped")
	}
	run := lastRunOf(h.Runs, act.ID, h.On)
	if run == nil || !run.Running {
		return Result{}, reject(RejectUnavailable, "It is not running")
	}
	on := h.On
	if h.Change != nil {
		on = h.Change.ID
	}
	return Result{Effects: []Effect{{Kind: EffectScriptStop, Press: &Press{On: on, Action: *act}}}}, nil
}

func reduceDismiss(r Rec, in Input, now time.Time) (Result, error) {
	args := mapArgs(in)
	h, err := HolderOf(r.T, args.Str("on"))
	if err != nil {
		return Result{}, &Rejection{Code: RejectNotFound, Reason: err.Error()}
	}
	act := h.Find(args.Str("action"))
	if act == nil {
		return Result{}, reject(RejectNotFound, "no action %s", args.Str("action"))
	}
	if !act.Request {
		return Result{}, reject(RejectUnavailable, "Only a run request has Not now")
	}
	if run := lastRunOf(h.Runs, act.ID, h.On); run != nil && run.Running {
		return Result{}, reject(RejectUnavailable, "It is running; stop it first")
	}
	a := *act
	ran := lastRunOf(h.Runs, a.ID, h.On) != nil
	h.Offers = deleteOffer(h.Offers, a.ID)
	return Result{Events: []Event{{"action", map[string]any{"on": h.On, "action": a.ID, "label": a.Label, "why": a.Why, "command": a.Command, "request": true, "dismissed": true, "ran": ran, "by": "owner"}}}}, nil
}

func deleteOffer(list []ActionSpec, id string) []ActionSpec {
	out := list[:0:0]
	for _, a := range list {
		if a.ID != id {
			out = append(out, a)
		}
	}
	return out
}

func reduceOutput(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[ActionOutputArgs](in)
	if err != nil {
		return Result{}, err
	}
	h, err := HolderOf(r.T, a.On)
	if err != nil {
		return Result{}, nil // the holder went away: nothing to show it on
	}
	for i := len(h.Runs) - 1; i >= 0; i-- {
		if run := &h.Runs[i]; run.Action == a.Action && run.Running {
			run.Output = TruncTail(a.Output, 4096)
			break
		}
	}
	return Result{}, nil
}

func init() {
	RegisterKind(&InputKind{Name: KindRunRequest, From: agentOnly, Reduce: reduceRunRequest})
	RegisterKind(&InputKind{Name: KindActionStop, From: ownerOnly, Reduce: reduceStop})
	RegisterKind(&InputKind{Name: KindActionDismiss, From: ownerOnly, Reduce: reduceDismiss})
	RegisterKind(&InputKind{Name: KindActionOutput, From: observed, Untouched: true, Reduce: reduceOutput})
}

// requestResultText is the agent's notice of a run request's result: the
// exit code and the output's tail (V0.149 (6)).
func requestResultText(why string, root, ok bool, exit int, out string) string {
	how := "ran"
	if root {
		how = "ran as root"
	}
	status := "it succeeded (exit 0)"
	if !ok {
		status = fmt.Sprintf("it failed (exit %d)", exit)
	}
	s := fmt.Sprintf("The owner %s your run request %q: %s.", how, why, status)
	out = strings.TrimSpace(out)
	if out == "" {
		return s + " It printed nothing."
	}
	tail := TruncTail(out, TailForAgent)
	if len(tail) < len(out) {
		s += " The output's last lines:"
	} else {
		s += " The output:"
	}
	return s + "\n```\n" + strings.ReplaceAll(tail, "```", "'''") + "\n```"
}

// The prose commands of an agent message (V0.149 (8)): a fenced block whose
// lines start with "! " (Claude Code's convention), or a shell block after
// a line that asks the owner to run it. The client finds the same blocks
// (web/src/runcard.ts).
var (
	fence    = regexp.MustCompile("(?m)^[ \t]*```([A-Za-z0-9_+-]*)[^\\n]*\\n([\\s\\S]*?)\\n?[ \t]*```[ \t]*$")
	shellish = map[string]bool{"sh": true, "bash": true, "shell": true, "console": true, "zsh": true}
	askRun   = regexp.MustCompile(`(?i)\b(run this|run these|please run|can you run|could you run|you need to run|you'll need to run|you will need to run|run the following|run:)`)
)

// ProseCommands are the commands an agent message asks the owner to run,
// each block as one script, "! " dropped.
func ProseCommands(text string) []string {
	var out []string
	for _, m := range fence.FindAllStringSubmatchIndex(text, -1) {
		lang, body := text[m[2]:m[3]], text[m[4]:m[5]]
		lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
		bang := len(lines) > 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" && !strings.HasPrefix(strings.TrimLeft(l, " \t"), "! ") {
				bang = false
			}
		}
		if bang {
			var cmd []string
			for _, l := range lines {
				if s := strings.TrimSpace(l); s != "" {
					cmd = append(cmd, strings.TrimPrefix(s, "! "))
				}
			}
			out = append(out, strings.Join(cmd, "\n"))
			continue
		}
		if shellish[strings.ToLower(lang)] && askRun.MatchString(lastLines(text[:m[0]], 2)) {
			out = append(out, strings.TrimSpace(body))
		}
	}
	return out
}

// lastLines are the last n lines of s that are not empty.
func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}

// ProseCommandText is the notice of an agent message that asks the owner
// to run a command.
const ProseCommandText = "You asked the owner to run a command in your message. The owner cannot run it there, least of all on the phone. Call mcp__fyr__run_request {command, why, root?} instead: Fyr shows a Run card and sends you the exit code and the output. Put several commands in one script; use root: true instead of sudo."

// runRequestNotices: an agent message with a prose command (V0.149 (8)).
func runRequestNotices(r Reduced) []Notice {
	if r.In.Kind != KindDigest {
		return nil
	}
	for _, ev := range r.Events {
		if ev.Type != "msg" {
			continue
		}
		if len(ProseCommands(assistantText(ev.Data))) > 0 {
			return []Notice{{About: "thread", Key: "prose-command", What: ProseCommandText}}
		}
	}
	return nil
}

// assistantText is the text of an assistant message in the CLI's stream.
func assistantText(data any) string {
	m, _ := data.(map[string]any)
	if m == nil || m["type"] != "assistant" {
		return ""
	}
	msg, _ := m["message"].(map[string]any)
	content, _ := msg["content"].([]any)
	var b strings.Builder
	for _, c := range content {
		if block, _ := c.(map[string]any); block != nil && block["type"] == "text" {
			s, _ := block["text"].(string)
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// pressData is an action event's data: the press, or with ended its
// result. A run request's carries its command, why and root, so the
// thread's log records what ran (V0.149 (6)).
func pressData(on, title string, a ActionSpec, more map[string]any) map[string]any {
	d := map[string]any{"on": on, "title": title, "action": a.ID, "label": a.Label, "run": a.Run}
	if more["ended"] != true {
		d["argv"] = a.Argv
	}
	if a.Request {
		d["request"], d["command"], d["why"], d["root"] = true, a.Command, a.Why, a.Root
	}
	for k, v := range more {
		d[k] = v
	}
	return d
}

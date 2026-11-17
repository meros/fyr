package core

import (
	"regexp"
	"time"
)

// GateRule denies a generic tool call that Fyr owns and names the Fyr tool
// to use instead (SPEC 12.3 step 2, 0.5 V0.12). The reason reaches the model.
type GateRule struct {
	Tool    string
	Pattern *regexp.Regexp
	Reason  string
}

var GateRules = []GateRule{
	{Tool: "Bash", Pattern: regexp.MustCompile(`(^|[;&|(\s])gh\s+pr\s+merge(\s|$)`),
		Reason: "Fyr owns merges. Use mcp__fyr__effect_request with action pr.merge and args {pr, method}; the user approves it and Fyr reports the result."},
}

// Gate returns a deny reason for a tool call, or "" to allow it.
func Gate(tool string, input map[string]any) string {
	cmd, _ := input["command"].(string)
	for _, r := range GateRules {
		if r.Tool == tool && r.Pattern.MatchString(cmd) {
			return r.Reason
		}
	}
	return ""
}

// LongBackground is the longest background Bash Fyr lets pass without a
// word (V0.142). The CLI ends a background Bash at its timeout (at most 2 h).
const LongBackground = time.Hour

// GateNudge returns a note for the model on an allowed tool call, or "":
// a background Bash meant to run an hour or more (V0.142).
func GateNudge(tool string, input map[string]any) string {
	if tool != "Bash" {
		return ""
	}
	if bg, _ := input["run_in_background"].(bool); !bg {
		return ""
	}
	ms, _ := input["timeout"].(float64)
	if time.Duration(ms)*time.Millisecond < LongBackground {
		return ""
	}
	return "[Fyr] This background Bash may run for hours. The CLI ends a background Bash at its timeout (at most 2 h), and a long one keeps this session's process busy. " +
		"For a process that must stay up (a proxy, a tunnel, a server), start it detached with a bounded lifetime instead: " +
		"`setsid nohup timeout 4h <cmd> > <log> 2>&1 & echo $! > <pidfile>`; check it with the pid file, stop it with `kill $(cat <pidfile>)` when done, and watch its log with Monitor if you need its events."
}

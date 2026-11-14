package core

import (
	"regexp"
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

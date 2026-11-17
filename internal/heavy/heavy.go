// Package heavy recognises heavy commands (full test suites, builds, e2e
// runs) in an agent's Bash command and names their class, the part of a job's
// identity that recurs across threads (decision 0029 section 2, #185; #174
// uses the same patterns). The host setting `heavyCommands` replaces the
// defaults.
package heavy

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern is one heavy command: Re matches it, Name is its class, where $1
// is Re's first group.
type Pattern struct {
	Name string `json:"name"`
	Re   string `json:"pattern"`
}

// Defaults are the patterns without a host setting. A quick check (one
// package, -run) is not a heavy job.
var Defaults = []Pattern{
	{"make $1", `\bmake\s+(?:-\S+\s+)*(test[\w:-]*|check|e2e[\w-]*|lint)\b`},
	{"go test ./...", `\bgo\s+test\b[^|;&]*\s\./(?:[\w.-]+/)*\.\.\.`},
	{"playwright test", `\bplaywright\s+test\b`},
	{"yarn $1", `\byarn\s+(?:run\s+)?(test[\w:-]*|screenshot:diff|e2e[\w:-]*|build|typecheck)\b`},
	{"npm run $1", `\bnpm\s+(?:run\s+)?(test[\w:-]*|e2e[\w:-]*|build)\b`},
	{"nix build", `\bnix\s+build\b`},
	{"cargo $1", `\bcargo\s+(test|build)\b`},
	{"pytest", `\bpytest\b`},
}

type rule struct {
	name string
	re   *regexp.Regexp
}

// Matcher names the class of a command.
type Matcher struct{ rules []rule }

// New compiles ps; a pattern that does not compile is an error.
func New(ps []Pattern) (*Matcher, error) {
	m := &Matcher{}
	for _, p := range ps {
		re, err := regexp.Compile(p.Re)
		if err != nil {
			return nil, fmt.Errorf("heavy command %q: %w", p.Name, err)
		}
		m.rules = append(m.rules, rule{p.Name, re})
	}
	return m, nil
}

// Default is the matcher of Defaults.
func Default() *Matcher {
	m, err := New(Defaults)
	if err != nil {
		panic(err)
	}
	return m
}

// message is a quoted message or title argument, so "git commit -m 'make
// test'" is not a run; a quoted script (sh -c '...') still is.
var message = regexp.MustCompile(`(?:\s-[mt]|\s--(?:message|title|body|subject))(?:=|\s+)(?:'[^']*'|"[^"]*")`)

// Class is the class of cmd's heavy command, the longest match of all
// patterns, or "" when it runs none.
func (m *Matcher) Class(cmd string) string {
	if m == nil {
		return ""
	}
	cmd = message.ReplaceAllString(cmd, " ''")
	best, bestLen := "", 0
	for _, r := range m.rules {
		loc := r.re.FindStringSubmatchIndex(cmd)
		if loc == nil || loc[1]-loc[0] <= bestLen {
			continue
		}
		name := r.name
		if len(loc) >= 4 && loc[2] >= 0 {
			name = strings.ReplaceAll(name, "$1", cmd[loc[2]:loc[3]])
		}
		best, bestLen = name, loc[1]-loc[0]
	}
	return best
}

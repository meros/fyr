package agent

import (
	"strings"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
)

// terminalOnly are the CLI's commands that need its interactive terminal
// (a screen, an editor, a login) or that would fight Fyr's own controls;
// they stay out of the composer's list (SPEC 0.5 V0.79 (1)).
var terminalOnly = map[string]bool{
	"clear": true, "login": true, "logout": true, "config": true, "exit": true, "quit": true,
	"help": true, "resume": true, "theme": true, "vim": true, "terminal-setup": true, "ide": true,
	"doctor": true, "status": true, "statusline": true, "permissions": true, "model": true,
	"mcp": true, "agents": true, "hooks": true, "memory": true, "export": true, "bug": true,
	"feedback": true, "upgrade": true, "release-notes": true, "privacy-settings": true,
	"install-github-app": true, "migrate-installer": true, "rewind": true, "plugin": true,
	"plugins": true, "login-method": true, "add-dir": true, "output-style": true, "fast": true,
	"usage": true,
	// The terminal's own screen and session settings, and Fyr's own effort
	// picker and thread title (16 Nov, from a live list).
	"heapdump": true, "reload-plugins": true, "reload-skills": true, "color": true, "focus": true,
	"autocompact": true, "auto-mode-setup": true, "effort": true, "rename": true,
	"workflow-launch-exec": true,
}

// commandsOf reads the commands of the CLI's initialize answer, without the
// terminal-only ones, each name once; nil when the answer has no list (a
// CLI that does not say).
func commandsOf(answer cli.Msg) []core.Command {
	inner, _ := answer["response"].(cli.Msg)
	raw, ok := inner["commands"].([]any)
	if !ok {
		return nil
	}
	out := []core.Command{}
	seen := map[string]bool{}
	for _, r := range raw {
		m, _ := r.(cli.Msg)
		name, _ := m["name"].(string)
		name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "/"))
		if name == "" || strings.ContainsAny(name, " \t\n") || seen[name] || terminalOnly[name] || strings.HasPrefix(name, "__") { // "__": the CLI's internal ones
			continue
		}
		seen[name] = true
		desc, _ := m["description"].(string)
		hint, _ := m["argumentHint"].(string)
		out = append(out, core.Command{Name: name, Description: strings.TrimSpace(desc), ArgumentHint: strings.TrimSpace(hint)})
	}
	return out
}

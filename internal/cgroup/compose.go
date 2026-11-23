package cgroup

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The compose project a thread runs, so its services can be named and
// stopped (V0.services-not-jobs); moved from internal/heavy, which only
// classifies commands (V0.guard-mechanism).

var (
	composeCmd  = regexp.MustCompile(`(^|[\s/;&|(])(docker\s+compose|docker-compose)\b`)
	composeDown = regexp.MustCompile(`\s(down|stop|kill|rm)\b`)
	composeUp   = regexp.MustCompile(`\sup\b`)
	unsafeName  = regexp.MustCompile(`[^a-z0-9_-]`)
)

// ComposeProject is the compose project a command names, the way compose
// itself names it: -p/--project-name, else COMPOSE_PROJECT_NAME in the
// command, else the directory of --project-directory or of the first -f file
// (relative to cwd), else cwd's name. up says the command starts it (`up`),
// down that it stops it (`down`, `stop`, `kill`, `rm`). "" when the command
// runs no compose.
func ComposeProject(command, cwd string) (project string, up, down bool) {
	loc := composeCmd.FindStringIndex(command)
	if loc == nil {
		return "", false, false
	}
	pre, rest := command[:loc[0]], command[loc[1]:]
	// The compose command ends at the next shell separator.
	if i := strings.IndexAny(rest, ";&|\n"); i >= 0 {
		rest = rest[:i]
	}
	args := strings.Fields(rest)
	arg := func(i int) string {
		if i < len(args) {
			return strings.Trim(args[i], `"'`)
		}
		return ""
	}
	var name, dir, file string
	for i := 0; i < len(args); i++ {
		a := args[i]
		k, v, eq := strings.Cut(a, "=")
		val := func() string {
			if eq {
				return strings.Trim(v, `"'`)
			}
			i++
			return arg(i)
		}
		switch k {
		case "-p", "--project-name":
			name = val()
		case "--project-directory":
			dir = val()
		case "-f", "--file":
			if f := val(); file == "" {
				file = f
			}
		}
	}
	if name == "" {
		for _, f := range strings.Fields(pre) {
			if v, ok := strings.CutPrefix(f, "COMPOSE_PROJECT_NAME="); ok {
				name = strings.Trim(v, `"'`)
			}
		}
	}
	if name == "" {
		switch {
		case dir != "":
			name = filepath.Base(composeJoin(cwd, dir))
		case file != "":
			name = filepath.Base(filepath.Dir(composeJoin(cwd, file)))
		case cwd != "":
			name = filepath.Base(cwd)
		}
	}
	name = unsafeName.ReplaceAllString(strings.ToLower(name), "")
	if name == "" || name == "." || name == "/" {
		return "", false, false
	}
	return name, composeUp.MatchString(" " + rest), composeDown.MatchString(" " + rest)
}

func composeJoin(cwd, p string) string {
	if filepath.IsAbs(p) || cwd == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}

package agycli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/meros/fyr/internal/cli"
)

// readMCPConfig reads Fyr's 0600 MCP config JSON file (`{"mcpServers": {...}}`)
// and translates each server entry into ACP's `mcpServers` array format.
func readMCPConfig(path string) ([]cli.Msg, error) {
	if path == "" {
		return []cli.Msg{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		MCPServers map[string]struct {
			Type      string            `json:"type"`
			URL       string            `json:"url"`
			ServerURL string            `json:"serverUrl"`
			Headers   map[string]string `json:"headers"`
			Command   string            `json:"command"`
			Args      []string          `json:"args"`
			Env       map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(raw.MCPServers))
	for name := range raw.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]cli.Msg, 0, len(names))
	for _, name := range names {
		srv := raw.MCPServers[name]
		u := srv.URL
		if u == "" {
			u = srv.ServerURL
		}
		if u != "" {
			typ := srv.Type
			if typ == "" {
				typ = "http"
			}
			entry := cli.Msg{"type": typ, "name": name, "url": u, "headers": mapToNameValues(srv.Headers)}
			out = append(out, entry)
			continue
		}
		if srv.Command != "" {
			args := srv.Args
			if args == nil {
				args = []string{}
			}
			entry := cli.Msg{"name": name, "command": srv.Command, "args": args, "env": mapToNameValues(srv.Env)}
			out = append(out, entry)
		}
	}
	return out, nil
}

func mapToNameValues(m map[string]string) []cli.Msg {
	if len(m) == 0 {
		return []cli.Msg{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]cli.Msg, 0, len(keys))
	for _, k := range keys {
		out = append(out, cli.Msg{"name": k, "value": m[k]})
	}
	return out
}

// translateToolCall maps Antigravity's native tool names and parameters into
// Fyr's canonical observation tool names so activity labels, PR detection,
// Bash gates, heavy-job admission, and MCP tool matching work identically.
func translateToolCall(name string, args map[string]any, mcpServers []string) (string, map[string]any) {
	if args == nil {
		args = map[string]any{}
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch name {
	case "run_command":
		in := copyMap(args)
		if cmd := str("CommandLine", "command"); cmd != "" {
			in["command"] = cmd
		}
		if desc := str("toolSummary", "Description", "description"); desc != "" {
			in["description"] = desc
		}
		if d, ok := args["IsDaemon"].(bool); ok && d {
			in["run_in_background"] = true
		}
		return "Bash", in
	case "view_file", "read_file":
		in := copyMap(args)
		if p := str("AbsolutePath", "Path", "file_path"); p != "" {
			in["file_path"] = p
		}
		return "Read", in
	case "write_to_file", "write_file":
		in := copyMap(args)
		if p := str("TargetFile", "Path", "file_path"); p != "" {
			in["file_path"] = p
		}
		return "Write", in
	case "replace_file_content", "multi_replace_file_content", "edit_file":
		in := copyMap(args)
		if p := str("TargetFile", "Path", "file_path"); p != "" {
			in["file_path"] = p
		}
		return "Edit", in
	case "grep_search":
		in := copyMap(args)
		if q := str("Query", "pattern"); q != "" {
			in["pattern"] = q
		}
		return "Grep", in
	case "find_by_name", "list_dir":
		in := copyMap(args)
		if pat := str("Pattern", "DirectoryPath", "SearchDirectory", "pattern"); pat != "" {
			in["pattern"] = pat
		}
		return "Glob", in
	case "search_web":
		in := copyMap(args)
		if q := str("query", "Query"); q != "" {
			in["query"] = q
		}
		return "WebSearch", in
	case "read_url_content":
		in := copyMap(args)
		if u := str("Url", "url"); u != "" {
			in["url"] = u
		}
		return "WebFetch", in
	case "invoke_subagent":
		in := copyMap(args)
		if r := str("Role", "description"); r != "" {
			in["description"] = r
		}
		if p := str("Prompt", "prompt"); p != "" {
			in["prompt"] = p
		}
		return "Agent", in
	case "ask_question":
		return "AskUserQuestion", translateAskQuestion(args)
	case "call_mcp_tool":
		srv := str("ServerName", "server_name")
		tool := str("ToolName", "tool_name")
		if srv != "" && tool != "" {
			if inner, ok := args["Arguments"].(map[string]any); ok {
				return "mcp__" + srv + "__" + tool, copyMap(inner)
			}
			return "mcp__" + srv + "__" + tool, copyMap(args)
		}
	}
	// Antigravity registers eager MCP tools as `mcp_<server>_<tool>` (single
	// underscore); Fyr uses `mcp__<server>__<tool>` (double underscore).
	if strings.HasPrefix(name, "mcp_") && !strings.HasPrefix(name, "mcp__") {
		rest := strings.TrimPrefix(name, "mcp_")
		for _, srv := range mcpServers {
			if strings.HasPrefix(rest, srv+"_") {
				return "mcp__" + srv + "__" + strings.TrimPrefix(rest, srv+"_"), copyMap(args)
			}
		}
		if srv, tool, ok := strings.Cut(rest, "_"); ok && srv != "" && tool != "" {
			return "mcp__" + srv + "__" + tool, copyMap(args)
		}
	}
	return name, copyMap(args)
}

// matchesToolPattern checks a tool name against a glob pattern list such as
// `["mcp__fyr__*"]`.
func matchesToolPattern(tool string, patterns []string) bool {
	for _, pat := range patterns {
		if pat == tool {
			return true
		}
		if ok, _ := filepath.Match(pat, tool); ok {
			return true
		}
		if strings.HasSuffix(pat, "*") && strings.HasPrefix(tool, strings.TrimSuffix(pat, "*")) {
			return true
		}
	}
	return false
}

// isTimerBlocked reports whether Fyr disabled CLI-managed timers (CronCreate /
// ScheduleWakeup) and the call is Antigravity's equivalent `schedule` tool.
func isTimerBlocked(rawName string, disallowed []string) bool {
	if rawName != "schedule" || len(disallowed) == 0 {
		return false
	}
	for _, d := range disallowed {
		if d == "schedule" || d == "CronCreate" || d == "ScheduleWakeup" {
			return true
		}
	}
	return false
}

func translateAskQuestion(args map[string]any) map[string]any {
	in := copyMap(args)
	rawQs, _ := args["questions"].([]any)
	if len(rawQs) == 0 {
		return in
	}
	qs := make([]any, 0, len(rawQs))
	for _, x := range rawQs {
		qm, _ := x.(map[string]any)
		if qm == nil {
			continue
		}
		q := copyMap(qm)
		if ms, ok := qm["is_multi_select"].(bool); ok {
			q["multiSelect"] = ms
		}
		if rawOpts, _ := qm["options"].([]any); len(rawOpts) > 0 {
			opts := make([]any, 0, len(rawOpts))
			for _, o := range rawOpts {
				switch v := o.(type) {
				case string:
					opts = append(opts, map[string]any{"label": v})
				case map[string]any:
					opts = append(opts, v)
				}
			}
			q["options"] = opts
		}
		qs = append(qs, q)
	}
	in["questions"] = qs
	return in
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

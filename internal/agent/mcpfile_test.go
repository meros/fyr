// SPEC V0.129 (1), decision 0028 slice 0: a thread's MCP token reaches the
// CLI in a 0600 file, never on its command line, where /proc shows it to
// every local user; the file is gone once the CLI has started.
package agent

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/provider"
)

// mcpAdapter records the MCP config file each start was given, and reads it
// while the start runs, as the CLI does.
type mcpAdapter struct {
	*stubAdapter
	mu    sync.Mutex
	paths []string
	body  string
	mode  os.FileMode
	args  []string
}

func (a *mcpAdapter) Start(l cli.Launch, h cli.Handlers) (cli.Session, error) {
	a.mu.Lock()
	a.paths = append(a.paths, l.MCPConfigFile)
	if b, err := os.ReadFile(l.MCPConfigFile); err == nil {
		a.body = string(b)
	}
	if st, err := os.Stat(l.MCPConfigFile); err == nil {
		a.mode = st.Mode().Perm()
	}
	a.args = claudecli.Args(l)
	a.mu.Unlock()
	return a.stubAdapter.Start(l, h)
}

func TestV129MCPTokenInAFileNotArgv(t *testing.T) {
	a := &mcpAdapter{stubAdapter: &stubAdapter{}}
	run := t.TempDir()
	m := New(stubHost{cwd: t.TempDir()}, Config{
		Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "stub"} },
		Adapters: map[string]cli.Adapter{"stub": a},
		Prompt:   "-",
		MCPURL:   "http://127.0.0.1:1/mcp",
		RunDir:   run,
	})
	t.Cleanup(m.StopAll)
	if _, err := m.ensure("t_1"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.paths) != 1 || !strings.HasPrefix(a.paths[0], run+string(os.PathSeparator)) {
		t.Fatalf("config file %v, want one under %s", a.paths, run)
	}
	if a.mode != 0o600 {
		t.Fatalf("mode %v", a.mode)
	}
	tok := m.sessions["t_1"].token
	if tok == "" || !strings.Contains(a.body, "Bearer "+tok) {
		t.Fatalf("the file holds no token: %s", a.body)
	}
	if strings.Contains(strings.Join(a.args, " "), tok) {
		t.Fatalf("the token is on the command line: %v", a.args)
	}
	if _, err := os.Stat(a.paths[0]); !os.IsNotExist(err) {
		t.Fatalf("the file is still there after the start: %v", err)
	}
	if th, ok := m.ThreadForToken(tok); !ok || th != "t_1" {
		t.Fatal("the token does not name its thread")
	}
}

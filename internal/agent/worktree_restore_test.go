// V0.worktree-sweep: a thread that starts in a worktree Fyr removed gets it
// back before its CLI starts; one Fyr cannot make again fails as before.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/provider"
)

func TestWorktreeSweepAStartInARemovedFolderMakesItAgain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wt")
	m := New(stubHost{cwd: dir}, Config{
		Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "stub"} },
		Adapters: map[string]cli.Adapter{"stub": &stubAdapter{}},
		Prompt:   "-",
	})
	t.Cleanup(m.StopAll)
	if _, err := m.ensure("t_1"); err == nil || !strings.Contains(err.Error(), "working directory") {
		t.Fatalf("no restore: %v", err)
	}
	var asked string
	m.SetRestoreDir(func(d string) (bool, error) {
		asked = d
		return true, os.MkdirAll(d, 0o755)
	})
	if _, err := m.ensure("t_1"); err != nil || asked != dir {
		t.Fatalf("restore: %v, asked %q", err, asked)
	}
}

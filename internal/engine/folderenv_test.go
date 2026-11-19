// V0.run-folder-env (#355): a Run card runs through the folder's direnv when its .envrc is allowed.
package engine

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFolderArgvWrapsOnlyAnAllowedEnvrc(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repo", "sub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	argv := []string{"sh", "-c", "gcloud info"}
	allowed := func(string, string) ([]byte, error) {
		return []byte(`{"state":{"foundRC":{"allowed":0,"path":"/x/.envrc"}}}`), nil
	}
	// No .envrc: as before, and direnv is not asked.
	asked := false
	if got := folderArgv(dir, argv, "/bin/direnv", func(string, string) ([]byte, error) { asked = true; return nil, nil }); !slices.Equal(got, argv) || asked {
		t.Fatalf("no envrc: %v asked=%v", got, asked)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", ".envrc"), []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An .envrc above the folder, allowed: through direnv exec in the folder.
	want := append([]string{"/bin/direnv", "exec", dir}, argv...)
	if got := folderArgv(dir, argv, "/bin/direnv", allowed); !slices.Equal(got, want) {
		t.Fatalf("allowed: %v", got)
	}
	// Not allowed, denied, unreadable, a failed status or no direnv: as before.
	for _, st := range []string{`{"state":{"foundRC":{"allowed":1,"path":"/x/.envrc"}}}`, `{"state":{"foundRC":{"allowed":2,"path":"/x/.envrc"}}}`, `{"state":{"foundRC":null}}`, `junk`} {
		if got := folderArgv(dir, argv, "/bin/direnv", func(string, string) ([]byte, error) { return []byte(st), nil }); !slices.Equal(got, argv) {
			t.Fatalf("%s: %v", st, got)
		}
	}
	if got := folderArgv(dir, argv, "/bin/direnv", func(string, string) ([]byte, error) { return nil, errors.New("x") }); !slices.Equal(got, argv) {
		t.Fatalf("status failed: %v", got)
	}
	if got := folderArgv(dir, argv, "", allowed); !slices.Equal(got, argv) {
		t.Fatalf("no direnv: %v", got)
	}
}

// With a real direnv: a variable the .envrc exports reaches the script once the folder is allowed.
func TestFolderArgvRealDirenv(t *testing.T) {
	direnv, err := exec.LookPath("direnv")
	if err != nil {
		t.Skip("no direnv")
	}
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data")) // direnv's allow list, kept out of the owner's
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("DIRENV_DIR", "")
	work := filepath.Join(dir, "w")
	os.MkdirAll(work, 0o755)
	os.WriteFile(filepath.Join(work, ".envrc"), []byte("export FYR_FOLDER_ENV=yes\n"), 0o644)
	run := func() string {
		argv := FolderArgv(work, []string{"sh", "-c", "echo v=$FYR_FOLDER_ENV"})
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = work
		out, _ := cmd.Output()
		return strings.TrimSpace(string(out))
	}
	if got := run(); got != "v=" {
		t.Fatalf("not allowed yet: %q", got)
	}
	allow := exec.Command(direnv, "allow", work)
	allow.Dir = work
	if out, err := allow.CombinedOutput(); err != nil {
		t.Fatalf("allow: %v %s", err, out)
	}
	if got := run(); !strings.HasSuffix(got, "v=yes") {
		t.Fatalf("allowed: %q", got)
	}
}

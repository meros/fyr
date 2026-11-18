// Package docs holds no code, only the checks on the change log.
package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// closedRows is how many rows the table in SPEC 0.5 has for good.
const closedRows = 213

var changeName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// SPEC V0.one-file-per-change: a row at the end of the table is what made every PR conflict.
func TestChangeTableIsClosed(t *testing.T) {
	spec, err := os.ReadFile("SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(spec), "\n") {
		if strings.HasPrefix(line, "| V0.") {
			rows++
		}
	}
	if rows != closedRows {
		t.Fatalf("SPEC 0.5 has %d V0 rows, want %d: the table is closed. Move the new row to docs/changes/<name>.md, with the first line `# V0.<name>: ...` (docs/changes/README.md)", rows, closedRows)
	}
}

// SPEC V0.one-file-per-change: the file's name is the entry's ID.
func TestChangeEntriesNameTheirFile(t *testing.T) {
	files, err := filepath.Glob("changes/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		if name == "README" {
			continue
		}
		if !changeName.MatchString(name) {
			t.Errorf("%s: a name is lowercase letters, digits and single dashes", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if want := "# V0." + name + ": "; !strings.HasPrefix(string(body), want) {
			t.Errorf("%s: the first line must start with %q", path, want)
		}
	}
}

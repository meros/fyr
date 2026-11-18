// SPEC 0.5 V0.183: the front takes its request guards from internal/httpguard
// and the host's legacy count by injection; its code imports no host server.
package front

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestV183TheFrontDoesNotImportTheServer(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			if im.Path.Value == `"github.com/meros/fyr/internal/server"` {
				t.Errorf("%s imports internal/server", f)
			}
		}
	}
}

package guard

import (
	"go/build"
	"slices"
	"testing"
)

// The mechanism does not import the policy, and the policy does not import a
// mechanism (V0.guard-mechanism): each speaks internal/guard/resource.
func TestLayering(t *testing.T) {
	const mod = "github.com/meros/fyr/internal/"
	cases := []struct {
		dir    string
		forbid []string
	}{
		{"../cgroup", []string{"slots", "heavy", "admission", "engine", "guard"}},
		{"resource", []string{"slots", "heavy", "admission", "engine", "cgroup", "guard"}},
		{".", []string{"cgroup", "slots", "heavy", "admission", "engine"}},
	}
	for _, c := range cases {
		for _, goos := range []string{"linux", "darwin"} {
			ctx := build.Default
			ctx.GOOS = goos
			p, err := ctx.ImportDir(c.dir, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range c.forbid {
				if slices.Contains(p.Imports, mod+f) {
					t.Errorf("%s (%s) imports internal/%s", c.dir, goos, f)
				}
			}
		}
	}
}

package deploy

import (
	"context"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/proc"
)

// SPEC 0.5 V0.80: the build and `make test` start in agents/self-deploy/; the
// git checks before them stay in the daemon's cgroup.
func TestV080BuildAndTestRunInTheDeployCgroup(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	d.Cgroup = t.TempDir()
	in := map[string]bool{}
	d.Run = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		a := proc.Command(ctx, "true").SysProcAttr
		in[strings.Join(args, " ")] = a.UseCgroupFD
		return []byte("ok"), nil
	}
	d.Deploy(context.Background(), "t1", req, "/src/fyr")
	var build, test bool
	for cmd, inCg := range in {
		switch {
		case strings.HasPrefix(cmd, "make -C /src/fyr build"):
			build = inCg
		case cmd == "make -C /src/fyr test":
			test = inCg
		case inCg:
			t.Errorf("%q ran in the deploy cgroup", cmd)
		}
	}
	if !build || !test {
		t.Fatalf("build %v test %v (%v)", build, test, in)
	}
}

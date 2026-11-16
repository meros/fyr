package deploy

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// SPEC 0.5 V0.86: the self-deploy's `make test` holds a slot while it
// runs; the build takes none; a deploy whose wait ends runs no tests.
func TestV086DeployTestTakesASlot(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	d.Slot = func(context.Context) (func(), error) {
		*steps = append(*steps, "slot")
		return func() { *steps = append(*steps, "release") }, nil
	}
	d.Deploy(context.Background(), "t1", req, "/src/fyr")
	at := func(s string) int {
		return slices.IndexFunc(*steps, func(x string) bool { return strings.HasPrefix(x, s) })
	}
	build, slot, test, release := at("make -C /src/fyr build"), at("slot"), at("make -C /src/fyr test"), at("release")
	if build < 0 || slot < 0 || test < 0 || release < 0 || !(build < slot && slot < test && test < release) {
		t.Fatalf("order: %v", *steps)
	}

	d, _, steps = newDeployer(t, "")
	d.Slot = func(context.Context) (func(), error) { return nil, errors.New("replaced") }
	if _, err := d.Deploy(context.Background(), "t1", req, "/src/fyr"); err == nil {
		t.Fatal("no error without a slot")
	}
	if at("make -C /src/fyr test") >= 0 {
		t.Fatalf("tests ran without a slot: %v", *steps)
	}
}

package cgroup

import (
	"context"
	"os"
	"time"

	"github.com/meros/fyr/internal/proc"
)

// PSTimeout bounds one run of ps: a process table that does not come back
// must not hold up a sampler, a sweep or a start.
const PSTimeout = 10 * time.Second

// PS runs `ps args...` (macOS and other hosts without /proc) and returns its
// output. ps is bounded by ctx and PSTimeout, whichever is first, and runs in
// the C locale so %cpu and etime parse the same everywhere.
func PS(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, PSTimeout)
	defer cancel()
	cmd := proc.Command(ctx, "ps", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

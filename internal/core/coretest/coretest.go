// Package coretest lets a test set up a thread's state through the engine's
// one path (decision 0020): Patch sends an Input whose kind runs a
// function. The kind exists only in a binary that imports this package, and
// only tests do.
package coretest

import (
	"time"

	"github.com/meros/fyr/internal/core"
)

// KindPatch is the test-only Input kind.
const KindPatch = "test.patch"

func init() {
	core.RegisterKind(&core.InputKind{Name: KindPatch, From: []string{core.ByOwner, core.ByAgent, core.ByObserved},
		Reduce: func(r core.Rec, in core.Input, now time.Time) (core.Result, error) {
			fn := in.Args.(func(*core.Thread, time.Time) []core.Event)
			return core.Result{Events: fn(r.T, now)}, nil
		}})
}

// Applier is the engine's one path.
type Applier interface {
	Apply(core.Input) (any, error)
}

// Patch changes thread id with fn and appends the events it returns.
func Patch(a Applier, id string, fn func(*core.Thread, time.Time) []core.Event) {
	a.Apply(core.Input{Source: core.Observer("test"), Kind: KindPatch, Thread: id, Args: fn})
}

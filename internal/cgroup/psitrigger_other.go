//go:build !linux

package cgroup

import (
	"context"
	"errors"
)

var errNoTree = errors.New("no cgroup tree")

// watchPressure: PSI triggers are Linux only; the guard's tick covers the rest.
func watchPressure(context.Context, string, string, func()) error {
	return errors.New("no PSI triggers on this OS")
}

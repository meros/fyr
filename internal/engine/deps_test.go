// SPEC 0.5 V0.182, V0.147: the engine calls every func in its dependency
// groups (deps.go) without e.mu. A hook called under it is a lock edge from
// the engine into another package, and with a call back into the engine the
// cycle that hung fyrd on 17 Nov (#206).
package engine

import (
	"bytes"
	"flag"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"
)

// depsSeen is every dependency func a guarded test saw called.
var depsSeen sync.Map

// goid is the calling goroutine's id, from its stack's first line.
func goid() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	n, _ := strconv.ParseUint(string(b[:bytes.IndexByte(b, ' ')]), 10, 64)
	return n
}

// depGroups are e's dependency groups, by name.
func depGroups(e *Engine) map[string]reflect.Value {
	return map[string]reflect.Value{
		"AgentDeps":      reflect.ValueOf(&e.AgentDeps).Elem(),
		"OutsideDeps":    reflect.ValueOf(&e.OutsideDeps).Elem(),
		"DeployDeps":     reflect.ValueOf(&e.DeployDeps).Elem(),
		"SlotDeps":       reflect.ValueOf(&e.SlotDeps).Elem(),
		"UndeclaredDeps": reflect.ValueOf(&e.UndeclaredDeps).Elem(),
		"LeftoverDeps":   reflect.ValueOf(&e.LeftoverDeps).Elem(),
	}
}

// guardDeps wraps every func e's dependency groups hold now, so a call made
// while the calling goroutine holds e.mu fails t. Call it after the test set
// its hooks.
func guardDeps(t *testing.T, e *Engine) {
	t.Helper()
	for _, g := range depGroups(e) {
		for i := range g.NumField() {
			f, name := g.Field(i), g.Type().Field(i).Name
			if f.Kind() != reflect.Func || f.IsNil() {
				continue
			}
			inner := reflect.ValueOf(f.Interface())
			f.Set(reflect.MakeFunc(f.Type(), func(args []reflect.Value) []reflect.Value {
				if h, ok := e.mu.Holder(); ok && h.Goroutine == goid() {
					t.Errorf("%s was called under e.mu (taken at %s)", name, h.Where)
				}
				depsSeen.Store(name, true)
				return inner.Call(args)
			}))
		}
	}
}

// depFuncs names every func field of the groups.
func depFuncs() []string {
	var out []string
	for _, g := range depGroups(New(nil)) {
		for i := range g.NumField() {
			if g.Field(i).Kind() == reflect.Func {
				out = append(out, g.Type().Field(i).Name)
			}
		}
	}
	slices.Sort(out)
	return out
}

// unguardedDeps names the funcs of the groups that no guarded test called,
// after a full run of the package (TestMain checks it), so the rule is
// checked for each; nil after a filtered or short run.
func unguardedDeps() []string {
	if f := flag.Lookup("test.run"); (f != nil && f.Value.String() != "") || testing.Short() {
		return nil
	}
	var missing []string
	for _, n := range depFuncs() {
		if _, ok := depsSeen.Load(n); !ok {
			missing = append(missing, n)
		}
	}
	return missing
}

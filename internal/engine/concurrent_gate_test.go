// V0.freeze-controller: a thread that runs a heavy command and starts another
// of the same class beside it asks the pool for room for both, learned or not.
// A cheaper different command still runs free (V0.admission-tidy).
package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestConcurrentRunOfTheSameCommandAsksForRoom(t *testing.T) {
	e, _, a, _ := heavyEngine(t, 2)
	needs := map[string]resource.Need{"yarn lint": {CPU: 1.5, Mem: 3 << 30}, "go vet ./...": {CPU: 0.5, Mem: 1 << 29}}
	e.SlotClassNeed = func(_, class string) resource.Need { return needs[class] }
	var asked []int
	room := true
	e.Slots.Admit = func(head resource.Holder, _ []resource.Holder) (bool, string, string) {
		if head.Instances > 1 {
			asked = append(asked, head.Instances)
			if !room {
				return false, "needs ≈ 6.0 GB memory, 1.0 GB free", resource.ResMem
			}
		}
		return true, "", ""
	}
	e.HeavySoft = func(class string) bool { return class == "yarn lint" } // learned: never waits alone
	guardDeps(t, e)
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "yarn lint", ToolUse: "tu1"}); r != "" {
		t.Fatalf("the first run: %q", r)
	}
	if len(asked) != 0 || e.HeavyOpenCalls(a) != 1 {
		t.Fatalf("the first run asked for room (%v) or is not counted (%d)", asked, e.HeavyOpenCalls(a))
	}
	// A different, cheaper command runs free beside it.
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "go vet ./...", ToolUse: "tu2"}); r != "" {
		t.Fatalf("a cheaper command was checked: %q", r)
	}
	e.HeavyDone(a, "tu2")
	// A second run of the same class asks for two, and waits when there is no room.
	room = false
	r := e.HeavyGate(HeavyCall{Thread: a, Class: "yarn lint", ToolUse: "tu3"})
	if !strings.Contains(r, "queued") || !strings.Contains(r, "1.0 GB free") {
		t.Fatalf("a second run at once ran unchecked: %q", r)
	}
	if len(asked) == 0 || asked[len(asked)-1] != 2 {
		t.Fatalf("asked for room for %v, want 2", asked)
	}
	if e.HeavyOpenCalls(a) != 1 {
		t.Fatalf("a refused run counts as open: %d", e.HeavyOpenCalls(a))
	}
	// With room it runs, and both are counted.
	room = true
	e.Slots.Fill()
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "yarn lint", ToolUse: "tu3"}); r != "" {
		t.Fatalf("the second run with room: %q", r)
	}
	if e.HeavyOpenCalls(a) != 2 {
		t.Fatalf("open calls %d, want 2", e.HeavyOpenCalls(a))
	}
	e.HeavyDone(a, "tu1")
	e.HeavyDone(a, "tu3")
	if e.HeavyOpenCalls(a) != 0 {
		t.Fatalf("still open after both ended: %d", e.HeavyOpenCalls(a))
	}
}

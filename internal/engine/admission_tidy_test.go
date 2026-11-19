// V0.admission-tidy: a thread that holds an admission and starts a heavier
// command is admitted again for the difference; a cheaper second command runs
// free; the gate takes the job's baseline at the grant; the open heavy calls
// are published for admission.
package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/slots"
)

func TestAdmissionTidyAHeavierSecondCommandIsAdmitted(t *testing.T) {
	e, _, a, b := heavyEngine(t, 2)
	needs := map[string]slots.Need{"go test ./...": {CPU: 1, Mem: 1 << 30}, "yarn e2e": {CPU: 6, Mem: 8 << 30}, "go vet ./...": {CPU: 0.5, Mem: 1 << 29}}
	e.SlotClassNeed = func(_, class string) slots.Need { return needs[class] }
	room := true
	e.Slots.Admit = func(head slots.Holder, _ []slots.Holder) (bool, string, string) {
		if head.Owner == a && !room {
			return false, "needs ≈ 8.6 GB memory, 2.0 GB free", slots.ResMem
		}
		return true, "", ""
	}
	var based []string
	e.SlotBaseline = func(th string) { based = append(based, th) }
	guardDeps(t, e) // neither is called under e.mu
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "go test ./...", ToolUse: "tu1"}); r != "" {
		t.Fatalf("first: %q", r)
	}
	if len(based) != 1 || based[0] != a {
		t.Fatalf("no baseline at the grant: %v", based)
	}
	if !e.HeavyOpen(a) || e.HeavyOpen(b) {
		t.Fatal("the open calls are not published")
	}
	e.HeavyGate(HeavyCall{Thread: b, Class: "go test ./...", ToolUse: "tu2"})
	room = false
	r := e.HeavyGate(HeavyCall{Thread: a, Class: "yarn e2e", ToolUse: "tu3"})
	if !strings.Contains(r, "queued") || !strings.Contains(r, "2.0 GB free") {
		t.Fatalf("a heavier second command ran unchecked: %q", r)
	}
	if !holds(e, a) {
		t.Fatal("the running command's hold was lost")
	}
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "yarn e2e", ToolUse: "tu3b"}); !strings.Contains(r, "queued") {
		t.Fatalf("a refused heavier command ran when sent again: %q", r)
	}
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "go vet ./...", ToolUse: "tu4"}); r != "" {
		t.Fatalf("a cheaper second command was checked: %q", r)
	}
	if len(based) != 2 {
		t.Fatalf("a baseline taken again for a hold already running: %v", based)
	}
	e.HeavyDone(a, "tu1")
	e.HeavyDone(a, "tu4")
	room = true
	e.Slots.Fill()
	if !holds(e, a) || !told(e, a, "admitted now: run the command again") {
		t.Fatal("the heavier command was not admitted once room came")
	}
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "yarn e2e", ToolUse: "tu5"}); r != "" {
		t.Fatalf("the admitted command was refused: %q", r)
	}
	e.HeavyDone(a, "tu5")
	if e.HeavyOpen(a) {
		t.Fatal("still open after its end")
	}
}

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// V0.settle-request: settle_request shows the owner a Settle chip on a
// project that asks (the default), and settles the thread once it is idle on
// a project set to auto. A new run clears the chip.
func TestSettleRequestAsksOrSettlesByProject(t *testing.T) {
	h := newHarness(t, time.Minute)
	ws := filepath.Join(h.dir, "acme")
	os.MkdirAll(ws, 0o755)
	h.cmd("project.create", "", core.Args{"name": "acme", "folder": ws})
	r := h.cmd("thread.create", "", core.Args{"cwd": ws, "text": "x"})
	a := r.ThreadID
	h.wait(a, "done", state(core.Done))

	out, isErr := callAs(t, h.e, a, "settle_request", map[string]any{"why": "the fix is merged"})
	if isErr || !strings.Contains(out, "Settle chip") {
		t.Fatalf("ask: %v %q", isErr, out)
	}
	th, _ := h.e.Thread(a)
	if th.SettleAsk == nil || th.SettleAsk.Why != "the fix is merged" || th.Settle.At != nil {
		t.Fatalf("ask: %+v settled %v", th.SettleAsk, th.Settle.At)
	}
	// The owner's Settle takes the chip away.
	if r := h.cmd("thread.settle", a, nil); !r.OK {
		t.Fatalf("settle: %+v", r)
	}
	if th, _ := h.e.Thread(a); th.SettleAsk != nil || th.Settle.At == nil {
		t.Fatalf("after settle: %+v", th.SettleAsk)
	}

	// Auto: a new thread in the project settles by itself.
	p := projectOf(h.e, a)
	if up := h.e.Command(api.Cmd{CommandID: "u1", Action: "project.update", Args: core.Args{"id": p.ID, "agentSettle": "auto"}}); !up.OK {
		t.Fatalf("%+v", up)
	}
	if up := h.e.Command(api.Cmd{CommandID: "u2", Action: "project.update", Args: core.Args{"id": p.ID, "agentSettle": "sometimes"}}); up.OK {
		t.Fatal("a bad agentSettle was taken")
	}
	b := h.cmd("thread.create", "", core.Args{"cwd": ws, "text": "y"}).ThreadID
	h.wait(b, "done", state(core.Done))
	out, isErr = callAs(t, h.e, b, "settle_request", map[string]any{"why": "done"})
	if isErr || !strings.Contains(out, "settles once") {
		t.Fatalf("auto: %v %q", isErr, out)
	}
	h.wait(b, "settled", func(th core.Thread, _ core.Attention) bool { return th.Settle.At != nil })
	if th, _ := h.e.Thread(b); th.SettleAsk != nil {
		t.Fatalf("auto left a chip: %+v", th.SettleAsk)
	}
}

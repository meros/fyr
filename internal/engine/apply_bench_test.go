package engine

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// benchThread is an engine with one thread that holds five PR changes, a
// summary and a run, as a thread a few days into its work does.
func benchThread(b *testing.B) (*Engine, string) {
	b.Helper()
	// The production path: the serialization check is sampled.
	serialAlways = false
	b.Cleanup(func() { serialAlways = true })
	st, err := store.Open(filepath.Join(b.TempDir(), "fyr.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { st.Close() })
	e := New(st)
	if err := e.Load(); err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	e.Now = func() time.Time { return now }
	r := e.Command(api.Cmd{CommandID: "c0", Action: "thread.create", Args: core.Args{"cwd": b.TempDir(), "text": "hi"}})
	if !r.OK {
		b.Fatalf("create: %+v", r)
	}
	id := r.ThreadID
	read := now
	for i := 1; i <= 5; i++ {
		u := fmt.Sprintf("https://github.com/acme/app/pull/%d", i)
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsGitHub), Kind: core.KindPRObserved, Thread: id, Args: core.PR{URL: u, Repo: "acme/app", Number: i, State: "OPEN", Checks: core.ChecksPass, Head: fmt.Sprintf("h%d", i), Title: "Fix", CheckedAt: &read}}); err != nil {
			b.Fatal(err)
		}
	}
	return e, id
}

// BenchmarkApplyChange is one apply that changes the thread (a digest with
// a new summary): the reduce, the store and the checks.
func BenchmarkApplyChange(b *testing.B) {
	e, id := benchThread(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindDigest, Thread: id, Args: core.DigestArgs{Summary: fmt.Sprintf("step %d", i)}}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkApplyIdle is one apply that changes nothing (the clock).
func BenchmarkApplyIdle(b *testing.B) {
	e, id := benchThread(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsClock), Kind: core.KindClock, Thread: id}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkThreadTick is the thread invariants' tick over 50 threads.
func BenchmarkThreadTick(b *testing.B) {
	e, _ := benchThread(b)
	for i := 0; i < 49; i++ {
		if r := e.Command(api.Cmd{CommandID: fmt.Sprintf("c%d", i+1), Action: "thread.create", Args: core.Args{"cwd": b.TempDir(), "text": "hi"}}); !r.OK {
			b.Fatalf("create: %+v", r)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.checkThreadTick(e.Now())
	}
}

// V0.structure-cost (4): the bytes an apply compares and writes are the records
// in memory: after applies that change the thread, only its feed, only its
// view, or nothing, the store holds what e.threads and e.views hold, and
// the kept bytes are theirs.
func TestStoredJSONIsWhatIsStored(t *testing.T) {
	e, st, _ := bare(t)
	id := bareThread(t, e)
	ins := []core.Input{
		{Source: core.Observer(core.ObsProcess), Kind: core.KindDigest, Thread: id, Args: core.DigestArgs{Summary: "one"}},
		{Source: core.Observer(core.ObsClock), Kind: core.KindClock, Thread: id},
		{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id},
		{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{}},
		{Source: core.Observer(core.ObsProcess), Kind: core.KindDigest, Thread: id, Args: core.DigestArgs{Summary: "two"}},
	}
	for _, in := range ins {
		if _, err := e.Apply(in); err != nil {
			t.Fatalf("%s: %v", in.Kind, err)
		}
	}
	if r := e.Command(api.Cmd{CommandID: "seen", Action: "thread.markSeen", ThreadID: id}); !r.OK && r.Status != 400 {
		t.Logf("markSeen: %+v", r)
	}
	ts, _ := st.Threads()
	vs, _ := st.Views(core.Owner)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, x := range ts {
		if x.ID != id {
			continue
		}
		got, _ := json.Marshal(x)
		want, _ := json.Marshal(e.threads[id])
		if string(got) != string(want) {
			t.Fatalf("stored thread differs from memory:\n%s\n%s", got, want)
		}
		if c := e.stored[id]; c != nil && c.t == e.threads[id] && string(c.thread) != string(want) {
			t.Fatalf("kept bytes differ from the thread:\n%s\n%s", c.thread, want)
		}
	}
	gv, _ := json.Marshal(vs[id])
	wv, _ := json.Marshal(e.views[id])
	if string(gv) != string(wv) {
		t.Fatalf("stored view differs from memory:\n%s\n%s", gv, wv)
	}
}

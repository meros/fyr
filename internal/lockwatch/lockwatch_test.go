// SPEC 0.5 V0.147: a watched mutex names its holder; the watchdog writes a
// dump for a stall once, and quits for a critical lock held too long.
package lockwatch

import (
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type thing struct{ mu Mutex }

func (x *thing) hold(release <-chan struct{}) {
	x.mu.Lock()
	defer x.mu.Unlock()
	<-release
}

func TestV147MutexNamesItsHolder(t *testing.T) {
	x := &thing{}
	release := make(chan struct{})
	go x.hold(release)
	deadline := time.Now().Add(2 * time.Second)
	var h Holder
	for {
		var ok bool
		if h, ok = x.mu.Holder(); ok || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if h.Lock != "lockwatch.(*thing)" || !strings.Contains(h.Where, "lockwatch.(*thing).hold") || h.Goroutine == 0 {
		t.Fatalf("%+v", h)
	}
	found := false
	for _, o := range Holders() {
		found = found || o.Lock == h.Lock && o.Since.Equal(h.Since)
	}
	if !found {
		t.Fatalf("not among the holders: %+v", Holders())
	}
	if x.mu.TryLock() {
		t.Fatal("TryLock took a held mutex")
	}
	close(release)
	x.mu.Lock()
	x.mu.Unlock()
	if _, ok := x.mu.Holder(); ok {
		t.Fatal("a free mutex has a holder")
	}
}

func TestV147WatchdogDumpsAStallOnceAndQuitsOnTheCoreLock(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var stalls atomic.Int32
	got := make(chan Stall, 4)
	quits := 0
	w := &Watchdog{Dir: dir, Stall: 10 * time.Second, Exit: 2 * time.Minute, Now: func() time.Time { return now },
		OnStall: func(s Stall) { stalls.Add(1); got <- s }, Quit: func() { quits++ }}
	core := &Mutex{Critical: true, Name: "engine.mu"}
	core.Lock()
	done := Start("GET /api/load", 0)
	slow := Start("POST /api/command", time.Hour)
	defer slow()
	w.Check()
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatalf("dumped before the limit: %v", es)
	}
	now = now.Add(11 * time.Second)
	w.Check()
	s := <-got
	if !strings.Contains(s.What, "engine.mu") || s.File == "" || s.Exiting {
		t.Fatalf("%+v", s)
	}
	b, _ := os.ReadFile(s.File)
	for _, want := range []string{"lock engine.mu held", "operation GET /api/load held", "goroutine ", "TestV147WatchdogDumpsAStallOnceAndQuitsOnTheCoreLock"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("the dump lacks %q:\n%.2000s", want, b)
		}
	}
	w.Check() // the same holders: no second dump
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("dumped twice for one stall: %d", len(es))
	}
	done()
	now = now.Add(2 * time.Minute)
	w.Check()
	if quits != 1 {
		t.Fatalf("the core lock held 2 min did not quit: %d", quits)
	}
	last, ok := Latest(dir, time.Time{})
	if !ok || !last.Exiting || !strings.Contains(last.What, "engine.mu") {
		t.Fatalf("latest: %+v %v", last, ok)
	}
	if stalls.Load() != 1 {
		t.Fatalf("an exit dump told the owner through the stuck engine: %d", stalls.Load())
	}
	core.Unlock()
}

// SPEC 0.5 V0.181 (#267): the front shows a stalled host offline, so the exit
// is a last resort: the engine's lock held 2 minutes no longer ends fyrd; held
// ExitAfter (10 minutes) it does, and the escalation is logged.
func TestV181ExitIsALastResort(t *testing.T) {
	var logs strings.Builder
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	now := time.Now()
	quits := 0
	w := &Watchdog{Dir: t.TempDir(), Now: func() time.Time { return now }, Quit: func() { quits++ }}
	core := &Mutex{Critical: true, Name: "engine.mu"}
	core.Lock()
	defer core.Unlock()
	for _, at := range []time.Duration{StallAfter, 2 * time.Minute, 5 * time.Minute, ExitAfter - time.Second} {
		now = time.Now().Add(at)
		w.Check()
		if quits != 0 {
			t.Fatalf("fyrd quit with the engine's lock held %s, before the last resort %s", at, ExitAfter)
		}
	}
	if ExitAfter < 10*time.Minute {
		t.Fatalf("ExitAfter %s", ExitAfter)
	}
	if !strings.Contains(logs.String(), "the front shows this host offline") {
		t.Fatalf("the stall of the engine's lock is not logged as such:\n%s", logs.String())
	}
	now = time.Now().Add(ExitAfter)
	w.Check()
	if quits != 1 {
		t.Fatalf("held %s, fyrd did not quit: %d", ExitAfter, quits)
	}
	if !strings.Contains(logs.String(), "last resort") {
		t.Fatalf("the escalation is not logged:\n%s", logs.String())
	}
}

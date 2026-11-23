package lockwatch

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/fsutil"
)

// The limits (SPEC 0.5 V0.147).
const (
	// StallAfter: a watched lock held, or an operation running, this long
	// is a stall: the dump is written and the owner told.
	StallAfter = 10 * time.Second
	// ExitAfter: a critical lock (the engine's) held this long ends fyrd
	// after its dump; the launcher starts it again and the runner keeps
	// the agents (V0.90). V0.181: a last resort. Until then the front
	// shows the stuck host offline and keeps serving the other hosts, the
	// browsers' streams and the peer links (it was 2 minutes).
	ExitAfter = 10 * time.Minute
	// Every is how often the watchdog looks.
	Every = time.Second
	// KeepDumps is how many dump files are kept.
	KeepDumps = 20
)

// Stall is one stall the watchdog saw.
type Stall struct {
	At      time.Time `json:"at"`
	What    string    `json:"what"`    // the longest holder, as a sentence
	File    string    `json:"file"`    // the dump's path
	Exiting bool      `json:"exiting"` // fyrd ends after this dump
}

// Watchdog looks at the watched locks and operations every Every.
type Watchdog struct {
	Dir   string // where dumps go: <data>/stalls
	Stall time.Duration
	Exit  time.Duration
	// OnStall hears each new stall; it runs on its own goroutine, since
	// the lock it would need may be the one that is stuck.
	OnStall func(Stall)
	// Quit ends the process after an exit dump; default os.Exit(70).
	Quit func()
	Now  func() time.Time

	told map[string]bool // holder key → dumped for it
}

func (w *Watchdog) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func key(h Holder) string { return h.Lock + "@" + h.Since.Format(time.RFC3339Nano) }

// Check looks once: the first time a holder passes Stall it writes a dump;
// a critical lock past Exit writes one more and quits.
func (w *Watchdog) Check() {
	stall, exit := w.Stall, w.Exit
	if stall <= 0 {
		stall = StallAfter
	}
	if exit <= 0 {
		exit = ExitAfter
	}
	if w.told == nil {
		w.told = map[string]bool{}
	}
	now := w.now()
	hs := Holders()
	live := map[string]bool{}
	var late []Holder
	exiting := false
	for _, h := range hs {
		k := key(h)
		live[k] = true
		d := h.Held(now)
		if h.Critical && d >= exit {
			exiting = true
		}
		lim := stall
		if h.Limit > 0 {
			lim = h.Limit
		}
		if d >= lim && !w.told[k] {
			w.told[k] = true
			late = append(late, h)
			if h.Critical {
				log.Printf("stall: %s held %s: the front shows this host offline; fyrd exits for a restart if it is still held after %s", h.Lock, d.Round(time.Second), exit)
			}
		}
	}
	for k := range w.told {
		if !live[k] {
			delete(w.told, k)
		}
	}
	if len(late) == 0 && !exiting {
		return
	}
	why := "stall"
	if exiting {
		why = fmt.Sprintf("a critical lock held over %s: fyrd exits", exit)
	}
	s := w.Dump(why, hs, now)
	s.Exiting = exiting
	if w.OnStall != nil && !exiting {
		go w.OnStall(s)
	}
	if exiting {
		log.Printf("stall: escalation, the last resort: a critical lock held over %s; fyrd exits (70) for the launcher to restart it, the runner keeps the agents", exit)
		if w.Quit != nil {
			w.Quit()
		} else {
			os.Exit(70)
		}
	}
}

// Dump writes every goroutine's stack and the held locks to Dir and logs a
// line naming the file; it returns the stall.
func (w *Watchdog) Dump(why string, hs []Holder, now time.Time) Stall {
	if hs == nil {
		hs = Holders()
	}
	s := Stall{At: now.UTC(), What: why}
	if len(hs) > 0 {
		s.What = hs[0].String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "fyrd stall dump %s: %s\n\n", now.UTC().Format(time.RFC3339), why)
	fmt.Fprintf(&b, "Held locks and running operations (the longest first):\n")
	if len(hs) == 0 {
		b.WriteString("  none\n")
	}
	for _, h := range hs {
		fmt.Fprintf(&b, "  %s\n", h)
	}
	b.WriteString("\nGoroutines:\n\n")
	b.Write(Stacks())
	if w.Dir != "" {
		if err := os.MkdirAll(w.Dir, 0o700); err == nil {
			s.File = filepath.Join(w.Dir, now.UTC().Format("20060102T150405.000Z")+".txt")
			if err := fsutil.WriteAtomic(s.File, []byte(b.String()), 0o600); err != nil { // Latest never reads half a dump
				log.Printf("stall: write %s: %v", s.File, err)
				s.File = ""
			}
			prune(w.Dir)
		}
	}
	log.Printf("stall (%s): %s; goroutine dump in %s", why, s.What, s.File)
	return s
}

// Run checks every Every until stop is closed.
func (w *Watchdog) Run(stop <-chan struct{}) {
	t := time.NewTicker(Every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			w.Check()
		}
	}
}

// Latest is the newest dump in dir since since; false without one.
func Latest(dir string, since time.Time) (Stall, bool) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return Stall{}, false
	}
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		if !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		at, err := time.Parse("20060102T150405.000Z", strings.TrimSuffix(e.Name(), ".txt"))
		if err != nil || at.Before(since) {
			continue
		}
		s := Stall{At: at, File: filepath.Join(dir, e.Name())}
		lines := headLines(s.File, 4)
		if len(lines) > 0 {
			s.Exiting = strings.Contains(lines[0], "fyrd exits")
		}
		if len(lines) > 3 {
			s.What = strings.TrimSpace(lines[3])
		}
		return s, true
	}
	return Stall{}, false
}

// headLines is the first n lines of the file at path: a dump holds every
// goroutine's stack (megabytes), and Latest needs its head only.
func headLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []string
	for len(lines) < n && sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func prune(dir string) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	for len(names) > KeepDumps {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

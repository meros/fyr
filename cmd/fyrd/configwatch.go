package main

// V0.180 (#264): fyrd reads its config once, at start. A NixOS switch that changes
// the file does not restart it (restartIfChanged is off: a restart of the unit ends
// the agents), so fyrd remembers what it loaded and says when the file changed
// under it: daemon.log, /healthz and Settings → Hosts. `systemctl reload fyr`
// (SIGHUP to the launcher) applies it with the agents kept (decision 0023).

import (
	"context"
	"crypto/sha256"
	"log"
	"os"
	"sync"
	"time"

	"github.com/meros/fyr/internal/deploy"
)

// configEvery is how often the file is looked at.
const configEvery = 30 * time.Second

// configChangedNote is what the owner reads.
const configChangedNote = "config changed; restart fyrd to apply"

type configWatch struct {
	path   string
	loaded [32]byte // the bytes fyrd read; zero when there was no file
	had    bool

	mu      sync.Mutex
	size    int64
	mtime   time.Time
	changed bool
}

func newConfigWatch(path string) *configWatch {
	b, err := os.ReadFile(path)
	return watchLoaded(path, b, err == nil)
}

// watchLoaded remembers the bytes loadConfig read (had: there was a file).
func watchLoaded(path string, b []byte, had bool) *configWatch {
	w := &configWatch{path: path, size: -1}
	if had {
		w.loaded, w.had = sha256.Sum256(b), true
	}
	if fi, err := os.Stat(path); err == nil {
		w.size, w.mtime = fi.Size(), fi.ModTime()
	}
	return w
}

// check looks at the file: a stat each time, the hash only when the stat moved.
// It reports whether the file differs from what fyrd loaded, and logs a turn.
func (w *configWatch) check() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	fi, err := os.Stat(w.path)
	var now bool
	switch {
	case err != nil:
		now = w.had
		w.size, w.mtime = -1, time.Time{}
	case fi.Size() == w.size && fi.ModTime().Equal(w.mtime):
		return w.changed
	default:
		w.size, w.mtime = fi.Size(), fi.ModTime()
		b, err := os.ReadFile(w.path)
		if err != nil {
			return w.changed // unreadable for a moment: keep what was known
		}
		now = !w.had || sha256.Sum256(b) != w.loaded
	}
	if now != w.changed {
		if now {
			log.Printf("config: %s changed since fyrd read it; restart fyrd to apply (on NixOS: systemctl reload fyr, which keeps the agents)", w.path)
		} else {
			log.Printf("config: %s is back to what fyrd read", w.path)
		}
	}
	w.changed = now
	return now
}

// Note is the owner's words, "" while the file is what fyrd loaded.
func (w *configWatch) Note() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.changed {
		return ""
	}
	return configChangedNote
}

// health is the /healthz check: a warning, never red.
func (w *configWatch) health() error {
	if n := w.Note(); n != "" {
		return deploy.Warning(n)
	}
	return nil
}

func (w *configWatch) run(ctx context.Context, every time.Duration) {
	if w == nil {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.check()
		}
	}
}

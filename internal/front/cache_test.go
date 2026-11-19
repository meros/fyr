package front

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// #22 point 6: a host's cache file is written when its state changed, and a
// busy host's at most every saveGap; the front's stop writes the rest.
func TestCacheWritesOnlyOnChangeAndAtMostEverySaveGap(t *testing.T) {
	dir := t.TempDir()
	f := New(Options{CacheDir: dir}, []HostConfig{{Name: "one", URL: "http://127.0.0.1:1"}})
	l := f.all()[0]
	path := filepath.Join(dir, "one.json")
	written := func() bool {
		_, err := os.Stat(path)
		os.Remove(path) // so the next write shows
		return err == nil
	}
	change := func(rev string) {
		f.mu.Lock()
		l.raw = map[string]map[string]any{"t_1": {"id": "t_1", "rev": rev}}
		l.dirty = true
		f.mu.Unlock()
	}
	t0 := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)

	change("1")
	f.save(t0, false)
	if !written() {
		t.Fatal("a changed host was not written")
	}
	f.mu.Lock()
	l.dirty = true // marked, but nothing the file holds changed
	f.mu.Unlock()
	f.save(t0.Add(time.Minute), false)
	if written() {
		t.Fatal("a host whose state did not change was written again")
	}
	change("2")
	f.save(t0.Add(time.Minute+saveEvery), false)
	if !written() {
		t.Fatal("a change after the gap was not written")
	}
	for i := 3; i < 10; i++ { // a busy host: a change every saveEvery
		change(string(rune('0' + i)))
		f.save(t0.Add(time.Minute+time.Duration(i-1)*saveEvery), false)
	}
	if written() {
		t.Fatal("a busy host was written within saveGap")
	}
	f.save(t0.Add(time.Minute+saveEvery+saveGap), false)
	if !written() {
		t.Fatal("a busy host's change was not written after saveGap")
	}
	change("x")
	f.save(t0.Add(time.Minute+saveEvery+saveGap+time.Second), true)
	if !written() {
		t.Fatal("the front's stop did not write the last change")
	}
}

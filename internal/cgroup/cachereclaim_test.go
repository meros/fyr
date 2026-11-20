// V0.cache-reclaim: the agents' page cache is dropped ahead of memory.high.
package cgroup

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitCache waits for the background reclaim of tr to end.
func waitCache(t *testing.T, tr *Tree) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		tr.cacheMu.Lock()
		running := tr.cache.running
		tr.cacheMu.Unlock()
		if !running {
			return
		}
	}
	t.Fatal("the cache reclaim did not end")
}

func TestCacheReclaimLeavesTheAgentsUnderNinetyPercentAlone(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "memory.high"), "12288M")
	write(t, filepath.Join(d, "memory.current"), "11000M") // 89.5 %
	write(t, filepath.Join(d, "memory.reclaim"), "")
	tr := &Tree{agents: d}
	tr.ReclaimCache(time.Now())
	waitCache(t, tr)
	if got := read(t, filepath.Join(d, "memory.reclaim")); got != "" {
		t.Fatalf("under %v of memory.high: wrote %q, want nothing", CacheReclaimAt, got)
	}
	write(t, filepath.Join(d, "memory.high"), "max")
	write(t, filepath.Join(d, "memory.current"), "30G")
	tr.ReclaimCache(time.Now())
	waitCache(t, tr)
	if got := read(t, filepath.Join(d, "memory.reclaim")); got != "" {
		t.Fatalf("no memory.high: wrote %q, want nothing", got)
	}
}

// 20 Nov 14:52: the agents at 11 976 of 12 288 MiB; a reclaim to 80 % asks
// for 1 646 MiB of cache, and swappiness=0 keeps it off the anonymous memory.
func TestCacheReclaimAsksTheAgentsForCacheDownToEightyPercent(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "memory.high"), "12288M")
	write(t, filepath.Join(d, "memory.current"), "11976M")
	write(t, filepath.Join(d, "memory.reclaim"), "")
	var lines []string
	tr := &Tree{agents: d, logf: func(f string, a ...any) { lines = append(lines, f) }}
	tr.ReclaimCache(time.Now())
	waitCache(t, tr)
	high, to := float64(12288<<20), CacheReclaimTo
	want := uint64(11976<<20) - uint64(high*to)
	if got := read(t, filepath.Join(d, "memory.reclaim")); got != itoaT(want)+" swappiness=0" {
		t.Fatalf("wrote %q, want %d bytes of cache only", got, want)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "page cache ahead of memory.high") {
		t.Fatalf("log %q, want one line that says what was dropped", lines)
	}
}

// The scope layout: the slice is systemd's (no memory.reclaim fyrd may
// write), each thread's scope is fyrd's; the cache is asked of the scopes in
// proportion to their inactive cache, and a scope with little is skipped.
func TestCacheReclaimSplitsOverTheScopesByTheirInactiveCache(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "memory.high"), "10240M")
	write(t, filepath.Join(d, "memory.current"), "10240M") // want: 2048M
	a, b, c, e := filepath.Join(d, "fyr-agent-t_a.scope"), filepath.Join(d, "fyr-agent-t_b.scope"), filepath.Join(d, "fyr-agent-t_c.scope"), filepath.Join(d, "other")
	write(t, filepath.Join(a, "memory.stat"), "anon 1\ninactive_file 3221225472\n") // 3 GiB
	write(t, filepath.Join(b, "memory.stat"), "anon 1\ninactive_file 1073741824\n") // 1 GiB
	write(t, filepath.Join(c, "memory.stat"), "anon 1\ninactive_file 10485760\n")   // 10 MiB: a share under the least
	write(t, filepath.Join(e, "memory.stat"), "inactive_file 1073741824\n")         // no memory.reclaim: not a cgroup fyrd writes
	for _, s := range []string{a, b, c} {
		write(t, filepath.Join(s, "memory.reclaim"), "")
	}
	tr := &Tree{agents: d}
	tr.ReclaimCache(time.Now())
	waitCache(t, tr)
	total := float64(3<<30 + 1<<30 + 10<<20)
	want := float64(2048 << 20)
	for _, c := range []struct {
		dir  string
		want string
	}{
		{a, itoaT(uint64(want*float64(3<<30)/total)) + " swappiness=0"},
		{b, itoaT(uint64(want*float64(1<<30)/total)) + " swappiness=0"},
		{c, ""},
	} {
		if got := read(t, filepath.Join(c.dir, "memory.reclaim")); got != c.want {
			t.Errorf("%s: wrote %q, want %q", filepath.Base(c.dir), got, c.want)
		}
	}
}

func itoaT(n uint64) string {
	var b []byte
	for {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
		if n == 0 {
			return string(b)
		}
	}
}

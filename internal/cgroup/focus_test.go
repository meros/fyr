// SPEC 0.5 V0.85: the thread a device shows gets cpu.weight 300 while the
// beat lasts, and 100 again when it expires or the device moves on.
package cgroup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestV085FocusFollowsTheBeat(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	if tr == nil {
		t.Fatalf("Setup is off: %v", l.lines)
	}
	weight := func(id string) string { return read(t, filepath.Join(o.FS, unit, "agents", id, "cpu.weight")) }
	tr.Thread("t_a")
	tr.Thread("t_b")
	now := time.Now()
	tr.Focus("phone", "t_a", now)
	if weight("t_a") != "300" || weight("t_b") != "100" {
		t.Fatalf("focused a: a=%s b=%s", weight("t_a"), weight("t_b"))
	}
	// A beat 30 s later keeps it; 45 s after the last beat it expires.
	tr.Focus("phone", "t_a", now.Add(30*time.Second))
	tr.Expire(now.Add(60 * time.Second))
	if weight("t_a") != "300" {
		t.Fatalf("expired early: %s", weight("t_a"))
	}
	tr.Expire(now.Add(76 * time.Second))
	if weight("t_a") != "100" {
		t.Fatalf("not expired: %s", weight("t_a"))
	}
	// The device moves to b: a drops at once, b rises.
	now = now.Add(80 * time.Second)
	tr.Focus("phone", "t_a", now)
	tr.Focus("phone", "t_b", now)
	if weight("t_a") != "100" || weight("t_b") != "300" {
		t.Fatalf("moved to b: a=%s b=%s", weight("t_a"), weight("t_b"))
	}
	// Two devices on b: one going off keeps it up.
	tr.Focus("desk", "t_b", now)
	tr.Unfocus("phone", "t_b", now)
	if weight("t_b") != "300" {
		t.Fatalf("one device left: %s", weight("t_b"))
	}
	tr.Unfocus("desk", "t_b", now)
	if weight("t_b") != "100" {
		t.Fatalf("both left: %s", weight("t_b"))
	}
	// A focus does not make a cgroup for a thread that has none; one made
	// while the thread is focused starts at 300.
	tr.Focus("phone", "t_c", now)
	if _, err := os.Stat(filepath.Join(o.FS, unit, "agents", "t_c")); err == nil {
		t.Fatal("focus made a cgroup")
	}
	tr.Thread("t_c")
	if weight("t_c") != "300" {
		t.Fatalf("new focused cgroup: %s", weight("t_c"))
	}
}

func TestV085FocusWithoutCgroups(t *testing.T) {
	var tr *Tree
	tr.Focus("phone", "t_a", time.Now())
	tr.Unfocus("phone", "t_a", time.Now())
	tr.Expire(time.Now())
}

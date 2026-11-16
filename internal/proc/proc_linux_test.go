package proc

import (
	"os/exec"
	"testing"
)

// SPEC 0.5 V0.80: a command made with a cgroup context starts in it (clone3
// CLONE_INTO_CGROUP); without one, or with "", it starts as before. A plain
// directory is not a cgroup, so the kernel refuses the start: the daemon
// only hands out directories its probe proved.
func TestV080CommandStartsInTheContextsCgroup(t *testing.T) {
	ctx, done := WithCgroup(t.Context(), t.TempDir())
	defer done()
	cmd := Command(ctx, "true")
	if a := cmd.SysProcAttr; a == nil || !a.UseCgroupFD || a.CgroupFD <= 2 || !a.Setpgid {
		t.Fatalf("attrs %+v", a)
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("a plain directory took the child")
	}
	plain, done2 := WithCgroup(t.Context(), "")
	defer done2()
	if a := Command(plain, "true").SysProcAttr; a.UseCgroupFD {
		t.Fatal("no cgroup asked, one set")
	}
	c := exec.Command("true")
	closeFD, err := IntoCgroup(c, "")
	closeFD()
	if err != nil || c.SysProcAttr != nil {
		t.Fatalf("IntoCgroup with no dir: %v %+v", err, c.SysProcAttr)
	}
}

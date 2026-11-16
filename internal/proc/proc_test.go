package proc

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// #39: Kill on a grouped child without a context stops what it started too,
// and Wait returns although the background child held stdout.
func TestKillStopsTheGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 600 & echo $!; wait")
	Group(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(line))
	if pid <= 0 {
		t.Fatalf("bad child pid %q", line)
	}
	if err := Kill(cmd); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(WaitDelay + time.Second):
		t.Fatal("Wait did not return after Kill")
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && !zombie(pid) {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the background child %d outlived Kill", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// zombie reports whether pid has exited and waits only to be reaped.
func zombie(pid int) bool {
	st, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	i := strings.LastIndexByte(string(st), ')')
	return err == nil && i >= 0 && strings.HasPrefix(string(st[i+1:]), " Z")
}

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

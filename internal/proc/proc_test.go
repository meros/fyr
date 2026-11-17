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

package cgroup

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// V0.forwarded-ports: a thread's ports are the ones its own processes listen
// on, read from the real /proc: this test's process is put in a thread's
// cgroup and listens.
func TestThreadPortsAreTheListeningPortsOfItsProcesses(t *testing.T) {
	needLinux(t)
	tr, agents := scratch(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	putProcs(t, filepath.Join(agents, "t_a"), os.Getpid())
	putProcs(t, filepath.Join(agents, "t_b"), 1)
	l := &Leftovers{Tree: tr}
	if got := l.ThreadPorts("t_a", nil); !slices.Contains(got, port) {
		t.Fatalf("t_a listens on %d: %v", port, got)
	}
	if got := l.ThreadPorts("t_b", nil); slices.Contains(got, port) {
		t.Fatalf("another thread has the port: %v", got)
	}
	for _, th := range []string{"", "t_none"} {
		if got := l.ThreadPorts(th, nil); len(got) != 0 {
			t.Fatalf("%q: %v", th, got)
		}
	}
	// A connected socket is no listening port: only the listener counts.
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	local := c.LocalAddr().(*net.TCPAddr).Port
	if got := l.ThreadPorts("t_a", nil); slices.Contains(got, local) {
		t.Fatalf("a client socket's port %d is listed: %v", local, got)
	}
}

func TestListenPortsReadsOnlyLoopbackAndAnyAddresses(t *testing.T) {
	proc := t.TempDir()
	fd := filepath.Join(proc, "42", "fd")
	os.MkdirAll(fd, 0o755)
	os.MkdirAll(filepath.Join(proc, "net"), 0o755)
	for i, in := range []string{"100", "101", "102", "103", "104", "105"} {
		os.Symlink("socket:["+in+"]", filepath.Join(fd, string(rune('3'+i))))
	}
	os.Symlink("/dev/null", filepath.Join(fd, "0"))
	head := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	os.WriteFile(filepath.Join(proc, "net", "tcp"), []byte(head+
		"   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 100 1 0 100 0 0 10 0\n"+ // 127.0.0.1:5173
		"   1: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 101 1 0 100 0 0 10 0\n"+ // 0.0.0.0:3000
		"   2: 0A00000A:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 102 1 0 100 0 0 10 0\n"+ // 10.0.0.10:8080: not loopback
		"   3: 0100007F:C350 0100007F:1435 01 00000000:00000000 00:00000000 00000000  1000        0 103 1 0 100 0 0 10 0\n"+ // established
		"   4: 0100007F:2328 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 999 1 0 100 0 0 10 0\n"), 0o644) // another process's
	os.WriteFile(filepath.Join(proc, "net", "tcp6"), []byte(head+
		"   0: 00000000000000000000000001000000:1F41 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 104 1 0 100 0 0 10 0\n"+ // [::1]:8001
		"   1: 00000000000000000000000000000000:1F42 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 105 1 0 100 0 0 10 0\n"), 0o644) // [::]:8002
	if got := ListenPorts(proc, []int{42, 43}); !slices.Equal(got, []int{3000, 5173, 8001, 8002}) {
		t.Fatalf("ports: %v", got)
	}
	if got := ListenPorts(proc, []int{43}); len(got) != 0 {
		t.Fatalf("a process without sockets: %v", got)
	}
}

func TestLsofPortsOfTheGivenProcesses(t *testing.T) {
	out := "p100\nn*:5173\nn127.0.0.1:9229\np200\nn[::1]:8001\nn192.168.1.5:8080\np300\nn*:7000\n"
	if got := lsofPorts(out, []int{100, 200}); !slices.Equal(got, []int{5173, 8001, 9229}) {
		t.Fatalf("ports: %v", got)
	}
	if got := lsofPorts(out, nil); len(got) != 0 {
		t.Fatalf("no processes: %v", got)
	}
}

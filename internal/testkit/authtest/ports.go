package authtest

import (
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// reservePort gives a loopback port that an install keeps across its restarts (its
// peer port, its host port), and release, called once no process listens on it
// any more (V0.port-race).
//
// A port the kernel chose with :0 cannot be kept that way: between the stop and the
// next start it is free, and any listen on :0 (the restarted fyrd's own HTTP
// listener, another test's) may be handed exactly that port. So the port is taken
// below the kernel's ephemeral range, where a listen on :0 and an outgoing
// connection never land, and a lock file held until release keeps every other
// harness, in this process or another test binary, off it.
func reservePort() (int, func(), error) {
	low := ephemeralLow()
	lo := max(1024, low-10000)
	if lo >= low {
		return 0, nil, fmt.Errorf("no port range below the ephemeral range (it starts at %d)", low)
	}
	// not os.TempDir: nix develop gives each shell its own TMPDIR, and the lock must
	// be seen by every test binary of this user on the host
	dir := filepath.Join("/tmp", fmt.Sprintf("fyr-test-ports-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, nil, err
	}
	for range 500 {
		p := lo + rand.IntN(low-lo)
		f, err := os.OpenFile(filepath.Join(dir, strconv.Itoa(p)), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return 0, nil, err
		}
		// an flock belongs to the open file, so it also keeps another reservation in
		// this process off the port; the kernel drops it if the process dies
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			f.Close()
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err != nil { // someone outside the harness listens there
			f.Close()
			continue
		}
		ln.Close()
		return p, func() { f.Close() }, nil
	}
	return 0, nil, fmt.Errorf("no free port in %d-%d", lo, low-1)
}

// ephemeralLow is where the kernel's range for :0 and outgoing connections
// starts. 32768 where it cannot be read: Linux's default, and below macOS's 49152.
func ephemeralLow() int {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		return 32768
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 32768
	}
	n, err := strconv.Atoi(f[0])
	if err != nil {
		return 32768
	}
	return n
}

// ReservedListen is a loopback address on a reserved port (reservePort) for a
// listener a test keeps across a restart, such as a fixed peerListen. The port is
// released when the test ends, after the cleanups registered later (a fyrd's Stop).
func ReservedListen(t testing.TB) string {
	t.Helper()
	p, release, err := reservePort()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(p))
}

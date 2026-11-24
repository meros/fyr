package cgroup

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/proc"
)

// V0.forwarded-ports: the TCP ports a thread's own processes listen on. Only
// those are forwarded to the owner's browser, so a link in a thread reaches
// the thread's servers and nothing else on the host.

// ThreadPIDs is the processes of a thread: its cgroup's, or without a cgroup
// tree its CLI's descendants and the processes remembered as its own.
func (l *Leftovers) ThreadPIDs(thread string) []int {
	if thread == "" {
		return nil
	}
	if l.Tree != nil {
		var out []int
		for _, c := range l.Tree.threadCgroups() {
			if c.Name != thread {
				continue
			}
			filepath.WalkDir(c.Dir, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && d.Name() == "cgroup.procs" {
					out = append(out, pids(p)...)
				}
				return nil
			})
		}
		return out
	}
	t := l.table(l.now())
	if t == nil {
		return nil
	}
	children := map[int][]int{}
	for p, pi := range t {
		children[pi.ppid] = append(children[pi.ppid], p)
	}
	seen := map[int]bool{}
	var walk func(int)
	walk = func(p int) {
		if _, ok := t[p]; !ok || seen[p] {
			return
		}
		seen[p] = true
		for _, c := range children[p] {
			walk(c)
		}
	}
	if l.CLIs != nil {
		if cli := l.CLIs()[thread]; cli > 0 {
			walk(cli)
		}
	}
	l.mu.Lock()
	var mine []int
	for k, th := range l.owned {
		if th != thread {
			continue
		}
		pidS, startS, _ := strings.Cut(k, "@")
		pid, _ := strconv.Atoi(pidS)
		if pi, ok := t[pid]; ok && strconv.FormatUint(pi.start, 10) == startS {
			mine = append(mine, pid)
		}
	}
	l.mu.Unlock()
	for _, p := range mine {
		walk(p)
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// loopbackOrAny reports a listening address a loopback dial reaches, from its
// hex form in /proc/net/tcp: 127.0.0.0/8, 0.0.0.0, ::1, :: and the IPv4
// addresses mapped into IPv6.
func loopbackOrAny(hexAddr string) bool {
	switch len(hexAddr) {
	case 8: // IPv4, in host byte order: 0100007F is 127.0.0.1
		return hexAddr == "00000000" || strings.HasSuffix(strings.ToUpper(hexAddr), "7F")
	case 32:
		u := strings.ToUpper(hexAddr)
		if u == strings.Repeat("0", 32) || u == strings.Repeat("0", 24)+"01000000" {
			return true
		}
		if strings.HasPrefix(u, strings.Repeat("0", 16)+"FFFF0000") {
			return loopbackOrAny(u[24:])
		}
	}
	return false
}

// ListenPorts reads, on Linux, the TCP ports the given processes listen on at
// an address a loopback dial reaches: each process's socket inodes
// (/proc/<pid>/fd) against the listening sockets of /proc/net/tcp and tcp6.
func ListenPorts(procDir string, pids []int) []int {
	if procDir == "" {
		procDir = "/proc"
	}
	inodes := map[string]bool{}
	for _, pid := range pids {
		dir := filepath.Join(procDir, strconv.Itoa(pid), "fd")
		es, _ := os.ReadDir(dir)
		for _, e := range es {
			if to, err := os.Readlink(filepath.Join(dir, e.Name())); err == nil {
				if in, ok := strings.CutPrefix(to, "socket:["); ok {
					inodes[strings.TrimSuffix(in, "]")] = true
				}
			}
		}
	}
	if len(inodes) == 0 {
		return nil
	}
	ports := map[int]bool{}
	for _, name := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(procDir, "net", name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			// sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode
			fl := strings.Fields(sc.Text())
			if len(fl) < 10 || fl[3] != "0A" || !inodes[fl[9]] { // 0A: LISTEN
				continue
			}
			addr, portHex, ok := strings.Cut(fl[1], ":")
			if !ok || !loopbackOrAny(addr) {
				continue
			}
			if p, err := strconv.ParseUint(portHex, 16, 16); err == nil && p > 0 {
				ports[int(p)] = true
			}
		}
		f.Close()
	}
	return sortedPorts(ports)
}

func sortedPorts(ports map[int]bool) []int {
	out := make([]int, 0, len(ports))
	for p := range ports {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// LsofListen runs lsof for the listening TCP sockets, on a host without /proc.
func LsofListen(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := proc.Command(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpn")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if len(out) > 0 {
		err = nil // lsof exits 1 when some process could not be read
	}
	return string(out), err
}

// lsofPorts reads `lsof -nP -iTCP -sTCP:LISTEN -Fpn` (a "p<pid>" line, then an
// "n<addr>:<port>" line per socket) for the given processes.
func lsofPorts(out string, pids []int) []int {
	want := map[int]bool{}
	for _, p := range pids {
		want[p] = true
	}
	ports := map[int]bool{}
	cur := 0
	for _, ln := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(ln, "p"):
			cur, _ = strconv.Atoi(ln[1:])
		case strings.HasPrefix(ln, "n") && want[cur]:
			i := strings.LastIndexByte(ln, ':')
			if i < 0 {
				continue
			}
			switch addr := strings.Trim(ln[1:i], "[]"); {
			case addr == "*", addr == "::", addr == "::1", addr == "0.0.0.0", addr == "localhost", strings.HasPrefix(addr, "127."):
				if p, err := strconv.Atoi(ln[i+1:]); err == nil && p > 0 && p < 65536 {
					ports[p] = true
				}
			}
		}
	}
	return sortedPorts(ports)
}

// ThreadPorts is the TCP ports the thread's processes listen on, reachable
// from loopback. lsof gives the listening sockets on a host without /proc
// (the tracker's PS mode); nil there means no port is known.
func (l *Leftovers) ThreadPorts(thread string, lsof func() (string, error)) []int {
	pids := l.ThreadPIDs(thread)
	if len(pids) == 0 {
		return nil
	}
	if l.PS == nil {
		return ListenPorts(l.Proc, pids)
	}
	if lsof == nil {
		return nil
	}
	out, err := lsof()
	if err != nil {
		return nil
	}
	return lsofPorts(out, pids)
}

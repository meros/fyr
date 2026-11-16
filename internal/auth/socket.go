package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// SocketPath is where an install's socket called name lives: <data>/name,
// or a short path in the runtime folder when that one is too long for a
// Unix socket (as the runner's, V0.90).
func SocketPath(data, name string) string {
	p := filepath.Join(data, name)
	if len(p) < 100 {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	sum := sha256.Sum256([]byte(data))
	return filepath.Join(dir, "fyr-"+hex.EncodeToString(sum[:6])+"-"+name)
}

// OwnerSocket is the owner socket's name (V0.130 (1)).
const OwnerSocket = "owner.sock"

// ServeSocket is where Tailscale Serve proxies the browser (V0.130 (3)).
const ServeSocket = "serve.sock"

// ListenUnix listens on a Unix socket made with mode 0600. A stale socket
// is removed; a live one is an error.
func ListenUnix(path string) (net.Listener, error) {
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, fmt.Errorf("something already listens on %s", path)
	}
	os.Remove(path)
	old := syscall.Umask(0o077)
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// OwnerListener accepts only connections whose peer is fyrd's own user or
// root (V0.130 (1)); any other is closed and logged.
type OwnerListener struct{ net.Listener }

func (l OwnerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uid, err := peerUID(c)
		if err == nil && PeerAllowed(uid, os.Getuid()) {
			return c, nil
		}
		log.Printf("auth: owner socket refused a connection (uid %d, %v)", uid, err)
		c.Close()
	}
}

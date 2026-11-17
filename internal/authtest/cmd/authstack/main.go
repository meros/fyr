// authstack starts the harness's live fixture for the browser harness
// (web/test/auth.e2e.mjs, decision 0030 4.5): a fake tailscaled, a Tailscale
// Serve emulator, a laptop fyrd and a server fyrd in temp folders. It prints
// one JSON line with the addresses, and stops everything when its stdin
// closes or it is signalled.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authtest"
)

func main() {
	l, err := authtest.NewLive()
	if err != nil {
		fmt.Fprintln(os.Stderr, "authstack:", err)
		os.Exit(1)
	}
	defer l.Close()
	// Two more devices of the owner that the install has not seen: one waits in each size of the e2e.
	var tablets []string
	for i, name := range []string{"tablet", "kindle"} {
		n := authtest.Node{Name: name + ".tail-test.ts.net", IP: fmt.Sprintf("100.64.0.%d", 20+i), UserID: 1, Online: true}
		l.TS.AddNode(n)
		sv, err := l.TS.ServeFor(n, l.Laptop.Sock(auth.ServeSocket))
		if err != nil {
			fmt.Fprintln(os.Stderr, "authstack:", err)
			os.Exit(1)
		}
		defer sv.Close()
		tablets = append(tablets, sv.URL)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{
		"ownerSock":  l.Laptop.Sock(auth.OwnerSocket), // the owner socket: the e2e adds, approves and switches through it
		"tablets":    tablets,                         // owner nodes the install has not seen
		"serve":      l.Phones.URL,                    // the phone's path: Serve in front of serve.sock
		"loopback":   l.Laptop.URL(),                  // the laptop's loopback port
		"public":     l.Self.Name + ":7443",
		"ownerLogin": l.OwnerLogin,
		"local":      l.Local,
		"tailnet":    l.Tailnet,
	})
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, os.Stdin); close(done) }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-done:
	case <-sig:
	}
}

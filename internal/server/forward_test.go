package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// V0.forwarded-ports: the host lists a thread's ports and carries a
// connection to one of them, both ways at once, and to no other port.
func TestForwardCarriesAConnectionToAThreadsPortOnly(t *testing.T) {
	hs, e := newServer(t)
	r := e.Command(api.Cmd{CommandID: "c1", Action: "thread.create", Args: core.Args{"cwd": t.TempDir(), "text": "x", "profile": "claude"}})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	// The thread's server: it answers each line it gets, upper-cased by a prefix.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	closed := make(chan struct{}, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { c.Close(); closed <- struct{}{} }()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					io.WriteString(c, "got "+sc.Text()+"\n")
				}
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	other, _ := net.Listen("tcp", "127.0.0.1:0") // a server of the host that is not the thread's
	defer other.Close()
	e.LeftoverDeps.ThreadPorts = func(thread string) []int {
		if thread == r.ThreadID {
			return []int{port}
		}
		return nil
	}
	base := hs.URL + "/api/threads/" + r.ThreadID

	resp := do(t, "GET", base+"/ports", "", nil, "")
	var got struct{ Ports []int }
	json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != 200 || len(got.Ports) != 1 || got.Ports[0] != port {
		t.Fatalf("ports: %d %+v", resp.StatusCode, got)
	}

	open := func(p string) (*http.Response, *io.PipeWriter) {
		pr, pw := io.Pipe()
		req, _ := http.NewRequest("POST", base+"/forward/"+p, pr)
		req.Header.Set("X-Fyr", "1")
		req.Header.Set("Accept-Encoding", "identity")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp, pw
	}
	resp, pw := open(strconv.Itoa(port))
	if resp.StatusCode != 200 {
		t.Fatalf("forward: %d", resp.StatusCode)
	}
	rd := bufio.NewReader(resp.Body)
	// Two exchanges on one connection: each answer is read before the next line is sent.
	for _, word := range []string{"one", "two"} {
		io.WriteString(pw, word+"\n")
		line := make(chan string, 1)
		go func() { s, _ := rd.ReadString('\n'); line <- s }()
		select {
		case s := <-line:
			if s != "got "+word+"\n" {
				t.Fatalf("answer to %s: %q", word, s)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no answer to %s while the request is still open", word)
		}
	}
	// The browser's side ends: the server's connection is closed.
	pw.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the server's connection stayed open after the browser's side ended")
	}
	resp.Body.Close()

	for _, p := range []string{strconv.Itoa(other.Addr().(*net.TCPAddr).Port), "0", "70000", "x"} {
		resp, pw := open(p)
		pw.Close()
		if resp.StatusCode != 404 {
			t.Errorf("port %s: %d, want 404", p, resp.StatusCode)
		}
		resp.Body.Close()
	}
	// The server stopped: its port is the thread's no more.
	e.LeftoverDeps.ThreadPorts = func(string) []int { return nil }
	resp, pw = open(strconv.Itoa(port))
	pw.Close()
	if resp.StatusCode != 404 {
		t.Errorf("after the server stopped: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

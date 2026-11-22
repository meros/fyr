// neutralstack starts the fixture of decision 0031 test 1 through the browser
// (web/test/neutral.e2e.mjs, slice 4): two scratch fyrds a and b, role both, no
// FYR_* in the environment, paired, each following the other. Each install
// holds one folder preview whose page loads its own file (app.js), as an
// agent's show would leave it. It prints one JSON line with each install's
// URL, the owner's session cookie for it and the preview's id. A line
// "restart a" (or b) on stdin restarts that install's fyrd, keeping its data,
// and answers "ok <its new URL>" (it listens on a new port; a cookie is not
// bound to the port, so the browser's session holds); closing stdin stops
// everything.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/testkit/authtest"
)

func fail(err error) {
	fmt.Fprintln(os.Stderr, "neutralstack:", err)
	authtest.Cleanup()
	os.Exit(1)
}

// preview leaves a folder preview in the install's attachments: index.html,
// whose script is its own relative file app.js, which writes the install's
// name into the page and posts it to the app that framed it.
func preview(data, name string) (string, error) {
	src, err := os.MkdirTemp("", "fyr-neutral-preview-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(src)
	page := `<!doctype html><title>neutral</title><p id="who">loading</p><script src="app.js"></script>`
	// The page has no origin (its sandbox): it tells the app around it by a message.
	js := fmt.Sprintf(`document.getElementById("who").textContent = "preview of %s"; parent.postMessage({ neutral: "preview of %s" }, "*");`, name, name)
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte(page), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "app.js"), []byte(js), 0o644); err != nil {
		return "", err
	}
	files, _, err := attach.ScanTree(src)
	if err != nil {
		return "", err
	}
	m, err := (&attach.Store{Dir: filepath.Join(data, "attachments")}).SaveTree("neutral", "index.html", files)
	return m.ID, err
}

func main() {
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "FYR_") {
			fail(fmt.Errorf("the environment carries %s", strings.SplitN(e, "=", 2)[0]))
		}
	}
	m, err := authtest.NewMesh(authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1})
	if err != nil {
		fail(err)
	}
	defer m.Close()
	out := map[string]any{}
	for _, pair := range [][2]string{{"a", "b"}, {"b", "a"}} {
		n := m.Node(pair[0])
		if _, err := n.Fyrd.WaitRoster(60*time.Second, func(r *authtest.Roster) bool { return strings.Contains(strings.Join(r.Others(), ","), pair[1]) }); err != nil {
			fail(fmt.Errorf("%s never paired with %s: %w", pair[0], pair[1], err))
		}
		link, err := n.Fyrd.OwnerOnce()
		if err != nil {
			fail(err)
		}
		cookie, err := authtest.Redeem(n.Fyrd.URL(), link)
		if err != nil {
			fail(err)
		}
		p, err := preview(n.Fyrd.Data, pair[0])
		if err != nil {
			fail(err)
		}
		out[pair[0]] = map[string]string{"url": n.Fyrd.URL(), "cookie": cookie, "preview": p}
	}
	json.NewEncoder(os.Stdout).Encode(out)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	for {
		select {
		case <-sig:
			return
		case l, ok := <-lines:
			if !ok {
				return
			}
			name, isRestart := strings.CutPrefix(strings.TrimSpace(l), "restart ")
			if !isRestart || (name != "a" && name != "b") {
				fmt.Println("unknown: " + l)
				continue
			}
			if err := m.Node(name).Fyrd.Restart(); err != nil {
				fmt.Println("error: " + err.Error())
				continue
			}
			fmt.Println("ok " + m.Node(name).Fyrd.URL())
		}
	}
}

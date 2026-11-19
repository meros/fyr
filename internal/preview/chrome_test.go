package preview

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"
)

// SPEC 0.5 V0.19: one long-lived Chrome takes screenshots at a size; a
// second shot reuses the browser and is fast.
func TestShootRealChrome(t *testing.T) {
	bin := FindChrome()
	if bin == "" {
		t.Skip("no Chrome on this host")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><body style="margin:0;background:#e8960f;height:3000px"><h1>Fyr</h1></body>`))
	}))
	defer page.Close()
	c := &Chrome{Bin: bin, Dir: t.TempDir(), Idle: time.Minute}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := c.Shoot(ctx, page.URL, 390, 844, false)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if w := img.Bounds().Dx(); w != 390 && w != 780 {
		t.Fatalf("width %d", w)
	}
	r, g, _, _ := img.At(10, img.Bounds().Dy()-10).RGBA()
	if r>>8 < 200 || g>>8 < 120 {
		t.Fatalf("not the page's amber background: %d %d", r>>8, g>>8)
	}
	// The second shot reuses the browser: the same process, still running.
	// The proof is the process, not a wall-time budget, which a busy host
	// broke (V0.fix-flakes).
	first := c.cmd
	full, err := c.Shoot(ctx, page.URL, 1280, 800, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.cmd != first || isClosed(c.dead) {
		t.Fatal("the second shot started another browser; the first was not reused")
	}
	fi, _ := png.Decode(bytes.NewReader(full))
	if fi.Bounds().Dy() < 2000 {
		t.Fatalf("full page height %d", fi.Bounds().Dy())
	}
	pid := c.cmd.Process.Pid
	c.Close()
	// The group's processes are killed; they are gone once reaped, which
	// takes a moment on a busy host.
	for end := time.Now().Add(10 * time.Second); syscall.Kill(-pid, 0) == nil; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("Chrome processes survived Close")
		}
	}
}

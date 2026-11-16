package projicon

import (
	"bytes"
	"context"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withText puts a tEXt chunk (metadata) into a PNG after IHDR.
func withText(b []byte, key, val string) []byte {
	data := append([]byte(key+"\x00"), val...)
	chunk := make([]byte, 0, 12+len(data))
	n := len(data)
	chunk = append(chunk, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	chunk = append(chunk, "tEXt"...)
	chunk = append(chunk, data...)
	chunk = append(chunk, 0, 0, 0, 0) // the CRC, set below: the decoder checks it
	crc := crc32.ChecksumIEEE(append([]byte("tEXt"), data...))
	chunk[len(chunk)-4], chunk[len(chunk)-3], chunk[len(chunk)-2], chunk[len(chunk)-1] = byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc)
	ihdrEnd := 8 + 25
	out := append([]byte{}, b[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, b[ihdrEnd:]...)
}

// SPEC V0.116 (safety): only PNG, JPEG and GIF are read; SVG, HTML and
// anything over 2 MB are refused with a reason the agent can act on.
func TestDecodeAcceptsImagesOnly(t *testing.T) {
	if _, err := Decode(pngBytes(t, solid(10, 10, color.White))); err != nil {
		t.Fatalf("png: %v", err)
	}
	var jb bytes.Buffer
	jpeg.Encode(&jb, solid(10, 10, color.White), nil)
	if _, err := Decode(jb.Bytes()); err != nil {
		t.Fatalf("jpeg: %v", err)
	}
	for name, b := range map[string][]byte{
		"svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html": []byte("<!doctype html><p>hi"),
		"junk": []byte("not an image at all"),
	} {
		if _, err := Decode(b); err == nil {
			t.Fatalf("%s was accepted", name)
		} else if name == "svg" && !strings.Contains(err.Error(), "PNG") {
			t.Fatalf("svg: the reason does not say what to do: %v", err)
		}
	}
	big := make([]byte, MaxBytes+1)
	copy(big, pngBytes(t, solid(4, 4, color.White)))
	if _, err := Decode(big); err == nil || !strings.Contains(err.Error(), "2 MB") {
		t.Fatalf("over 2 MB: %v", err)
	}
}

// SPEC V0.116: a picture over the pixel limit is refused before it is
// decoded (a small file can claim a huge picture).
func TestDecodeRefusesHugeDimensions(t *testing.T) {
	b := pngBytes(t, solid(2, 2, color.White))
	// IHDR width and height are bytes 16..24.
	b[16], b[17], b[18], b[19] = 0, 0x01, 0, 0 // 65536 wide
	if _, err := Decode(b); err == nil {
		t.Fatal("a 65536-wide picture was accepted")
	}
}

// SPEC V0.116: the stored icon is square at 64 and 128 px, a PNG without
// the source's metadata, and its version changes with its content.
func TestSaveSquaresScalesAndStrips(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	src := withText(pngBytes(t, solid(300, 200, color.NRGBA{200, 30, 60, 255})), "Comment", "secret-gps-here")
	img, err := Decode(src)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.Save("p_1", img)
	if err != nil || v1 == "" {
		t.Fatalf("%q %v", v1, err)
	}
	for _, n := range Sizes {
		b, err := os.ReadFile(s.Path("p_1", n))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("secret-gps-here")) {
			t.Fatalf("%d px keeps the metadata", n)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || format != "png" || cfg.Width != n || cfg.Height != n {
			t.Fatalf("%d px: %v %s %dx%d", n, err, format, cfg.Width, cfg.Height)
		}
	}
	v2, _ := s.Save("p_1", solid(50, 50, color.NRGBA{0, 0, 255, 255}))
	if v2 == v1 {
		t.Fatal("the version did not change with the picture")
	}
	if s.Path("p_1", 77) != "" || s.Path("../x", 64) != "" {
		t.Fatal("a size or id outside the set has a path")
	}
	s.Remove("p_1")
	if _, err := os.Stat(filepath.Join(s.Dir, "p_1")); !os.IsNotExist(err) {
		t.Fatalf("Remove left the files: %v", err)
	}
}

// SPEC V0.116: a wide logo is padded, not cut: the middle of a 4:1
// wordmark keeps its ends.
func TestSquareKeepsAWideLogo(t *testing.T) {
	img := solid(400, 100, color.NRGBA{0, 0, 0, 0})
	for y := 0; y < 100; y++ {
		img.Set(2, y, color.NRGBA{255, 0, 0, 255}) // the left end
	}
	sq := Square(img)
	b := sq.Bounds()
	if b.Dx() != b.Dy() {
		t.Fatalf("not square: %v", b)
	}
	if _, _, _, a := sq.At(b.Min.X+2, b.Min.Y+b.Dy()/2).RGBA(); a == 0 {
		t.Fatal("the left end of the wordmark was cut")
	}
}

// SPEC V0.116: the colour comes from the icon's main colour, as a palette
// name; a grey icon gives slate.
func TestMainColor(t *testing.T) {
	for want, c := range map[string]color.NRGBA{
		"rose":  {200, 30, 80, 255},
		"blue":  {40, 100, 190, 255},
		"green": {40, 140, 90, 255},
		"slate": {128, 128, 128, 255},
	} {
		img := solid(40, 40, color.NRGBA{255, 255, 255, 255})
		for y := 8; y < 32; y++ {
			for x := 8; x < 32; x++ {
				img.Set(x, y, c)
			}
		}
		if got := MainColor(img); got != want {
			t.Errorf("%v: %s, want %s", c, got, want)
		}
	}
}

// SPEC V0.116 (safety): a fetch refuses local network addresses, follows
// at most a few redirects, and stops at 2 MB.
func TestFetchBlocksLocalNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes(t, solid(4, 4, color.White)))
	}))
	defer srv.Close()
	for _, u := range []string{srv.URL + "/x.png", "http://localhost:1/x.png", "http://10.0.0.1/x.png", "http://169.254.169.254/latest", "http://[::1]/x.png", "file:///etc/passwd", "ftp://example.com/x.png"} {
		if _, err := Fetch(context.Background(), u); err == nil {
			t.Errorf("%s was fetched", u)
		}
	}
	// A test may allow loopback to check the rest of the fetch.
	f := Fetcher{AllowLoopback: true}
	b, err := f.Fetch(context.Background(), srv.URL+"/x.png")
	if err != nil || len(b) == 0 {
		t.Fatalf("%v", err)
	}
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, MaxBytes+10))
	}))
	defer huge.Close()
	if _, err := f.Fetch(context.Background(), huge.URL); err == nil || !strings.Contains(err.Error(), "2 MB") {
		t.Fatalf("over 2 MB: %v", err)
	}
	loop := httptest.NewServer(nil)
	loop.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loop.URL+r.URL.Path+"x", http.StatusFound)
	})
	defer loop.Close()
	if _, err := f.Fetch(context.Background(), loop.URL+"/"); err == nil {
		t.Fatal("followed redirects forever")
	}
}

// SPEC V0.116: a file is read only when it is a regular file under 2 MB.
func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "logo.png")
	os.WriteFile(p, pngBytes(t, solid(4, 4, color.White)), 0o644)
	if _, err := ReadFile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(dir); err == nil {
		t.Fatal("read a folder")
	}
	if _, err := ReadFile("logo.png"); err == nil {
		t.Fatal("read a relative path")
	}
}

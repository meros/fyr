// Package projicon is a project's image icon (SPEC 0.5 V0.116): it reads a
// file or fetches a URL with limits, accepts PNG, JPEG and GIF only, makes
// the picture square, scales it to 64 and 128 px and stores it as PNG,
// which drops the source's metadata. It also names the picture's main
// colour from the project palette.
package projicon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/meros/fyr/internal/fsutil"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // registers the GIF decoder
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// MaxBytes is the most a source file may have (2 MB); MaxPixels the most
// pixels a picture may claim before it is decoded.
const (
	MaxBytes  = 2 << 20
	MaxPixels = 4096 * 4096
)

// Sizes are the stored squares, in px.
var Sizes = []int{64, 128}

var errTooBig = fmt.Errorf("the file is over 2 MB")

// Decode reads an image: PNG, JPEG or GIF, at most MaxBytes and MaxPixels.
func Decode(b []byte) (image.Image, error) {
	if len(b) > MaxBytes {
		return nil, errTooBig
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		head := strings.ToLower(string(b[:min(len(b), 512)]))
		if strings.Contains(head, "<svg") || strings.Contains(head, "riff") || strings.HasPrefix(head, "\x00\x00\x01\x00") {
			return nil, errors.New("only PNG, JPEG and GIF are read: convert the SVG, WebP or ICO to a PNG first (for example with rsvg-convert or magick)")
		}
		return nil, errors.New("not an image: only PNG, JPEG and GIF are read")
	}
	if format != "png" && format != "jpeg" && format != "gif" {
		return nil, fmt.Errorf("%s is not read: only PNG, JPEG and GIF", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels || cfg.Width > 1<<14 || cfg.Height > 1<<14 {
		return nil, fmt.Errorf("the picture is %d×%d; at most %d pixels", cfg.Width, cfg.Height, MaxPixels)
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("the picture cannot be read: %v", err)
	}
	return img, nil
}

// Square makes img square: a picture that is nearly square (up to 5:4) is
// cut to its middle; a wider or taller one (a wordmark) is padded with
// transparency, so nothing of the logo is cut.
func Square(img image.Image) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	long, short := max(w, h), min(w, h)
	if long*4 <= short*5 {
		x0, y0 := b.Min.X+(w-short)/2, b.Min.Y+(h-short)/2
		out := image.NewNRGBA(image.Rect(0, 0, short, short))
		draw.Draw(out, out.Bounds(), img, image.Pt(x0, y0), draw.Src)
		return out
	}
	out := image.NewNRGBA(image.Rect(0, 0, long, long))
	off := image.Pt((long-w)/2, (long-h)/2)
	draw.Draw(out, image.Rectangle{Min: off, Max: off.Add(image.Pt(w, h))}, img, b.Min, draw.Src)
	return out
}

// scale is a box filter from a square src to n×n (area average with
// premultiplied alpha; a smaller source is scaled up by the nearest pixel).
func scale(src *image.NRGBA, n int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, n, n))
	s := src.Bounds().Dx()
	for y := 0; y < n; y++ {
		y0, y1 := y*s/n, max((y+1)*s/n, y*s/n+1)
		for x := 0; x < n; x++ {
			x0, x1 := x*s/n, max((x+1)*s/n, x*s/n+1)
			var r, g, bl, a, cnt float64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					c := src.NRGBAAt(xx, yy)
					al := float64(c.A)
					r += float64(c.R) * al
					g += float64(c.G) * al
					bl += float64(c.B) * al
					a += al
					cnt++
				}
			}
			if a == 0 {
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{uint8(r/a + .5), uint8(g/a + .5), uint8(bl/a + .5), uint8(a/cnt + .5)})
		}
	}
	return out
}

// Store keeps each project's squares under Dir/<project id>/<size>.png.
type Store struct{ Dir string }

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Path is the stored square of project id at size n, or "" for a size or
// id outside the set.
func (s *Store) Path(id string, n int) string {
	if !idRe.MatchString(id) {
		return ""
	}
	for _, x := range Sizes {
		if x == n {
			return filepath.Join(s.Dir, id, fmt.Sprintf("%d.png", n))
		}
	}
	return ""
}

// Save stores img as project id's icon and returns its version: a short
// hash of the stored picture, which the client puts in the icon's URL.
func (s *Store) Save(id string, img image.Image) (string, error) {
	if !idRe.MatchString(id) {
		return "", fmt.Errorf("bad project id %q", id)
	}
	sq := Square(img)
	dir := filepath.Join(s.Dir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	h := sha256.New()
	for _, n := range Sizes {
		var b bytes.Buffer
		if err := png.Encode(&b, scale(sq, n)); err != nil {
			return "", err
		}
		h.Write(b.Bytes())
		// 0o600 as CreateTemp made it before.
		if err := fsutil.WriteAtomic(s.Path(id, n), b.Bytes(), 0o600); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// Remove deletes project id's icon files.
func (s *Store) Remove(id string) {
	if idRe.MatchString(id) {
		os.RemoveAll(filepath.Join(s.Dir, id))
	}
}

// ReadFile reads a regular file by its full path, at most MaxBytes.
func ReadFile(p string) ([]byte, error) {
	if !filepath.IsAbs(p) {
		return nil, errors.New("give the full path of the file")
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("no file %s", p)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a file", p)
	}
	if fi.Size() > MaxBytes {
		return nil, errTooBig
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readMax(f)
}

func readMax(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBytes {
		return nil, errTooBig
	}
	return b, nil
}

// Fetcher fetches an image over http or https from the public internet.
type Fetcher struct {
	// AllowLoopback lets a test reach its own server; never set in Fyr.
	AllowLoopback bool
}

// Fetch is Fetcher{}.Fetch.
func Fetch(ctx context.Context, u string) ([]byte, error) { return Fetcher{}.Fetch(ctx, u) }

// blocked is an address on this machine or the local network: loopback,
// private, link-local (cloud metadata), CGNAT and Tailscale, multicast,
// unspecified.
func blocked(a netip.Addr, loopbackOK bool) bool {
	a = a.Unmap()
	if a.IsLoopback() {
		return !loopbackOK
	}
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	return a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || cgnat.Contains(a) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(a)
}

// Fetch gets u: http or https only, every address it connects to (after
// each redirect too) on the public internet, at most 3 redirects, 20 s and
// MaxBytes.
func (f Fetcher) Fetch(ctx context.Context, u string) ([]byte, error) {
	pu, err := url.Parse(u)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return nil, errors.New("give an http or https URL")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		a, err := netip.ParseAddr(host)
		if err != nil || blocked(a, f.AllowLoopback) {
			return fmt.Errorf("%s is on the local network; only public addresses are fetched", host)
		}
		return nil
	}}
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("more than 3 redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("a redirect left http")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Fyr project icon")
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif;q=0.9,*/*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch: %s", resp.Status)
	}
	if resp.ContentLength > MaxBytes {
		return nil, errTooBig
	}
	return readMax(resp.Body)
}

// palette is the light value of each project hue (web/src/app.css --p-*).
var palette = []struct {
	name    string
	r, g, b float64
}{
	{"slate", 0x56, 0x61, 0x6d}, {"blue", 0x2f, 0x6f, 0xb0}, {"sky", 0x1f, 0x7e, 0xa8}, {"teal", 0x1d, 0x7a, 0x74},
	{"green", 0x2e, 0x7d, 0x5b}, {"olive", 0x6b, 0x7a, 0x1f}, {"violet", 0x7a, 0x4f, 0xc0}, {"indigo", 0x4a, 0x55, 0xb5},
	{"rose", 0xb8, 0x40, 0x6a}, {"brown", 0x8a, 0x4f, 0x3a},
}

func hsv(r, g, b float64) (h, s, v float64) {
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	v = mx / 255
	if mx == 0 {
		return 0, 0, v
	}
	s = (mx - mn) / mx
	if mx == mn {
		return 0, s, v
	}
	switch mx {
	case r:
		h = math.Mod((g-b)/(mx-mn), 6)
	case g:
		h = (b-r)/(mx-mn) + 2
	default:
		h = (r-g)/(mx-mn) + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, v
}

// MainColor names the palette hue closest to the picture's main colour:
// the most common hue among its opaque, coloured pixels (white, black and
// grey do not count). A picture with too little colour is "slate".
func MainColor(img image.Image) string {
	sq := scale(Square(img), 64)
	var bins [36]float64
	var sum [36][3]float64
	total, coloured := 0.0, 0.0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := sq.NRGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			total++
			r, g, b := float64(c.R), float64(c.G), float64(c.B)
			h, s, v := hsv(r, g, b)
			if s < 0.25 || v < 0.15 {
				continue
			}
			coloured++
			i := int(h/10) % 36
			bins[i] += s
			sum[i][0], sum[i][1], sum[i][2] = sum[i][0]+r*s, sum[i][1]+g*s, sum[i][2]+b*s
		}
	}
	if total == 0 || coloured < total*0.08 {
		return "slate"
	}
	best := 0
	for i := range bins {
		// a bin with its neighbours, so a hue on a bin edge is not split
		if bins[i]+bins[(i+35)%36]/2+bins[(i+1)%36]/2 > bins[best]+bins[(best+35)%36]/2+bins[(best+1)%36]/2 {
			best = i
		}
	}
	w := bins[best]
	r, g, b := sum[best][0]/w, sum[best][1]/w, sum[best][2]/w
	h, _, v := hsv(r, g, b)
	name, dist := "slate", math.Inf(1)
	for _, p := range palette[1:] {
		ph, _, pv := hsv(p.r, p.g, p.b)
		dh := math.Abs(h - ph)
		dh = math.Min(dh, 360-dh)
		d := dh + 40*math.Abs(v-pv)
		if d < dist {
			name, dist = p.name, d
		}
	}
	return name
}

package desktop

import (
	"encoding/json"
	"github.com/meros/fyr/internal/fsutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Geometry is the window's size, restored at the next start.
type Geometry struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized,omitempty"`
}

// DefaultGeometry is the first window's size.
var DefaultGeometry = Geometry{Width: 1280, Height: 820}

// Prefs is what the app remembers between starts, in
// ~/.config/fyr/desktop.json: the window's size and the origin it last
// signed in to, so a start loads the app with the session it has and asks
// for a one-time link only on a 401.
type Prefs struct {
	Window Geometry `json:"window"`
	Origin string   `json:"origin,omitempty"`
	// Endpoint is this app's native push device (POST /push/native), kept
	// so its settings survive restarts; PushOff: the owner turned the
	// device's notifications off in Settings.
	Endpoint string `json:"endpoint,omitempty"`
	PushOff  bool   `json:"pushOff,omitempty"`
}

// LoadPrefs reads path; a missing or broken file gives the defaults.
func LoadPrefs(path string) Prefs {
	p := Prefs{Window: DefaultGeometry}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &p)
	}
	if p.Window.Width < 400 || p.Window.Height < 300 {
		p.Window = DefaultGeometry
	}
	if !strings.HasPrefix(p.Endpoint, NativePrefix) {
		p.Endpoint = ""
	}
	return p
}

// Save writes p to path through a temporary file.
func (p Prefs) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	// 0o644 as before: window geometry and the endpoint, not a secret.
	return fsutil.WriteAtomic(path, b, 0o644)
}

// OriginOf is the scheme://host:port of a link, "" when it has none.
func OriginOf(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// External reports whether a link the page follows leaves Fyr, so the
// default browser opens it: any http(s) link off Fyr's origin, and mailto:
// and the like. Fyr's own pages, about:, data: and blob: stay.
func External(origin, link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "", "about", "data", "blob", "javascript":
		return false
	case "http", "https":
		return origin == "" || !strings.EqualFold(u.Scheme+"://"+u.Host, origin)
	}
	return true
}

// ChildWindow reports whether a page that asked for a window of its own
// (target=_blank, window.open) gets one in the app: a Fyr page, as a preview
// does, and the empty, about:, data: and blob: pages a script makes for
// itself. Every other link goes to the default browser (V0.desktop-popup-links):
// WebKitGTK reports no navigation decision for such a request, it reaches the
// shell's create handler directly, so only that handler can send it out.
func ChildWindow(origin, link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return !External(origin, link)
	case "", "about", "data", "blob":
		return true
	}
	return false
}

// OpenPath is the page a click on a notification opens: the thread, as a
// tap on a push does (/?open=, V0.15), or the inbox for a push about no
// thread.
func OpenPath(ref string) string {
	if ref == "" {
		return "/"
	}
	return "/?open=" + url.QueryEscape(ref)
}

// Signin decides when a signed-out window asks the owner socket for a new
// link: at once the first time, then not again within Gap, so a front
// that refuses the link (a revoked device, enforce mode without the owner
// way) shows its signed-out page instead of looping.
type Signin struct {
	Gap  time.Duration // default 30 s
	mu   sync.Mutex
	last time.Time
}

// Allow reports whether a new link may be asked for at now.
func (s *Signin) Allow(now time.Time) bool {
	gap := s.Gap
	if gap == 0 {
		gap = 30 * time.Second
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.last.IsZero() && now.Sub(s.last) < gap {
		return false
	}
	s.last = now
	return true
}

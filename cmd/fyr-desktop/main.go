//go:build linux && cgo

// fyr-desktop is Fyr's Linux app (decision 0038, #461): Fyr in its own
// window, signed in through the owner socket, a tray item (Open, Open in
// Browser, Pause or Resume, Show Log, Quit) and the desktop's own
// notifications for what would be a push.
//
//	fyr-desktop [-background] [-data DIR] [-unit fyr.service] [-icon PNG]
//
// Its logic is internal/desktop; the GTK side is shell.go.
package main

/*
#include <glib.h>
*/
import "C"

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/desktop"
)

type app struct {
	owner     desktop.Owner
	svc       desktop.Service
	nixos     bool
	logFile   string
	prefsPath string

	mu     sync.Mutex
	saveMu sync.Mutex
	prefs  desktop.Prefs
	origin atomic.Value // string: the origin the window signed in to

	signin   desktop.Signin
	notes    desktop.Notifications
	offline  atomic.Bool // the window's last load failed: load again when fyrd answers
	feedOK   atomic.Bool // fyrd's feed answers now
	running  atomic.Bool // fyrd's unit runs, as the tray last read it
	refresh  chan struct{}
	tokenMu  sync.Mutex
	tokens   map[int][2]string // a notification sent (ref, state), by token, until the server gives its id
	device   *desktop.Device
	nextTok  int
	started  sync.Once
	stopFeed context.CancelFunc
	win      *desktop.Chrome
}

// chromium is the Chromium the package bundles (flake.nix sets it with -X);
// without it the app looks for one on PATH.
var chromium string

var the *app

func main() {
	log.SetFlags(log.LstdFlags)
	// V0.desktop-system-theme: a session that sets GTK_THEME to a dark theme made the window report
	// prefers-color-scheme: dark whatever the desktop's colour scheme is. Without it Chromium follows the desktop's
	// setting (the portal's colour scheme), light or dark.
	os.Unsetenv("GTK_THEME")
	home, _ := os.UserHomeDir()
	data := flag.String("data", filepath.Join(home, ".local", "share", "fyr"), "Fyr's data directory")
	unit := flag.String("unit", "fyr.service", "fyrd's systemd unit")
	icon := flag.String("icon", defaultIcon(), "the app's icon (PNG)")
	background := flag.Bool("background", false, "start in the tray without the window (autostart)")
	browser := flag.String("chromium", "", "the Chromium that draws the window (default: the bundled one, else one on PATH)")
	flag.Parse()

	cfg, _ := os.UserConfigDir()
	state := filepath.Join(home, ".local", "share", "fyr-desktop")
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		state = filepath.Join(x, "fyr-desktop")
	}
	the = &app{
		owner:     desktop.Owner{Socket: auth.SocketPath(*data, auth.OwnerSocket)},
		svc:       desktop.Service{Unit: *unit},
		nixos:     desktop.NixOS(),
		logFile:   filepath.Join(*data, "daemon.log"),
		prefsPath: filepath.Join(cfg, "fyr", "desktop.json"),
		refresh:   make(chan struct{}, 1),
		tokens:    map[int][2]string{},
	}
	the.prefs = desktop.LoadPrefs(the.prefsPath)
	the.origin.Store(the.prefs.Origin)
	exe := *browser
	if exe == "" {
		exe = desktop.FindChromium(chromium)
	}
	if exe == "" {
		log.Fatal("fyr-desktop: no Chromium found; install chromium or pass -chromium")
	}
	the.win = &desktop.Chrome{
		Exec:       exe,
		Profile:    filepath.Join(state, "chromium"),
		Size:       the.prefs.Window,
		Origin:     the.originNow,
		SignedOut:  the.signedOut,
		LoadFailed: the.loadFailed,
		External:   shellOpenURI,
		Bridge:     the.bridge,
		Bounds:     the.windowSize,
	}
	os.Exit(shellRun(*icon, *background))
}

// defaultIcon is the icon the package installs next to the binary
// (share/icons/hicolor/512x512/apps), else none.
func defaultIcon() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	p := filepath.Join(filepath.Dir(exe), "..", "share", "icons", "hicolor", "512x512", "apps", "io.github.meros.Fyr.png")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return filepath.Clean(p)
}

// start runs once, when GTK is up in the first instance.
func (a *app) start() {
	ctx, cancel := context.WithCancel(context.Background())
	a.stopFeed = cancel
	go a.watchService(ctx)
	host, _ := os.Hostname()
	a.device = &desktop.Device{Owner: a.owner, Name: "Fyr app on " + host,
		Load: func() (string, bool) {
			a.mu.Lock()
			defer a.mu.Unlock()
			return a.prefs.Endpoint, a.prefs.PushOff
		},
		Save: func(ep string, off bool) {
			a.mu.Lock()
			if a.prefs.Endpoint == ep && a.prefs.PushOff == off {
				a.mu.Unlock()
				return
			}
			a.prefs.Endpoint, a.prefs.PushOff = ep, off
			a.mu.Unlock()
			a.persist()
		},
		Show: a.show, Up: a.feedUp}
	go a.device.Run(ctx)
	if o := a.originNow(); o == "" {
		go a.signIn() // the window opens on the first link
	}
}

// persist writes the newest prefs. Saves are one at a time and each writes
// what holds when it runs, so a slow save of an older copy never wins.
func (a *app) persist() {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	p := a.prefs
	a.mu.Unlock()
	if err := p.Save(a.prefsPath); err != nil {
		log.Printf("fyr-desktop: %v", err)
	}
}

func (a *app) originNow() string { s, _ := a.origin.Load().(string); return s }

// signIn loads a one-time link from the owner socket.
func (a *app) signIn() {
	u, err := a.owner.Once()
	if err != nil {
		log.Printf("fyr-desktop: sign in: %v", err)
		a.offline.Store(true)
		a.win.Load(offlinePage("Nothing answers on " + a.owner.Socket + "."))
		return
	}
	if o := desktop.OriginOf(u); o != "" && o != a.originNow() {
		a.origin.Store(o)
		a.mu.Lock()
		a.prefs.Origin = o
		a.mu.Unlock()
		a.persist()
	}
	a.offline.Store(false)
	a.win.Load(u)
}

// feedUp: fyrd answers again (a start, a restart, an update). A window that
// failed to load loads again.
func (a *app) feedUp(up bool) {
	a.feedOK.Store(up)
	if up && a.offline.Load() {
		a.offline.Store(false)
		if o := a.originNow(); o != "" {
			a.win.Load(o + "/")
		} else {
			go a.signIn()
		}
	}
	a.poke()
}

func (a *app) show(n desktop.Notice) {
	p := a.notes.Plan(n)
	a.tokenMu.Lock()
	a.nextTok++
	tok := a.nextTok
	a.tokens[tok] = [2]string{p.Ref, n.State}
	a.tokenMu.Unlock()
	shellNotify(tok, p.Summary, p.Body, p.Urgency, p.Replaces)
}

func (a *app) poke() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

// watchService keeps the tray's status line and Pause/Resume in step with
// the unit, and does at start what desktop.Service.Ensure says.
func (a *app) watchService(ctx context.Context) {
	if _, err := a.svc.Ensure(a.nixos); err != nil {
		log.Printf("fyr-desktop: %v", err)
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		st := a.svc.State()
		a.running.Store(st.Running())
		label := "Resume"
		if st.Running() {
			label = "Pause"
		}
		shellStatus(st.Words(), label, st.Installed)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.refresh:
		}
	}
}

const (
	menuOpen = iota
	menuBrowser
	menuPause
	menuLog
	menuQuit
)

func (a *app) menu(item int) {
	switch item {
	case menuOpen:
		a.win.Show("")
	case menuBrowser:
		go func() {
			u, err := a.owner.Once()
			if err != nil {
				log.Printf("fyr-desktop: open in browser: %v", err)
				return
			}
			shellOpenURI(u)
		}()
	case menuPause:
		go func() {
			var err error
			if a.running.Load() {
				err = a.svc.Pause()
			} else {
				err = a.svc.Resume()
			}
			if err != nil {
				log.Printf("fyr-desktop: %v", err)
			}
			a.poke()
		}()
	case menuLog:
		shellOpenURI("file://" + a.logFile)
	case menuQuit:
		if a.stopFeed != nil {
			a.stopFeed()
		}
		go func() {
			a.win.Quit()
			shellQuit()
		}()
	}
}

//export goStartup
func goStartup() { the.started.Do(the.start) }

//export goActivate
func goActivate() { go the.win.Show("") }

//export goActivationToken
func goActivationToken(token *C.char) { the.win.ActivationToken(C.GoString(token)) }

// signedOut: the window's main page answered 401. It asks the owner socket for
// a new link, at most once in desktop.Signin's gap; inside it the signed-out
// page shows.
func (a *app) signedOut() bool {
	if !a.signin.Allow(time.Now()) {
		return false
	}
	go a.signIn()
	return true
}

func (a *app) loadFailed() {
	a.offline.Store(true)
	// fyrd answers on its socket but not where the window looked: it moved to
	// another port. A new link carries the new origin.
	if a.feedOK.Load() && a.signin.Allow(time.Now()) {
		go a.signIn()
		return
	}
	a.win.Load(offlinePage("Nothing answers at " + a.originNow() + "."))
}

func (a *app) windowSize(g desktop.Geometry) {
	a.mu.Lock()
	if g.Maximized { // keep the size to come back to
		g.Width, g.Height = a.prefs.Window.Width, a.prefs.Window.Height
	}
	a.prefs.Window = g
	a.mu.Unlock()
	a.persist()
}

//export goMenu
func goMenu(item C.int) { the.menu(int(item)) }

//export goNotified
func goNotified(token C.int, id C.guint32) {
	the.tokenMu.Lock()
	rs, ok := the.tokens[int(token)]
	delete(the.tokens, int(token))
	the.tokenMu.Unlock()
	if ok {
		the.notes.Posted(rs[0], rs[1], uint32(id))
	}
}

//export goNotifyAction
func goNotifyAction(id C.guint32, action *C.char) {
	ref, ok := the.notes.Clicked(uint32(id))
	if !ok {
		return // another app's
	}
	if o := the.originNow(); o != "" {
		go the.win.Show(o + desktop.OpenPath(ref))
	} else {
		go the.win.Show("")
	}
}

//export goNotifyClosed
func goNotifyClosed(id C.guint32) { the.notes.Closed(uint32(id)) }

// bridge answers the page's fyrPush message (web/src/pwa.ts): turning on
// stores the device on the owner socket. The window answers only Fyr's own
// page in its main frame.
func (a *app) bridge(m []byte) (any, error) {
	return a.device.Bridge(m, func(threadID, state string) {
		for _, id := range a.notes.Stale(threadID, state) {
			shellCloseNotification(id)
		}
	})
}

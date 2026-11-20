package peerlink

// One link, both ways (decision 0030 Amendment 1, V0.172). A link is one TLS 1.3
// connection with ALPN fyr-peer/2 between two members, opened by either side.
// After a one-line hello the dialer is the HTTP/2 client of the connection and
// opens a single tunnel stream (POST /link/tunnel); inside that stream a second
// HTTP/2 session runs with the roles reversed, so the acceptor can send requests
// too. Both are the standard library's unencrypted HTTP/2 (http.Transport.
// NewClientConn and http.Server.Serve on a one-connection listener); nothing is
// pooled, so a link is never redialled behind the manager's back.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/roster"
)

// Keepalive: a PING after this much idle time on each session, and the link is
// dead when it is not answered in PingTimeout.
const (
	SendPing    = 15 * time.Second
	PingTimeout = 10 * time.Second
)

const tunnelPath = "/link/tunnel"
const pingPath = "/link/ping"

// drainedPath is the announcement that the sender is done with a replaced link:
// it sends no new request on it and has none open (V0.peerlink-replace-drain).
const drainedPath = "/link/drained"

// errDraining is a request refused before it was sent, because the link it was
// given has been replaced since; the mesh sends it on the new one.
var errDraining = errors.New("the link was replaced")

// hello is the one line each side sends before HTTP/2 starts, so the acceptor can
// say why it will not take the link (the dialer would see only a closed connection).
type hello struct {
	V      int    `json:"v"`
	OK     bool   `json:"ok,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Drain: this side announces when it is done with a replaced link, and closes
	// one only after the peer's announcement (V0.peerlink-replace-drain). An older
	// peer sends no such field.
	Drain bool `json:"drain,omitempty"`
}

func writeHello(c net.Conn, h hello) error {
	b, _ := json.Marshal(h)
	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.Write(append(b, '\n'))
	c.SetWriteDeadline(time.Time{})
	return err
}

// readHello reads one line without reading past it.
func readHello(c net.Conn) (hello, error) {
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.SetReadDeadline(time.Time{})
	var line []byte
	one := make([]byte, 1)
	for len(line) < 512 {
		if _, err := io.ReadFull(c, one); err != nil {
			return hello{}, err
		}
		if one[0] == '\n' {
			var h hello
			err := json.Unmarshal(line, &h)
			return h, err
		}
		line = append(line, one[0])
	}
	return hello{}, errors.New("the hello is too long")
}

// Link is a live link to one peer.
type Link struct {
	PeerID  string
	PeerKey ed25519.PublicKey
	// Dialer is the id of the install that opened the connection.
	Dialer string
	Local  string
	Since  time.Time

	conn net.Conn
	out  *http.ClientConn // requests to the peer

	mu      sync.Mutex
	closed  bool
	drain   bool
	done    chan struct{}
	ready   chan struct{}
	cancel  context.CancelFunc
	closers []func()

	// V0.peerlink-replace-drain: starting counts the requests between RoundTrip
	// and the session taking them; serving the peer's requests this side answers;
	// peerDone is closed when the peer announced it is done with the link.
	starting   int
	tunnel     int // 1 on the dialer's side: the tunnel is a request of its outer session
	serving    atomic.Int64
	peerDrains bool
	peerDone   chan struct{}
	doneOnce   sync.Once
}

// setPeerDrains records that the peer said in its hello it announces the end of
// a replaced link (V0.peerlink-replace-drain).
func (l *Link) setPeerDrains(v bool) {
	l.mu.Lock()
	l.peerDrains = v
	l.mu.Unlock()
}

// Done is closed when the link is closed.
func (l *Link) Done() <-chan struct{} { return l.done }

// Ready is closed when the link carries a request each way.
func (l *Link) Ready() <-chan struct{} { return l.ready }

// Close ends the link: both sessions, and the connection.
func (l *Link) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	cl := l.closers
	out := l.out
	l.mu.Unlock()
	close(l.done)
	l.cancel()
	if out != nil {
		out.Close()
	}
	l.conn.Close()
	for _, f := range cl {
		f()
	}
}

func (l *Link) onClose(f func()) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		f()
		return
	}
	l.closers = append(l.closers, f)
	l.mu.Unlock()
}

// Draining: the link lost a duplicate race; it takes no new requests.
func (l *Link) Draining() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.drain
}

// InFlight is the requests this side has open on the link, the tunnel not
// counted (with it, a link this side opened never drained before the limit).
func (l *Link) InFlight() int {
	l.mu.Lock()
	out, n := l.out, l.starting
	tunnel := l.tunnel
	l.mu.Unlock()
	if out != nil {
		n += max(out.InFlight()-tunnel, 0)
	}
	return n
}

// Serving is the peer's requests this side is answering on the link.
func (l *Link) Serving() int { return int(l.serving.Load()) }

// RoundTrip sends a request to the peer over the link. A link that drains takes
// none: the request was picked before the link was replaced, and nothing of it
// has been sent (V0.peerlink-replace-drain).
func (l *Link) RoundTrip(r *http.Request) (*http.Response, error) {
	l.mu.Lock()
	out := l.out
	if out == nil {
		l.mu.Unlock()
		return nil, errors.New("the link is not up")
	}
	if l.drain {
		l.mu.Unlock()
		return nil, errDraining
	}
	l.starting++ // counted from here, so a drain cannot miss it
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.starting--
		l.mu.Unlock()
	}()
	return l.send(out, r)
}

func (l *Link) send(out *http.ClientConn, r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host, r2.Host = "http", "peer.link", ""
	return out.RoundTrip(r2)
}

// peerAnnounces: the peer's hello said it announces the end of a replaced link.
func (l *Link) peerAnnounces() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peerDrains
}

// announce tells the peer this side is done with the link (it drains and has no
// request open on it). It goes past the drain check: it is the last request.
func (l *Link) announce(ctx context.Context) error {
	l.mu.Lock()
	out := l.out
	l.mu.Unlock()
	if out == nil {
		return errors.New("the link is not up")
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://peer.link"+drainedPath, nil)
	resp, err := l.send(out, req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func h2cProtocols() *http.Protocols {
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	return p
}

func newTransport(dial func(ctx context.Context) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		Protocols:   h2cProtocols(),
		HTTP2:       &http.HTTP2Config{SendPingTimeout: SendPing, PingTimeout: PingTimeout},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
	}
}

func newServer(h http.Handler, peerKey ed25519.PublicKey) *http.Server {
	return &http.Server{
		Handler:           h,
		Protocols:         h2cProtocols(),
		HTTP2:             &http.HTTP2Config{MaxConcurrentStreams: 128, SendPingTimeout: SendPing, PingTimeout: PingTimeout, MaxReadFrameSize: 1 << 20},
		MaxHeaderBytes:    64 << 10,
		ReadHeaderTimeout: 10 * time.Second,
		// every request on either session is from the key the outer handshake proved
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			ctx = context.WithValue(ctx, rotationKey{}, []roster.Statement(nil))
			return context.WithValue(ctx, callerKey{}, peerKey)
		},
	}
}

// onceListener hands a single connection to http.Server.Serve.
type onceListener struct {
	c    net.Conn
	once sync.Once
	done chan struct{}
}

func newOnce(c net.Conn) *onceListener { return &onceListener{c: c, done: make(chan struct{})} }

func (l *onceListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.c })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *onceListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}
func (l *onceListener) Addr() net.Addr { return &net.TCPAddr{} }

// onceDial returns c the first time, then fails.
func onceDial(c net.Conn) func(context.Context) (net.Conn, error) {
	var mu sync.Mutex
	used := false
	return func(context.Context) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		if used {
			return nil, errors.New("the link's session is single use")
		}
		used = true
		return c, nil
	}
}

// duplex is a tunnel stream as a net.Conn.
type duplex struct {
	r       io.Reader
	w       io.Writer
	flush   func() error
	closeFn func()
	closing atomic.Bool
	wmu     sync.Mutex
	wclosed bool
}

func (d *duplex) Read(p []byte) (int, error) { return d.r.Read(p) }
func (d *duplex) Write(p []byte) (int, error) {
	d.wmu.Lock()
	defer d.wmu.Unlock()
	if d.wclosed { // a response writer must not be written after its handler returned
		return 0, io.ErrClosedPipe
	}
	n, err := d.w.Write(p)
	if err == nil && d.flush != nil {
		err = d.flush() // HTTP/2 SETTINGS and PING stall unless every write goes out
	}
	return n, err
}
func (d *duplex) Close() error {
	d.wmu.Lock()
	d.wclosed = true
	d.wmu.Unlock()
	if !d.closing.CompareAndSwap(false, true) { // closeFn may call Close again: not a sync.Once
		return nil
	}
	if c, ok := d.w.(io.Closer); ok {
		c.Close()
	}
	if c, ok := d.r.(io.Closer); ok {
		c.Close()
	}
	if d.closeFn != nil {
		d.closeFn()
	}
	return nil
}
func (d *duplex) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (d *duplex) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (d *duplex) SetDeadline(t time.Time) error      { return nil }
func (d *duplex) SetReadDeadline(t time.Time) error  { return nil }
func (d *duplex) SetWriteDeadline(t time.Time) error { return nil }

// base is a link that is not up yet.
// plain hides a *tls.Conn from net/http, which would otherwise run its own ALPN
// logic on it: the link speaks unencrypted HTTP/2 inside the TLS stream.
type plain struct{ net.Conn }

func newLink(conn net.Conn, local, peerID string, peerKey ed25519.PublicKey, dialer string) (*Link, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	l := &Link{PeerID: peerID, PeerKey: peerKey, Dialer: dialer, Local: local, Since: time.Now(), conn: conn,
		done: make(chan struct{}), ready: make(chan struct{}), peerDone: make(chan struct{}), cancel: cancel}
	return l, ctx
}

// handler answers the link's own requests (the ping, the peer's announcement that
// it is done with the link) and counts the peer's others while they are served.
func (l *Link) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pingPath:
			w.WriteHeader(http.StatusNoContent)
			return
		case drainedPath:
			l.doneOnce.Do(func() { close(l.peerDone) })
			w.WriteHeader(http.StatusNoContent)
			return
		}
		l.serving.Add(1)
		defer l.serving.Add(-1)
		next.ServeHTTP(w, r)
	})
}

// check sends this side's ping and marks the link ready when it is answered.
func (l *Link) check(ctx context.Context) {
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://peer.link"+pingPath, nil)
		resp, err := l.RoundTrip(req)
		if err != nil {
			l.Close()
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			l.Close()
			return
		}
		select {
		case <-l.ready:
		default:
			close(l.ready)
		}
	}()
}

// NewDialerLink makes the link on a connection this install dialed (the TLS and
// the hello are done): the outer HTTP/2 client, the tunnel, and the inner server
// that serves the peer's requests with h.
func NewDialerLink(conn net.Conn, local, peerID string, peerKey ed25519.PublicKey, h http.Handler) (*Link, error) {
	conn = plain{conn}
	l, ctx := newLink(conn, local, peerID, peerKey, local)
	tr := newTransport(onceDial(conn))
	cc, err := tr.NewClientConn(ctx, "http", "peer.link:80")
	if err != nil {
		l.Close()
		return nil, err
	}
	l.mu.Lock()
	l.out = cc
	l.mu.Unlock()
	pr, pw := io.Pipe()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://peer.link"+tunnelPath, pr)
	resp, err := cc.RoundTrip(req)
	if err != nil {
		pw.Close()
		l.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		l.Close()
		return nil, errors.New("the peer refused the tunnel: " + resp.Status)
	}
	l.mu.Lock()
	l.tunnel = 1
	l.mu.Unlock()
	tun := &duplex{r: resp.Body, w: pw, closeFn: l.Close}
	srv := newServer(l.handler(h), peerKey)
	ln := newOnce(tun)
	go srv.Serve(ln)
	l.onClose(func() { srv.Close(); ln.Close(); tun.Close() })
	l.check(ctx)
	return l, nil
}

// NewAcceptorLink makes the link on a connection a peer dialed: the outer HTTP/2
// server, which serves the dialer's requests with h and, on /link/tunnel, becomes
// the client of the inner session.
func NewAcceptorLink(conn net.Conn, local, peerID string, peerKey ed25519.PublicKey, h http.Handler) *Link {
	conn = plain{conn}
	l, ctx := newLink(conn, local, peerID, peerKey, peerID)
	mux := http.NewServeMux()
	var tunnelOnce sync.Once
	mux.HandleFunc("POST "+tunnelPath, func(w http.ResponseWriter, r *http.Request) {
		opened := false
		tunnelOnce.Do(func() { opened = true })
		if !opened {
			http.Error(w, "one tunnel per link", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		rc.Flush()
		tun := &duplex{r: r.Body, w: w, flush: rc.Flush, closeFn: l.Close}
		tr := newTransport(onceDial(tun))
		cc, err := tr.NewClientConn(ctx, "http", "peer.link:80")
		if err != nil {
			l.Close()
			return
		}
		l.mu.Lock()
		l.out = cc
		l.mu.Unlock()
		l.check(ctx)
		select { // the handler stays until the link ends: its response is the tunnel
		case <-l.done:
		case <-r.Context().Done():
			l.Close()
		}
		tun.Close() // no write after the handler returns
	})
	mux.Handle("/", l.handler(h))
	srv := newServer(mux, peerKey)
	srv.Handler = mux
	ln := newOnce(conn)
	go srv.Serve(ln)
	l.onClose(func() { srv.Close(); ln.Close() })
	return l
}

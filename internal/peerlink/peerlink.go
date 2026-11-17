// Package peerlink is the link between installs (decision 0030 3.6, V0.158):
// TLS 1.3 with a self-signed certificate over each install's Ed25519 key, both
// ways, no 0-RTT. The dialer requires the exact install id it meant to reach;
// the listener learns the dialer's key from its certificate and asks the roster
// whether it is a member. Names, addresses, expiry and public CAs play no part.
// The tailnet is one route under it; so is a LAN.
package peerlink

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/roster"
)

// ALPN is the protocol the peer port speaks.
const ALPN = "fyr-peer/1"

// Cert is a self-signed certificate over the install key. Nothing about it is
// trusted but the key.
func Cert(k *identity.Key, rotation ...roster.Statement) (tls.Certificate, error) {
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: k.ID()},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	if len(rotation) > 0 { // the rotations that lead to this key, signed by the old ones
		b, _ := json.Marshal(rotation)
		tpl.ExtraExtensions = []pkix.Extension{{Id: rotationOID, Value: b}}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, k.Public, k.Private)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k.Private}, nil
}

// rotationOID marks the certificate extension that carries a key rotation.
var rotationOID = asn1.ObjectIdentifier{1, 3, 9999, 1}

// Rotation is the chain of rotations a certificate carries, if any.
func Rotation(c *x509.Certificate) []roster.Statement {
	for _, e := range c.Extensions {
		if e.Id.Equal(rotationOID) {
			var chain []roster.Statement
			if json.Unmarshal(e.Value, &chain) == nil && len(chain) <= 16 {
				return chain
			}
		}
	}
	return nil
}

// PeerKey is the Ed25519 key a connection's certificate carries.
func PeerKey(certs []*x509.Certificate) (ed25519.PublicKey, error) {
	if len(certs) == 0 {
		return nil, errors.New("no certificate")
	}
	pub, ok := certs[0].PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("the certificate is not an Ed25519 one")
	}
	return pub, nil
}

// ServerConfig is the peer listener's TLS: 1.3 only, a client certificate asked
// for and not verified (the HTTP layer checks its key against the roster, and
// the handshake has proved the client holds it).
func ServerConfig(k *identity.Key, rotation ...roster.Statement) (*tls.Config, error) {
	c, err := Cert(k, rotation...)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{c}, NextProtos: []string{ALPN},
		ClientAuth: tls.RequestClientCert,
	}, nil
}

// ErrWrongInstall: the install that answered is not the one meant.
var ErrWrongInstall = errors.New("the install that answered is not the one that was meant")

// ClientConfig dials the install with id want: its certificate's key must hash
// to it.
func ClientConfig(k *identity.Key, want string, o Options) (*tls.Config, error) {
	c, err := Cert(k, o.Rotation...)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{c}, NextProtos: []string{ALPN},
		InsecureSkipVerify: true, // by key id below, not by name or chain
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("no certificate")
			}
			cert, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			pub, err := PeerKey([]*x509.Certificate{cert})
			if err != nil {
				return err
			}
			if got := identity.IDOf(pub); got != want {
				// The install may have rotated its key since this one last met it: its
				// certificate then carries the rotations, signed by the key it had.
				if old := o.OldKey; old != nil {
					if k := old(want); k != nil {
						if end, ok := roster.ChainFrom(Rotation(cert), k); ok && end.Equal(pub) {
							if o.OnRotation != nil {
								o.OnRotation(Rotation(cert))
							}
							return nil
						}
					}
				}
				return fmt.Errorf("%w: wanted %s, got %s", ErrWrongInstall, want, got)
			}
			return nil
		},
	}, nil
}

// Options are what a dial may know beyond the id it wants.
type Options struct {
	// Rotation is shown in this side's certificate: the rotations that lead to its
	// key (roster.RotationChain), for a peer that knows only an older key.
	Rotation []roster.Statement
	// OldKey is the key the roster holds for an install id, so a rotated peer is
	// accepted by its chain; OnRotation hears the chain that was accepted, to merge.
	OldKey     func(id string) ed25519.PublicKey
	OnRotation func([]roster.Statement)
}

// Dialer dials peers with this install's key. Resolve, when set, maps a
// host:port to the address to connect to (a network with no DNS, a test).
type Dialer struct {
	Key     *identity.Key
	Resolve func(hostport string) string
	Timeout time.Duration
	// Opts gives the rotation options at each dial (the roster changes).
	Opts func() Options
}

// DialError: the link could not be made (no route, a refused connection, a
// handshake that failed or named another install). Nothing of the request was
// sent, so a caller may try another route.
type DialError struct{ Err error }

func (e *DialError) Error() string { return "peer link: " + e.Err.Error() }
func (e *DialError) Unwrap() error { return e.Err }

// DialTLS connects to addr and requires the install want. It is a
// DialTLSContext of an http.Transport.
func (d *Dialer) DialTLS(ctx context.Context, want, addr string) (net.Conn, error) {
	c, err := d.dialTLS(ctx, want, addr)
	if err != nil {
		return nil, &DialError{err}
	}
	return c, nil
}

func (d *Dialer) dialTLS(ctx context.Context, want, addr string) (net.Conn, error) {
	var o Options
	if d.Opts != nil {
		o = d.Opts()
	}
	cfg, err := ClientConfig(d.Key, want, o)
	if err != nil {
		return nil, err
	}
	to := addr
	if d.Resolve != nil {
		if r := d.Resolve(addr); r != "" {
			to = r
		}
	}
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	nd := &net.Dialer{Timeout: timeout}
	conn, err := (&tls.Dialer{NetDialer: nd, Config: cfg}).DialContext(ctx, "tcp", to)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Client is an HTTP client that reaches the install want at addr over the link.
func (d *Dialer) Client(want, addr string) *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: d.Transport(want, addr)}
}

// Transport is that client's transport; every request goes to addr, whatever
// its URL says.
func (d *Dialer) Transport(want, addr string) *http.Transport {
	return &http.Transport{
		DialTLSContext:  func(ctx context.Context, _, _ string) (net.Conn, error) { return d.DialTLS(ctx, want, addr) },
		MaxIdleConns:    4,
		IdleConnTimeout: 60 * time.Second,
	}
}

type callerKey struct{}
type rotationKey struct{}

// conn is an accepted peer connection. It is not a *tls.Conn on purpose: net/http
// would then refuse an ALPN protocol it does not know, and the peer port speaks
// its own, "fyr-peer/1", which is HTTP/1.1 inside TLS 1.3.
type conn struct {
	*tls.Conn
	caller   ed25519.PublicKey
	rotation []roster.Statement
}

type listener struct {
	net.Listener
	cfg *tls.Config
	ch  chan net.Conn
	one chan struct{}
}

// Listen wraps inner (a TCP listener) as the peer port: it accepts TLS 1.3 with
// ALPN fyr-peer/1, does each handshake apart from the others (a stalled client
// holds up no one), and drops a connection that has not finished it in 10 s.
func Listen(inner net.Listener, k *identity.Key, rotation ...roster.Statement) (net.Listener, error) {
	cfg, err := ServerConfig(k, rotation...)
	if err != nil {
		return nil, err
	}
	l := &listener{Listener: inner, cfg: cfg, ch: make(chan net.Conn), one: make(chan struct{})}
	go l.loop()
	return l, nil
}

func (l *listener) loop() {
	for {
		raw, err := l.Listener.Accept()
		if err != nil {
			close(l.one)
			return
		}
		go func() {
			tc := tls.Server(raw, l.cfg)
			raw.SetDeadline(time.Now().Add(10 * time.Second))
			if err := tc.Handshake(); err != nil {
				raw.Close()
				return
			}
			raw.SetDeadline(time.Time{})
			c := &conn{Conn: tc}
			if st := tc.ConnectionState(); st.NegotiatedProtocol != ALPN {
				raw.Close() // a client that did not ask for fyr-peer/1 is not one of ours
				return
			} else if pub, err := PeerKey(st.PeerCertificates); err == nil {
				c.caller = pub
				c.rotation = Rotation(st.PeerCertificates[0])
			}
			select {
			case l.ch <- c:
			case <-l.one:
				raw.Close()
			}
		}()
	}
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.one:
		return nil, net.ErrClosed
	}
}

// ConnContext is http.Server's ConnContext for a server on Listen: it puts the
// connection's proven key where Caller finds it.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	if pc, ok := c.(*conn); ok {
		ctx = context.WithValue(ctx, rotationKey{}, pc.rotation)
		return context.WithValue(ctx, callerKey{}, pc.caller)
	}
	return ctx
}

// Serve runs h on the peer port at inner until the server is closed.
func Serve(inner net.Listener, k *identity.Key, h http.Handler, rotation ...roster.Statement) (*http.Server, error) {
	l, err := Listen(inner, k, rotation...)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: h, ConnContext: ConnContext, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(l)
	return srv, nil
}

// Caller is the install at the other end of an accepted connection: the key its
// certificate proved. False when the client sent no certificate.
func Caller(r *http.Request) (ed25519.PublicKey, bool) {
	pub, _ := r.Context().Value(callerKey{}).(ed25519.PublicKey)
	return pub, len(pub) == ed25519.PublicKeySize
}

// ConnKey is the key the server of a connection from DialTLS proved.
func ConnKey(c net.Conn) (ed25519.PublicKey, error) {
	tc, ok := c.(*tls.Conn)
	if !ok {
		return nil, errors.New("not a TLS connection")
	}
	return PeerKey(tc.ConnectionState().PeerCertificates)
}

// Once is a transport that uses the connection c for its first dial: a request
// over a connection whose key the caller has read.
func Once(c net.Conn) *http.Transport {
	var used bool
	return &http.Transport{
		DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if used {
				return nil, errors.New("the connection was used")
			}
			used = true
			return c, nil
		},
		DisableKeepAlives: true,
	}
}

// CallerRotation is the rotations the caller's certificate carried.
func CallerRotation(r *http.Request) []roster.Statement {
	c, _ := r.Context().Value(rotationKey{}).([]roster.Statement)
	return c
}

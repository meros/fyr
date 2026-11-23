package discovery

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// mDNS for the LAN (decision 0030 3.6): an install advertises
// `fyr-<short id>._fyr._tcp.local` with a TXT `v=1`, `id=<id>`, the peer port and an
// address, and browses for the others'. Standard library only: a small subset of
// RFC 6762/6763 (one PTR, SRV, TXT and A/AAAA set, no compression written, read with
// pointers). An answer is a candidate and a claim; nothing here is trust.

const (
	service = "_fyr._tcp.local"
	ttl     = 120
	// Live is how long an answer stays a candidate without being heard again.
	Live = 2 * time.Minute
)

var group = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// shortID is the instance name's part of an install id.
func shortID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", ""))[:min(8, len(strings.ReplaceAll(id, "-", "")))]
}

func putName(b []byte, name string) []byte {
	for _, l := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0)
}

func putRR(b []byte, name string, typ uint16, data []byte) []byte {
	b = putName(b, name)
	b = binary.BigEndian.AppendUint16(b, typ)
	b = binary.BigEndian.AppendUint16(b, 1|0x8000) // IN, cache flush
	b = binary.BigEndian.AppendUint32(b, ttl)
	b = binary.BigEndian.AppendUint16(b, uint16(len(data)))
	return append(b, data...)
}

func header(flags, qd, an uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[2:], flags)
	binary.BigEndian.PutUint16(b[4:], qd)
	binary.BigEndian.PutUint16(b[6:], an)
	return b
}

// buildQuery asks who is `_fyr._tcp.local`.
func buildQuery() []byte {
	b := header(0, 1, 0)
	b = putName(b, service)
	b = binary.BigEndian.AppendUint16(b, 12) // PTR
	return binary.BigEndian.AppendUint16(b, 1)
}

func buildAnswer(id string, port int, ips []net.IP) []byte {
	return buildAnswerWith("fyr-"+shortID(id), "v=1", "id="+id, port, ips)
}

func buildAnswerWith(instance, txt1, txt2 string, port int, ips []net.IP) []byte {
	inst := instance + "." + service
	host := instance + ".local"
	n := 3 + len(ips)
	b := header(0x8400, 0, uint16(n))
	b = putRR(b, service, 12, putName(nil, inst))
	srv := binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, 0), 0)
	srv = binary.BigEndian.AppendUint16(srv, uint16(port))
	b = putRR(b, inst, 33, putName(srv, host))
	var txt []byte
	for _, s := range []string{txt1, txt2} {
		txt = append(txt, byte(len(s)))
		txt = append(txt, s...)
	}
	b = putRR(b, inst, 16, txt)
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			b = putRR(b, host, 1, v4)
		} else {
			b = putRR(b, host, 28, ip.To16())
		}
	}
	return b
}

// reader walks a DNS message.
type reader struct {
	b   []byte
	pos int
}

func (r *reader) u16() (uint16, bool) {
	if r.pos+2 > len(r.b) {
		return 0, false
	}
	v := binary.BigEndian.Uint16(r.b[r.pos:])
	r.pos += 2
	return v, true
}

// name reads a name at the cursor, following compression pointers (bounded).
func (r *reader) name() (string, bool) {
	var parts []string
	pos, jumped, hops := r.pos, false, 0
	for {
		if pos >= len(r.b) {
			return "", false
		}
		l := int(r.b[pos])
		switch {
		case l == 0:
			if !jumped {
				r.pos = pos + 1
			}
			return strings.ToLower(strings.Join(parts, ".")), true
		case l&0xC0 == 0xC0:
			if pos+2 > len(r.b) || hops > 8 {
				return "", false
			}
			if !jumped {
				r.pos = pos + 2
			}
			jumped = true
			hops++
			pos = int(binary.BigEndian.Uint16(r.b[pos:]) & 0x3FFF)
		case l > 63 || pos+1+l > len(r.b):
			return "", false
		default:
			parts = append(parts, string(r.b[pos+1:pos+1+l]))
			pos += 1 + l
		}
	}
}

// isQuery: a question for our service.
func isQuery(m []byte) bool {
	if len(m) < 12 || m[2]&0x80 != 0 {
		return false
	}
	r := &reader{b: m, pos: 12}
	qd := int(binary.BigEndian.Uint16(m[4:]))
	for i := 0; i < qd; i++ {
		name, ok := r.name()
		if !ok {
			return false
		}
		typ, ok1 := r.u16()
		_, ok2 := r.u16()
		if !ok1 || !ok2 {
			return false
		}
		if name == service && (typ == 12 || typ == 255) {
			return true
		}
	}
	return false
}

// answer is what one message says about one install.
type answer struct {
	Instance string
	ID       string
	Port     int
	IPs      []net.IP
}

// parseAnswer reads an answer for our service: v=1 and an id in the TXT, a port in the
// SRV. Anything else is not an install's.
func parseAnswer(m []byte) (answer, bool) {
	var a answer
	if len(m) < 12 || m[2]&0x80 == 0 {
		return a, false
	}
	r := &reader{b: m, pos: 12}
	for i, qd := 0, int(binary.BigEndian.Uint16(m[4:])); i < qd; i++ {
		if _, ok := r.name(); !ok {
			return a, false
		}
		r.pos += 4
	}
	an := int(binary.BigEndian.Uint16(m[6:])) + int(binary.BigEndian.Uint16(m[8:])) + int(binary.BigEndian.Uint16(m[10:]))
	var v1 bool
	for i := 0; i < an; i++ {
		name, ok := r.name()
		typ, ok1 := r.u16()
		_, ok2 := r.u16()
		if !ok || !ok1 || !ok2 || r.pos+6 > len(m) {
			return a, false
		}
		r.pos += 4 // ttl
		l, _ := r.u16()
		end := r.pos + int(l)
		if end > len(m) {
			return a, false
		}
		switch typ {
		case 12:
			if name == service {
				sub := &reader{b: m, pos: r.pos}
				if inst, ok := sub.name(); ok {
					a.Instance = strings.TrimSuffix(inst, "."+service)
				}
			}
		case 33:
			if l >= 7 {
				a.Port = int(binary.BigEndian.Uint16(m[r.pos+4:]))
			}
		case 16:
			for p := r.pos; p < end; {
				n := int(m[p])
				if p+1+n > end {
					break
				}
				switch s := string(m[p+1 : p+1+n]); {
				case s == "v=1":
					v1 = true
				case strings.HasPrefix(s, "id="):
					a.ID = s[3:]
				}
				p += 1 + n
			}
		case 1:
			if l == 4 {
				a.IPs = append(a.IPs, net.IP(append([]byte(nil), m[r.pos:end]...)))
			}
		case 28:
			if l == 16 {
				if ip := net.IP(append([]byte(nil), m[r.pos:end]...)); !ip.IsLinkLocalUnicast() {
					a.IPs = append(a.IPs, ip)
				}
			}
		}
		r.pos = end
	}
	if !v1 || a.ID == "" || a.Port <= 0 || a.Port > 65535 || len(a.ID) > 64 {
		return a, false
	}
	return a, true
}

// MDNS advertises this install and collects the others'.
type MDNS struct {
	// ID and Port are this install's id and peer port; Port 0 advertises nothing.
	ID   string
	Port int
	// NoAdvertise: browse only (`discovery.mdns: false`, decision 0030 3.6).
	NoAdvertise bool
	// Iface limits the interface by name; "" uses every multicast-capable one.
	Iface string
	// Every is how often it announces and asks (default 30 s).
	Every time.Duration
	Logf  func(string, ...any)

	mu    sync.Mutex
	heard map[string]*heardAt // by "id|addr"
	conns []*net.UDPConn
	sends []*net.UDPConn
}

type heardAt struct {
	c    Candidate
	seen time.Time
}

func (m *MDNS) logf(f string, a ...any) {
	if m.Logf != nil {
		m.Logf(f, a...)
	}
}

// Start joins the group and runs until ctx ends. An error means no multicast here:
// the install runs on without it.
func (m *MDNS) Start(ctx context.Context) error {
	ifs, _ := net.Interfaces()
	for i := range ifs {
		ifi := &ifs[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || (m.Iface == "" && ifi.Flags&net.FlagLoopback != 0) || (m.Iface != "" && ifi.Name != m.Iface) {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", ifi, group)
		if err != nil {
			continue
		}
		m.conns = append(m.conns, c)
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				if s, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ipn.IP}); err == nil {
					m.sends = append(m.sends, s)
				}
			}
		}
	}
	if len(m.conns) == 0 || len(m.sends) == 0 {
		for _, c := range append(m.conns, m.sends...) {
			c.Close()
		}
		m.conns, m.sends = nil, nil
		return net.ErrClosed
	}
	every := m.Every
	if every <= 0 {
		every = 30 * time.Second
	}
	for _, c := range m.conns {
		go m.listen(ctx, c)
	}
	go func() {
		<-ctx.Done()
		for _, c := range append(m.conns, m.sends...) {
			c.Close()
		}
	}()
	go func() {
		t := time.NewTimer(0)
		defer t.Stop()
		for n := 0; ; n++ {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			m.send(buildQuery())
			m.announce()
			d := every
			if n < 3 {
				d = time.Duration(n+1) * 500 * time.Millisecond
			}
			t.Reset(d)
		}
	}()
	return nil
}

func (m *MDNS) listen(ctx context.Context, c *net.UDPConn) {
	buf := make([]byte, 9000)
	for ctx.Err() == nil {
		n, src, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		msg := buf[:n]
		if isQuery(msg) {
			m.announce()
			continue
		}
		m.hearFrom(msg, src.IP, time.Now())
	}
}

// localIPs are the addresses this install is on.
func (m *MDNS) localIPs() []net.IP {
	var out []net.IP
	for _, s := range m.sends {
		out = append(out, s.LocalAddr().(*net.UDPAddr).IP)
	}
	return out
}

func (m *MDNS) announce() {
	if m.NoAdvertise || m.Port == 0 || m.ID == "" {
		return
	}
	m.send(buildAnswer(m.ID, m.Port, m.localIPs()))
}

func (m *MDNS) send(msg []byte) {
	for _, s := range m.sends {
		s.WriteToUDP(msg, group)
	}
}

func (m *MDNS) hear(msg []byte, now time.Time) { m.hearFrom(msg, nil, now) }

// hearFrom takes in one answer; src is the sender when the message names no address.
func (m *MDNS) hearFrom(msg []byte, src net.IP, now time.Time) {
	a, ok := parseAnswer(msg)
	if !ok || a.ID == m.ID {
		return
	}
	ips := a.IPs
	if len(ips) == 0 && src != nil {
		ips = []net.IP{src}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.heard == nil {
		m.heard = map[string]*heardAt{}
	}
	for _, ip := range ips {
		addr := net.JoinHostPort(ip.String(), strconv.Itoa(a.Port))
		m.heard[a.ID+"|"+addr] = &heardAt{c: Candidate{ID: a.ID, Addr: addr, Source: "mdns"}, seen: now}
	}
}

// expire drops what was last heard more than Live before now.
func (m *MDNS) expire(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, h := range m.heard {
		if now.Sub(h.seen) > Live {
			delete(m.heard, k)
		}
	}
}

// Candidates implements Source.
func (m *MDNS) Candidates(ctx context.Context) ([]Candidate, error) {
	m.expire(time.Now())
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Candidate
	for _, h := range m.heard {
		out = append(out, h.c)
	}
	return out, nil
}

var _ Source = (*MDNS)(nil)

// File is a Source that reads a JSON list of candidates from a path: the bus a test
// (or a fixed setup without multicast) writes. A missing file has none; one that
// cannot be read or parsed is an error, not an empty LAN.
func File(path string) Source { return fileSource(path) }

type fileSource string

func (f fileSource) Candidates(ctx context.Context) ([]Candidate, error) {
	b, err := os.ReadFile(string(f))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Candidate
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("LAN bus %s: %w", string(f), err)
	}
	return out, nil
}

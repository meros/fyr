package authtest

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// Seen is one request a Recorder saw, as the host behind it would.
type Seen struct {
	Method string
	Path   string // with the query
	Host   string
	Header http.Header
}

// Recorder is a recording reverse proxy in front of a host: the harness's
// "recording host" (decision 0030 4.2). A front follows the Recorder's URL;
// the test reads what the front sent.
type Recorder struct {
	URL string
	// StripActor makes the host behind it an older build: Fyr-Actor and
	// Fyr-Origin do not reach it (decision 0030 slice 1: a new front must keep
	// following an old host).
	StripActor bool

	mu   sync.Mutex
	seen []Seen
	srv  *httptest.Server
}

// NewRecorder starts a Recorder that forwards to target (http://addr).
func NewRecorder(target string) (*Recorder, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	r := &Recorder{}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			r.mu.Lock()
			r.seen = append(r.seen, Seen{Method: pr.In.Method, Path: pr.In.URL.RequestURI(), Host: pr.In.Host, Header: pr.In.Header.Clone()})
			strip := r.StripActor
			r.mu.Unlock()
			pr.SetURL(u)
			pr.Out.Host = pr.In.Host // the host sees what the front sent
			pr.Out.Header = pr.In.Header.Clone()
			if strip {
				pr.Out.Header.Del("Fyr-Actor")
				pr.Out.Header.Del("Fyr-Origin")
			}
		},
		FlushInterval: -1,
	}
	r.srv = httptest.NewServer(proxy)
	r.URL = r.srv.URL
	return r, nil
}

func (r *Recorder) Close() { r.srv.Close() }

// Mark is a position in the log; Since returns what came after it.
func (r *Recorder) Mark() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}

func (r *Recorder) Since(mark int) []Seen {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Seen(nil), r.seen[mark:]...)
}

// SetStripActor switches the older-host mode.
func (r *Recorder) SetStripActor(on bool) {
	r.mu.Lock()
	r.StripActor = on
	r.mu.Unlock()
}

// Has reports whether h carries the header, case-insensitively.
func (s Seen) Has(name string) bool {
	for k := range s.Header {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

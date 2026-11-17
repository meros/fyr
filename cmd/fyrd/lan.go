package main

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/server"
)

// serveLAN serves the front over HTTPS with the owner's certificate (decision 0030 3.5):
// the one way a browser off this machine and off the tailnet reaches Fyr. A browser
// cannot pin a key, so the certificate is the owner's, from a CA the phone trusts; the
// session is the only credential (modes never observe this listener), a Tailscale
// header on it is refused, and no plain HTTP is served. Without `tls` in the config
// nothing here runs and nothing is served off loopback.
func serveLAN(cfg config, fr *front.Front, serve func(net.Listener, http.Handler)) {
	t := cfg.TLS
	if t.Cert == "" && t.Key == "" && t.Listen == "" {
		return
	}
	if t.Cert == "" || t.Key == "" || t.Listen == "" {
		log.Printf("tls: cert, key and listen are all needed; nothing is served off loopback")
		return
	}
	pair, err := tls.LoadX509KeyPair(t.Cert, t.Key)
	if err != nil {
		log.Printf("tls: %v; nothing is served off loopback", err)
		return
	}
	ln, err := tls.Listen("tcp", t.Listen, &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}})
	if err != nil {
		log.Printf("tls: listen %s: %v; nothing is served off loopback", t.Listen, err)
		return
	}
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	name := t.Name
	if name == "" {
		name = host
	}
	if strings.Contains(name, ":") && !strings.HasPrefix(name, "[") {
		name = "[" + name + "]"
	}
	fr.SetLANOrigin("https://" + name + ":" + port)
	serve(ln, server.ViaLAN(fr.Handler()))
	log.Printf("tls: serving the front at https://%s (sessions only; a phone signs in by QR or code)", ln.Addr())
}

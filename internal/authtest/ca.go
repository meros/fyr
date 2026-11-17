package authtest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"
)

// CA is a test certificate authority: the stand-in for the public CAs that
// sign a node's *.ts.net certificate (the WebPKI probe of condition 1) and
// for the owner's CA on a LAN (`tls: {cert, key}`).
type CA struct {
	Cert *x509.Certificate
	Pool *x509.CertPool
	key  *ecdsa.PrivateKey
}

// NewCA makes a CA.
func NewCA() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fyr test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &CA{Cert: cert, Pool: pool, key: key}, nil
}

// Issue signs a leaf for the names (DNS names and IPs).
func (ca *CA) Issue(names ...string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0]},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(12 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// TLSFront is an HTTPS server with a certificate from the CA, for the names
// given: a node's Serve endpoint as a WebPKI client sees it.
type TLSFront struct {
	Addr string // 127.0.0.1:PORT
	srv  *httptest.Server
	ca   *CA
}

// Serve starts h over TLS with a certificate for names.
func (ca *CA) Serve(h http.Handler, names ...string) (*TLSFront, error) {
	cert, err := ca.Issue(append(names, "127.0.0.1")...)
	if err != nil {
		return nil, err
	}
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	return &TLSFront{Addr: srv.Listener.Addr().String(), srv: srv, ca: ca}, nil
}

func (f *TLSFront) Close() { f.srv.Close() }

// Client is an HTTP client that trusts the CA (and only it) and dials this
// front whatever host the URL names: https://peer.tail-test.ts.net:7443/…
// reaches it, with the name checked against the certificate.
func (f *TLSFront) Client() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: f.ca.Pool, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, f.Addr)
		}}}
}

// WriteCert issues a leaf for the names and writes it as PEM files in dir: what a
// `tls: {cert, key}` in a config points at.
func (ca *CA) WriteCert(dir string, names ...string) (certFile, keyFile string, err error) {
	c, err := ca.Issue(names...)
	if err != nil {
		return "", "", err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		return "", "", err
	}
	certFile, keyFile = filepath.Join(dir, "lan.crt"), filepath.Join(dir, "lan.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]}), 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0o600)
}

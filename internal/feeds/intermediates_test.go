package feeds

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestCert(t *testing.T, tmpl *x509.Certificate, parent *testCA) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.SerialNumber = big.NewInt(time.Now().UnixNano())
	tmpl.NotBefore = time.Now().Add(-time.Hour)
	tmpl.NotAfter = time.Now().Add(time.Hour)
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testCA{cert, key}
}

func newTestCA(t *testing.T, name string, parent *testCA) *testCA {
	return newTestCert(t, &x509.Certificate{
		Subject:               pkix.Name{CommonName: name},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, parent)
}

// TestMissingIntermediate serves a feed over HTTPS with only the leaf
// certificate, as euro-fusion.org does, and checks the client downloads the
// intermediate the leaf names.
func TestMissingIntermediate(t *testing.T) {
	root := newTestCA(t, "Test Root", nil)
	inter := newTestCA(t, "Test Intermediate", root)
	otherRoot := newTestCA(t, "Other Root", nil)

	var downloads atomic.Int32
	aia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Write(inter.cert.Raw)
	}))
	defer aia.Close()

	leaf := newTestCert(t, &x509.Certificate{
		Subject:               pkix.Name{CommonName: "feed"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IssuingCertificateURL: []string{aia.URL + "/inter.cer"},
	}, inter)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleRSS))
	}))
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{leaf.cert.Raw}, // no intermediate
		PrivateKey:  leaf.key,
	}}}
	srv.StartTLS()
	defer srv.Close()

	roots := x509.NewCertPool()
	roots.AddCert(root.cert)
	get := func(client *http.Client, url string) (*http.Response, error) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
		}
		return resp, err
	}

	client := newClientWithRoots(true, roots)
	resp, err := get(client, srv.URL)
	if err != nil {
		t.Fatalf("leaf-only chain: %v", err)
	}
	if resp.ProtoMajor != 2 {
		t.Errorf("protocol = %s, want HTTP/2", resp.Proto)
	}
	// A second connection uses the cached intermediate.
	client.CloseIdleConnections()
	if _, err := get(client, srv.URL); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if n := downloads.Load(); n != 1 {
		t.Errorf("intermediate downloaded %d times, want 1", n)
	}

	// The chain must still end at a trusted root.
	others := x509.NewCertPool()
	others.AddCert(otherRoot.cert)
	_, err = get(newClientWithRoots(true, others), srv.URL)
	var verr *tls.CertificateVerificationError
	if !errors.As(err, &verr) {
		t.Errorf("untrusted root: err = %v, want a certificate verification error", err)
	}

	// And the certificate must match the host.
	_, err = get(newClientWithRoots(true, roots), strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	if !errors.As(err, &verr) || !strings.Contains(err.Error(), "localhost") {
		t.Errorf("wrong host: err = %v, want a host name mismatch", err)
	}
}

func TestChainVerifierWithoutIssuerURL(t *testing.T) {
	root := newTestCA(t, "Test Root", nil)
	inter := newTestCA(t, "Test Intermediate", root)
	leaf := newTestCert(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: "example.com"},
		DNSNames:    []string{"example.com"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, inter)
	roots := x509.NewCertPool()
	roots.AddCert(root.cert)
	v := newChainVerifier(roots, &net.Dialer{})
	ctx := context.Background()

	if err := v.verify(ctx, []*x509.Certificate{leaf.cert, inter.cert}, "example.com"); err != nil {
		t.Errorf("full chain: %v", err)
	}
	err := v.verify(ctx, []*x509.Certificate{leaf.cert}, "example.com")
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Errorf("leaf only, no issuer URL: err = %v, want unknown authority", err)
	}
}

// TestIntermediateCache checks downloads are cached, failures for a few
// minutes and intermediates for a day.
func TestIntermediateCache(t *testing.T) {
	root := newTestCA(t, "Test Root", nil)
	inter := newTestCA(t, "Test Intermediate", root)

	var downloads atomic.Int32
	var failing atomic.Bool
	failing.Store(true)
	aia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if failing.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Write(inter.cert.Raw)
	}))
	defer aia.Close()

	leaf := newTestCert(t, &x509.Certificate{
		Subject:               pkix.Name{CommonName: "example.com"},
		DNSNames:              []string{"example.com"},
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IssuingCertificateURL: []string{aia.URL + "/inter.cer"},
	}, inter)
	roots := x509.NewCertPool()
	roots.AddCert(root.cert)
	v := newChainVerifier(roots, &net.Dialer{})
	now := time.Now()
	v.now = func() time.Time { return now }
	verify := func() error {
		return v.verify(context.Background(), []*x509.Certificate{leaf.cert}, "example.com")
	}
	want := func(step string, n int32, ok bool) {
		t.Helper()
		err := verify()
		if (err == nil) != ok {
			t.Errorf("%s: err = %v, want success %v", step, err, ok)
		}
		if got := downloads.Load(); got != n {
			t.Errorf("%s: %d downloads, want %d", step, got, n)
		}
	}

	want("issuer down", 1, false)
	failing.Store(false)
	want("failure cached", 1, false)
	now = now.Add(failedIntermediateTTL + time.Second)
	want("failure expired", 2, true)
	want("intermediate cached", 2, true)
	now = now.Add(intermediateTTL + time.Second)
	want("intermediate expired", 3, true)
}

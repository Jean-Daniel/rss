package feeds

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Some servers send only their own certificate and leave out the
// intermediate that issued it. Browsers, and Go on macOS, repair such a chain
// by downloading the missing issuer from the URL the certificate names
// (its "CA Issuers" access information); Go's own verifier, used on Linux and
// so in the container, does not. chainVerifier does the same repair, so those
// feeds work in the container too.
//
// It is no less strict than Go's default verification: the chain must still
// end at a trusted root and the certificate must match the host name. A
// downloaded certificate is only ever used as an intermediate, so a bad or
// forged one just fails to build a chain.

const (
	maxIntermediateFetches = 3        // issuers chased per certificate chain
	maxIntermediateSize    = 64 << 10 // an intermediate is a couple of KB
	maxCachedIntermediates = 256
	intermediateTimeout    = 10 * time.Second
)

type chainVerifier struct {
	roots  *x509.CertPool // nil: the system roots
	client *http.Client   // downloads issuers; uses the same address checks

	mu    sync.Mutex
	cache map[string]*x509.Certificate // by issuer URL
}

func newChainVerifier(roots *x509.CertPool, dialer *net.Dialer) *chainVerifier {
	return &chainVerifier{
		roots: roots,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:               nil,
				DialContext:         dialer.DialContext,
				TLSClientConfig:     &tls.Config{RootCAs: roots},
				TLSHandshakeTimeout: intermediateTimeout,
				MaxIdleConns:        2,
				IdleConnTimeout:     30 * time.Second,
			},
			Timeout: intermediateTimeout,
		},
		cache: map[string]*x509.Certificate{},
	}
}

// verify checks the server's certificates for host, downloading missing
// intermediates when the chain does not reach a trusted root.
func (v *chainVerifier) verify(ctx context.Context, certs []*x509.Certificate, host string) error {
	if len(certs) == 0 {
		return errors.New("tls: server sent no certificate")
	}
	opts := x509.VerifyOptions{
		Roots:         v.roots,
		DNSName:       host,
		Intermediates: x509.NewCertPool(),
	}
	for _, c := range certs[1:] {
		opts.Intermediates.AddCert(c)
	}
	_, err := certs[0].Verify(opts)
	var unknown x509.UnknownAuthorityError
	if err == nil || !errors.As(err, &unknown) {
		return wrapVerifyError(certs, err)
	}

	// Walk up from the server's certificate, adding each issuer we can
	// download, until the chain reaches a root.
	cur := certs[0]
	for range maxIntermediateFetches {
		issuer, ferr := v.issuer(ctx, cur)
		if ferr != nil {
			return wrapVerifyError(certs, fmt.Errorf("%w (fetching the missing intermediate certificate: %v)", err, ferr))
		}
		if issuer == nil {
			break
		}
		opts.Intermediates.AddCert(issuer)
		if _, err = certs[0].Verify(opts); err == nil || !errors.As(err, &unknown) {
			return wrapVerifyError(certs, err)
		}
		cur = issuer
	}
	return wrapVerifyError(certs, err)
}

// wrapVerifyError reports a failure the way crypto/tls does, so callers that
// check for *tls.CertificateVerificationError still recognize it.
func wrapVerifyError(certs []*x509.Certificate, err error) error {
	if err == nil {
		return nil
	}
	return &tls.CertificateVerificationError{UnverifiedCertificates: certs, Err: err}
}

// issuer downloads the certificate that issued c, from the first usable URL
// in c. It returns nil, nil when c names none.
func (v *chainVerifier) issuer(ctx context.Context, c *x509.Certificate) (*x509.Certificate, error) {
	var lastErr error
	for _, u := range c.IssuingCertificateURL {
		v.mu.Lock()
		cached := v.cache[u]
		v.mu.Unlock()
		if cached != nil {
			return cached, nil
		}
		issuer, err := v.download(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		v.mu.Lock()
		if len(v.cache) >= maxCachedIntermediates {
			clear(v.cache)
		}
		v.cache[u] = issuer
		v.mu.Unlock()
		return issuer, nil
	}
	return nil, lastErr
}

func (v *chainVerifier) download(ctx context.Context, rawURL string) (*x509.Certificate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported issuer URL %s", rawURL)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	data, err := readLimited(resp.Body, maxIntermediateSize)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rawURL, err)
	}
	// Usually DER, sometimes PEM. (PKCS #7 bundles are rare and not handled.)
	if block, _ := pem.Decode(data); block != nil && block.Type == "CERTIFICATE" {
		data = block.Bytes
	}
	cert, err := x509.ParseCertificate(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rawURL, err)
	}
	if !cert.IsCA {
		return nil, fmt.Errorf("%s: not a CA certificate", rawURL)
	}
	return cert, nil
}

// dialTLS returns a DialTLSContext function that connects with dialer and
// verifies the server with v. It verifies against the host it was asked to
// connect to, rather than the TLS server name, which is empty for an IP
// address.
func (v *chainVerifier) dialTLS(dialer *net.Dialer, handshakeTimeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		raw, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
		defer cancel()
		conn := tls.Client(raw, &tls.Config{
			ServerName: host,
			NextProtos: []string{"h2", "http/1.1"},
			// Verification happens in VerifyConnection, which can fetch
			// missing intermediates. It runs on every handshake.
			InsecureSkipVerify: true,
			VerifyConnection: func(cs tls.ConnectionState) error {
				return v.verify(hctx, cs.PeerCertificates, host)
			},
		})
		if err := conn.HandshakeContext(hctx); err != nil {
			raw.Close()
			return nil, err
		}
		return conn, nil
	}
}

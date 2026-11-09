// Package feeds fetches subscribed feeds in the background and stores their
// new items and favicons.
package feeds

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const userAgent = "Fever/2 (personal feed reader)"

// newClient returns the HTTP client used for feeds and favicons. Unless
// allowPrivate is set, it refuses to connect to loopback, private and
// link-local addresses, so a feed (or a redirect in one) cannot make the
// server query the database container or other internal services.
func newClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !allowPrivate {
		dialer.Control = denyPrivate
	}
	transport := &http.Transport{
		// No proxy: the address check above must see the real destination.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   45 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// reserved lists special-purpose ranges that are not on the public internet
// beyond what netip already recognizes (loopback, private, link-local).
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // "this network"
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT, some VPNs
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking, some lab networks
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved, and broadcast
}

// nat64 is the well-known NAT64 prefix: it embeds an IPv4 address, which is
// what must be checked.
var nat64 = netip.MustParsePrefix("64:ff9b::/96")

func denyPrivate(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !isPublic(ip) {
		return fmt.Errorf("%s is a private address (set FETCH_ALLOW_PRIVATE=true to allow feeds on your network)", ip)
	}
	return nil
}

func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if nat64.Contains(ip) {
		b := ip.As16()
		ip = netip.AddrFrom4([4]byte(b[12:]))
	}
	switch {
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsUnspecified(),
		ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		return false
	}
	for _, p := range reserved {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// errTooLarge is returned when a response is bigger than allowed.
var errTooLarge = errors.New("response too large")

// readLimited reads at most max bytes, failing instead of truncating.
func readLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errTooLarge
	}
	return data, nil
}

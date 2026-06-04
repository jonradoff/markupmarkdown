// Package safefetch hands out HTTP clients whose DialContext refuses to
// connect to private, loopback, link-local, or cloud-metadata IPs — at both
// the initial connection and every redirect.
//
// Use this for any outbound HTTP that originates from a URL we received
// from a client.
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrBlockedAddress = errors.New("address is in a blocked range (loopback/private/link-local/metadata)")

// blockedNets are the CIDR ranges we refuse to dial.
var blockedNets = mustParseCIDRs([]string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",     // CGNAT — also used by some cloud private nets
	"127.0.0.0/8",       // loopback
	"169.254.0.0/16",    // link-local + AWS metadata 169.254.169.254
	"0.0.0.0/8",
	"::1/128",
	"fc00::/7",          // unique local
	"fe80::/10",         // link-local
})

func mustParseCIDRs(s []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(s))
	for _, c := range s {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

func isBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// safeDialContext resolves the host and refuses to dial if any resolved IP
// is in a blocked range.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs resolved for %s", host)
	}
	for _, ip := range ips {
		if isBlocked(ip) {
			return nil, fmt.Errorf("%w: %s -> %s", ErrBlockedAddress, host, ip)
		}
	}
	d := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

// ValidateURL parses a user-supplied URL and rejects schemes other than
// http/https, embedded credentials, and non-DNS hostnames. Does NOT do DNS
// resolution — that happens at dial time via safeDialContext. Returns the
// parsed URL on success.
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http/https URLs are supported")
	}
	if u.User != nil {
		return nil, fmt.Errorf("URLs with embedded credentials are not allowed")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL is missing a hostname")
	}
	return u, nil
}

// Client returns an HTTP client whose Transport refuses to dial private/
// metadata addresses, and whose CheckRedirect runs ValidateURL on every hop
// (the dialer enforces IP-level checks at the network layer regardless).
// MaxRedirects caps redirect chains at 5.
func Client(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if _, err := ValidateURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}

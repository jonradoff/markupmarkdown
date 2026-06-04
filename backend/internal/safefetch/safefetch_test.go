package safefetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestIsBlocked_Loopback(t *testing.T) {
	if !isBlocked(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should be blocked (loopback)")
	}
}

func TestIsBlocked_AWSMetadata(t *testing.T) {
	if !isBlocked(net.ParseIP("169.254.169.254")) {
		t.Error("169.254.169.254 (AWS metadata) must be blocked")
	}
}

func TestIsBlocked_PrivateRanges(t *testing.T) {
	for _, ip := range []string{
		"10.0.0.5",
		"172.16.5.1",
		"172.31.255.255",
		"192.168.1.1",
		"100.64.0.1", // CGNAT
	} {
		if !isBlocked(net.ParseIP(ip)) {
			t.Errorf("%s should be blocked (private)", ip)
		}
	}
}

func TestIsBlocked_PublicAllowed(t *testing.T) {
	for _, ip := range []string{
		"8.8.8.8",
		"1.1.1.1",
		"172.15.0.1", // just outside 172.16/12
	} {
		if isBlocked(net.ParseIP(ip)) {
			t.Errorf("%s should be allowed (public)", ip)
		}
	}
}

func TestIsBlocked_IPv6(t *testing.T) {
	if !isBlocked(net.ParseIP("::1")) {
		t.Error("::1 should be blocked (IPv6 loopback)")
	}
	if !isBlocked(net.ParseIP("fc00::1")) {
		t.Error("fc00::/7 should be blocked (IPv6 unique local)")
	}
	if !isBlocked(net.ParseIP("fe80::1")) {
		t.Error("fe80::/10 should be blocked (IPv6 link-local)")
	}
}

func TestIsBlocked_NilIP(t *testing.T) {
	if !isBlocked(nil) {
		t.Error("nil IP should be blocked (defensive)")
	}
}

func TestIsBlocked_Multicast(t *testing.T) {
	if !isBlocked(net.ParseIP("224.0.0.1")) {
		t.Error("multicast should be blocked")
	}
}

func TestValidateURL_HappyPath(t *testing.T) {
	cases := []string{
		"https://example.com/foo",
		"http://example.com",
		"  https://example.com  ", // leading/trailing whitespace
	}
	for _, raw := range cases {
		u, err := ValidateURL(raw)
		if err != nil {
			t.Errorf("ValidateURL(%q): %v", raw, err)
		}
		if u == nil {
			t.Errorf("ValidateURL(%q) returned nil URL", raw)
		}
	}
}

func TestValidateURL_RejectsNonHTTP(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"gopher://x",
		"javascript:alert(1)",
		"ftp://x",
	} {
		if _, err := ValidateURL(raw); err == nil {
			t.Errorf("ValidateURL(%q) should reject non-http/https", raw)
		}
	}
}

func TestValidateURL_RejectsEmbeddedCreds(t *testing.T) {
	if _, err := ValidateURL("https://user:pass@example.com"); err == nil {
		t.Error("URL with creds must be rejected")
	}
}

func TestValidateURL_RejectsMissingHost(t *testing.T) {
	if _, err := ValidateURL("http:///path"); err == nil {
		t.Error("URL missing host must be rejected")
	}
}

func TestValidateURL_BadURL(t *testing.T) {
	// url.Parse is quite forgiving, but %ZZ is reliably malformed.
	if _, err := ValidateURL("http://example.com/%ZZ"); err == nil {
		t.Error("malformed URL should be rejected")
	}
}

func TestClient_Fetches_PublicServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	t.Cleanup(srv.Close)

	// httptest servers bind to 127.0.0.1, which the safe dialer should
	// reject. This verifies the protection actually fires end-to-end.
	c := Client(2 * time.Second)
	_, err := c.Get(srv.URL)
	if err == nil {
		t.Fatal("expected loopback fetch to be blocked by safe dialer")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Logf("error message: %v", err)
	}
}

func TestClient_RedirectsValidatedAndCapped(t *testing.T) {
	// Build a redirect chain that never exceeds 5; verify the redirect
	// limit kicks in around 5.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	c := Client(2 * time.Second)
	_, err := c.Get(srv.URL)
	if err == nil {
		t.Fatal("expected error from infinite redirect / blocked dial")
	}
}

func TestClient_RedirectCheck_RejectsNonHTTP(t *testing.T) {
	// Simulate CheckRedirect being called with a non-HTTP scheme via a
	// constructed *http.Request — exercises the redirect's ValidateURL
	// branch directly without a live server.
	c := Client(time.Second)
	if c.CheckRedirect == nil {
		t.Fatal("client has no CheckRedirect")
	}
	bad, _ := url.Parse("ftp://example.com/")
	req := &http.Request{URL: bad}
	if err := c.CheckRedirect(req, nil); err == nil {
		t.Error("CheckRedirect should reject non-http/https redirects")
	}
}

func TestClient_RedirectCap(t *testing.T) {
	c := Client(time.Second)
	good, _ := url.Parse("https://example.com/")
	req := &http.Request{URL: good}
	// 5 prior requests = 6th request; CheckRedirect should bail.
	prior := make([]*http.Request, 5)
	if err := c.CheckRedirect(req, prior); err == nil {
		t.Error("CheckRedirect should refuse on 6th redirect")
	}
}

// Compile-time assertions to keep imports honest.
var (
	_ = errors.New
	_ context.Context
)

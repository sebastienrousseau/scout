// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package egress

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// proxyClient is a client that dials through the proxy, the way a server
// reading HTTP_PROXY out of its environment would.
func proxyClient(t *testing.T, p *Proxy) *http.Client {
	t.Helper()
	u, err := url.Parse(p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
			// The fixture upstreams are httptest TLS servers with their
			// own certificate. Nothing here inspects the tunnel; this is
			// so the fixture can complete a handshake through it.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a test fixture's self-signed certificate
		},
	}
}

func startProxy(t *testing.T, policy Policy) *Proxy {
	t.Helper()
	p, err := Start(policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// TestProxyRecordsAPlainRequest: a plain http call arrives in absolute
// form and names its destination in the request line.
func TestProxyRecordsAPlainRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	defer upstream.Close()

	p := startProxy(t, nil)
	res, err := proxyClient(t, p).Get(upstream.URL + "/thing")
	if err != nil {
		t.Fatalf("through the proxy: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "hello" {
		t.Errorf("the response did not survive the proxy: %q", body)
	}

	dials := p.Dials()
	if len(dials) != 1 {
		t.Fatalf("want one destination, got %+v", dials)
	}
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	if dials[0].Host != host || dials[0].Port != port {
		t.Errorf("recorded %s, want %s:%s", dials[0].Target(), host, port)
	}
	if dials[0].Tunnelled {
		t.Error("a plain request is not a tunnel")
	}
	if !dials[0].Allowed {
		t.Error("the default policy should allow")
	}
}

// TestProxyRecordsATunnelWithoutReadingIt is the case that matters. An
// https destination arrives as CONNECT, in clear text, before any
// handshake — so the hostname is knowable with no certificate authority
// and no interception of what was actually sent.
func TestProxyRecordsATunnelWithoutReadingIt(t *testing.T) {
	const secret = "the-body-nobody-should-read"
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != secret {
			t.Errorf("the tunnelled body was altered: %q", b)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	p := startProxy(t, nil)
	res, err := proxyClient(t, p).Post(upstream.URL, "text/plain", strings.NewReader(secret))
	if err != nil {
		t.Fatalf("through the tunnel: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	dials := p.Dials()
	if len(dials) != 1 {
		t.Fatalf("want one destination, got %+v", dials)
	}
	if !dials[0].Tunnelled {
		t.Error("an https destination should be recorded as a tunnel")
	}
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "https://"))
	if dials[0].Host != host || dials[0].Port != port {
		t.Errorf("recorded %s, want %s:%s", dials[0].Target(), host, port)
	}
}

// TestProxyCountsRepeats, so a report can say a destination was reached
// once or four hundred times.
func TestProxyCountsRepeats(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	p := startProxy(t, nil)
	c := proxyClient(t, p)
	for range 3 {
		res, err := c.Get(upstream.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
	}
	dials := p.Dials()
	if len(dials) != 1 || dials[0].Count != 3 {
		t.Fatalf("want one destination counted 3 times, got %+v", dials)
	}
}

// TestProxyRefusesByPolicy: observing is the default, but a policy that
// says no has to actually stop the connection, not merely note it.
func TestProxyRefusesByPolicy(t *testing.T) {
	var reached bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer upstream.Close()

	p := startProxy(t, func(string, string) bool { return false })
	res, err := proxyClient(t, p).Get(upstream.URL)
	if err == nil {
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("status %d, want 403", res.StatusCode)
		}
	}
	if reached {
		t.Error("a refused destination was reached anyway")
	}

	dials := p.Dials()
	if len(dials) != 1 {
		t.Fatalf("a refusal must still be recorded: %+v", dials)
	}
	if dials[0].Allowed {
		t.Error("the refusal was not recorded as one")
	}
}

// TestProxyEnvSetsBothCases: the variable name is a convention rather than
// a standard, and a library that reads only one spelling would otherwise
// slip past the observation entirely.
func TestProxyEnvSetsBothCases(t *testing.T) {
	p := startProxy(t, nil)
	env := strings.Join(p.Env(), "\n")
	for _, name := range []string{
		"HTTP_PROXY=", "http_proxy=", "HTTPS_PROXY=", "https_proxy=",
	} {
		if !strings.Contains(env, name+p.Addr()) {
			t.Errorf("%s is not pointed at the proxy:\n%s", name, env)
		}
	}
	// An inherited NO_PROXY would carve a hole in the observation.
	for _, name := range []string{"NO_PROXY=", "no_proxy="} {
		if !strings.Contains(env, name+"\n") && !strings.HasSuffix(env, name) {
			t.Errorf("%s is not neutralised:\n%s", name, env)
		}
	}
}

// TestProxyListensOnLoopbackOnly. A proxy reachable from off the machine
// is an open relay, which is worse than anything it exists to detect.
func TestProxyListensOnLoopbackOnly(t *testing.T) {
	p := startProxy(t, nil)
	host, _, err := net.SplitHostPort(strings.TrimPrefix(p.Addr(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Errorf("listening on %q, want loopback", host)
	}
}

// TestProxyReportsAnUnreachableUpstream rather than hanging or pretending
// the connection succeeded.
func TestProxyReportsAnUnreachableUpstream(t *testing.T) {
	p := startProxy(t, nil)
	res, err := proxyClient(t, p).Get("http://127.0.0.1:1/nothing")
	if err == nil {
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusBadGateway {
			t.Errorf("status %d, want 502", res.StatusCode)
		}
	}
	// It was still an attempt, and the attempt is the finding.
	if len(p.Dials()) != 1 {
		t.Errorf("a failed dial must still be recorded: %+v", p.Dials())
	}
}

// TestProxyCloseIsIdempotent, because it runs on a defer.
func TestProxyCloseIsIdempotent(t *testing.T) {
	p, err := Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	_ = p.Close()
}

// TestSplitTargetDefaultsThePort.
func TestSplitTargetDefaultsThePort(t *testing.T) {
	for _, tc := range []struct{ in, fb, host, port string }{
		{"example.com:8443", "443", "example.com", "8443"},
		{"example.com", "443", "example.com", "443"},
		{"example.com", "80", "example.com", "80"},
		{" example.com ", "80", "example.com", "80"},
	} {
		h, p := splitTarget(tc.in, tc.fb)
		if h != tc.host || p != tc.port {
			t.Errorf("splitTarget(%q, %q) = %q, %q; want %q, %q", tc.in, tc.fb, h, p, tc.host, tc.port)
		}
	}
}

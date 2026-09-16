// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package auth

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// URLPolicy decides whether a URL learned from a remote document may be
// fetched or sent credentials.
//
// Every endpoint in the OAuth chain after the operator's own is
// server-controlled: authorization_servers comes from the resource,
// token_endpoint and registration_endpoint come from the authorization
// server, and the resource_metadata hint comes from a response header. A
// client that follows them unchecked will exchange its client secret with
// whatever host the resource names, including a plaintext one or a cloud
// metadata address reachable from the CI runner scout is running on.
//
// The zero value is the strict policy: HTTPS only, public hosts only.
type URLPolicy struct {
	// AllowHTTP permits http:// URLs. Loopback is always permitted so a
	// local development authorization server keeps working.
	AllowHTTP bool
	// AllowPrivate permits hosts that resolve to loopback, link-local,
	// private or otherwise non-public addresses.
	AllowPrivate bool
	// Resolver is used to resolve hostnames; nil means the default resolver.
	Resolver func(ctx context.Context, host string) ([]net.IP, error)
}

// PolicyError explains why a discovered URL was refused.
type PolicyError struct {
	Kind   string // "authorization server", "token endpoint", ...
	URL    string
	Reason string
}

func (e *PolicyError) Error() string {
	return fmt.Sprintf("auth: refusing %s %q: %s", e.Kind, e.URL, e.Reason)
}

// Validate checks a URL discovered from a remote document. kind names the
// field for the error message.
func (p URLPolicy) Validate(ctx context.Context, kind, raw string) error {
	deny := func(reason string) error { return &PolicyError{Kind: kind, URL: raw, Reason: reason} }
	if raw == "" {
		return deny("empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return deny("not a URL: " + err.Error())
	}
	if u.Host == "" {
		return deny("not absolute")
	}
	scheme := strings.ToLower(u.Scheme)
	host := u.Hostname()
	loopback := isLoopbackHost(host)
	switch scheme {
	case "https":
	case "http":
		if !p.AllowHTTP && !loopback {
			return deny("must use https; a token or client secret sent here would cross the network in the clear")
		}
	default:
		return deny("scheme " + scheme + " is not http(s)")
	}
	if u.Fragment != "" {
		return deny("must not carry a fragment")
	}
	if p.AllowPrivate || loopback {
		return nil
	}
	ips, err := p.resolve(ctx, host)
	if err != nil {
		// A name that does not resolve is not a policy failure; the fetch
		// will fail on its own and say so more clearly than this could.
		return nil //nolint:nilerr // deliberate: resolution failure is not a refusal
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return deny(fmt.Sprintf("resolves to the non-public address %s; discovered endpoints must not point inside the network scout runs in (pass --insecure-allow-private-hosts if this is deliberate)", ip))
		}
	}
	return nil
}

func (p URLPolicy) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if p.Resolver != nil {
		return p.Resolver(ctx, host)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// isPublicIP reports whether ip is routable on the public internet. The
// cloud metadata addresses (169.254.169.254, fd00:ec2::254) are link-local
// and unique-local respectively, so they are covered.
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Carrier-grade NAT (RFC 6598) and IPv4 benchmarking ranges are not
	// covered by the stdlib predicates.
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 100 && v4[1]&0xc0 == 64: // 100.64.0.0/10
			return false
		case v4[0] == 198 && v4[1]&0xfe == 18: // 198.18.0.0/15
			return false
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
			return false
		}
	}
	return true
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package scout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/sebastienrousseau/scout/trace"
	"github.com/sebastienrousseau/scout/transport"
)

// Era is which generation of the protocol a server speaks.
type Era string

const (
	// EraStateless is 2026-07-28 and later: no handshake, no session, every
	// request carrying its own protocol metadata.
	EraStateless Era = "stateless"
	// EraSession is 2025-03-26 through 2025-11-25: an initialize handshake
	// establishes connection state carried by a session header.
	EraSession Era = "session"
	// EraUnknown means detection has not run or could not decide.
	EraUnknown Era = "unknown"
)

// Negotiation records how the era was decided, so a report can say what was
// tried rather than only what was concluded.
type Negotiation struct {
	Era Era `json:"era"`
	// Version is the protocol version in use.
	Version string `json:"version"`
	// Attempted lists the versions offered, newest first.
	Attempted []string `json:"attempted,omitempty"`
	// ServerSupported is what the server said it supports, when it told us.
	ServerSupported []string `json:"server_supported,omitempty"`
	// Reason is the plain-language account of how the era was decided.
	Reason string `json:"reason,omitempty"`
	// Discovered is the server/discover result, when the server answered it.
	Discovered *DiscoverResult `json:"discovered,omitempty"`
}

// DiscoverResult is what server/discover returned. The RPC is optional in
// the 2026-07-28 revision, so its absence is not a failure.
type DiscoverResult struct {
	ResultType   string             `json:"resultType,omitempty"`
	ServerInfo   Implementation     `json:"serverInfo"`
	Capabilities ServerCapabilities `json:"capabilities"`
	Instructions string             `json:"instructions,omitempty"`
	// Extensions the server advertises, by reverse-DNS identifier.
	Extensions []string `json:"extensions,omitempty"`
}

// StatelessVersions are the stateless revisions scout offers, newest first.
var StatelessVersions = []string{transport.V20260728}

// SessionVersions are the handshake revisions scout offers, newest first.
var SessionVersions = []string{transport.V20251125, transport.V20250618, transport.V20250326}

// Negotiate decides which era the server speaks and configures the
// transport for it.
//
// The order follows the specification's backward-compatibility rule: try a
// stateless request first, and on 400 read the body before concluding
// anything. A modern server explains itself with a JSON-RPC error — an
// unsupported version, a missing capability, a header mismatch — and should
// be retried or corrected, not abandoned. Only an empty or unrecognised
// body means the server predates the revision and wants an initialize
// handshake.
func (c *Client) Negotiate(ctx context.Context) (*Negotiation, error) {
	ctx = trace.Ensure(ctx)
	n := &Negotiation{Era: EraUnknown, Attempted: slices.Clone(StatelessVersions)}

	for _, v := range StatelessVersions {
		res, err := c.tryStateless(ctx, v)
		switch {
		case err == nil:
			n.Era, n.Version, n.Discovered = EraStateless, v, res
			n.Reason = fmt.Sprintf("the server answered a stateless %s request", v)
			c.setNegotiation(n)
			return n, nil

		case isMethodNotFound(err):
			// The version is fine; this server simply does not implement
			// the optional discovery RPC. That is still a stateless server.
			n.Era, n.Version = EraStateless, v
			n.Reason = fmt.Sprintf("the server accepted a stateless %s request but does not implement server/discover", v)
			c.setNegotiation(n)
			return n, nil

		default:
			var uv *transport.UnsupportedVersionError
			if errors.As(err, &uv) {
				n.ServerSupported = uv.Supported
				// The server named what it speaks. If that includes a
				// stateless version we do too, take it; otherwise this is a
				// session-era server that is merely polite about it.
				if pick := firstCommon(uv.Supported, StatelessVersions); pick != "" && pick != v {
					n.Attempted = append(n.Attempted, pick)
					if res, err := c.tryStateless(ctx, pick); err == nil || isMethodNotFound(err) {
						n.Era, n.Version, n.Discovered = EraStateless, pick, res
						n.Reason = "the server named " + pick + " among its supported versions"
						c.setNegotiation(n)
						return n, nil
					}
				}
				n.Reason = fmt.Sprintf("the server does not support %s; it offers %v", v, uv.Supported)
				continue
			}
			if modern, ok := asModernRejection(err); ok {
				// It speaks the revision but refused this particular
				// request. Falling back would hide a real problem.
				n.Era, n.Version = EraStateless, v
				n.Reason = "the server answered with a " + modern
				c.setNegotiation(n)
				return n, fmt.Errorf("scout: the server speaks %s but rejected the request: %w", v, err)
			}
			if isLegacyMethodNotFound(err) {
				n.Reason = "the server answered server/discover with a plain JSON-RPC -32601 at HTTP 200, which is how a handshake-era server reports a method it does not know"
				continue
			}
			if !looksLegacy(err) {
				return nil, fmt.Errorf("scout: negotiating protocol version: %w", err)
			}
			n.Reason = "the server did not answer a stateless request with a recognised error, so it predates " + v
		}
	}

	// Session era: the initialize handshake settles the version.
	init, err := c.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("scout: no stateless version was accepted and the initialize handshake failed: %w", err)
	}
	n.Era, n.Version = EraSession, init.ProtocolVersion
	n.Attempted = append(n.Attempted, SessionVersions...)
	if n.Reason == "" {
		n.Reason = "the server completed an initialize handshake"
	}
	n.Reason += fmt.Sprintf("; it negotiated %s", init.ProtocolVersion)
	c.setNegotiation(n)
	return n, nil
}

// tryStateless configures the transport for a stateless version and probes
// it with server/discover.
func (c *Client) tryStateless(ctx context.Context, version string) (*DiscoverResult, error) {
	caps, err := json.Marshal(ClientCapabilities{})
	if err != nil {
		return nil, err
	}
	c.tr.SetDialect(&transport.Stateless{
		ProtocolVersion: version,
		ClientInfo:      transport.Implementation{Name: c.cfg.ClientInfo.Name, Title: c.cfg.ClientInfo.Title, Version: c.cfg.ClientInfo.Version},
		Capabilities:    caps,
	})
	var out DiscoverResult
	if err := c.tr.Call(ctx, "server/discover", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Negotiation returns the result of the last Negotiate call, or nil.
func (c *Client) Negotiation() *Negotiation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.negotiated
}

func (c *Client) setNegotiation(n *Negotiation) {
	c.mu.Lock()
	c.negotiated = n
	c.mu.Unlock()
	if n.Era == EraSession {
		// Restore the handshake binding for everything that follows.
		c.tr.SetDialect(&transport.Sessioned{})
	}
}

// isMethodNotFound reports whether the server answered "no such method" in
// the shape a 2026-07-28 server must use: HTTP 404 with a JSON-RPC -32601
// body. For server/discover that means a stateless server that simply does
// not implement the optional RPC.
//
// The HTTP status is the whole signal. A handshake-era server handed
// server/discover also answers -32601, but as an ordinary JSON-RPC error
// with HTTP 200 — it does not know the method either. Treating a plain
// -32601 as proof of a stateless server would label every legacy server as
// current, which is the one thing this check exists to avoid.
func isMethodNotFound(err error) bool {
	var he *transport.HTTPStatusError
	return errors.As(err, &he) &&
		he.StatusCode == http.StatusNotFound &&
		he.RPCError != nil &&
		he.RPCError.Code == transport.CodeMethodNotFound
}

// isLegacyMethodNotFound reports the handshake-era shape of "no such
// method": a JSON-RPC error delivered with a 2xx status.
func isLegacyMethodNotFound(err error) bool {
	var he *transport.HTTPStatusError
	if errors.As(err, &he) {
		return false // carried by an HTTP status, so not the 200 form
	}
	var rpc *transport.RPCError
	return errors.As(err, &rpc) && rpc.Code == transport.CodeMethodNotFound
}

// asModernRejection names the modern error the server answered with, if it
// answered with one.
func asModernRejection(err error) (string, bool) {
	var mc *transport.MissingCapabilityError
	if errors.As(err, &mc) {
		return "missing-capability error, which only a 2026-07-28 server sends", true
	}
	var hm *transport.HeaderMismatchError
	if errors.As(err, &hm) {
		return "header-mismatch error, which only a 2026-07-28 server sends", true
	}
	return "", false
}

// looksLegacy reports whether a failed stateless attempt is consistent with
// a server that predates the revision, rather than one that is simply
// broken or unreachable.
func looksLegacy(err error) bool {
	var he *transport.HTTPStatusError
	if !errors.As(err, &he) {
		// A malformed body or an unparsable response is also what a legacy
		// server looks like when handed a request it does not understand.
		return !errors.Is(err, transport.ErrNoResponse)
	}
	if he.Modern() {
		return false
	}
	switch he.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusNotImplemented, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

func firstCommon(theirs, ours []string) string {
	for _, o := range ours {
		if slices.Contains(theirs, o) {
			return o
		}
	}
	return ""
}

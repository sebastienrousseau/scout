// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/transport"
)

// checkEra reports which generation of the protocol the server speaks.
//
// The 2026-07-28 revision removed the initialize handshake and the session
// header and moved every request's metadata into _meta. A server still on
// the handshake revisions is not broken — the older versions remain
// implementable — but an operator choosing a server for an agent needs to
// know how far behind it is, and a server author needs to know the clock is
// running. Neither shows up anywhere else in the report.
//
// The probe is a single stateless request, sent before the handshake and
// undone afterwards, so nothing downstream sees a changed binding.
func checkEra(ctx context.Context, s *Session) Finding {
	c := s.check("handshake.protocol_era", "Protocol generation")
	if s.Opts.SkipEraCheck {
		return c.skip("--skip-era-check was passed, so the probe that identifies the protocol generation was not sent")
	}

	// One request, on the transport the rest of the phase uses, with the
	// dialect put back afterwards. Running a full negotiation here would
	// repeat the initialize handshake that follows, which is both wasteful
	// and extra load on a server scout is only meant to observe.
	disc, stateless := statelessProbe(ctx, s, s.Client.Transport(), "handshake")
	if stateless {
		detail := "speaks " + scout.StatelessVersions[0] + ", the current stateless revision"
		if disc != nil && disc.ServerInfo.Name != "" {
			detail += " (" + disc.ServerInfo.Name + " " + disc.ServerInfo.Version + ")"
		}
		s.Era = &scout.Negotiation{
			Era: scout.EraStateless, Version: scout.StatelessVersions[0], Discovered: disc,
			Reason: "it answered a stateless request",
		}
		return c.pass(detail)
	}

	// Not stateless. The version is whatever the handshake that follows
	// negotiates, so record the era now and let the handshake fill it in.
	s.Era = &scout.Negotiation{Era: scout.EraSession, Reason: "it did not answer a stateless request"}
	return c.warn(
		"speaks a handshake revision: it uses initialize and "+transport.HeaderSessionID+", which "+scout.StatelessVersions[0]+" removed",
		"the current revision is "+scout.StatelessVersions[0]+
			": requests carry their own protocol version, client identity and capabilities in _meta, sessions are gone, and "+
			transport.HeaderMethod+"/"+transport.HeaderName+" let a gateway route without parsing the body. "+
			"Agents on current clients will keep working through the compatibility rules, but plan the move")
}

// statelessBareProbe asks, without credentials, whether the endpoint
// answers a stateless request. It goes over the bare transport so first
// contact stays unauthenticated, as ADR-0001 requires.
func statelessBareProbe(ctx context.Context, s *Session) (*scout.DiscoverResult, bool) {
	return statelessProbe(ctx, s, s.Bare, "discovery")
}

// statelessProbe sends one server/discover on tr under the stateless
// dialect and reports whether the endpoint answered it, restoring the
// dialect afterwards.
//
// A server on the current revision either answers, or refuses with the
// shape the revision defines for an unimplemented RPC: HTTP 404 carrying a
// JSON-RPC -32601. A handshake-era server answers -32601 too — it does not
// know the method either — but at HTTP 200, so the status is the whole
// signal. Reading a bare -32601 as proof would label every legacy server as
// current, which is the one thing this check exists to avoid.
func statelessProbe(ctx context.Context, s *Session, tr *transport.Streamable, phase string) (*scout.DiscoverResult, bool) {
	restore := tr.Dialect()
	defer func() {
		tr.SetDialect(restore)
		tr.Reset()
	}()

	caps, err := json.Marshal(struct{}{})
	if err != nil {
		return nil, false
	}
	tr.SetDialect(&transport.Stateless{
		ProtocolVersion: scout.StatelessVersions[0],
		ClientInfo:      transport.Implementation{Name: "scout", Version: s.Opts.Version},
		Capabilities:    caps,
	})

	var out scout.DiscoverResult
	cctx := telemetry.WithPhase(ctx, phase, "stateless probe")
	err = tr.Call(cctx, "server/discover", map[string]any{}, &out)
	switch {
	case err == nil:
		return &out, true
	case isStatelessMethodNotFound(err):
		// It took the request; it just does not implement the optional RPC.
		return nil, true
	}
	return nil, false
}

// isStatelessMethodNotFound is the current revision's shape for an
// unimplemented RPC: HTTP 404 carrying a JSON-RPC -32601. A handshake-era
// server answers -32601 too, but at HTTP 200.
func isStatelessMethodNotFound(err error) bool {
	var he *transport.HTTPStatusError
	return errors.As(err, &he) &&
		he.StatusCode == http.StatusNotFound &&
		he.RPCError != nil &&
		he.RPCError.Code == transport.CodeMethodNotFound
}

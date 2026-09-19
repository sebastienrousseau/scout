// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/auth"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/transport"
)

// phaseDiscovery makes first contact without credentials and, on a 401,
// walks the OAuth discovery chain, checking each document it finds.
func phaseDiscovery(ctx context.Context, s *Session) []Finding {
	var out []Finding
	tr := s.Bare

	// First contact must be unauthenticated even when credentials were
	// supplied, so we can see whether the server enforces auth at all. The
	// bare transport carries no token, no API-key headers, no basic auth.
	//
	// Which request that is depends on the generation the server speaks: the
	// stateless revision removed initialize, so sending one there proves
	// nothing about authorization. The era is settled first, on the same
	// credential-free transport.
	c := s.check("discovery.first_contact", "Unauthenticated first contact")
	s.settleEra(ctx)
	raw, err := s.firstContact(ctx)
	if err != nil {
		out = append(out, c.fail(Critical, "request failed: "+err.Error(), "the endpoint must accept a JSON-RPC POST"))
		s.blocked = "endpoint does not answer HTTP"
		return out
	}
	tr.Reset()
	s.Reached = true
	switch {
	case raw.Status == http.StatusUnauthorized:
		s.RequiresAuth = true
		out = append(out, c.pass("401 Unauthorized: the server enforces authorization"))
	case raw.Status == http.StatusOK && raw.Response != nil && raw.Response.Error == nil:
		out = append(out, c.info("200 OK without credentials: the server is open"))
		if s.Opts.Creds.Effective() != creds.ModeNone {
			out = append(out, s.check("discovery.creds_unused", "Credentials supplied to an open server").
				warn("credentials were provided but the server did not demand any", "confirm this endpoint is meant to be public"))
		}
		return out
	case raw.Status == http.StatusOK:
		out = append(out, c.warn(fmt.Sprintf("200 OK but the body was not an initialize result: %s", truncate(string(raw.Body), 200)), "return a JSON-RPC result for initialize"))
		return out
	case raw.Status == http.StatusForbidden:
		s.RequiresAuth = true
		out = append(out, c.warn("403 Forbidden on first contact; the spec expects 401 with a WWW-Authenticate challenge", "answer 401 and point clients to the resource metadata"))
	default:
		// A server on the current revision rejects a handshake-era
		// initialize because the method is gone, not because the operator
		// got anything wrong. Reporting "unexpected HTTP 400" would blame
		// the server for scout's own limit, so ask it directly.
		if disc, ok := statelessBareProbe(ctx, s); ok {
			name := ""
			if disc != nil && disc.ServerInfo.Name != "" {
				name = " (" + disc.ServerInfo.Name + " " + disc.ServerInfo.Version + ")"
			}
			s.Era = &scout.Negotiation{Era: scout.EraStateless, Version: scout.StatelessVersions[0], Discovered: disc,
				Reason: "it answered a stateless request after refusing a handshake-era initialize"}
			out = append(out, c.fail(Critical,
				"the server speaks "+scout.StatelessVersions[0]+name+", the stateless revision that removed initialize",
				"nothing is wrong with this server. scout's diagnostic still opens with the handshake-era initialize, so it cannot complete a run against a stateless-only server yet: the transport speaks the revision but the nine-phase pipeline has not been moved onto it"))
			s.blocked = "the server speaks " + scout.StatelessVersions[0] + ", which scout cannot yet run a full diagnostic against"
			return out
		}
		out = append(out, c.fail(Critical, fmt.Sprintf("unexpected HTTP %d: %s", raw.Status, truncate(string(raw.Body), 200)), "answer 200 for an open server or 401 for a protected one"))
		s.blocked = fmt.Sprintf("first contact returned HTTP %d", raw.Status)
		return out
	}

	// Challenge shape.
	c = s.check("discovery.challenge", "WWW-Authenticate challenge")
	hdr := raw.Header.Get("WWW-Authenticate")
	challenges := auth.ParseWWWAuthenticate(hdr)
	bearer, ok := auth.FindBearer(challenges)
	switch {
	case hdr == "":
		out = append(out, c.warn("no WWW-Authenticate header on the 401", "add `WWW-Authenticate: Bearer resource_metadata=\"…\"` so clients can discover the authorization server"))
		bearer = auth.Challenge{Scheme: "Bearer", Params: map[string]string{}}
	case !ok:
		out = append(out, c.ev(hdr).warn("challenge has no Bearer scheme: "+hdr, "MCP clients look for a Bearer challenge"))
		bearer = auth.Challenge{Scheme: "Bearer", Params: map[string]string{}}
	case bearer.Params["resource_metadata"] == "":
		out = append(out, c.ev(hdr).warn("Bearer challenge lacks resource_metadata; falling back to well-known lookup", "include resource_metadata in the challenge"))
	default:
		out = append(out, c.ev(hdr).pass("Bearer challenge with resource_metadata"))
	}
	s.Challenge = bearer
	if sc := bearer.Params["scope"]; sc != "" {
		out = append(out, s.check("discovery.challenge.scope", "Challenge advertises required scope").info("scope="+sc))
	}

	// Overrides short-circuit discovery.
	if s.Opts.Creds.TokenURL != "" {
		out = append(out, s.check("discovery.override", "Discovery bypassed by --token-url").info("token endpoint "+s.Opts.Creds.TokenURL))
		d, err := s.Client.Discover(ctx, bearer)
		if err != nil {
			out = append(out, s.check("discovery.override.build", "Override endpoints").fail(Major, err.Error(), ""))
			return out
		}
		s.Discovery = d
		return out
	}
	if s.Opts.Creds.Effective() == creds.ModeBearer {
		// A static token needs no discovery, but we still probe it for the
		// report, non-fatally.
		d, err := s.Client.Discover(telemetry.WithPhase(ctx, "discovery", "metadata (informational)"), bearer)
		c = s.check("discovery.prm", "Protected resource metadata")
		if err != nil {
			out = append(out, c.warn("not discoverable: "+err.Error(), "a bearer token was supplied so this is not blocking, but agents that must obtain their own tokens will fail"))
		} else {
			s.Discovery = d
			out = append(out, c.ev(d.PRMSource).pass("found; authorization server "+d.Server.Issuer))
		}
		return out
	}

	// Full discovery with per-document findings.
	disc := s.Client.Discoverer()
	c = s.check("discovery.prm", "Protected resource metadata (RFC 9728)")
	prm, from, err := disc.DiscoverPRM(telemetry.WithPhase(ctx, "discovery", "PRM"), s.Opts.Endpoint, bearer.Params["resource_metadata"])
	if err != nil {
		if errors.Is(err, auth.ErrNoAuthorizationServers) {
			out = append(out, c.ev(from).fail(Critical, "document lists no authorization_servers", "populate authorization_servers"))
		} else {
			out = append(out, c.fail(Critical, "not found: "+err.Error(), "serve /.well-known/oauth-protected-resource (path-aware) or reference it from the challenge"))
		}
		s.blocked = "authorization metadata is not discoverable"
		return out
	}
	out = append(out, c.ev(from).pass(fmt.Sprintf("found at %s; %d authorization server(s)", from, len(prm.AuthorizationServers))))

	c = s.check("discovery.prm.resource", "PRM resource matches endpoint")
	canon, _ := auth.CanonicalResource(s.Opts.Endpoint)
	switch {
	case prm.Resource == "":
		out = append(out, c.warn("resource field is empty", "set resource to the canonical MCP endpoint URL"))
	case strings.TrimSuffix(prm.Resource, "/") == strings.TrimSuffix(canon, "/"):
		out = append(out, c.pass(prm.Resource))
	default:
		out = append(out, c.fail(Major, fmt.Sprintf("resource %q differs from endpoint %q", prm.Resource, canon),
			"RFC 9728 requires the metadata to identify the resource it protects; a mismatch is how a token minted for one server gets requested on another's behalf. Fix the resource field, or re-run with --allow-resource-mismatch if you know it is benign"))
	}
	// Every listed server is checked, not only the first: a list whose
	// first entry is https and whose second is not would otherwise pass
	// while the client fell through to the plaintext one.
	var plaintext []string
	for _, as := range prm.AuthorizationServers {
		if !strings.HasPrefix(as, "https://") && !isLoopback(s.URL.Hostname()) {
			plaintext = append(plaintext, as)
		}
	}
	if len(plaintext) > 0 {
		out = append(out, s.check("discovery.as.https", "Authorization server uses HTTPS").
			fail(Major, strings.Join(plaintext, ", "),
				"authorization servers must be reached over TLS; scout will not send a client secret or token to a plaintext endpoint (override with --insecure-allow-http-auth)"))
	}

	c = s.check("discovery.as", "Authorization server metadata (RFC 8414 / OIDC)")
	var md *auth.ServerMetadata
	var errs []string
	var policyErrs []error
	for _, issuer := range prm.AuthorizationServers {
		m, err := disc.DiscoverServer(telemetry.WithPhase(ctx, "discovery", "AS metadata"), issuer)
		if err != nil {
			errs = append(errs, err.Error())
			policyErrs = append(policyErrs, err)
			continue
		}
		md = m
		break
	}
	if md == nil {
		// A refusal is not the same as a document that is missing, and the
		// operator needs to be told which one happened.
		if pe := firstPolicyError(policyErrs); pe != nil {
			out = append(out, c.fail(Critical, pe.Error(),
				"the server named an authorization server scout will not talk to; fix the metadata, or re-run with --insecure-allow-http-auth / --insecure-allow-private-hosts if you trust this endpoint"))
			s.blocked = "the authorization server this resource names was refused as unsafe"
			return out
		}
		out = append(out, c.fail(Critical, "no authorization server published metadata: "+strings.Join(errs, "; "), "serve /.well-known/oauth-authorization-server or /.well-known/openid-configuration"))
		s.blocked = "authorization server metadata is not discoverable"
		return out
	}
	out = append(out, c.ev("issuer="+md.Issuer, "token="+md.TokenEndpoint).pass("issuer "+md.Issuer))

	c = s.check("discovery.as.pkce", "PKCE S256 advertised")
	switch {
	case len(md.CodeChallengeMethodsSupported) == 0:
		out = append(out, c.warn("code_challenge_methods_supported is absent", "advertise S256; MCP clients must use PKCE"))
	case contains(md.CodeChallengeMethodsSupported, "S256"):
		out = append(out, c.pass("S256"))
	default:
		out = append(out, c.fail(Major, "S256 not supported: "+strings.Join(md.CodeChallengeMethodsSupported, ","), "support S256"))
	}
	c = s.check("discovery.as.grants", "Grant types advertised")
	if len(md.GrantTypesSupported) == 0 {
		out = append(out, c.info("grant_types_supported absent (defaults apply)"))
	} else {
		out = append(out, c.info(strings.Join(md.GrantTypesSupported, ", ")))
	}
	c = s.check("discovery.registration", "Client registration path")
	switch {
	case md.ClientIDMetadataDocumentSupported:
		out = append(out, c.pass("client ID metadata documents supported"+dcrNote(md)))
	case md.RegistrationEndpoint != "":
		out = append(out, c.pass("dynamic client registration at "+md.RegistrationEndpoint))
	default:
		out = append(out, c.warn("neither CIMD nor dynamic registration offered", "agents will need pre-registered client ids; scout uses --client-id"))
	}

	d, err := s.Client.Discover(ctx, bearer)
	if err != nil {
		out = append(out, s.check("discovery.assemble", "Assemble discovery").fail(Major, err.Error(), ""))
		return out
	}
	s.Discovery = d
	return out
}

// firstPolicyError returns the first refusal among errs, or nil when they
// were all ordinary fetch failures.
func firstPolicyError(errs []error) *auth.PolicyError {
	for _, err := range errs {
		var pe *auth.PolicyError
		if errors.As(err, &pe) {
			return pe
		}
	}
	return nil
}

func dcrNote(md *auth.ServerMetadata) string {
	if md.RegistrationEndpoint != "" {
		return " (dynamic registration also available)"
	}
	return ""
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// phaseAuth obtains a token with the supplied credentials and then checks
// that the server actually validates tokens.
func phaseAuth(ctx context.Context, s *Session) []Finding {
	var out []Finding
	mode := s.Opts.Creds.Effective()

	// First, and before any early return: what the server exposes to
	// somebody with no credentials at all. It is a question about the
	// server rather than about what the operator supplied, and the case it
	// exists for — an open server, no credentials given — is exactly the
	// one the branch below returns early from.
	out = append(out, checkUnauthenticatedTools(ctx, s))

	if !s.RequiresAuth {
		if mode == creds.ModeNone {
			out = append(out, s.check("auth.mode", "Credentials").skip("open server, no credentials supplied"))
			return out
		}
		out = append(out, s.check("auth.mode", "Credentials").info("server is open; supplied credentials will still be sent ("+s.Opts.Creds.Describe()+")"))
	}
	out = append(out, s.check("auth.mode", "Credential mode").info(s.Opts.Creds.Describe()))
	for field, src := range s.Opts.Creds.Sources {
		out = append(out, s.check("auth.source."+field, "Source of "+field).info(src))
	}

	switch mode {
	case creds.ModeNone:
		out = append(out, s.check("auth.token", "Token acquisition").fail(Critical, "server requires authorization and no credentials were supplied",
			"supply what the server's operator gave you: a token with --token-env NAME; an API key with --header \"X-API-Key: …\"; an OAuth client with --auth client-credentials --client-id ID --client-secret-env NAME (add tenant or profile parameters with --param key=value and a scope with --scope); or a user login with `scout login "+s.Opts.Endpoint+"`"))
		s.blocked = "no credentials for a protected server"
		return out

	case creds.ModeBearer:
		s.Token = &TokenInfo{Type: "Bearer", Source: s.Opts.Creds.Sources["token"]}
		out = append(out, s.check("auth.token", "Pre-issued bearer token").info("using supplied token"))

	case creds.ModeClientCredentials:
		if s.Discovery == nil {
			out = append(out, s.check("auth.token", "Token acquisition").skip("discovery did not complete"))
			s.blocked = "cannot obtain a token without discovery"
			return out
		}
		c := s.check("auth.registration", "Client identity")
		reg, err := s.Client.Register(telemetry.WithPhase(ctx, "auth", "registration"), s.Discovery)
		if err != nil {
			out = append(out, c.fail(Critical, err.Error(), "supply --client-id/--client-secret or enable dynamic registration"))
			s.blocked = "no client identity"
			return out
		}
		out = append(out, c.pass(fmt.Sprintf("%s (client_id %s)", reg.Method, reg.ClientID)))

		c = s.check("auth.token", "Client-credentials token exchange")
		src, err := s.Client.ClientCredentialsSource(s.Discovery)
		if err != nil {
			out = append(out, c.fail(Critical, err.Error(), ""))
			s.blocked = "token exchange impossible"
			return out
		}
		t0 := time.Now()
		tok, err := src.Token(telemetry.WithPhase(ctx, "auth", "token"))
		if err != nil {
			var te *auth.TokenError
			advice := "check the client id, secret and extra parameters"
			if errors.As(err, &te) && te.Code == "invalid_target" {
				advice = "the server rejected the resource indicator; pass --resource with the value it expects"
			}
			out = append(out, c.fail(Critical, err.Error(), advice))
			s.blocked = "token exchange failed"
			return out
		}
		s.Client.SetTokenSource(src)
		s.Token = &TokenInfo{Type: tok.TokenType, Scope: tok.Scope, Requested: s.Discovery.Scope, Expiry: tok.Expiry, Refreshable: tok.RefreshToken != "", Source: "client_credentials"}
		out = append(out, c.pass(fmt.Sprintf("token issued in %s", ms(time.Since(t0)))))
		out = append(out, tokenShapeFindings(s, tok)...)

	case creds.ModeAuthorizationCode:
		c := s.check("auth.token", "Stored authorization-code token")
		var stored *creds.StoredToken
		if s.Opts.Store != nil {
			stored, _ = s.Opts.Store.Get(s.Opts.Endpoint)
		}
		if stored == nil {
			out = append(out, c.fail(Critical, "no stored token for this endpoint (--auth authorization-code reads ~/.config/scout/tokens.json)", "run `scout login "+s.Opts.Endpoint+"` once to authorize in the browser; scout stores the refresh token and later runs reuse it"))
			s.blocked = "no stored user token"
			return out
		}
		src := stored.Source(creds.HTTP{C: s.Client.HTTPClient()})
		tok, err := src.Token(telemetry.WithPhase(ctx, "auth", "refresh"))
		if err != nil {
			out = append(out, c.fail(Critical, "stored token unusable: "+err.Error(), "run `scout login` again"))
			s.blocked = "stored token unusable"
			return out
		}
		s.Client.SetTokenSource(src)
		s.Token = &TokenInfo{Type: tok.TokenType, Scope: tok.Scope, Expiry: tok.Expiry, Refreshable: tok.RefreshToken != "", Source: "store (" + stored.SavedAt.Format(time.RFC3339) + ")"}
		out = append(out, c.pass("token loaded from store; issuer "+stored.Issuer))
		out = append(out, tokenShapeFindings(s, tok)...)
	}

	if !s.RequiresAuth {
		return out
	}
	// Negative probes: a server that "requires" auth must reject bad tokens.
	c := s.check("auth.rejects_garbage", "Server rejects an invalid token")
	tr := s.Bare
	id := tr.NextID()
	raw, err := tr.Do(telemetry.WithPhase(ctx, "auth", "garbage token"), transport.RawOptions{
		Request: &transport.Request{JSONRPC: "2.0", ID: &id, Method: "initialize", Params: initParams(s)},
		Headers: map[string]string{"Authorization": "Bearer scout-invalid-token-" + s.TraceID[:8]}, OmitSession: true,
	})
	tr.Reset()
	switch {
	case err != nil:
		out = append(out, c.warn("request failed: "+err.Error(), ""))
	case raw.Status == http.StatusUnauthorized:
		if raw.Header.Get("WWW-Authenticate") == "" {
			out = append(out, c.warn("401 without WWW-Authenticate", "include the challenge on every 401"))
		} else {
			out = append(out, c.pass("401 with WWW-Authenticate"))
		}
	case raw.Status == http.StatusForbidden:
		out = append(out, c.warn("403 for an invalid token; 401 is expected", "return 401 for tokens that cannot be validated"))
	case raw.Status/100 == 2:
		out = append(out, c.fail(Critical, fmt.Sprintf("HTTP %d: the server accepted a made-up bearer token", raw.Status), "validate token signatures/introspection on every request"))
	default:
		out = append(out, c.warn(fmt.Sprintf("HTTP %d for an invalid token", raw.Status), "return 401"))
	}
	return out
}

func tokenShapeFindings(s *Session, tok *auth.Token) []Finding {
	var out []Finding
	c := s.check("auth.token.type", "Token type")
	if strings.EqualFold(tok.TokenType, "bearer") {
		out = append(out, c.pass("Bearer"))
	} else {
		out = append(out, c.warn(fmt.Sprintf("token_type %q", tok.TokenType), "return token_type=Bearer"))
	}
	c = s.check("auth.token.expiry", "Token lifetime")
	switch {
	case tok.Expiry.IsZero():
		out = append(out, c.warn("no expires_in: token lifetime unknown", "return expires_in so clients can refresh proactively"))
	case time.Until(tok.Expiry) < time.Minute:
		out = append(out, c.warn(fmt.Sprintf("expires in %s", time.Until(tok.Expiry).Round(time.Second)), "very short tokens force constant refreshes"))
	default:
		out = append(out, c.pass(fmt.Sprintf("expires in %s", time.Until(tok.Expiry).Round(time.Second))))
	}
	if s.Discovery != nil && s.Discovery.Scope != "" {
		c = s.check("auth.token.scope", "Granted scope covers requested scope")
		switch {
		case tok.Scope == "":
			out = append(out, c.info("token response carries no scope; requested "+s.Discovery.Scope))
		case scopeCovers(tok.Scope, s.Discovery.Scope):
			out = append(out, c.pass(tok.Scope))
		default:
			out = append(out, c.warn(fmt.Sprintf("granted %q, requested %q", tok.Scope, s.Discovery.Scope), "some tool calls may fail with insufficient_scope"))
		}
	}
	return out
}

func scopeCovers(granted, requested string) bool {
	have := map[string]bool{}
	for _, s := range strings.Fields(granted) {
		have[s] = true
	}
	for _, s := range strings.Fields(requested) {
		if !have[s] {
			return false
		}
	}
	return true
}

func initParams(s *Session) []byte {
	b, _ := jsonMarshal(map[string]any{
		"protocolVersion": scout.SupportedProtocolVersions[0],
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "scout", "version": s.Opts.Version},
	})
	return b
}

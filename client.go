// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package scout

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/sebastienrousseau/scout/auth"
	"github.com/sebastienrousseau/scout/trace"
	"github.com/sebastienrousseau/scout/transport"
)

// AuthMode selects how the client authorizes.
type AuthMode string

const (
	// AuthNone connects without credentials and fails on a 401.
	AuthNone AuthMode = "none"
	// AuthBearer sends a pre-issued token handed to the operator out of
	// band. No discovery or token exchange happens.
	AuthBearer AuthMode = "bearer"
	// AuthClientCredentials is the B2B flow: a confidential client
	// exchanges its own credentials for a token.
	AuthClientCredentials AuthMode = "client_credentials"
	// AuthAuthorizationCode is the B2C flow: an end user consents in a
	// browser and the client redeems the code with PKCE.
	AuthAuthorizationCode AuthMode = "authorization_code"
)

// Overrides pins authorization server endpoints when the server does not
// publish discovery metadata. Any field left empty is discovered.
type Overrides struct {
	AuthorizationURL string
	TokenURL         string
	Resource         string
}

// AuthConfig configures how the client authorizes.
type AuthConfig struct {
	Mode AuthMode
	// Token is the pre-issued bearer token for AuthBearer.
	Token string
	// Registration controls how a client identity is obtained.
	Registration auth.RegistrationOptions
	// RedirectURI is required for AuthAuthorizationCode.
	RedirectURI string
	// Scope requested on the first token request. When empty, the scope
	// from the WWW-Authenticate challenge (or PRM scopes_supported) is used.
	Scope string
	// Extra parameters sent on token requests (client credentials) or the
	// authorization request (authorization code). Use it for server
	// extensions such as a tenant profile identifier.
	Extra url.Values
	// TokenAuthMethod overrides the token endpoint auth method.
	TokenAuthMethod string
	// Overrides bypass discovery for the endpoints given.
	Overrides Overrides
	// StepUp, when set, is consulted on insufficient_scope; defaults to
	// re-requesting the token with the required scope.
	StepUp auth.StepUpFunc
}

// Config configures a Client.
type Config struct {
	// Endpoint is the MCP server URL (the Streamable HTTP endpoint).
	Endpoint string
	// HTTPClient supplies the base transport and timeouts. Its Transport
	// is wrapped with tracing, fixed headers and token handling.
	HTTPClient *http.Client
	// Headers are sent verbatim on every request to the MCP server and, once
	// discovery has validated it, the authorization server (API keys, tenant
	// selectors, basic auth). They are never sent to an origin neither the
	// operator nor a validated discovery document named.
	Headers    map[string]string
	ClientInfo Implementation
	Auth       AuthConfig
	// URLPolicy governs which discovered URLs may be fetched or credentialed.
	// The zero value is strict: https only, public hosts only.
	URLPolicy auth.URLPolicy
	// AllowResourceMismatch permits a protected-resource metadata document
	// whose "resource" does not match the endpoint. RFC 9728 requires the
	// client to check this binding; skipping it invites a token mix-up.
	AllowResourceMismatch bool
}

// Status of a Connect call.
type Status string

const (
	// StatusConnected means initialize succeeded.
	StatusConnected Status = "connected"
	// StatusAuthorizationRequired means the user must visit AuthorizationURL
	// and the caller must then call CompleteAuthorization.
	StatusAuthorizationRequired Status = "authorization_required"
)

// Discovery is what the authorization step learned about the server.
type Discovery struct {
	Challenge auth.Challenge
	PRM       *auth.ProtectedResourceMetadata
	PRMSource string // URL the PRM was fetched from ("" when overridden)
	Server    *auth.ServerMetadata
	// Registration is nil until Register has run.
	Registration *auth.Registration
	Resource     string
	Scope        string
	Overridden   bool
}

// Endpoint returns the token-layer view of the discovery.
func (d *Discovery) Endpoint(authMethod string) auth.Endpoint {
	return auth.Endpoint{AuthorizationURL: d.Server.AuthorizationEndpoint, TokenURL: d.Server.TokenEndpoint, AuthMethod: authMethod}
}

// ConnectResult describes the outcome of Connect.
type ConnectResult struct {
	Status           Status
	AuthorizationURL string // set when Status == StatusAuthorizationRequired
	State            string // OAuth state to verify on the redirect
	Initialize       *InitializeResult
	Discovery        *Discovery // populated when an auth flow ran
}

// Client is an MCP client bound to one server.
type Client struct {
	cfg  Config
	tr   *transport.Streamable
	atr  *auth.Transport
	http *http.Client // for discovery / token calls: traced + headers, no bearer
	disc *auth.Discoverer
	reg  *auth.Registrar

	// allowed is the set of origins that may receive credentials: the MCP
	// endpoint, plus any authorization server that survived validation.
	allowed *auth.OriginSet

	mu         sync.Mutex
	init       *InitializeResult
	pending    *auth.AuthorizationCodeFlow
	last       *ConnectResult
	negotiated *Negotiation
}

// New builds a Client. It does not contact the server.
// DefaultClientVersion is the version scout announces to a server when the
// caller sets no ClientInfo of its own.
//
// It is a fallback, not the build version: cmd sets the real one from the
// ldflags stamp. It said "0.1.0" from the first import, which was never a
// version scout had — and under this project's convention, where every
// release increments by 0.0.1, it is not one it will reach for a long time.
const DefaultClientVersion = "0.0.1"

func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("scout: Endpoint is required")
	}
	if _, err := auth.CanonicalResource(cfg.Endpoint); err != nil {
		return nil, err
	}
	if cfg.Auth.Mode == "" {
		cfg.Auth.Mode = AuthNone
	}
	switch cfg.Auth.Mode {
	case AuthNone, AuthBearer, AuthClientCredentials, AuthAuthorizationCode:
	default:
		return nil, fmt.Errorf("scout: unknown auth mode %q", cfg.Auth.Mode)
	}
	if cfg.Auth.Mode == AuthAuthorizationCode && cfg.Auth.RedirectURI == "" {
		return nil, errors.New("scout: RedirectURI is required for authorization_code")
	}
	if cfg.Auth.Mode == AuthBearer && cfg.Auth.Token == "" {
		return nil, errors.New("scout: Token is required for bearer")
	}
	if cfg.ClientInfo.Name == "" {
		cfg.ClientInfo = Implementation{Name: "scout", Version: DefaultClientVersion}
	}
	base := http.DefaultClient
	if cfg.HTTPClient != nil {
		base = cfg.HTTPClient
	}
	baseRT := base.Transport
	if baseRT == nil {
		baseRT = http.DefaultTransport
	}
	// Every credential this client holds is bound to this set. It starts as
	// the endpoint the operator named and grows only when discovery
	// produces an authorization server that passed URLPolicy.
	allowed, err := auth.NewOriginSet(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	var plainRT http.RoundTripper = trace.RoundTripper{Base: baseRT}
	if len(cfg.Headers) > 0 {
		plainRT = auth.NewHeaderTransport(plainRT, cfg.Headers, allowed)
	}
	plain := *base
	plain.Transport = plainRT
	plain.CheckRedirect = auth.CheckRedirect(allowed, base.CheckRedirect)

	atr := auth.NewTransport(plainRT, nil)
	atr.Allowed = allowed
	if cfg.Auth.Mode == AuthBearer {
		atr.SetSource(auth.StaticSource{AccessToken: cfg.Auth.Token})
	}
	authed := *base
	authed.Transport = atr
	authed.CheckRedirect = auth.CheckRedirect(allowed, base.CheckRedirect)

	c := &Client{
		cfg:     cfg,
		tr:      transport.New(cfg.Endpoint, &authed),
		atr:     atr,
		http:    &plain,
		disc:    &auth.Discoverer{Client: &plain, Policy: cfg.URLPolicy},
		reg:     &auth.Registrar{Client: &plain, Policy: cfg.URLPolicy},
		allowed: allowed,
	}
	atr.StepUp = c.stepUp
	return c, nil
}

// AllowedOrigins lists the origins this client may send credentials to.
func (c *Client) AllowedOrigins() []string { return c.allowed.Origins() }

// admit adds the origins of the authorization server endpoints to the
// credential allow-list. It runs only after URLPolicy accepted them.
func (c *Client) admit(md *auth.ServerMetadata) error {
	for _, raw := range []string{md.Issuer, md.TokenEndpoint, md.AuthorizationEndpoint, md.RegistrationEndpoint} {
		if raw == "" {
			continue
		}
		if err := c.allowed.Add(raw); err != nil {
			return err
		}
	}
	return nil
}

// Config returns the configuration the client was built with.
func (c *Client) Config() Config { return c.cfg }

// Transport exposes the underlying Streamable HTTP transport.
func (c *Client) Transport() *transport.Streamable { return c.tr }

// HTTPClient returns the client used for discovery and token requests: it
// carries tracing and fixed headers but no bearer token.
func (c *Client) HTTPClient() *http.Client { return c.http }

// Discoverer exposes the metadata fetcher.
func (c *Client) Discoverer() *auth.Discoverer { return c.disc }

// ServerInfo returns the initialize result, or nil before Connect.
func (c *Client) ServerInfo() *InitializeResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.init
}

// LastConnect returns the most recent ConnectResult.
func (c *Client) LastConnect() *ConnectResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// TokenSource returns the active token source, or nil when unauthenticated.
func (c *Client) TokenSource() auth.TokenSource { return c.atr.Source() }

// SetTokenSource installs a token source (for example one built by the
// caller from a stored refresh token).
func (c *Client) SetTokenSource(src auth.TokenSource) { c.atr.SetSource(src) }

// Connect runs the authorization state machine and the MCP handshake.
//
//	initialize ──200──▶ connected
//	     │401
//	     ▼
//	Discover (PRM: hint, then well-known → AS metadata) ─▶ Register
//	     │
//	     ├─ client_credentials ─▶ token ─▶ Initialize ─▶ connected
//	     └─ authorization_code ─▶ StatusAuthorizationRequired
//	                               (CompleteAuthorization finishes it)
//
// Each step is also exported so a caller can run them one at a time.
func (c *Client) Connect(ctx context.Context) (*ConnectResult, error) {
	ctx = trace.Ensure(ctx)
	res, err := c.Initialize(ctx)
	if err == nil {
		out := &ConnectResult{Status: StatusConnected, Initialize: res}
		c.setLast(out)
		return out, nil
	}
	challenge, ok := Unauthorized(err)
	if !ok {
		return nil, err
	}
	switch c.cfg.Auth.Mode {
	case AuthNone:
		return nil, fmt.Errorf("scout: server requires authorization but Auth.Mode is none: %w", err)
	case AuthBearer:
		return nil, fmt.Errorf("scout: server rejected the supplied bearer token: %w", err)
	}

	d, err := c.Discover(ctx, challenge)
	if err != nil {
		return nil, err
	}
	if _, err := c.Register(ctx, d); err != nil {
		return nil, err
	}
	out := &ConnectResult{Discovery: d}

	switch c.cfg.Auth.Mode {
	case AuthClientCredentials:
		src, err := c.ClientCredentialsSource(d)
		if err != nil {
			return nil, err
		}
		c.atr.SetSource(src)
		res, err := c.Initialize(ctx)
		if err != nil {
			return nil, fmt.Errorf("scout: initialize after token exchange: %w", err)
		}
		out.Status, out.Initialize = StatusConnected, res
		c.setLast(out)
		return out, nil

	case AuthAuthorizationCode:
		u, state, err := c.StartAuthorization(d)
		if err != nil {
			return nil, err
		}
		out.Status, out.AuthorizationURL, out.State = StatusAuthorizationRequired, u, state
		c.setLast(out)
		return out, nil
	}
	return nil, fmt.Errorf("scout: unknown auth mode %q", c.cfg.Auth.Mode)
}

// Discover resolves protected-resource and authorization-server metadata
// for the challenge, honouring Overrides. It does not register a client.
func (c *Client) Discover(ctx context.Context, challenge auth.Challenge) (*Discovery, error) {
	d := &Discovery{Challenge: challenge}
	ov := c.cfg.Auth.Overrides
	if ov.TokenURL != "" {
		d.Overridden = true
		d.Server = &auth.ServerMetadata{TokenEndpoint: ov.TokenURL, AuthorizationEndpoint: ov.AuthorizationURL, CodeChallengeMethodsSupported: []string{"S256"}}
		// Overrides come from the operator, not the server, so they are
		// admitted without the discovery policy. They still have to parse.
		if err := c.admit(d.Server); err != nil {
			return nil, err
		}
		d.Resource = ov.Resource
		if d.Resource == "" {
			d.Resource, _ = auth.CanonicalResource(c.cfg.Endpoint)
		}
		d.Scope = c.scope(challenge, nil)
		return d, nil
	}
	prm, src, err := c.disc.DiscoverPRM(ctx, c.cfg.Endpoint, challenge.Params["resource_metadata"])
	if err != nil {
		return nil, err
	}
	d.PRM, d.PRMSource = prm, src
	if err := c.checkResourceBinding(prm); err != nil {
		return nil, err
	}
	var derrs []error
	for _, issuer := range prm.AuthorizationServers {
		m, err := c.disc.DiscoverServer(ctx, issuer)
		if err != nil {
			derrs = append(derrs, err)
			continue
		}
		d.Server = m
		break
	}
	if d.Server == nil {
		return nil, fmt.Errorf("scout: no usable authorization server: %w", errors.Join(derrs...))
	}
	if ov.AuthorizationURL != "" {
		d.Server.AuthorizationEndpoint = ov.AuthorizationURL
	}
	if err := c.admit(d.Server); err != nil {
		return nil, err
	}
	d.Resource = ov.Resource
	if d.Resource == "" {
		d.Resource = prm.Resource
	}
	if d.Resource == "" {
		d.Resource, _ = auth.CanonicalResource(c.cfg.Endpoint)
	}
	d.Scope = c.scope(challenge, prm)
	return d, nil
}

// ErrResourceMismatch reports a protected-resource metadata document whose
// "resource" does not identify the endpoint it was fetched for. RFC 9728
// requires the client to verify this binding: without it, a resource can
// hand out metadata for somebody else's API and collect tokens minted for
// it.
var ErrResourceMismatch = errors.New("scout: protected resource metadata does not identify this endpoint")

func (c *Client) checkResourceBinding(prm *auth.ProtectedResourceMetadata) error {
	if c.cfg.AllowResourceMismatch || prm.Resource == "" {
		return nil
	}
	canon, err := auth.CanonicalResource(c.cfg.Endpoint)
	if err != nil {
		return err
	}
	got, err := auth.CanonicalResource(prm.Resource)
	if err != nil {
		return fmt.Errorf("%w: resource %q is not an absolute URL", ErrResourceMismatch, prm.Resource)
	}
	if strings.TrimSuffix(got, "/") != strings.TrimSuffix(canon, "/") {
		return fmt.Errorf("%w: it claims %q but this endpoint is %q", ErrResourceMismatch, got, canon)
	}
	return nil
}

// Register obtains a client identity for the discovered server and records
// it on d.
func (c *Client) Register(ctx context.Context, d *Discovery) (*auth.Registration, error) {
	reg, err := c.reg.Register(ctx, d.Server, c.registrationOptions())
	if err != nil {
		return nil, err
	}
	d.Registration = reg
	return reg, nil
}

// ClientCredentialsSource builds the B2B token source from a completed
// discovery. It does not fetch a token until first use.
func (c *Client) ClientCredentialsSource(d *Discovery) (*auth.ClientCredentialsSource, error) {
	if d.Registration == nil {
		return nil, errors.New("scout: Register before building a token source")
	}
	if len(d.Server.GrantTypesSupported) > 0 && !slices.Contains(d.Server.GrantTypesSupported, "client_credentials") {
		return nil, fmt.Errorf("scout: authorization server %s does not advertise client_credentials", d.Server.Issuer)
	}
	return &auth.ClientCredentialsSource{
		HTTP: c.http, Endpoint: d.Endpoint(c.cfg.Auth.TokenAuthMethod),
		Creds:    auth.Credentials{ClientID: d.Registration.ClientID, ClientSecret: d.Registration.ClientSecret},
		Resource: d.Resource, Scope: d.Scope, Extra: c.cfg.Auth.Extra,
	}, nil
}

// StartAuthorization begins the authorization-code flow and returns the URL
// the user must visit plus the state to verify on redirect.
func (c *Client) StartAuthorization(d *Discovery) (string, string, error) {
	if d.Registration == nil {
		return "", "", errors.New("scout: Register before starting authorization")
	}
	if len(d.Server.CodeChallengeMethodsSupported) > 0 && !slices.Contains(d.Server.CodeChallengeMethodsSupported, "S256") {
		return "", "", fmt.Errorf("scout: authorization server %s does not support PKCE S256", d.Server.Issuer)
	}
	flow := &auth.AuthorizationCodeFlow{
		HTTP: c.http, Endpoint: d.Endpoint(c.cfg.Auth.TokenAuthMethod),
		Creds:    auth.Credentials{ClientID: d.Registration.ClientID, ClientSecret: d.Registration.ClientSecret},
		Resource: d.Resource, RedirectURI: c.cfg.Auth.RedirectURI, Scope: d.Scope, Extra: c.cfg.Auth.Extra,
	}
	u, err := flow.Start()
	if err != nil {
		return "", "", err
	}
	c.mu.Lock()
	c.pending = flow
	c.mu.Unlock()
	return u, flow.State(), nil
}

// CompleteAuthorization finishes the authorization-code flow with the code
// and state received on the redirect URI, then runs Initialize.
//
// It is shorthand for CompleteAuthorizationFrom with no issuer, and is kept
// for callers whose redirect handler does not surface the iss parameter.
func (c *Client) CompleteAuthorization(ctx context.Context, code, state string) (*ConnectResult, error) {
	return c.CompleteAuthorizationFrom(ctx, code, state, "")
}

// CompleteAuthorizationFrom finishes the authorization-code flow with the
// code, state and iss received on the redirect URI, then runs Initialize.
//
// iss is the RFC 9207 issuer identifier. When the authorization server
// advertised authorization_response_iss_parameter_supported, or simply sent
// one, it must match the issuer the code was requested from: that is what
// stops a mix-up attack where a malicious authorization server relays a
// code minted by an honest one.
func (c *Client) CompleteAuthorizationFrom(ctx context.Context, code, state, iss string) (*ConnectResult, error) {
	ctx = trace.Ensure(ctx)
	c.mu.Lock()
	flow := c.pending
	last := c.last
	c.mu.Unlock()
	if flow == nil {
		return nil, errors.New("scout: no authorization in progress; call Connect first")
	}
	if err := c.checkIssuer(last, iss); err != nil {
		return nil, err
	}
	src, err := flow.Complete(ctx, code, state)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.pending = nil
	c.mu.Unlock()
	c.atr.SetSource(src)
	res, err := c.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("scout: initialize after authorization: %w", err)
	}
	out := &ConnectResult{Status: StatusConnected, Initialize: res}
	if last != nil {
		out.Discovery = last.Discovery
	}
	c.setLast(out)
	return out, nil
}

// ErrIssuerMismatch reports an authorization response whose iss parameter
// names a different authorization server than the one the request went to.
var ErrIssuerMismatch = errors.New("scout: authorization response came from the wrong issuer")

func (c *Client) checkIssuer(last *ConnectResult, iss string) error {
	if last == nil || last.Discovery == nil || last.Discovery.Server == nil {
		return nil
	}
	want := last.Discovery.Server.Issuer
	switch {
	case want == "":
		return nil
	case iss == "":
		if last.Discovery.Server.AuthorizationResponseIssParameterSupported {
			return fmt.Errorf("%w: %s advertises RFC 9207 but the redirect carried no iss", ErrIssuerMismatch, want)
		}
		return nil
	case strings.TrimSuffix(iss, "/") != strings.TrimSuffix(want, "/"):
		return fmt.Errorf("%w: redirect says %q, the code was requested from %q", ErrIssuerMismatch, iss, want)
	}
	return nil
}

// Resume connects using a previously obtained token source (for example a
// stored refresh token) without re-running discovery.
func (c *Client) Resume(ctx context.Context, src auth.TokenSource) (*ConnectResult, error) {
	c.atr.SetSource(src)
	return c.Connect(ctx)
}

func (c *Client) stepUp(ctx context.Context, required string) (auth.TokenSource, error) {
	if c.cfg.Auth.StepUp != nil {
		return c.cfg.Auth.StepUp(ctx, required)
	}
	src := c.atr.Source()
	if src == nil {
		return nil, errors.New("scout: insufficient_scope with no token source")
	}
	return src.WithScope(required), nil
}

func (c *Client) scope(challenge auth.Challenge, prm *auth.ProtectedResourceMetadata) string {
	if c.cfg.Auth.Scope != "" {
		return c.cfg.Auth.Scope
	}
	if s := challenge.Params["scope"]; s != "" {
		return s
	}
	if prm != nil && len(prm.ScopesSupported) > 0 {
		return strings.Join(prm.ScopesSupported, " ")
	}
	return ""
}

func (c *Client) registrationOptions() auth.RegistrationOptions {
	o := c.cfg.Auth.Registration
	if o.Metadata.ClientName == "" {
		o.Metadata.ClientName = c.cfg.ClientInfo.Name
	}
	if o.Metadata.SoftwareVersion == "" {
		o.Metadata.SoftwareVersion = c.cfg.ClientInfo.Version
	}
	switch c.cfg.Auth.Mode {
	case AuthClientCredentials:
		if len(o.Metadata.GrantTypes) == 0 {
			o.Metadata.GrantTypes = []string{"client_credentials"}
		}
		if o.Metadata.TokenEndpointAuthMethod == "" {
			o.Metadata.TokenEndpointAuthMethod = "client_secret_basic"
		}
	case AuthAuthorizationCode:
		if len(o.Metadata.GrantTypes) == 0 {
			o.Metadata.GrantTypes = []string{"authorization_code", "refresh_token"}
		}
		if len(o.Metadata.ResponseTypes) == 0 {
			o.Metadata.ResponseTypes = []string{"code"}
		}
		if len(o.Metadata.RedirectURIs) == 0 {
			o.Metadata.RedirectURIs = []string{c.cfg.Auth.RedirectURI}
		}
		if o.Metadata.TokenEndpointAuthMethod == "" {
			o.Metadata.TokenEndpointAuthMethod = "none"
		}
	}
	return o
}

func (c *Client) setLast(r *ConnectResult) {
	c.mu.Lock()
	c.last = r
	if r.Initialize != nil {
		c.init = r.Initialize
	}
	c.mu.Unlock()
}

// Initialize starts a fresh session: it clears any session state, sends
// initialize, records the negotiated protocol version, and sends the
// initialized notification.
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	c.tr.Reset()
	return c.initialize(trace.Ensure(ctx))
}

func (c *Client) initialize(ctx context.Context) (*InitializeResult, error) {
	params := initializeParams{
		ProtocolVersion: SupportedProtocolVersions[0],
		Capabilities:    ClientCapabilities{},
		ClientInfo:      c.cfg.ClientInfo,
	}
	var res InitializeResult
	if err := c.tr.Call(ctx, "initialize", params, &res); err != nil {
		return nil, err
	}
	if !slices.Contains(SupportedProtocolVersions, res.ProtocolVersion) {
		return nil, fmt.Errorf("scout: server negotiated unsupported protocol version %q", res.ProtocolVersion)
	}
	c.tr.SetProtocolVersion(res.ProtocolVersion)
	if err := c.tr.Notify(ctx, "notifications/initialized", nil); err != nil {
		return nil, fmt.Errorf("scout: initialized notification: %w", err)
	}
	c.mu.Lock()
	c.init = &res
	c.mu.Unlock()
	return &res, nil
}

// call wraps transport.Call with one automatic re-initialize on session
// expiry.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	err := c.tr.Call(ctx, method, params, result)
	if errors.Is(err, transport.ErrSessionExpired) {
		if _, ierr := c.initialize(ctx); ierr != nil {
			return fmt.Errorf("scout: re-initialize after session expiry: %w", ierr)
		}
		err = c.tr.Call(ctx, method, params, result)
	}
	return err
}

// Call sends an arbitrary JSON-RPC request on the current session.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	return c.call(trace.Ensure(ctx), method, params, result)
}

// ListTools returns every tool, following pagination cursors.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	ctx = trace.Ensure(ctx)
	var all []Tool
	cursor := ""
	for {
		var page listToolsResult
		if err := c.call(ctx, "tools/list", listToolsParams{Cursor: cursor}, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Tools...)
		if page.NextCursor == "" || page.NextCursor == cursor {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

// CallTool invokes a tool. A tool-level failure is reported through
// CallToolResult.IsError, not as an error.
func (c *Client) CallTool(ctx context.Context, name string, args any) (*CallToolResult, error) {
	ctx = trace.Ensure(ctx)
	var res CallToolResult
	if err := c.call(ctx, "tools/call", callToolParams{Name: name, Arguments: args}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Unauthorized extracts the Bearer challenge from a 401 transport error.
func Unauthorized(err error) (auth.Challenge, bool) {
	var he *transport.HTTPStatusError
	if !errors.As(err, &he) || he.StatusCode != http.StatusUnauthorized {
		return auth.Challenge{}, false
	}
	ch, ok := auth.FindBearer(auth.ParseWWWAuthenticate(he.Header.Get("WWW-Authenticate")))
	if !ok {
		// A 401 with no usable challenge still means "authorize"; the
		// well-known fallback handles discovery.
		return auth.Challenge{Scheme: "Bearer", Params: map[string]string{}}, true
	}
	return ch, true
}

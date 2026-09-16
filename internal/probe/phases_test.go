// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sebastienrousseau/scout/auth"
	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/telemetry"
)

func ccCreds() *creds.Credentials {
	return &creds.Credentials{Mode: creds.ModeClientCredentials, ClientID: "static-id", ClientSecret: "static-secret-value"}
}

func run(t *testing.T, f *fakeServer, cr *creds.Credentials, mut func(*Options)) (*Session, map[string]Finding) {
	t.Helper()
	o := Options{Endpoint: f.srv.URL + "/mcp", Creds: cr, Recorder: telemetry.New(), HTTPClient: f.srv.Client(), Version: "t", RPS: -1, Samples: 2, Concurrency: 2}
	if mut != nil {
		mut(&o)
	}
	s, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return s, findingsByID(s)
}

func expect(t *testing.T, fs map[string]Finding, id string, st Status, contains string) {
	t.Helper()
	f, ok := fs[id]
	if !ok {
		t.Errorf("%s: missing", id)
		return
	}
	if f.Status != st || !strings.Contains(f.Detail, contains) {
		t.Errorf("%s: got %s %q, want %s containing %q", id, f.Status, f.Detail, st, contains)
	}
}

func TestRunValidation(t *testing.T) {
	if _, err := Run(context.Background(), Options{Endpoint: "/relative"}); err == nil {
		t.Error("relative endpoint must fail")
	}
	if _, err := Run(context.Background(), Options{Endpoint: "https://x/mcp", Creds: &creds.Credentials{Mode: creds.ModeAuthorizationCode}, Only: []string{"none-such"}}); err != nil {
		t.Errorf("no phases selected: %v", err)
	}
	if worst([]Finding{{Status: Info}}) != Pass || worst(nil) != Skip || worst([]Finding{{Status: Skip}, {Status: Warn}}) != Warn {
		t.Error("worst ordering")
	}
	if truncate("  abc  ", 2) != "ab…" || truncate("abc", 5) != "abc" {
		t.Error("truncate")
	}
	if !isLoopback("localhost") || isLoopback("example.com") {
		t.Error("isLoopback")
	}
	if dcrNote(&auth.ServerMetadata{}) != "" || dcrNote(&auth.ServerMetadata{RegistrationEndpoint: "x"}) == "" {
		t.Error("dcrNote")
	}
}

func TestNetPhaseFailures(t *testing.T) {
	rec := telemetry.New()
	s, err := Run(context.Background(), Options{Endpoint: "https://nonexistent.invalid/mcp", Recorder: rec, Only: []string{"net", "handshake"}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findingsByID(s)
	expect(t, fs, "net.scheme", Pass, "https")
	expect(t, fs, "net.dns", Fail, "lookup failed")
	if s.Results[1].Status != Skip || !strings.Contains(s.Results[1].Skipped, "does not resolve") {
		t.Errorf("later phase not skipped: %+v", s.Results[1])
	}
	s, _ = Run(context.Background(), Options{Endpoint: "http://127.0.0.1:1/mcp", Recorder: telemetry.New(), Only: []string{"net"}})
	fs = findingsByID(s)
	expect(t, fs, "net.scheme", Info, "loopback")
	expect(t, fs, "net.tcp", Fail, "connect failed")
	// Plain http to a public host is critical (no connection is attempted after DNS on a bogus host).
	s, _ = Run(context.Background(), Options{Endpoint: "http://nonexistent.invalid/mcp", Recorder: telemetry.New(), Only: []string{"net"}})
	expect(t, findingsByID(s), "net.scheme", Fail, "clear text")
	// A TLS server whose certificate the system does not trust fails the handshake honestly.
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tls.Close()
	s, _ = Run(context.Background(), Options{Endpoint: tls.URL + "/mcp", Recorder: telemetry.New(), Only: []string{"net", "discovery"}})
	fs = findingsByID(s)
	expect(t, fs, "net.tcp", Pass, "connected")
	expect(t, fs, "net.tls", Fail, "handshake failed")
	if s.blocked == "" {
		t.Error("TLS failure must block")
	}
}

func TestDiscoveryBranches(t *testing.T) {
	// 403 on first contact, then no Bearer scheme in the challenge.
	f := newFakeServer(t)
	f.q.firstContactStatus = 403
	_, fs := run(t, f, ccCreds(), nil)
	expect(t, fs, "discovery.first_contact", Warn, "403")
	expect(t, fs, "discovery.challenge", Warn, "no WWW-Authenticate")
	expect(t, fs, "discovery.prm", Pass, "found")
	expect(t, fs, "handshake.initialize", Pass, "")

	f = newFakeServer(t)
	f.q.firstContactStatus = 500
	s, fs := run(t, f, ccCreds(), nil)
	expect(t, fs, "discovery.first_contact", Fail, "unexpected HTTP 500")
	if s.blocked == "" {
		t.Error("must block")
	}

	f = newFakeServer(t)
	f.q.challenge = `Basic realm="x"`
	_, fs = run(t, f, ccCreds(), nil)
	expect(t, fs, "discovery.challenge", Warn, "no Bearer scheme")

	f = newFakeServer(t)
	f.q.challenge = `Bearer realm="x"`
	_, fs = run(t, f, ccCreds(), nil)
	expect(t, fs, "discovery.challenge", Warn, "lacks resource_metadata")
	expect(t, fs, "discovery.prm", Pass, "found")

	f = newFakeServer(t)
	f.q.prmEmptyServers = true
	s, fs = run(t, f, ccCreds(), nil)
	expect(t, fs, "discovery.prm", Fail, "no authorization_servers")
	if s.blocked == "" {
		t.Error("must block")
	}

	f = newFakeServer(t)
	f.q.prmMissing = true
	f.noChallenge = true
	_, fs = run(t, f, ccCreds(), nil)
	expect(t, fs, "discovery.prm", Fail, "not found")

	f = newFakeServer(t)
	f.q.prmResource = "https://other.example/mcp"
	f.q.asMissing = true
	s, fs = run(t, f, ccCreds(), nil)
	// A resource that names another endpoint is now refused, not tolerated:
	// that binding is what stops a token mix-up.
	expect(t, fs, "discovery.prm.resource", Fail, "differs")
	expect(t, fs, "discovery.as", Fail, "no authorization server published metadata")
	if s.blocked == "" {
		t.Error("must block")
	}

	f = newFakeServer(t)
	f.q.prmResource = " "
	f.q.asNoPKCE = true
	f.q.asNoRegistration = true
	_, fs = run(t, f, &creds.Credentials{Mode: creds.ModeClientCredentials, ClientID: "static-id", ClientSecret: "s"}, nil)
	expect(t, fs, "discovery.as.pkce", Warn, "absent")
	expect(t, fs, "discovery.registration", Warn, "neither")

	f = newFakeServer(t)
	f.q.asPKCE = []string{"plain"}
	f.q.asCIMD = true
	_, fs = run(t, f, &creds.Credentials{Mode: creds.ModeClientCredentials, ClientMetadataURL: "https://c/md"}, nil)
	expect(t, fs, "discovery.as.pkce", Fail, "S256 not supported")
	expect(t, fs, "discovery.registration", Pass, "metadata documents")
	expect(t, fs, "auth.registration", Pass, "cimd")

	// --token-url override bypasses discovery.
	f = newFakeServer(t)
	cr := ccCreds()
	cr.TokenURL = f.srv.URL + "/as/token"
	_, fs = run(t, f, cr, nil)
	expect(t, fs, "discovery.override", Info, "token endpoint")
	expect(t, fs, "auth.token", Pass, "token issued")
	cr = ccCreds()
	cr.TokenURL = "http://127.0.0.1:1/token"
	s, fs = run(t, f, cr, nil)
	expect(t, fs, "auth.token", Fail, "")
	if s.blocked == "" {
		t.Error("must block on token failure")
	}

	// Bearer mode: informational discovery, both outcomes.
	f = newFakeServer(t)
	f.acceptAnyToken = true
	_, fs = run(t, f, &creds.Credentials{Mode: creds.ModeBearer, Token: "operator-token"}, func(o *Options) { o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "discovery.prm", Pass, "authorization server")
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.prmMissing = true
	f.noChallenge = true
	_, fs = run(t, f, &creds.Credentials{Mode: creds.ModeBearer, Token: "operator-token"}, func(o *Options) { o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "discovery.prm", Warn, "not discoverable")
	expect(t, fs, "auth.rejects_garbage", Fail, "made-up")
}

func TestAuthBranches(t *testing.T) {
	// Garbage token answered with 403, then 401 without header.
	f := newFakeServer(t)
	f.q.garbageStatus = 403
	_, fs := run(t, f, ccCreds(), func(o *Options) { o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "auth.rejects_garbage", Warn, "403")
	f = newFakeServer(t)
	f.q.garbageNoHeader = true
	_, fs = run(t, f, ccCreds(), func(o *Options) { o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "auth.rejects_garbage", Warn, "without WWW-Authenticate")
	f = newFakeServer(t)
	f.q.garbageStatus = 500
	_, fs = run(t, f, ccCreds(), func(o *Options) { o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "auth.rejects_garbage", Warn, "HTTP 500")

	// Token shape findings.
	f = newFakeServer(t)
	f.q.tokenNoExpiry = true
	f.q.tokenType = "mac"
	f.q.tokenScope = "mcp:other"
	_, fs = run(t, f, ccCreds(), func(o *Options) { o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "auth.token.type", Warn, "mac")
	expect(t, fs, "auth.token.expiry", Warn, "unknown")
	expect(t, fs, "auth.token.scope", Warn, "granted")
	f = newFakeServer(t)
	f.q.tokenShortExpiry = true
	f.q.tokenScope = " "
	_, fs = run(t, f, ccCreds(), func(o *Options) { o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "auth.token.expiry", Warn, "expires in")

	// Open server with credentials supplied.
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.noChallenge = true
	orig := f.srv.Config.Handler
	f.srv.Config.Handler = httpHandlerFunc(func(w httpResponseWriter, r httpRequest) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer anything")
		}
		orig.ServeHTTP(w, r)
	})
	_, fs = run(t, f, &creds.Credentials{Mode: creds.ModeBearer, Token: "tok-1234"}, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake"} })
	expect(t, fs, "discovery.creds_unused", Warn, "did not demand")
	expect(t, fs, "auth.mode", Info, "")
	if _, ok := fs["auth.rejects_garbage"]; ok {
		t.Error("garbage probe must not run against an open server")
	}

	// Authorization-code mode from the store: missing, refreshable, and dead.
	f = newFakeServer(t)
	store := &creds.Store{Path: filepath.Join(t.TempDir(), "tokens.json")}
	ac := &creds.Credentials{Mode: creds.ModeAuthorizationCode}
	s, fs := run(t, f, ac, func(o *Options) { o.Store = store; o.Only = []string{"net", "discovery", "auth", "handshake"} })
	expect(t, fs, "auth.token", Fail, "no stored token")
	if s.blocked == "" {
		t.Error("must block")
	}
	if err := store.Put(creds.StoredToken{Endpoint: f.srv.URL + "/mcp", AccessToken: "stale", RefreshToken: "rt-ok", TokenURL: f.srv.URL + "/as/token", ClientID: "static-id", ClientSecret: "s", Issuer: f.srv.URL + "/as", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	_, fs = run(t, f, ac, func(o *Options) { o.Store = store })
	expect(t, fs, "auth.token", Pass, "loaded from store")
	expect(t, fs, "handshake.initialize", Pass, "")
	expect(t, fs, "resilience.token_refresh", Pass, "")
	if err := store.Put(creds.StoredToken{Endpoint: f.srv.URL + "/mcp", AccessToken: "stale", RefreshToken: "dead", TokenURL: f.srv.URL + "/as/token", ClientID: "static-id", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	_, fs = run(t, f, ac, func(o *Options) { o.Store = store; o.Only = []string{"net", "discovery", "auth"} })
	expect(t, fs, "auth.token", Fail, "unusable")

	// Credentials rejected at initialize.
	f = newFakeServer(t)
	f.q.rejectCredentials = true
	s, fs = run(t, f, &creds.Credentials{Mode: creds.ModeBearer, Token: "tok-1234"}, nil)
	expect(t, fs, "handshake.initialize", Fail, "answered 401")
	if !strings.Contains(s.blocked, "rejected") {
		t.Errorf("blocked = %q", s.blocked)
	}
}

func TestHandshakeAndProtocolBranches(t *testing.T) {
	f := newFakeServer(t)
	f.acceptAnyToken = true
	f.q.protocolVersion = "1999-01-01"
	s, fs := run(t, f, &creds.Credentials{Mode: creds.ModeBearer, Token: "tok-1234"}, nil)
	expect(t, fs, "handshake.initialize", Fail, "unsupported protocol")
	if s.blocked == "" {
		t.Error("must block")
	}
	bearer := &creds.Credentials{Mode: creds.ModeBearer, Token: "tok-1234"}
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.protocolVersion = "2025-06-18"
	f.q.serverName = "x"
	f.q.noCapabilities = true
	f.q.noInstructions = true
	f.q.stateless = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "resilience"} })
	expect(t, fs, "handshake.protocol_version", Info, "2025-06-18")
	expect(t, fs, "handshake.server_info", Warn, "version is empty")
	expect(t, fs, "handshake.capabilities", Warn, "no capabilities")
	expect(t, fs, "handshake.instructions", Info, "none")
	expect(t, fs, "handshake.session", Info, "stateless")
	expect(t, fs, "resilience.session_reinit", Skip, "stateless")
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.serverVersion = "1"
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake"} })
	expect(t, fs, "handshake.server_info", Fail, "name is empty")

	// Lenient / wrong protocol behaviour.
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.unknownMethodCode = -1
	f.q.wrongID = true
	f.q.malformedOK = true
	f.q.invalidParamsOK = true
	f.q.unknownToolOK = true
	f.q.lenientAccept = true
	f.q.getStream = true
	f.q.bogusSessionOK = true
	f.q.lenientVersion = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "protocol", "resilience"} })
	expect(t, fs, "protocol.unknown_method", Warn, "-32601 expected")
	expect(t, fs, "protocol.id_echo", Fail, "sent id")
	expect(t, fs, "protocol.malformed_json", Fail, "truncated body")
	expect(t, fs, "protocol.invalid_params", Fail, "succeeded")
	expect(t, fs, "protocol.unknown_tool", Fail, "returned success")
	expect(t, fs, "protocol.accept_header", Info, "lenient")
	expect(t, fs, "protocol.get_stream", Pass, "event-stream")
	expect(t, fs, "protocol.bogus_session", Warn, "never issued")
	expect(t, fs, "protocol.version_header", Info, "accepted")
	expect(t, fs, "resilience.session_reinit", Warn, "accepted the bogus session")

	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.unknownMethodHTTP = 400
	f.q.pingFail = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "protocol", "resilience"} })
	expect(t, fs, "protocol.unknown_method", Warn, "HTTP 400")
	expect(t, fs, "protocol.ping", Fail, "")
	expect(t, fs, "resilience.session_reinit", Fail, "recovery failed")
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.unknownMethodOK = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "protocol"} })
	expect(t, fs, "protocol.unknown_method", Fail, "no error")
}

func TestCatalogBranches(t *testing.T) {
	bearer := &creds.Credentials{Mode: creds.ModeBearer, Token: "tok-1234"}
	f := newFakeServer(t)
	f.acceptAnyToken = true
	f.q.catalog = "dupes"
	_, fs := run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "catalog"} })
	expect(t, fs, "catalog.tools.unique", Fail, "get_time")

	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.catalog = "bad"
	f.q.catalog = "bad"
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution"} })
	expect(t, fs, "catalog.tools.descriptions", Fail, "missing")
	expect(t, fs, "catalog.tools.input_schema", Fail, "nodesc")
	expect(t, fs, "catalog.tools.annotations", Pass, "")
	expect(t, fs, "catalog.tools.output_schema", Info, "without outputSchema")
	if _, ok := fs["catalog.tools.title"]; !ok {
		t.Error("title finding expected")
	}
	// brokenschema is mutating-but-not-destructive; with AllowMutations it is attempted and its schema is unparsable.
	found := false
	for _, r := range fsSession(t, f, bearer).ToolResults {
		if r.Name == "brokenschema" && strings.Contains(r.SkipReason, "unparsable") {
			found = true
		}
	}
	if !found {
		t.Error("unparsable inputSchema should be a skip reason")
	}

	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.catalog = "empty"
	f.q.resourcesFail = true
	f.q.templatesFail = true
	_, fs = run(t, f, bearer, func(o *Options) {
		o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution", "performance"}
	})
	expect(t, fs, "catalog.tools.list", Pass, "0 tools")
	expect(t, fs, "catalog.resources.list", Fail, "listing failed")
	expect(t, fs, "execution.tools", Skip, "no tools")
	expect(t, fs, "performance.tools", Skip, "no tool completed")

	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.catalog = "nocap"
	f.q.noCapabilities = true
	f.q.toolsFail = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "catalog"} })
	expect(t, fs, "catalog.tools.list", Info, "not supported")
	expect(t, fs, "catalog.resources.list", Info, "not supported")
	expect(t, fs, "catalog.prompts.list", Info, "not supported")
	expect(t, fs, "catalog.empty", Fail, "no tools")

	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.noCapabilities = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "catalog"} })
	expect(t, fs, "catalog.tools.list", Warn, "not declared")
	expect(t, fs, "catalog.resources.list", Warn, "without the capability")
	expect(t, fs, "catalog.prompts.list", Warn, "without the capability")

	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.catalog = "relative"
	f.q.templatesFail = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "catalog"} })
	expect(t, fs, "catalog.resources.uris", Fail, "doc/1")
	expect(t, fs, "catalog.resources.mime", Info, "without mimeType")
	expect(t, fs, "catalog.resources.templates", Info, "not supported")
	expect(t, fs, "catalog.prompts.descriptions", Warn, "undescribed")

	if !schemaIsObject([]byte(`{"type":["null","object"]}`)) || schemaIsObject([]byte(`{"type":["string"]}`)) || schemaIsObject([]byte(`{bad`)) || schemaIsObject([]byte(`{"type":5}`)) {
		t.Error("schemaIsObject")
	}
	if !strings.Contains(list([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}), "+1 more") {
		t.Error("list truncation")
	}
}

func fsSession(t *testing.T, f *fakeServer, cr *creds.Credentials) *Session {
	t.Helper()
	s, _ := run(t, f, cr, func(o *Options) {
		o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution"}
		o.Policy = diagnostics.Policy{AllowMutations: true}
	})
	return s
}

func TestExecutionBranches(t *testing.T) {
	bearer := &creds.Credentials{Mode: creds.ModeBearer, Token: "tok-1234"}
	f := newFakeServer(t)
	f.acceptAnyToken = true
	f.q.allToolsError = true
	f.q.resourcesEmpty = true
	f.q.promptsEmpty = true
	_, fs := run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution"} })
	expect(t, fs, "execution.tools", Warn, "every call returned isError")
	expect(t, fs, "execution.resources", Warn, "empty")
	expect(t, fs, "execution.prompts", Warn, "empty")

	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.slowTool = 300 * time.Millisecond
	f.q.readFail = true
	f.q.promptsFail = true
	_, fs = run(t, f, bearer, func(o *Options) {
		o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution"}
		o.CallTimeout = 50 * time.Millisecond
		o.Policy = diagnostics.Policy{Only: []string{"get_time"}}
	})
	expect(t, fs, "execution.tools", Fail, "protocol errors")
	expect(t, fs, "execution.resources", Fail, "failed")
	expect(t, fs, "execution.prompts", Fail, "failed")
	expect(t, fs, "execution.policy", Info, "only get_time")

	f = newFakeServer(t)
	f.acceptAnyToken = true
	s, fs := run(t, f, bearer, func(o *Options) {
		o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution"}
		o.Policy = diagnostics.Policy{Deny: []string{"get_time", "search", "lax"}, AllowDestructive: false}
	})
	expect(t, fs, "execution.tools", Warn, "0 of 4")
	_ = s
	if !strings.Contains(policyDescribe(diagnostics.Policy{AllowDestructive: true, Deny: []string{"x"}}), "ALL tools") {
		t.Error("policyDescribe destructive")
	}
	if !strings.Contains(policyDescribe(diagnostics.Policy{AllowMutations: true}), "mutations") {
		t.Error("policyDescribe mutations")
	}
}

func TestPerformanceAndResilienceBranches(t *testing.T) {
	bearer := &creds.Credentials{Mode: creds.ModeBearer, Token: "tok-1234"}
	// Rate limiting with Retry-After, throttled burst note, and cold/warm info.
	f := newFakeServer(t)
	f.acceptAnyToken = true
	f.rateLimitAfter = 12
	_, fs := run(t, f, bearer, func(o *Options) { o.RPS = 50; o.Samples = 2; o.Concurrency = 4 })
	if v, ok := fs["performance.concurrency"]; !ok || !strings.Contains(v.Detail, "rate-limited") {
		t.Errorf("concurrency = %+v", v)
	}
	expect(t, fs, "performance.throttle", Info, "capped")
	// 429 without Retry-After under --allow-load; and unthrottled with no 429 → warning.
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.rateLimitAfter = 12
	f.q.noRetryAfter = true
	_, fs = run(t, f, bearer, func(o *Options) { o.AllowLoad = true; o.Concurrency = 4 })
	if v := fs["performance.concurrency"]; v.Status != Warn && !strings.Contains(v.Detail, "429") {
		t.Errorf("no retry-after: %+v", v)
	}
	f = newFakeServer(t)
	f.acceptAnyToken = true
	_, fs = run(t, f, bearer, func(o *Options) { o.AllowLoad = true; o.Concurrency = 2 })
	expect(t, fs, "performance.rate_limit", Warn, "no 429")
	// Errors under concurrency.
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.burstFail = true
	f.q.burstFailAfter = 6
	_, fs = run(t, f, bearer, func(o *Options) { o.Concurrency = 3; o.Samples = 3 })
	if v := fs["performance.concurrency"]; v.Status != Fail || !strings.Contains(v.Detail, "errors") {
		t.Errorf("burst errors: %+v", v)
	}
	// Ping failures and slow tools.
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.pingFail = true
	_, fs = run(t, f, bearer, func(o *Options) {
		o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution", "performance"}
		o.Concurrency = 0
	})
	expect(t, fs, "performance.ping", Fail, "every ping failed")
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.slowTool = 60 * time.Millisecond
	_, fs = run(t, f, bearer, func(o *Options) {
		o.Only = []string{"net", "discovery", "auth", "handshake", "catalog", "execution", "performance"}
		o.Policy = diagnostics.Policy{Only: []string{"get_time"}}
		o.Samples = 1
		o.Concurrency = 0
	})
	expect(t, fs, "performance.tools", Pass, "1 tools")
	// Resilience: recovery failure when the server starts rejecting everything after the session probe.
	f = newFakeServer(t)
	f.acceptAnyToken = true
	_, fs = run(t, f, bearer, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "resilience"} })
	expect(t, fs, "resilience.session_reinit", Pass, "re-initialized")
	f = newFakeServer(t)
	f.acceptAnyToken = true
	f.q.stateless = true
	// Not protected: open server + bearer → token_refresh only runs when auth is required.
	_, fs = run(t, f, &creds.Credentials{Mode: creds.ModeNone}, func(o *Options) { o.Only = []string{"net", "discovery", "auth", "handshake", "resilience"} })
	if _, ok := fs["resilience.token_refresh"]; ok && fs["resilience.token_refresh"].Status != Skip {
		t.Errorf("token refresh should not run: %+v", fs["resilience.token_refresh"])
	}
}

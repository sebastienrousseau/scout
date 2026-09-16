// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package scout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sebastienrousseau/scout/transport"
)

// statelessServer is a 2026-07-28 server: it validates the required _meta
// and routing headers the way the specification says a server must, so a
// client that gets them wrong fails this test rather than passing quietly.
func statelessServer(t *testing.T, opts statelessOpts) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string         `json:"name"`
				Meta map[string]any `json:"_meta"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		w.Header().Set("Content-Type", "application/json")

		rpcErr := func(status, code int, msg string, data string) {
			w.WriteHeader(status)
			if data == "" {
				data = "null"
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%q,"data":%s}}`, id, code, msg, data)
		}

		// Required per-request metadata.
		ver, _ := req.Params.Meta["io.modelcontextprotocol/protocolVersion"].(string)
		if ver == "" {
			rpcErr(http.StatusBadRequest, -32602, "missing protocolVersion in _meta", "")
			return
		}
		if _, ok := req.Params.Meta["io.modelcontextprotocol/clientCapabilities"]; !ok {
			rpcErr(http.StatusBadRequest, -32602, "missing clientCapabilities in _meta", "")
			return
		}
		// The header must agree with the body.
		if h := r.Header.Get("MCP-Protocol-Version"); h != ver {
			rpcErr(http.StatusBadRequest, transport.CodeHeaderMismatch, "protocol version header does not match body", "")
			return
		}
		if h := r.Header.Get("Mcp-Method"); h != req.Method {
			rpcErr(http.StatusBadRequest, transport.CodeHeaderMismatch, "Mcp-Method does not match body", "")
			return
		}
		if req.Method == "tools/call" {
			name, ok := transport.DecodeHeaderValue(r.Header.Get("Mcp-Name"))
			if !ok || name != req.Params.Name {
				rpcErr(http.StatusBadRequest, transport.CodeHeaderMismatch, "Mcp-Name does not match body", "")
				return
			}
		}
		if ver != opts.version {
			rpcErr(http.StatusBadRequest, transport.CodeUnsupportedProtocolVersion, "unsupported version",
				fmt.Sprintf(`{"supported":[%q]}`, opts.version))
			return
		}
		if req.Method == "server/discover" && opts.noDiscover {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"no such method"}}`, id)
			return
		}
		switch req.Method {
		case "server/discover":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","serverInfo":{"name":"stateless-demo","version":"2.0"},"capabilities":{"tools":{}},"instructions":"hi"}}`, id)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","tools":[{"name":"t","description":"d","inputSchema":{"type":"object"}}]}}`, id)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete"}}`, id)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type statelessOpts struct {
	version    string
	noDiscover bool
}

func newClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(Config{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client(), ClientInfo: Implementation{Name: "scout", Version: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNegotiateStateless(t *testing.T) {
	srv := statelessServer(t, statelessOpts{version: transport.V20260728})
	c := newClient(t, srv)
	n, err := c.Negotiate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Era != EraStateless || n.Version != transport.V20260728 {
		t.Fatalf("negotiation = %+v", n)
	}
	if n.Discovered == nil || n.Discovered.ServerInfo.Name != "stateless-demo" {
		t.Errorf("server/discover result = %+v", n.Discovered)
	}
	if c.Negotiation() != n {
		t.Error("the negotiation must be recorded on the client")
	}
	if c.Transport().Dialect().Stateful() {
		t.Error("the transport must be left on the stateless binding")
	}
	// And the connection actually works afterwards, headers and all.
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools after negotiation: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "t" {
		t.Errorf("tools = %+v", tools)
	}
}

// server/discover is optional. A server that does not implement it is still
// a stateless server, and must not be mistaken for a legacy one.
func TestNegotiateStatelessWithoutDiscover(t *testing.T) {
	srv := statelessServer(t, statelessOpts{version: transport.V20260728, noDiscover: true})
	c := newClient(t, srv)
	n, err := c.Negotiate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Era != EraStateless {
		t.Fatalf("era = %v (%s)", n.Era, n.Reason)
	}
	if n.Discovered != nil {
		t.Errorf("nothing was discovered: %+v", n.Discovered)
	}
	if !strings.Contains(n.Reason, "server/discover") {
		t.Errorf("the reason should say why: %q", n.Reason)
	}
}

// legacyServer speaks only the handshake revisions and answers a stateless
// request the way a server that predates them would: a bare 400 with no
// JSON-RPC error body.
func legacyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		if req.Method == "server/discover" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Bad Request"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-legacy")
		switch req.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"legacy","version":"1.0"}}}`, id)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, id)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{}}`, id)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNegotiateFallsBackToHandshake(t *testing.T) {
	srv := legacyServer(t)
	c := newClient(t, srv)
	n, err := c.Negotiate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Era != EraSession {
		t.Fatalf("era = %v (%s)", n.Era, n.Reason)
	}
	if n.Version != transport.V20251125 {
		t.Errorf("version = %q", n.Version)
	}
	if !c.Transport().Dialect().Stateful() {
		t.Error("the transport must be left on the session binding")
	}
	if c.Transport().SessionID() != "sess-legacy" {
		t.Errorf("the session assigned during the handshake must survive: %q", c.Transport().SessionID())
	}
	if !strings.Contains(n.Reason, "2025-11-25") {
		t.Errorf("the reason should name the negotiated version: %q", n.Reason)
	}
}

// A server that answers with a modern error is a modern server. Falling
// back to a handshake would hide a real problem behind a version change.
func TestNegotiateDoesNotFallBackOnModernError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32021,"message":"needs elicitation","data":{"requiredCapabilities":["elicitation"]}}}`))
	}))
	defer srv.Close()

	c := newClient(t, srv)
	n, err := c.Negotiate(context.Background())
	if err == nil {
		t.Fatal("a modern rejection must be reported, not worked around")
	}
	var mc *transport.MissingCapabilityError
	if !errors.As(err, &mc) {
		t.Errorf("the cause should survive: %v", err)
	}
	if n == nil || n.Era != EraStateless {
		t.Errorf("the server is still stateless: %+v", n)
	}
}

// When the server names the versions it supports, take one we share rather
// than dropping to a handshake.
func TestNegotiateHonoursAdvertisedVersions(t *testing.T) {
	srv := statelessServer(t, statelessOpts{version: transport.V20260728})
	c := newClient(t, srv)
	// Pretend scout offers a newer version first that this server refuses.
	orig := StatelessVersions
	StatelessVersions = []string{"2099-01-01", transport.V20260728}
	t.Cleanup(func() { StatelessVersions = orig })

	n, err := c.Negotiate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Era != EraStateless || n.Version != transport.V20260728 {
		t.Fatalf("negotiation = %+v", n)
	}
	if len(n.ServerSupported) == 0 {
		t.Errorf("the versions the server named should be recorded: %+v", n)
	}
	if !strings.Contains(n.Reason, transport.V20260728) {
		t.Errorf("reason = %q", n.Reason)
	}
}

func TestNegotiateUnreachableServer(t *testing.T) {
	c, err := New(Config{Endpoint: "http://127.0.0.1:1/mcp", ClientInfo: Implementation{Name: "scout"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Negotiate(context.Background()); err == nil {
		t.Fatal("an unreachable server must be an error, not a fallback verdict")
	}
}

// A handshake-era server handed server/discover answers -32601 as an
// ordinary JSON-RPC error at HTTP 200, because it does not know the method
// either. Reading that as "a stateless server without the optional RPC"
// would label every legacy server as current — the one mistake this
// detection exists to avoid.
func TestNegotiateLegacyMethodNotFoundIsNotStateless(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-1")
		switch req.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"legacy","version":"1"}}}`, id)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			// HTTP 200 with a JSON-RPC error: the handshake-era shape.
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"no such method"}}`, id)
		}
	}))
	defer srv.Close()

	c := newClient(t, srv)
	n, err := c.Negotiate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Era != EraStateless {
		// expected
	} else {
		t.Fatalf("a 200 + -32601 must not be read as a stateless server: %+v", n)
	}
	if n.Era != EraSession || n.Version != transport.V20251125 {
		t.Fatalf("negotiation = %+v", n)
	}
}

// The stateless shape is HTTP 404 with a -32601 body, which is how a
// current server reports an RPC it does not implement.
func TestNegotiateStatelessMethodNotFoundIsStateless(t *testing.T) {
	srv := statelessServer(t, statelessOpts{version: transport.V20260728, noDiscover: true})
	c := newClient(t, srv)
	n, err := c.Negotiate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Era != EraStateless {
		t.Fatalf("a 404 + -32601 is a stateless server without the optional RPC: %+v", n)
	}
}

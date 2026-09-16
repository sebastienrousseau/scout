// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/transport"
)

// statelessOpts tunes how the fake 2026-07-28 server misbehaves.
type statelessOpts struct {
	noDiscover      bool
	acceptMismatch  bool // do not validate the mirrored headers against the body
	serveGETStream  bool // still serve the stream the revision removed
	varyByConnCount bool // answer differently on alternating calls
}

// statelessFake is a server on the stateless revision that validates what
// the specification says a server must: the required _meta fields, and the
// routing headers against the body.
func statelessFake(t *testing.T, o statelessOpts) *httptest.Server {
	t.Helper()
	// The performance phase issues concurrent requests, so the counters this
	// fake keeps are shared across handler goroutines.
	var mu sync.Mutex
	var listCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if o.serveGETStream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(": hello\n\n"))
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string         `json:"name"`
				Meta map[string]any `json:"_meta"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`))
			return
		}
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		w.Header().Set("Content-Type", "application/json")
		fail := func(status, code int, msg string) {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%q}}`, id, code, msg)
		}

		ver, _ := req.Params.Meta[transport.MetaProtocolVersion].(string)
		if ver == "" {
			fail(http.StatusBadRequest, -32602, "missing _meta protocolVersion")
			return
		}
		if _, ok := req.Params.Meta[transport.MetaClientCapabilities]; !ok {
			fail(http.StatusBadRequest, -32602, "missing _meta clientCapabilities")
			return
		}
		if !o.acceptMismatch {
			if h := r.Header.Get(transport.HeaderProtocolVersion); h != ver {
				fail(http.StatusBadRequest, transport.CodeHeaderMismatch, "protocol version header does not match body")
				return
			}
			if h := r.Header.Get(transport.HeaderMethod); h != req.Method {
				fail(http.StatusBadRequest, transport.CodeHeaderMismatch, "Mcp-Method does not match body")
				return
			}
		}
		if r.Header.Get(transport.HeaderSessionID) != "" {
			fail(http.StatusBadRequest, transport.CodeHeaderMismatch, "this revision has no sessions")
			return
		}

		switch req.Method {
		case "server/discover":
			if o.noDiscover {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"not implemented"}}`, id)
				return
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","serverInfo":{"name":"stateless-fake","version":"2.0"},"capabilities":{"tools":{}},"instructions":"A stateless server for tests."}}`, id)
		case "tools/list":
			mu.Lock()
			listCalls++
			seq := listCalls
			mu.Unlock()
			n := 1
			// A server that answers differently from one call to the next
			// is keeping state somewhere, whatever it advertises.
			if o.varyByConnCount && seq%2 == 0 {
				n = 2
			}
			tools := make([]string, 0, n)
			for i := 0; i < n; i++ {
				tools = append(tools, fmt.Sprintf(`{"name":"t%d","description":"A tool that looks things up for you.","annotations":{"readOnlyHint":true},"inputSchema":{"type":"object","required":["q"],"properties":{"q":{"type":"string"}}}}`, i))
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","tools":[%s]}}`, id, strings.Join(tools, ","))
		case "tools/call":
			if req.Params.Name == "" {
				fail(http.StatusBadRequest, -32602, "name is required")
				return
			}
			if !strings.HasPrefix(req.Params.Name, "t") {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","isError":true,"content":[{"type":"text","text":"no such tool"}]}}`, id)
				return
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","content":[{"type":"text","text":"ok"}]}}`, id)
		case "ping":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete"}}`, id)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"no such method"}}`, id)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runStateless(t *testing.T, srv *httptest.Server, mutate func(*Options)) *Session {
	t.Helper()
	opts := Options{
		Endpoint: srv.URL, HTTPClient: srv.Client(), Version: "test",
		Creds: &creds.Credentials{Mode: creds.ModeNone},
		Samples: 1, Concurrency: 2, RPS: 0, CallTimeout: 5 * time.Second,
	}
	if mutate != nil {
		mutate(&opts)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return s
}

func findingByID(s *Session, id string) (Finding, bool) {
	for _, p := range s.Results {
		for _, f := range p.Findings {
			if f.ID == id {
				return f, true
			}
		}
	}
	return Finding{}, false
}

// The whole nine-phase diagnostic must complete against a server on the
// stateless revision. Before the pipeline was made dialect-aware it opened
// with an initialize the revision had removed, and every run stopped at
// first contact.
func TestStatelessServerCompletesEveryPhase(t *testing.T) {
	s := runStateless(t, statelessFake(t, statelessOpts{}), nil)

	if s.Blocked() != "" {
		t.Fatalf("the run was blocked: %s", s.Blocked())
	}
	if !s.Stateless() {
		t.Fatalf("era = %+v", s.Era)
	}
	for _, pr := range s.Results {
		if pr.Status == Skip && pr.Skipped != "" {
			t.Errorf("phase %s was skipped: %s", pr.Name, pr.Skipped)
		}
	}
	if s.Init == nil || s.Init.ServerInfo.Name != "stateless-fake" {
		t.Errorf("server was not identified: %+v", s.Init)
	}
	if len(s.Tools) != 1 {
		t.Errorf("catalog = %d tools", len(s.Tools))
	}
	// The findings that replace the handshake ones.
	for _, id := range []string{"handshake.protocol_era", "handshake.server_info", "protocol.routing_headers", "resilience.stateless"} {
		f, ok := findingByID(s, id)
		if !ok {
			t.Errorf("%s is missing", id)
			continue
		}
		if f.Status != Pass {
			t.Errorf("%s = %s: %s", id, f.Status, f.Detail)
		}
	}
	// And the ones that only make sense on the older revisions must not run.
	for _, id := range []string{"handshake.initialize", "protocol.bogus_session", "resilience.session_reinit"} {
		if _, ok := findingByID(s, id); ok {
			t.Errorf("%s has no meaning on the stateless revision", id)
		}
	}
}

// The specification requires every 2026-07-28 server to implement
// server/discover: with initialize gone it is the only way a client learns
// a server's identity, capabilities and supported versions. Its absence is
// a failure, not a remark.
func TestStatelessWithoutDiscover(t *testing.T) {
	s := runStateless(t, statelessFake(t, statelessOpts{noDiscover: true}), nil)
	if s.Blocked() != "" {
		t.Fatalf("blocked: %s", s.Blocked())
	}
	if !s.Stateless() {
		t.Fatalf("a 404 with -32601 is still a stateless server: %+v", s.Era)
	}
	f, ok := findingByID(s, "handshake.server_info")
	if !ok || f.Status != Fail {
		t.Errorf("handshake.server_info = %+v", f)
	}
	// And the run still completes: a missing optional-looking RPC must not
	// stop scout from diagnosing everything else.
	if s.Blocked() != "" {
		t.Errorf("the run should continue: %s", s.Blocked())
	}
}

// The mirrored headers only buy anything if the server refuses a request
// whose headers disagree with its body. A server that accepts the mismatch
// lets a gateway and the server itself act on different requests.
func TestRoutingHeaderMismatchIsReported(t *testing.T) {
	s := runStateless(t, statelessFake(t, statelessOpts{acceptMismatch: true}), nil)
	f, ok := findingByID(s, "protocol.routing_headers")
	if !ok {
		t.Fatal("protocol.routing_headers is missing")
	}
	if f.Status != Fail {
		t.Errorf("accepting a header/body mismatch must fail: %+v", f)
	}
	if !strings.Contains(f.Advice, "-32020") {
		t.Errorf("the advice should name the error to return: %q", f.Advice)
	}
}

// The stateless revision removed the standalone GET stream.
func TestGETStreamOnStatelessIsReported(t *testing.T) {
	s := runStateless(t, statelessFake(t, statelessOpts{serveGETStream: true}), nil)
	f, ok := findingByID(s, "protocol.get_stream")
	if !ok || f.Status != Warn {
		t.Errorf("a GET stream on this revision should warn: %+v", f)
	}
	// And a server that answers 405 passes.
	s2 := runStateless(t, statelessFake(t, statelessOpts{}), nil)
	f2, _ := findingByID(s2, "protocol.get_stream")
	if f2.Status != Pass {
		t.Errorf("405 is what this revision requires: %+v", f2)
	}
}

// A server whose answer depends on how many requests it has seen is not
// stateless, whatever it claims, and cannot sit behind a load balancer.
func TestStatefulnessIsCaught(t *testing.T) {
	s := runStateless(t, statelessFake(t, statelessOpts{varyByConnCount: true}), nil)
	f, ok := findingByID(s, "resilience.stateless")
	if !ok {
		t.Fatal("resilience.stateless is missing")
	}
	if f.Status != Fail {
		t.Errorf("a connection-dependent answer must fail: %+v", f)
	}
}

// Opting out of the era probe means scout cannot know which request to open
// with, so a stateless server is no longer diagnosable. That tradeoff has
// to be visible rather than silent.
func TestSkipEraCheckStopsAtFirstContact(t *testing.T) {
	s := runStateless(t, statelessFake(t, statelessOpts{}), func(o *Options) { o.SkipEraCheck = true })
	if s.Blocked() == "" {
		t.Fatal("without the era probe, an initialize-era first contact cannot succeed here")
	}
	f, ok := findingByID(s, "handshake.protocol_era")
	if ok && f.Status != Skip {
		t.Errorf("the era finding should record that it was skipped: %+v", f)
	}
}

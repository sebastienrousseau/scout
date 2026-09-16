// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/transport"
)

// phaseHandshake runs initialize with credentials and inspects the result.
func phaseHandshake(ctx context.Context, s *Session) []Finding {
	var out []Finding
	// Which generation the server speaks was decided in the discovery phase,
	// because on the current revision there is no handshake to decide it.
	out = append(out, checkEra(ctx, s))
	if s.Stateless() {
		out = append(out, s.setupStateless(ctx)...)
		return out
	}
	c := s.check("handshake.initialize", "initialize succeeds")
	res, err := s.Client.Initialize(telemetry.WithPhase(ctx, "handshake", "initialize"))
	if err != nil {
		if ch, ok := scout.Unauthorized(err); ok {
			detail := "the server answered 401 to a request carrying the token"
			if e := ch.Params["error"]; e != "" {
				detail += " (" + e
				if d := ch.Params["error_description"]; d != "" {
					detail += ": " + d
				}
				detail += ")"
			} else {
				detail += " and gave no error code in WWW-Authenticate"
			}
			advice := "the token is not accepted here: check it has not expired, that --resource matches the audience the server expects, that --scope covers what it requires, and that any tenant parameter is passed with --param"
			if s.Opts.Creds.Effective() == creds.ModeBearer {
				advice = "the supplied token is not accepted here: confirm it was issued for this endpoint (audience/resource), has not expired, and carries the scope the server requires; if the operator gave you a client id and secret instead, use --auth client-credentials"
			}
			out = append(out, c.fail(Critical, detail, advice))
			s.blocked = "credentials rejected at initialize"
			return out
		}
		out = append(out, c.fail(Critical, err.Error(), "initialize must return a result"))
		s.blocked = "initialize failed"
		return out
	}
	s.Init = res
	s.Reached = true
	out = append(out, c.pass(fmt.Sprintf("%s %s", res.ServerInfo.Name, res.ServerInfo.Version)))

	c = s.check("handshake.protocol_version", "Negotiated protocol version")
	if res.ProtocolVersion == scout.SupportedProtocolVersions[0] {
		out = append(out, c.pass(res.ProtocolVersion))
	} else {
		out = append(out, c.info(fmt.Sprintf("%s (scout offered %s)", res.ProtocolVersion, scout.SupportedProtocolVersions[0])))
	}

	c = s.check("handshake.server_info", "serverInfo populated")
	switch {
	case res.ServerInfo.Name == "":
		out = append(out, c.fail(Minor, "serverInfo.name is empty", "set name and version"))
	case res.ServerInfo.Version == "":
		out = append(out, c.warn("serverInfo.version is empty", "set version so clients can report it"))
	default:
		out = append(out, c.pass(res.ServerInfo.Name+" "+res.ServerInfo.Version))
	}

	c = s.check("handshake.capabilities", "Capabilities advertised")
	var caps []string
	if res.Capabilities.Tools != nil {
		caps = append(caps, "tools")
	}
	if res.Capabilities.Resources != nil {
		caps = append(caps, "resources")
	}
	if res.Capabilities.Prompts != nil {
		caps = append(caps, "prompts")
	}
	if res.Capabilities.Logging != nil {
		caps = append(caps, "logging")
	}
	if len(caps) == 0 {
		out = append(out, c.warn("no capabilities declared", "declare tools/resources/prompts so clients know what to list"))
	} else {
		out = append(out, c.pass(strings.Join(caps, ", ")))
	}

	c = s.check("handshake.instructions", "Server instructions")
	if res.Instructions == "" {
		out = append(out, c.info("none provided"))
	} else {
		out = append(out, c.pass(fmt.Sprintf("%d characters", len(res.Instructions))))
	}

	c = s.check("handshake.session", "Mcp-Session-Id issued")
	if sid := s.Client.Transport().SessionID(); sid != "" {
		s.SessionID = true
		out = append(out, c.pass("session id present"))
	} else {
		out = append(out, c.info("stateless server (no session id)"))
	}
	return out
}

// phaseProtocol sends deliberately unusual requests and checks the server
// answers them the way the JSON-RPC and MCP specifications require.
func phaseProtocol(ctx context.Context, s *Session) []Finding {
	var out []Finding
	tr := s.Client.Transport()
	pctx := func(label string) context.Context { return telemetry.WithPhase(ctx, "protocol", label) }

	live, liveParams := s.liveness()
	c := s.check("protocol.ping", live)
	if err := s.Client.Call(pctx(live), live, liveParams, nil); err != nil {
		out = append(out, c.fail(Major, err.Error(), "implement ping; clients use it for liveness"))
	} else {
		out = append(out, c.pass("ok"))
	}

	c = s.check("protocol.unknown_method", "Unknown method returns -32601")
	id := tr.NextID()
	raw, err := tr.Do(pctx("unknown method"), transport.RawOptions{Request: &transport.Request{JSONRPC: "2.0", ID: &id, Method: "scout/does_not_exist"}})
	switch {
	case err != nil:
		out = append(out, c.warn("request failed: "+err.Error(), ""))
	case raw.Response != nil && raw.Response.Error != nil && raw.Response.Error.Code == -32601:
		out = append(out, c.pass("-32601 Method not found"))
	case raw.Response != nil && raw.Response.Error != nil:
		out = append(out, c.warn(fmt.Sprintf("error code %d (%s); -32601 expected", raw.Response.Error.Code, raw.Response.Error.Message), "use -32601 for unknown methods"))
	case raw.Status >= 400:
		out = append(out, c.warn(fmt.Sprintf("HTTP %d instead of a JSON-RPC error", raw.Status), "answer 200 with a JSON-RPC error object"))
	default:
		out = append(out, c.fail(Minor, "no error returned for an unknown method", "return -32601"))
	}

	c = s.check("protocol.id_echo", "Response id matches request id")
	id = tr.NextID()
	raw, err = tr.Do(pctx("id echo"), transport.RawOptions{Request: &transport.Request{JSONRPC: "2.0", ID: &id, Method: live, Params: liveJSON(liveParams)}})
	switch {
	case err != nil:
		out = append(out, c.warn("request failed: "+err.Error(), ""))
	case raw.Response == nil:
		out = append(out, c.fail(Major, fmt.Sprintf("HTTP %d, body is not a JSON-RPC response: %s", raw.Status, truncate(string(raw.Body), 120)), ""))
	case raw.Response.ID == nil || *raw.Response.ID != id:
		out = append(out, c.fail(Major, fmt.Sprintf("sent id %d, got %v", id, raw.Response.ID), "echo the request id"))
	case raw.Response.JSONRPC != "2.0":
		out = append(out, c.warn(`jsonrpc field is not "2.0"`, "set jsonrpc: \"2.0\""))
	default:
		out = append(out, c.pass("id echoed, jsonrpc 2.0"))
	}

	c = s.check("protocol.malformed_json", "Malformed JSON is rejected")
	raw, err = tr.Do(pctx("malformed json"), transport.RawOptions{Body: []byte(`{"jsonrpc":"2.0","id":1,"method":`)})
	switch {
	case err != nil:
		out = append(out, c.warn("request failed: "+err.Error(), ""))
	case raw.Status == http.StatusBadRequest:
		out = append(out, c.pass("HTTP 400"))
	case raw.Response != nil && raw.Response.Error != nil && raw.Response.Error.Code == -32700:
		out = append(out, c.pass("-32700 Parse error"))
	case raw.Status/100 == 2:
		out = append(out, c.fail(Minor, fmt.Sprintf("HTTP %d for a truncated body", raw.Status), "return 400 or -32700"))
	default:
		out = append(out, c.info(fmt.Sprintf("HTTP %d", raw.Status)))
	}

	c = s.check("protocol.invalid_params", "tools/call without a name is rejected")
	id = tr.NextID()
	raw, err = tr.Do(pctx("invalid params"), transport.RawOptions{Request: &transport.Request{JSONRPC: "2.0", ID: &id, Method: "tools/call", Params: json.RawMessage(`{}`)}})
	switch {
	case err != nil:
		out = append(out, c.warn("request failed: "+err.Error(), ""))
	case raw.Response != nil && raw.Response.Error != nil:
		if raw.Response.Error.Code == -32602 {
			out = append(out, c.pass("-32602 Invalid params"))
		} else {
			out = append(out, c.info(fmt.Sprintf("error %d: %s", raw.Response.Error.Code, raw.Response.Error.Message)))
		}
	case raw.Status >= 400:
		out = append(out, c.info(fmt.Sprintf("HTTP %d", raw.Status)))
	default:
		out = append(out, c.fail(Minor, "a tools/call with no name succeeded", "validate params and return -32602"))
	}

	c = s.check("protocol.unknown_tool", "Unknown tool is reported")
	res, err := s.Client.CallTool(pctx("unknown tool"), "scout_no_such_tool", map[string]any{})
	switch {
	case err != nil:
		var rpc *transport.RPCError
		if asRPC(err, &rpc) {
			out = append(out, c.pass(fmt.Sprintf("JSON-RPC error %d", rpc.Code)))
		} else {
			out = append(out, c.warn(err.Error(), ""))
		}
	case res.IsError:
		out = append(out, c.pass("isError result: "+truncate(res.Text(), 80)))
	default:
		out = append(out, c.fail(Major, "calling a non-existent tool returned success", "return -32602 or an isError result"))
	}

	c = s.check("protocol.accept_header", "Request without Accept header")
	id = tr.NextID()
	raw, err = tr.Do(pctx("no accept"), transport.RawOptions{Request: &transport.Request{JSONRPC: "2.0", ID: &id, Method: live, Params: liveJSON(liveParams)}, Headers: map[string]string{"Accept": ""}})
	switch {
	case err != nil:
		out = append(out, c.info("request failed: "+err.Error()))
	case raw.Status == http.StatusNotAcceptable:
		out = append(out, c.info("406: server insists on Accept (spec-strict)"))
	case raw.Status/100 == 2:
		out = append(out, c.info("accepted without Accept header (lenient)"))
	default:
		out = append(out, c.info(fmt.Sprintf("HTTP %d", raw.Status)))
	}

	// What a GET should do depends on the generation. The handshake
	// revisions let a client open a standalone stream that way; the
	// stateless revision removed it, and a server that still serves one is
	// carrying a mechanism no current client will use.
	c = s.check("protocol.get_stream", "GET on the MCP endpoint")
	raw, err = tr.Do(pctx("GET stream"), transport.RawOptions{HTTPMethod: http.MethodGet, Headers: map[string]string{"Accept": "text/event-stream"}, SkipDialect: true})
	switch {
	case err != nil:
		out = append(out, c.info("GET failed: "+truncate(err.Error(), 100)))
	case s.Stateless() && raw.Status == http.StatusMethodNotAllowed:
		out = append(out, c.pass("405, as this revision requires"))
	case s.Stateless() && raw.Status/100 == 2 && raw.ContentType == "text/event-stream":
		out = append(out, c.warn("a GET still opens an event stream, which "+scout.StatelessVersions[0]+" removed",
			"answer 405 to GET: server-initiated streams were replaced by subscriptions/listen, and a client on this revision will never open one this way"))
	case s.Stateless():
		out = append(out, c.info(fmt.Sprintf("HTTP %d %s (this revision expects 405)", raw.Status, raw.ContentType)))
	case raw.Status == http.StatusMethodNotAllowed:
		out = append(out, c.info("405: no server-initiated stream (allowed by spec)"))
	case raw.Status/100 == 2 && raw.ContentType == "text/event-stream":
		out = append(out, c.pass("text/event-stream"))
	default:
		out = append(out, c.info(fmt.Sprintf("HTTP %d %s", raw.Status, raw.ContentType)))
	}

	if s.SessionID {
		c = s.check("protocol.bogus_session", "Unknown session id is rejected")
		id = tr.NextID()
		raw, err = tr.Do(pctx("bogus session"), transport.RawOptions{Request: &transport.Request{JSONRPC: "2.0", ID: &id, Method: live, Params: liveJSON(liveParams)}, Headers: map[string]string{transport.HeaderSessionID: "scout-bogus-" + s.TraceID[:8]}})
		switch {
		case err != nil:
			out = append(out, c.info("request failed: "+err.Error()))
		case raw.Status == http.StatusNotFound:
			out = append(out, c.pass("404"))
		case raw.Status == http.StatusBadRequest:
			out = append(out, c.pass("400"))
		case raw.Status/100 == 2:
			out = append(out, c.warn("server answered a request carrying a session id it never issued", "reject unknown session ids with 404"))
		default:
			out = append(out, c.info(fmt.Sprintf("HTTP %d", raw.Status)))
		}
	}

	c = s.check("protocol.version_header", "Bad MCP-Protocol-Version is rejected")
	id = tr.NextID()
	raw, err = tr.Do(pctx("bad version"), transport.RawOptions{Request: &transport.Request{JSONRPC: "2.0", ID: &id, Method: live, Params: liveJSON(liveParams)}, Headers: map[string]string{transport.HeaderProtocolVersion: "1999-01-01"}})
	switch {
	case err != nil:
		out = append(out, c.info("request failed: "+err.Error()))
	case raw.Status == http.StatusBadRequest:
		out = append(out, c.pass("400"))
	case raw.Status/100 == 2:
		out = append(out, c.info("accepted (server does not validate the header)"))
	default:
		out = append(out, c.info(fmt.Sprintf("HTTP %d", raw.Status)))
	}
	return out
}

// phaseResilience checks recovery paths: session expiry and token refresh.
// liveJSON marshals liveness params, which are either nil or an empty
// object depending on the revision.
func liveJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func phaseResilience(ctx context.Context, s *Session) []Finding {
	var out []Finding
	tr := s.Client.Transport()
	if s.Stateless() {
		// There is no session to lose. What matters instead is whether the
		// server really is stateless, because that is what lets a plain
		// round-robin load balancer sit in front of it.
		return append(out, s.checkStatelessness(ctx))
	}
	if s.SessionID {
		c := s.check("resilience.session_reinit", "Client recovers from a lost session")
		orig := tr.SessionID()
		tr.SetSessionID("scout-expired-" + s.TraceID[:8])
		err := s.Client.Ping(telemetry.WithPhase(ctx, "resilience", "expired session"))
		switch {
		case err == nil && tr.SessionID() != "" && tr.SessionID() != "scout-expired-"+s.TraceID[:8]:
			out = append(out, c.pass("404 observed, re-initialized, ping succeeded on a new session"))
		case err == nil:
			out = append(out, c.warn("server accepted the bogus session; recovery not exercised", "return 404 for unknown sessions so clients re-initialize"))
			tr.SetSessionID(orig)
		default:
			out = append(out, c.fail(Major, "recovery failed: "+err.Error(), ""))
			tr.SetSessionID(orig)
		}
	} else {
		out = append(out, s.check("resilience.session_reinit", "Session recovery").skip("stateless server"))
	}

	if s.Token != nil && s.RequiresAuth {
		c := s.check("resilience.token_refresh", "Token source can renew")
		src := s.Client.TokenSource()
		if src == nil {
			out = append(out, c.skip("no token source"))
		} else {
			src.Invalidate()
			if err := s.Client.Ping(telemetry.WithPhase(ctx, "resilience", "after invalidate")); err != nil {
				out = append(out, c.fail(Major, "call after token invalidation failed: "+err.Error(), "check refresh_token / client-credentials re-issue"))
			} else {
				out = append(out, c.pass("call succeeded after invalidating the cached token"))
			}
		}
	}
	return out
}

func asRPC(err error, target **transport.RPCError) bool {
	for err != nil {
		var e *transport.RPCError
		if errors.As(err, &e) {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

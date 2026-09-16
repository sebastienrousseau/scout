// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"
)

// RawResult is the HTTP-level view of one exchange, for conformance probes
// that need to see status codes and headers rather than only the JSON-RPC
// result.
type RawResult struct {
	Status      int
	Header      http.Header
	ContentType string
	Body        []byte // capped at 1 MiB
	Duration    time.Duration
	// Response is set when the body parsed as a single JSON-RPC response
	// (from JSON or from the first matching SSE event).
	Response *Response
}

// RawOptions tweaks a Do call away from the well-formed default.
type RawOptions struct {
	// HTTPMethod defaults to POST.
	HTTPMethod string
	// Body is sent verbatim; when nil and Request is set, Request is
	// marshalled.
	Body []byte
	// Request, when set and Body is nil, is marshalled as the body.
	Request *Request
	// Headers override the defaults (set a key to "" to delete it).
	Headers map[string]string
	// OmitSession suppresses the Mcp-Session-Id header.
	OmitSession bool
	// OmitProtocolVersion suppresses the MCP-Protocol-Version header.
	OmitProtocolVersion bool
}

// NextID reserves a fresh JSON-RPC id.
func (s *Streamable) NextID() int64 { return s.nextID.Add(1) }

// Do performs one arbitrary HTTP exchange against the endpoint and reports
// what came back. It never mutates session state except to record a new
// Mcp-Session-Id the server hands out.
func (s *Streamable) Do(ctx context.Context, opts RawOptions) (*RawResult, error) {
	method := opts.HTTPMethod
	if method == "" {
		method = http.MethodPost
	}
	body := opts.Body
	if body == nil && opts.Request != nil {
		b, err := json.Marshal(opts.Request)
		if err != nil {
			return nil, err
		}
		body = b
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.Endpoint, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	if !opts.OmitProtocolVersion {
		if v := s.ProtocolVersion(); v != "" {
			req.Header.Set(HeaderProtocolVersion, v)
		}
	}
	if !opts.OmitSession {
		if sid := s.SessionID(); sid != "" {
			req.Header.Set(HeaderSessionID, sid)
		}
	}
	for k, v := range opts.Headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	start := time.Now()
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out := &RawResult{Status: resp.StatusCode, Header: resp.Header.Clone()}
	out.ContentType, _, _ = mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if sid := resp.Header.Get(HeaderSessionID); sid != "" {
		s.sessionID.Store(sid)
	}
	// Bound how long we wait on a stream that never ends (GET SSE).
	out.Body, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	out.Duration = time.Since(start)
	if out.ContentType == "text/event-stream" {
		if opts.Request != nil && opts.Request.ID != nil {
			if r, err := readSSEResponse(bytes.NewReader(out.Body), *opts.Request.ID); err == nil {
				out.Response = r
			}
		}
	} else if len(out.Body) > 0 {
		var r Response
		if json.Unmarshal(out.Body, &r) == nil && (r.ID != nil || r.Error != nil) {
			out.Response = &r
		}
	}
	return out, nil
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sebastienrousseau/scout/trace"
)

func TestRecorderCapturesTimingsHeadersAndRedacts(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "session=abc")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"error":{"code":-32601,"message":"nope"}}`))
	}))
	defer srv.Close()
	rec := New()
	rec.CaptureBodies = true
	rec.Redactor.Add("s3cr3t-token-value")
	hc := &http.Client{Transport: rec.Wrap(srv.Client().Transport)}
	ctx := WithPhase(trace.WithID(context.Background(), "t1"), "protocol", "unknown method")
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/mcp?code=oauthcode", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":7,"method":"x","token":"s3cr3t-token-value"}`)))
	req.Header.Set("Authorization", "Bearer s3cr3t-token-value")
	req.Header.Set("X-Api-Key", "k")
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bytes.NewBuffer(nil).ReadFrom(resp.Body)
	_ = resp.Body.Close()
	evs := rec.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d", len(evs))
	}
	e := evs[0]
	if e.Phase != "protocol" || e.Label != "unknown method" || e.TraceID != "t1" || e.Status != 200 {
		t.Errorf("event = %+v", e)
	}
	if e.Timings.Total == 0 || e.Timings.TTFB == 0 || e.Timings.TLS == 0 || e.Timings.Connect == 0 {
		t.Errorf("timings not captured: %+v", e.Timings)
	}
	if e.TLS == nil || e.TLS.Version == "" {
		t.Errorf("tls info missing: %+v", e.TLS)
	}
	if e.RequestHeaders["Authorization"] != "Bearer ***" || e.RequestHeaders["X-Api-Key"] != "***" || e.ResponseHeaders["Set-Cookie"] != "***" {
		t.Errorf("headers not redacted: %v %v", e.RequestHeaders, e.ResponseHeaders)
	}
	if strings.Contains(e.RequestBody, "s3cr3t") || !strings.Contains(e.RequestBody, "***") {
		t.Errorf("body not redacted: %s", e.RequestBody)
	}
	if !strings.Contains(e.URL, "code=%2A%2A%2A") && !strings.Contains(e.URL, "code=***") {
		t.Errorf("query param not redacted: %s", e.URL)
	}
	if e.RPC == nil || e.RPC.Method != "x" || e.RPC.ErrorCode != -32601 {
		t.Errorf("rpc info = %+v", e.RPC)
	}
	if e.RequestBytes == 0 || e.ResponseBytes == 0 {
		t.Errorf("bytes = %d/%d", e.RequestBytes, e.ResponseBytes)
	}
	var har bytes.Buffer
	if err := rec.WriteHAR(&har, "test"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(har.Bytes(), &doc); err != nil {
		t.Fatalf("har not json: %v", err)
	}
	if strings.Contains(har.String(), "s3cr3t") {
		t.Error("secret leaked into HAR")
	}
	var nd bytes.Buffer
	_ = rec.WriteNDJSON(&nd)
	if strings.Count(nd.String(), "\n") != 1 {
		t.Errorf("ndjson lines = %d", strings.Count(nd.String(), "\n"))
	}
	s := rec.Summary()
	if s.Requests != 1 || s.ByStatus["2xx"] != 1 || s.ByPhase["protocol"] != 1 || s.NewConns != 1 {
		t.Errorf("summary = %+v", s)
	}
}

func TestRecorderRecordsTransportErrors(t *testing.T) {
	rec := New()
	hc := &http.Client{Transport: rec.Wrap(nil)}
	_, err := hc.Get("http://127.0.0.1:1/unreachable")
	if err == nil {
		t.Fatal("expected error")
	}
	evs := rec.Events()
	if len(evs) != 1 || evs[0].Error == "" || evs[0].Status != 0 {
		t.Errorf("events = %+v", evs)
	}
}

func TestRedactor(t *testing.T) {
	r := &Redactor{}
	r.Add("abc") // too short, ignored
	r.Add("longsecret")
	r.Add("longsecret-extended")
	if got := r.String("x longsecret-extended y longsecret z"); got != "x *** y *** z" {
		t.Errorf("got %q", got)
	}
	if got := r.Form("grant_type=x&client_secret=longsecret&refresh_token=rt1234"); strings.Contains(got, "rt1234") || strings.Contains(got, "longsecret") {
		t.Errorf("form = %q", got)
	}
	if got := r.URL("https://u:pw@h/p?state=s&x=1"); !strings.Contains(got, "state=%2A%2A%2A") || strings.Contains(got, "pw@") {
		t.Errorf("url = %q", got)
	}
	if got := r.Header("Authorization", "Basic dXNlcjpwYXNz"); got != "Basic ***" {
		t.Errorf("header = %q", got)
	}
	if got := r.Header("Content-Type", "application/json"); got != "application/json" {
		t.Errorf("plain header = %q", got)
	}
}

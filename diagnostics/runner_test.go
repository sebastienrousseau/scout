// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package diagnostics_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/trace"
)

func fakeMCP(t *testing.T, calls map[string]int, traceIDs *[]string) *httptest.Server {
	t.Helper()
	yes := true
	tools := []scout.Tool{
		{Name: "get_time", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object","required":["iso"],"properties":{"iso":{"type":"string"}}}`), Annotations: &scout.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "search", InputSchema: json.RawMessage(`{"type":"object","required":["q"],"properties":{"q":{"type":"string"}}}`), OutputSchema: json.RawMessage(`{"type":"object","required":["hits"],"properties":{"hits":{"type":"array"}}}`), Annotations: &scout.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "slow", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &scout.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "broken", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &scout.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "delete_all", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*traceIDs = append(*traceIDs, r.Header.Get(trace.Header))
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		reply := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":%s}`, *req.ID, b)
		}
		switch req.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "fake", "version": "1"}})
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/list":
			reply(map[string]any{"tools": tools})
		case "tools/call":
			var p struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			calls[p.Name]++
			switch p.Name {
			case "get_time":
				reply(scout.CallToolResult{Content: []scout.Content{{Type: "text", Text: "now"}}, StructuredContent: json.RawMessage(`{"iso":"2026-01-01T00:00:00Z"}`)})
			case "search":
				if _, ok := p.Args["q"]; !ok {
					reply(scout.CallToolResult{IsError: true, Content: []scout.Content{{Type: "text", Text: "missing q"}}})
					return
				}
				reply(scout.CallToolResult{Content: []scout.Content{{Type: "text", Text: "ok"}}, StructuredContent: json.RawMessage(`{"hits":"not-an-array"}`)})
			case "slow":
				time.Sleep(120 * time.Millisecond)
				reply(scout.CallToolResult{Content: []scout.Content{{Type: "text", Text: "zzz"}}})
			case "broken":
				w.WriteHeader(500)
			default:
				reply(scout.CallToolResult{Content: []scout.Content{{Type: "text", Text: "done"}}})
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunnerEndToEnd(t *testing.T) {
	calls := map[string]int{}
	var traceIDs []string
	srv := fakeMCP(t, calls, &traceIDs)
	c, err := scout.New(scout.Config{Endpoint: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.WithID(context.Background(), "run-123")
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	r := diagnostics.NewRunner(diagnostics.Options{RequestsPerSecond: -1, OutlierFloor: 50 * time.Millisecond, OutlierFactor: 3})
	rep, err := r.Run(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TraceID != "run-123" {
		t.Errorf("trace id = %q", rep.TraceID)
	}
	for _, id := range traceIDs {
		if id != "run-123" {
			t.Fatalf("trace header not propagated: %v", traceIDs)
		}
	}
	if calls["delete_all"] != 0 {
		t.Fatal("destructive tool was invoked under default policy")
	}
	if rep.ToolsDiscovered != 5 || rep.ToolsExecuted != 4 || rep.ToolsSkipped != 1 {
		t.Errorf("counts: %+v", rep)
	}
	if rep.Successful != 3 || rep.ProtocolErrors != 1 || rep.ToolErrors != 0 {
		t.Errorf("outcomes: ok=%d proto=%d tool=%d", rep.Successful, rep.ProtocolErrors, rep.ToolErrors)
	}
	if len(rep.SchemaMismatches) != 1 || !strings.HasPrefix(rep.SchemaMismatches[0], "search:") {
		t.Errorf("schema mismatches = %v", rep.SchemaMismatches)
	}
	if len(rep.Outliers) != 1 || !strings.HasPrefix(rep.Outliers[0], "slow ") {
		t.Errorf("outliers = %v (latency %+v)", rep.Outliers, rep.Latency)
	}
	if len(rep.MissingSchemas) != 3 || len(rep.Unannotated) != 1 {
		t.Errorf("missing schemas = %v unannotated = %v", rep.MissingSchemas, rep.Unannotated)
	}
	if rep.Latency.Count != 4 || rep.Latency.Max < 120*time.Millisecond {
		t.Errorf("latency = %+v", rep.Latency)
	}
	// 100 - 12.5 (1/4 failed) - 5 (schema) - 1 (unannotated) - 3 (missing schemas) = 78.5
	if rep.QualityScore != 78.5 {
		t.Errorf("score = %v deductions = %v", rep.QualityScore, rep.Deductions)
	}
	if _, err := json.Marshal(rep); err != nil {
		t.Errorf("report must serialise: %v", err)
	}
}

func TestRunnerAllowDestructiveOptIn(t *testing.T) {
	calls := map[string]int{}
	var ids []string
	srv := fakeMCP(t, calls, &ids)
	c, _ := scout.New(scout.Config{Endpoint: srv.URL, HTTPClient: srv.Client()})
	if _, err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := diagnostics.NewRunner(diagnostics.Options{RequestsPerSecond: -1, Policy: diagnostics.Policy{AllowDestructive: true, Only: []string{"delete_all"}}})
	rep, err := r.Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if calls["delete_all"] != 1 || rep.ToolsExecuted != 1 || rep.ToolsSkipped != 4 {
		t.Errorf("calls = %v report = %+v", calls, rep)
	}
}

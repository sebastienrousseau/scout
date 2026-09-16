// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// demoServer is a small, well-behaved MCP server: enough catalog for a
// full run, deterministic answers so two runs can be compared.
func demoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-demo")
		switch req.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"demo","version":"1.0"},"instructions":"A demo server for engine tests."}}`, id)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"lookup","title":"Lookup","description":"Look up a record by its identifier.","annotations":{"readOnlyHint":true},"inputSchema":{"type":"object","required":["q"],"properties":{"q":{"type":"string"}}},"outputSchema":{"type":"object","required":["found"],"properties":{"found":{"type":"boolean"}}}}]}}`, id)
		case "tools/call":
			if req.Params.Name == "" {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"name is required"}}`, id)
				return
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"ok"}],"structuredContent":{"found":true}}}`, id)
		case "ping":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{}}`, id)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"no such method"}}`, id)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func demoSpec(srv *httptest.Server) RunSpec {
	return RunSpec{
		Version: "test",
		Target:  TargetSpec{Endpoint: srv.URL + "/mcp"},
		Pacing:  PacingSpec{Samples: 1, Concurrency: 2, RPS: 0, CallTimeout: 5 * time.Second},
	}.WithDefaults()
}

// A spec must survive JSON, because that is how the web UI sends one. If a
// field does not round-trip, that capability is CLI-only in practice.
func TestSpecRoundTripsThroughJSON(t *testing.T) {
	in := RunSpec{
		Version: "v",
		Target:  TargetSpec{Endpoint: "https://mcp.example.com/mcp"},
		Creds:   CredSpec{Mode: "client-credentials", ClientID: "acme", TokenEnv: "MY_TOKEN", Scope: "read"},
		Policy: PolicySpec{
			AllowMutations: true, Only: []string{"a", "b"}, Deny: []string{"c"},
			ToolArgs:     map[string]map[string]any{"a": {"q": "x"}},
			SkipEraCheck: true,
		},
		Pacing: PacingSpec{Samples: 3, Concurrency: 8, RPS: 5, CallTimeout: 12 * time.Second, Seed: 99},
		Phases: PhaseSpec{Only: []string{"net", "catalog"}},
		Output: OutputSpec{Format: FormatJSON, Verbose: true, ReportDir: "/tmp/x"},
	}.WithDefaults()

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out RunSpec
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	// Compare through JSON so map ordering does not matter.
	again, _ := json.Marshal(out)
	if string(b) != string(again) {
		t.Errorf("spec did not round-trip:\n%s\n%s", b, again)
	}
	if out.Pacing.CallTimeout != 12*time.Second || out.Policy.ToolArgs["a"]["q"] != "x" {
		t.Errorf("fields lost in transit: %+v", out)
	}
}

// A spec that crosses a network must carry references to credentials, not
// credentials. This is why the web UI can accept one at all.
func TestSecretsNeverSerialise(t *testing.T) {
	s := RunSpec{
		Target: TargetSpec{Endpoint: "https://x/mcp"},
		Creds: CredSpec{
			Token:        "super-secret-token",
			ClientSecret: "super-secret-client-secret",
			Basic:        "user:super-secret-password",
			Headers:      map[string]string{"X-Api-Key": "api-key-that-must-not-travel"},
			TokenEnv:     "SCOUT_TOKEN",
		},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"super-secret-token", "super-secret-client-secret", "super-secret-password", "api-key-that-must-not-travel"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("a serialised spec leaked %q:\n%s", secret, b)
		}
	}
	// The reference does travel: that is how a remote surface authenticates.
	if !strings.Contains(string(b), "SCOUT_TOKEN") {
		t.Errorf("the environment reference should survive: %s", b)
	}
}

func TestWithDefaults(t *testing.T) {
	got := RunSpec{Target: TargetSpec{Endpoint: "https://x/mcp"}}.WithDefaults()
	if got.Pacing.Samples != DefaultSamples || got.Pacing.Concurrency != DefaultConcurrency {
		t.Errorf("pacing defaults: %+v", got.Pacing)
	}
	if got.Pacing.CallTimeout != DefaultCallTimeout || got.Pacing.Seed != DefaultSeed {
		t.Errorf("timeout/seed defaults: %+v", got.Pacing)
	}
	if got.Output.Format != FormatText || got.Creds.RedirectPort != DefaultRedirectPort {
		t.Errorf("output/creds defaults: %+v %+v", got.Output, got.Creds)
	}
	// An explicit zero RPS means "no throttle" and must survive defaulting.
	explicit := RunSpec{Target: TargetSpec{Endpoint: "https://x/mcp"}, Pacing: PacingSpec{RPS: 0}}.WithDefaults()
	if explicit.Pacing.RPS != 0 {
		t.Errorf("an explicit 0 rps must not be defaulted away: %v", explicit.Pacing.RPS)
	}
}

// One validator for every surface, so a web client gets the CLI's error.
func TestValidate(t *testing.T) {
	ok := RunSpec{Target: TargetSpec{Endpoint: "https://x/mcp"}}.WithDefaults()
	if err := ok.Validate(); err != nil {
		t.Fatalf("a minimal spec should be valid: %v", err)
	}
	cases := map[string]RunSpec{
		"no endpoint":      {},
		"relative endpoint": {Target: TargetSpec{Endpoint: "/mcp"}},
		"bad format":       {Target: TargetSpec{Endpoint: "https://x/mcp"}, Output: OutputSpec{Format: "pdf"}},
		"unknown phase":    {Target: TargetSpec{Endpoint: "https://x/mcp"}, Phases: PhaseSpec{Only: []string{"nonsense"}}},
		"unknown skip":     {Target: TargetSpec{Endpoint: "https://x/mcp"}, Phases: PhaseSpec{Skip: []string{"nonsense"}}},
	}
	for name, spec := range cases {
		s := spec
		if s.Output.Format == "" {
			s = s.WithDefaults()
			s.Output.Format = spec.Output.Format
			if s.Output.Format == "" {
				s.Output.Format = FormatText
			}
		}
		if err := s.Validate(); err == nil {
			t.Errorf("%s should be invalid", name)
		}
	}
	// An unknown phase error should name the phases that exist.
	err := RunSpec{Target: TargetSpec{Endpoint: "https://x/mcp"}, Output: OutputSpec{Format: FormatText}, Phases: PhaseSpec{Only: []string{"nope"}}}.Validate()
	if err == nil || !strings.Contains(err.Error(), "catalog") {
		t.Errorf("the error should list known phases: %v", err)
	}
}

func TestFormatValid(t *testing.T) {
	for _, f := range Formats {
		if !f.Valid() {
			t.Errorf("%q should be valid", f)
		}
	}
	if Format("pdf").Valid() {
		t.Error("pdf is not a format scout renders")
	}
}

func TestPhaseNames(t *testing.T) {
	all := RunSpec{}.PhaseNames()
	if len(all) != 9 {
		t.Errorf("expected nine phases, got %d", len(all))
	}
	only := RunSpec{Phases: PhaseSpec{Only: []string{"net", "catalog"}}}.PhaseNames()
	if len(only) != 2 || only[0] != "net" {
		t.Errorf("Only: %v", only)
	}
	skip := RunSpec{Phases: PhaseSpec{Skip: []string{"net"}}}.PhaseNames()
	if len(skip) != 8 || skip[0] == "net" {
		t.Errorf("Skip: %v", skip)
	}
}

// Choosing a tool is an explicit opt-in, so the policy widens to cover it —
// identically for every surface, which is why it lives here and not in one.
func TestSelectTools(t *testing.T) {
	s := RunSpec{}
	s.SelectTools(nil, nil)
	if len(s.Policy.Only) != 0 || s.Policy.AllowMutations {
		t.Error("selecting nothing changes nothing")
	}
	s.SelectTools([]string{"a", "b", "c"}, map[string]string{"b": "mutating", "c": "destructive"})
	if len(s.Policy.Only) != 3 {
		t.Errorf("Only = %v", s.Policy.Only)
	}
	if !s.Policy.AllowMutations || !s.Policy.AllowDestructive {
		t.Errorf("a deliberate choice must widen the policy: %+v", s.Policy)
	}
}

// The whole point of the package: a process may hold any number of runs at
// once. Before RunSpec this was impossible — the configuration lived in
// package-level variables, so a second run would have overwritten the first.
func TestConcurrentRunsAreIndependent(t *testing.T) {
	a, b := demoServer(t), demoServer(t)
	specA, specB := demoSpec(a), demoSpec(b)
	specB.Phases.Only = []string{"net", "handshake"}

	var wg sync.WaitGroup
	var resA, resB *Result
	wg.Add(2)
	go func() { defer wg.Done(); resA = Run(context.Background(), specA, nil) }()
	go func() { defer wg.Done(); resB = Run(context.Background(), specB, nil) }()
	wg.Wait()

	if resA.Report == nil || resB.Report == nil {
		t.Fatalf("both runs should produce a report: %v %v", resA.Err, resB.Err)
	}
	if resA.Report.Target.Endpoint == resB.Report.Target.Endpoint {
		t.Error("the two runs shared a target; state leaked between them")
	}
	if len(resA.Report.Phases) == len(resB.Report.Phases) {
		t.Errorf("the phase selection leaked: %d vs %d", len(resA.Report.Phases), len(resB.Report.Phases))
	}
	if len(resB.Report.Phases) != 2 {
		t.Errorf("run B should have run two phases, got %d", len(resB.Report.Phases))
	}
}

// Parity in one sentence: the same spec produces the same report, whoever
// built it. A surface that changed the outcome would be a surface with a
// capability of its own.
func TestSameSpecSameReport(t *testing.T) {
	srv := demoServer(t)
	spec := demoSpec(srv)
	spec.Phases.Only = []string{"net", "handshake", "catalog", "execution"}

	fingerprint := func(r *Result) string {
		var b strings.Builder
		for _, p := range r.Report.Phases {
			fmt.Fprintf(&b, "%s=%s\n", p.Name, p.Status)
			for _, f := range p.Findings {
				fmt.Fprintf(&b, "  %s %s\n", f.ID, f.Status)
			}
		}
		fmt.Fprintf(&b, "score=%.2f grade=%s", r.Report.Score.Total, r.Report.Score.Grade)
		return b.String()
	}

	// Once with no sink, once with one: observing a run must not change it.
	first := Run(context.Background(), spec, nil)
	var seen int
	second := Run(context.Background(), spec, SinkFunc(func(Event) { seen++ }))

	if first.Report == nil || second.Report == nil {
		t.Fatalf("both runs should produce a report: %v %v", first.Err, second.Err)
	}
	if fingerprint(first) != fingerprint(second) {
		t.Errorf("the same spec produced different reports:\n--- without sink ---\n%s\n--- with sink ---\n%s", fingerprint(first), fingerprint(second))
	}
	if seen == 0 {
		t.Error("the sink received no events")
	}
}

func TestRunEmitsEveryEventKind(t *testing.T) {
	srv := demoServer(t)
	spec := demoSpec(srv)
	spec.Output.CaptureBodies = true

	var mu sync.Mutex
	kinds := map[EventKind]int{}
	res := Run(context.Background(), spec, SinkFunc(func(e Event) {
		mu.Lock()
		kinds[e.Kind]++
		mu.Unlock()
	}))
	if res.Report == nil {
		t.Fatalf("no report: %v", res.Err)
	}
	for _, k := range []EventKind{EventPhaseStart, EventFinding, EventPhaseDone, EventRequest} {
		if kinds[k] == 0 {
			t.Errorf("no %s event was emitted", k)
		}
	}
}

func TestRunRejectsABadSpec(t *testing.T) {
	res := Run(context.Background(), RunSpec{}, nil)
	if res.Err == nil {
		t.Fatal("an empty spec must not run")
	}
	if res.Report != nil {
		t.Error("a rejected spec produces no report")
	}
	// A named environment variable that is not set is the caller's error,
	// and it is reported before any request is made.
	spec := RunSpec{Target: TargetSpec{Endpoint: "https://x/mcp"}, Creds: CredSpec{TokenEnv: "SCOUT_TEST_UNSET_VAR"}}.WithDefaults()
	if res := Run(context.Background(), spec, nil); res.Err == nil {
		t.Error("an unset environment reference must be reported")
	}
}

func TestRunHonoursCancellation(t *testing.T) {
	srv := demoServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Run(ctx, demoSpec(srv), nil)
	if res.Err == nil {
		t.Error("a cancelled context should stop the run")
	}
}

func TestRenderEveryFormat(t *testing.T) {
	srv := demoServer(t)
	spec := demoSpec(srv)
	spec.Phases.Only = []string{"net", "handshake"}
	res := Run(context.Background(), spec, nil)
	if res.Report == nil {
		t.Fatalf("no report: %v", res.Err)
	}
	for _, f := range Formats {
		s := spec
		s.Output.Format = f
		var b strings.Builder
		if err := res.Render(&b, s, 100); err != nil {
			t.Errorf("render %s: %v", f, err)
		}
		if b.Len() == 0 {
			t.Errorf("render %s produced nothing", f)
		}
		if f == FormatJSON {
			var probe map[string]any
			if err := json.Unmarshal([]byte(b.String()), &probe); err != nil {
				t.Errorf("json output does not parse: %v", err)
			}
		}
	}
	// A result with no report renders nothing rather than panicking.
	empty := &Result{}
	var b strings.Builder
	if err := empty.Render(&b, spec, 80); err != nil || b.Len() != 0 {
		t.Errorf("empty result: %q %v", b.String(), err)
	}
}

// A report directory is what one person hands another, so every rendering
// is written rather than making them guess which one they can open.
func TestWriteDir(t *testing.T) {
	srv := demoServer(t)
	spec := demoSpec(srv)
	spec.Phases.Only = []string{"net", "handshake"}
	dir := filepath.Join(t.TempDir(), "out")
	spec.Output.ReportDir = dir

	res := Run(context.Background(), spec, nil)
	if res.Report == nil {
		t.Fatalf("no report: %v", res.Err)
	}
	files, err := res.WriteDir(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 6 {
		t.Errorf("wrote %d files, want 6: %v", len(files), files)
	}
	for _, name := range []string{"report.json", "report.md", "report.html", "report.txt", "telemetry.ndjson", "telemetry.har"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	if len(res.Report.Files) != 6 {
		t.Error("the report should record what was written")
	}
	// The HAR must be openable by a browser's devtools, so it has to parse.
	b, _ := os.ReadFile(filepath.Join(dir, "telemetry.har"))
	var har struct {
		Log struct {
			Entries []any `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(b, &har); err != nil {
		t.Errorf("the HAR does not parse: %v", err)
	}
	if len(har.Log.Entries) == 0 {
		t.Error("the HAR has no entries")
	}

	// A result assembled without a recorder still writes.
	bare := &Result{Report: res.Report}
	if _, err := bare.WriteDir(filepath.Join(t.TempDir(), "bare"), "test"); err != nil {
		t.Errorf("WriteDir without a recorder: %v", err)
	}
	if _, err := (&Result{}).WriteDir(t.TempDir(), "test"); err != nil {
		t.Errorf("WriteDir with no report: %v", err)
	}
}

func TestResultFailed(t *testing.T) {
	if (&Result{}).Failed() {
		t.Error("an empty result has not failed")
	}
}

func TestSpecString(t *testing.T) {
	s := RunSpec{Target: TargetSpec{Endpoint: "https://x/mcp"}, Creds: CredSpec{Mode: "bearer", Token: "secret-value"}}
	if got := s.String(); strings.Contains(got, "secret-value") {
		t.Errorf("String must not carry the token: %q", got)
	} else if !strings.Contains(got, "https://x/mcp") {
		t.Errorf("String should name the target: %q", got)
	}
}

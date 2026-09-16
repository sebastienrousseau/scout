// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package probe runs scout's step-by-step diagnostic against one MCP
// server. Each phase makes real requests, records what it observed, and
// emits findings with evidence; nothing is reported as passing without a
// request that showed it.
package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/auth"
	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/trace"
	"github.com/sebastienrousseau/scout/transport"
)

// Status of a finding or phase.
type Status string

// Finding statuses. Pass needs a request that showed the property; Info is
// an observation with no judgement; Warn is a deviation an agent can live
// with; Fail is one it cannot; Skip means the check did not run.
const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
	Info Status = "info"
)

// Severity of a failed or warned finding.
type Severity string

// Severities drive the score: Critical zeroes a category, Major costs 40,
// Minor 15. Note carries no deduction.
const (
	Critical Severity = "critical"
	Major    Severity = "major"
	Minor    Severity = "minor"
	Note     Severity = "info"
)

// Finding is one observed fact about the server.
type Finding struct {
	ID       string        `json:"id"`
	Phase    string        `json:"phase"`
	Title    string        `json:"title"`
	Status   Status        `json:"status"`
	Severity Severity      `json:"severity,omitempty"`
	Detail   string        `json:"detail,omitempty"`
	Evidence []string      `json:"evidence,omitempty"`
	Advice   string        `json:"advice,omitempty"`
	Duration Millis       `json:"duration_ms,omitempty"`
}

// PhaseResult groups the findings of one phase.
type PhaseResult struct {
	Name     string        `json:"name"`
	Title    string        `json:"title"`
	Status   Status        `json:"status"`
	Duration Millis       `json:"duration_ms"`
	Skipped  string        `json:"skipped,omitempty"`
	// Summary is the one-line, plain-language outcome of the phase.
	Summary  string    `json:"summary,omitempty"`
	Findings []Finding `json:"findings"`
}

// Options configures a run.
type Options struct {
	Endpoint   string
	Creds      *creds.Credentials
	Store      *creds.Store
	Recorder   *telemetry.Recorder
	HTTPClient *http.Client // base client; its Transport is wrapped by the recorder
	Version    string

	Policy       diagnostics.Policy
	// URLPolicy governs which discovered OAuth endpoints may be fetched or
	// sent credentials. The zero value is strict.
	URLPolicy auth.URLPolicy
	// AllowResourceMismatch continues past a protected-resource document
	// that does not name this endpoint.
	AllowResourceMismatch bool
	// SkipEraCheck suppresses the single server/discover that identifies
	// which generation of the protocol the server speaks. It exists because
	// that probe is a request reaching the server under test, and this
	// project treats an unavoidable new request as a breaking change.
	SkipEraCheck bool
	Samples      int
	Concurrency  int
	RPS          float64
	CallTimeout  time.Duration
	Seed         uint64
	FillOptional bool
	AllowLoad    bool
	MaxResources int
	MaxPrompts   int
	// ToolArgs overrides generated arguments per tool.
	ToolArgs map[string]map[string]any

	// Only and Skip select phases by name.
	Only []string
	Skip []string

	// Progress is called as phases start and findings land.
	Progress func(phase string, f *Finding)
	// PhaseDone is called with each phase's result, including skipped ones.
	PhaseDone func(PhaseResult)
}

// TokenInfo is what the auth phase learned about the token in use.
type TokenInfo struct {
	Type        string    `json:"type,omitempty"`
	Scope       string    `json:"scope,omitempty"`
	Requested   string    `json:"requested_scope,omitempty"`
	Expiry      time.Time `json:"expiry,omitempty"`
	Refreshable bool      `json:"refreshable"`
	Source      string    `json:"source,omitempty"`
}

// Session is the shared state across phases.
type Session struct {
	Opts      Options
	Started   time.Time
	TraceID   string
	URL       *url.URL
	Client    *scout.Client
	Transport http.RoundTripper // recorder-wrapped base
	// Bare is a transport to the endpoint with tracing and recording but
	// no credentials of any kind, for probes that must arrive unauthenticated.
	Bare *transport.Streamable

	// Reached is set once the endpoint answered an HTTP request at all.
	Reached      bool
	RequiresAuth bool
	Challenge    auth.Challenge
	Discovery    *scout.Discovery
	// Era is which generation of the protocol the server speaks.
	Era          *scout.Negotiation
	Token        *TokenInfo
	Init         *scout.InitializeResult
	SessionID    bool

	Tools     []scout.Tool
	Resources []scout.Resource
	Templates []scout.ResourceTemplate
	Prompts   []scout.Prompt

	ToolResults     []ToolResult
	ResourceResults []ResourceResult
	PromptResults   []PromptResult
	Perf            *PerfResult

	Results []PhaseResult
	blocked string
}

// Phase is one diagnostic step.
type Phase struct {
	Name  string
	Title string
	// Describe says in one line what the phase checks.
	Describe string
	Run      func(ctx context.Context, s *Session) []Finding
}

// Phases in execution order.
var Phases = []Phase{
	{"net", "Connectivity", "DNS, TCP and TLS", phaseNet},
	{"discovery", "Authorization", "How the server asks to be authorized", phaseDiscovery},
	{"auth", "Credentials", "Your credentials, and whether bad ones are refused", phaseAuth},
	{"handshake", "Handshake", "The MCP initialize exchange", phaseHandshake},
	{"protocol", "Protocol", "Behaviour on edge cases an agent will hit", phaseProtocol},
	{"catalog", "Catalog", "Tools, resources and prompts, as an agent reads them", phaseCatalog},
	{"execution", "Execution", "Safe calls, results checked against their contracts", phaseExecution},
	{"performance", "Performance", "Latency under repeat and parallel calls", phasePerformance},
	{"resilience", "Resilience", "Recovery from a lost session or an expired token", phaseResilience},
}

// PhaseNames lists the phase names in order.
func PhaseNames() []string {
	out := make([]string, len(Phases))
	for i, p := range Phases {
		out[i] = p.Name
	}
	return out
}

// Run executes the selected phases.
func Run(ctx context.Context, opts Options) (*Session, error) {
	if opts.Recorder == nil {
		opts.Recorder = telemetry.New()
	}
	if opts.Creds == nil {
		opts.Creds = &creds.Credentials{Mode: creds.ModeNone}
	}
	for _, sec := range opts.Creds.Secrets() {
		opts.Recorder.Redactor.Add(sec)
	}
	if opts.CallTimeout == 0 {
		opts.CallTimeout = 30 * time.Second
	}
	if opts.Samples == 0 {
		opts.Samples = 5
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 4
	}
	if opts.MaxResources == 0 {
		opts.MaxResources = 25
	}
	if opts.MaxPrompts == 0 {
		opts.MaxPrompts = 25
	}
	u, err := url.Parse(opts.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("endpoint %q is not an absolute URL", opts.Endpoint)
	}
	ctx = trace.Ensure(ctx)
	s := &Session{Opts: opts, Started: time.Now(), TraceID: trace.FromContext(ctx), URL: u}

	base := opts.HTTPClient
	if base == nil {
		base = &http.Client{Timeout: 60 * time.Second}
	}
	hc := *base
	hc.Transport = opts.Recorder.Wrap(base.Transport)
	s.Transport = hc.Transport

	pol := opts.URLPolicy
	if !pol.AllowPrivate && endpointIsPrivate(ctx, u) {
		// The operator pointed scout at a host inside their own network, so
		// an authorization server in the same place is expected rather than
		// suspicious. The guard still applies to a public endpoint naming an
		// internal one, which is the case that matters.
		pol.AllowPrivate = true
	}
	cfg := scout.Config{
		Endpoint: opts.Endpoint, HTTPClient: &hc,
		ClientInfo:            scout.Implementation{Name: "scout", Version: opts.Version},
		URLPolicy:             pol,
		AllowResourceMismatch: opts.AllowResourceMismatch,
	}
	opts.Creds.Apply(&cfg)
	client, err := scout.New(cfg)
	if err != nil {
		return nil, err
	}
	s.Client = client
	bare := hc
	bare.Transport = trace.RoundTripper{Base: hc.Transport}
	s.Bare = transport.New(opts.Endpoint, &bare)

	for _, p := range Phases {
		if !selected(p.Name, opts) {
			continue
		}
		pr := PhaseResult{Name: p.Name, Title: p.Title}
		start := time.Now()
		if s.blocked != "" && p.Name != "net" {
			pr.Status, pr.Skipped = Skip, s.blocked
		} else {
			if opts.Progress != nil {
				opts.Progress(p.Name, nil)
			}
			pctx := telemetry.WithPhase(ctx, p.Name, "")
			pr.Findings = p.Run(pctx, s)
			pr.Status = worst(pr.Findings)
			pr.Summary = summarize(p.Name, s, pr)
			for i := range pr.Findings {
				pr.Findings[i].Phase = p.Name
				if opts.Progress != nil {
					opts.Progress(p.Name, &pr.Findings[i])
				}
			}
		}
		pr.Duration = Millis(time.Since(start))
		s.Results = append(s.Results, pr)
		if opts.PhaseDone != nil {
			opts.PhaseDone(pr)
		}
		if ctx.Err() != nil {
			return s, ctx.Err()
		}
	}
	return s, nil
}

// endpointIsPrivate reports whether the endpoint the operator named is
// itself on a non-public address.
func endpointIsPrivate(ctx context.Context, u *url.URL) bool {
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if a.IP.IsLoopback() || a.IP.IsPrivate() || a.IP.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

func selected(name string, o Options) bool {
	if len(o.Only) > 0 && !slices.Contains(o.Only, name) {
		return false
	}
	return !slices.Contains(o.Skip, name)
}

func worst(fs []Finding) Status {
	if len(fs) == 0 {
		return Skip
	}
	rank := map[Status]int{Skip: 0, Info: 1, Pass: 2, Warn: 3, Fail: 4}
	w := Skip
	allSkip := true
	for _, f := range fs {
		if f.Status != Skip {
			allSkip = false
		}
		if rank[f.Status] > rank[w] {
			w = f.Status
		}
	}
	if allSkip {
		return Skip
	}
	if w == Info {
		return Pass
	}
	return w
}

// --- finding builder ------------------------------------------------------

type check struct {
	s     *Session
	f     Finding
	from  int
	start time.Time
}

func (s *Session) check(id, title string) *check {
	return &check{s: s, f: Finding{ID: id, Title: title}, from: s.Opts.Recorder.Count(), start: time.Now()}
}

func (c *check) done(st Status, sev Severity, detail, advice string) Finding {
	c.f.Status, c.f.Severity, c.f.Detail, c.f.Advice = st, sev, detail, advice
	c.f.Duration = Millis(time.Since(c.start))
	if to := c.s.Opts.Recorder.Count(); to > c.from {
		if to-c.from == 1 {
			c.f.Evidence = append(c.f.Evidence, fmt.Sprintf("req#%d", to))
		} else {
			c.f.Evidence = append(c.f.Evidence, fmt.Sprintf("req#%d-%d", c.from+1, to))
		}
	}
	return c.f
}

func (c *check) pass(detail string) Finding                  { return c.done(Pass, "", detail, "") }
func (c *check) info(detail string) Finding                  { return c.done(Info, "", detail, "") }
func (c *check) skip(reason string) Finding                  { return c.done(Skip, "", reason, "") }
func (c *check) warn(detail, advice string) Finding          { return c.done(Warn, Minor, detail, advice) }
func (c *check) fail(sev Severity, d, advice string) Finding { return c.done(Fail, sev, d, advice) }
func (c *check) ev(items ...string) *check                   { c.f.Evidence = append(c.f.Evidence, items...); return c }

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ms renders a duration in milliseconds. It accepts both time.Duration and
// Millis so call sites need not convert.
func ms[T ~int64](d T) string { return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond)) }

// Blocked returns the reason later phases were skipped, or "".
func (s *Session) Blocked() string { return s.blocked }

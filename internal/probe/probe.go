// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package probe runs scout's step-by-step diagnostic against one MCP
// server. Each phase makes real requests, records what it observed, and
// emits findings with evidence; nothing is reported as passing without a
// request that showed it.
package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
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
	ID       string   `json:"id"`
	Phase    string   `json:"phase"`
	Title    string   `json:"title"`
	Status   Status   `json:"status"`
	Severity Severity `json:"severity,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	Advice   string   `json:"advice,omitempty"`
	// DocURL addresses this check in the published inventory. A report
	// read six months after the run is the normal case, not the edge one,
	// and "protocol.malformed_json" is only self-explanatory to somebody
	// who already knows what it means.
	DocURL   string `json:"doc_url,omitempty"`
	Duration Millis `json:"duration_ms,omitempty"`
}

// DocBase is where the generated check inventory is published.
const DocBase = "https://scoutmcp.io/manual/checks/"

// DocURL addresses id in the published inventory.
//
// The fragment must match the anchor scripts/checkinventory writes into
// each table row; checks_doc_test.go fails the build when they diverge, so
// a link that ships in every report cannot quietly rot.
//
// A computed family id — auth.source.<field> — is addressed by its family
// rather than by the value, because the inventory documents the family and
// the value is whatever this particular server happened to have.
func DocURL(id string) string {
	if id == "" {
		return ""
	}
	for _, f := range DocFamilies {
		if strings.HasPrefix(id, f) {
			id = strings.TrimSuffix(f, ".")
			break
		}
	}
	return DocBase + "#check-" + strings.ReplaceAll(id, ".", "-")
}

// DocFamilies lists the checks whose id is built at run time, as the
// literal prefix everything after it is a value of.
//
// A family gets one row in the inventory, so every instance has to resolve
// to that row. checks_doc_test.go fails the build when the inventory grows
// a family this list does not know about — which is the only way a new
// family could ship with a dead link in every report that contains it.
var DocFamilies = []string{"auth.source."}

// PhaseResult groups the findings of one phase.
type PhaseResult struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	// Started is when the phase began. The loop already measured it to
	// compute Duration and threw it away; a trace exporter needs the
	// absolute instant, and laying phases end to end from the run's start
	// would be a guess dressed as a measurement.
	Started  time.Time `json:"started"`
	Duration Millis    `json:"duration_ms"`
	Skipped  string    `json:"skipped,omitempty"`
	// Summary is the one-line, plain-language outcome of the phase.
	Summary  string    `json:"summary,omitempty"`
	Findings []Finding `json:"findings"`
}

// Options configures a run.
type Options struct {
	Endpoint string
	// Stdio, when set, runs the server as a child process and speaks to it
	// over its pipes. Endpoint must be empty.
	//
	// Most MCP servers are programs rather than endpoints, so this is not
	// an alternative mode so much as the common one. What it costs is that
	// the phases which are about HTTP — reachability, authorization,
	// header conformance — have no subject, and each says so by name
	// rather than being left out of the report.
	Stdio      *scout.StdioConfig
	Creds      *creds.Credentials
	Store      *creds.Store
	Recorder   *telemetry.Recorder
	HTTPClient *http.Client // base client; its Transport is wrapped by the recorder
	Version    string

	Policy diagnostics.Policy
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
	// no credentials of any kind, for probes that must arrive
	// unauthenticated. Nil over stdio: there is no unauthenticated view of
	// a program the operator chose to run.
	Bare *transport.Streamable
	// Pipe is the child-process transport, or nil over HTTP. It is the one
	// place a phase asks which kind of run this is.
	Pipe *transport.Stdio

	// Reached is set once the endpoint answered an HTTP request at all.
	Reached      bool
	RequiresAuth bool
	Challenge    auth.Challenge
	Discovery    *scout.Discovery
	// Era is which generation of the protocol the server speaks.
	Era       *scout.Negotiation
	Token     *TokenInfo
	Init      *scout.InitializeResult
	SessionID bool

	Tools     []scout.Tool
	Resources []scout.Resource
	Templates []scout.ResourceTemplate
	Prompts   []scout.Prompt

	ToolResults     []ToolResult
	ResourceResults []ResourceResult
	PromptResults   []PromptResult
	Perf            *PerfResult
	// MRTR records every input_required result the run saw, so
	// protocol.mrtr can judge whether they were answerable. Observational
	// rather than probed: scout cannot make a server ask for input, and a
	// tool that needs it is the only thing that produces one.
	MRTR []MRTRObservation

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
	// RunStdio replaces Run when the server is a child process, for a
	// phase whose question still has an answer over a pipe but a different
	// subject. Nil means Run handles both.
	RunStdio func(ctx context.Context, s *Session) []Finding
	// StdioSkip is why this phase has no subject over a pipe. Set, the
	// phase is reported as skipped with this reason rather than omitted:
	// the difference between a report that is honest about what it did not
	// look at and one that reads as a clean result.
	StdioSkip string
}

// Phases in execution order.
var Phases = []Phase{
	{Name: "net", Title: "Connectivity", Describe: "DNS, TCP and TLS", Run: phaseNet, RunStdio: phaseNetStdio},
	{Name: "discovery", Title: "Authorization", Describe: "How the server asks to be authorized", Run: phaseDiscovery,
		StdioSkip: "a child process has no origin and no metadata to discover. " +
			"The trust decision was made when the operator chose which program to run, and scout cannot second-guess it from here"},
	{Name: "auth", Title: "Credentials", Describe: "Your credentials, and whether bad ones are refused", Run: phaseAuth,
		StdioSkip: "there is nothing to authenticate to. A pipe carries no bearer token, so there is also no wrong one to send " +
			"and no refusal to check; a server that needs a secret is given it in its arguments or its environment"},
	{Name: "handshake", Title: "Handshake", Describe: "The MCP initialize exchange", Run: phaseHandshake},
	{Name: "protocol", Title: "Protocol", Describe: "Behaviour on edge cases an agent will hit", Run: phaseProtocol},
	{Name: "catalog", Title: "Catalog", Describe: "Tools, resources and prompts, as an agent reads them", Run: phaseCatalog},
	{Name: "execution", Title: "Execution", Describe: "Safe calls, results checked against their contracts", Run: phaseExecution},
	{Name: "performance", Title: "Performance", Describe: "Latency under repeat and parallel calls", Run: phasePerformance},
	{Name: "resilience", Title: "Resilience", Describe: "Recovery from a lost session or an expired token", Run: phaseResilience,
		RunStdio: phaseResilienceStdio},
}

// PhaseTitle returns the human title of a phase, or the name itself when
// it is not one scout knows.
func PhaseTitle(name string) string {
	for _, p := range Phases {
		if p.Name == name {
			return p.Title
		}
	}
	return name
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
	ctx = trace.Ensure(ctx)

	if opts.Stdio != nil {
		if opts.Endpoint != "" {
			return nil, errors.New("a run has one target: an endpoint or a command, not both")
		}
		return runStdio(ctx, opts)
	}

	u, err := url.Parse(opts.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("endpoint %q is not an absolute URL", opts.Endpoint)
	}
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

	return s.runPhases(ctx)
}

// runStdio starts the server as a child process and runs the same phases
// against it.
//
// The process is this function's for its whole life: it is started here and
// reaped here, on every path out including a panic in a phase. A diagnostic
// that leaves a server running has done damage no report undoes, and the
// place to guarantee that is the one place that started it.
func runStdio(ctx context.Context, opts Options) (s *Session, err error) {
	s = &Session{Opts: opts, Started: time.Now(), TraceID: trace.FromContext(ctx)}

	cfg := *opts.Stdio
	cfg.Observe = s.recordPipe(s.command())
	client, cerr := scout.NewStdio(ctx, scout.Config{
		Stdio:      &cfg,
		ClientInfo: scout.Implementation{Name: "scout", Version: opts.Version},
	})
	if cerr != nil {
		return nil, cerr
	}
	s.Client = client
	pipe, ok := client.Stdio()
	if !ok {
		// Unreachable unless the client stops honouring Config.Stdio, and a
		// nil Pipe would panic in the first phase instead of saying so.
		_ = client.Close()
		return nil, errors.New("internal: a stdio client without a pipe")
	}
	s.Pipe = pipe
	var once sync.Once
	shutdown := func() { once.Do(func() { _ = client.Close() }) }
	defer shutdown()

	// Which generation the server speaks is settled before the handshake,
	// as it is over HTTP — but on the pipe rather than on a credential-free
	// transport, because there is no such thing here.
	s.settleEraStdio(ctx)

	res, rerr := s.runPhases(ctx)
	if rerr != nil {
		return res, rerr
	}
	// Two checks can only be made once the process is gone: whether it
	// stopped when its input closed, and whether anything it started
	// outlived it. So the shutdown happens here rather than on the defer,
	// and the resilience phase adopts what it found.
	shutdown()
	s.adoptCustody()
	return res, nil
}

// runPhases executes the selected phases against a prepared session. It is
// shared so that adding a transport cannot accidentally give one of them a
// different loop, a different skip rule or a different order.
func (s *Session) runPhases(ctx context.Context) (*Session, error) {
	opts := s.Opts
	for _, p := range Phases {
		if !selected(p.Name, opts) {
			continue
		}
		start := time.Now()
		pr := PhaseResult{Name: p.Name, Title: p.Title, Started: start}
		run := p.Run
		if s.overStdio() && p.RunStdio != nil {
			run = p.RunStdio
		}
		switch {
		// A blocked run skips the phases that would only produce
		// meaningless failures against a server that is not answering. The
		// exception over stdio is every phase with its own stdio
		// implementation: those look at the process scout started rather
		// than at the protocol, so they always have an answer — and a run
		// that got blocked is exactly when "the server wrote a banner to
		// stdout" or "the process exited" is the finding that explains
		// everything else.
		case s.blocked != "" && p.Name != "net" && !s.observesTheProcess(p):
			pr.Status, pr.Skipped = Skip, s.blocked
		case s.overStdio() && p.StdioSkip != "":
			pr.Status, pr.Skipped = Skip, p.StdioSkip
		default:
			if opts.Progress != nil {
				opts.Progress(p.Name, nil)
			}
			pctx := telemetry.WithPhase(ctx, p.Name, "")
			pr.Findings = run(pctx, s)
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
	c.f.Status, c.f.Severity, c.f.Detail, c.f.Advice = st, sev, oneLine(detail), advice
	c.f.DocURL = DocURL(c.f.ID)
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

// oneLine collapses whitespace in a detail.
//
// A detail is one line in every rendering scout produces, and some of what
// goes into one comes from the server: a validation library that returns a
// pretty-printed array puts its newlines straight through the terminal
// layout, the Markdown table and the JUnit message. Nothing writes a detail
// that means to be multi-line, so this is a normalisation rather than a
// truncation — the text is all still there.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}

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

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package engine is the one place a scout run is configured and started.
//
// Every surface — the CLI, the TUI, and the web UI — builds a RunSpec and
// calls Run. Nothing else configures a run, and no capability lives in a
// surface rather than under it: the moment one does, the surfaces are no
// longer peers and no amount of front-end work brings that back.
package engine

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout/auth"
	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/probe"
)

// RunSpec is the whole of a run's intent, in one serialisable value.
//
// It round-trips through JSON so the web UI can post exactly what the CLI
// would have assembled from flags. Secrets are the one exception: fields
// holding a credential value are marked json:"-" so a spec that crosses a
// network carries references to credentials, never the credentials. A
// surface that needs to supply a secret directly does it in-process.
type RunSpec struct {
	// Target is the MCP server under test.
	Target TargetSpec `json:"target"`
	// Creds says how to authenticate, by reference where it can.
	Creds CredSpec `json:"credentials"`
	// Policy decides which tools may be invoked.
	Policy PolicySpec `json:"policy"`
	// Pacing bounds how hard the server is exercised.
	Pacing PacingSpec `json:"pacing"`
	// Phases selects which parts of the diagnostic run.
	Phases PhaseSpec `json:"phases"`
	// Output says what to produce and where to put it.
	Output OutputSpec `json:"output"`
	// Version is the scout build identifier recorded in the report.
	Version string `json:"version,omitempty"`
}

// TargetSpec identifies the server under test.
type TargetSpec struct {
	// Endpoint is the Streamable HTTP URL.
	Endpoint string `json:"endpoint"`
}

// CredSpec mirrors the credential surface. Fields that hold a secret
// value are excluded from JSON; the *Env fields name an environment
// variable to read instead, which is how a spec crossing a process or a
// network boundary carries credentials safely.
type CredSpec struct {
	Mode string `json:"mode,omitempty"`

	Token    string `json:"-"`
	TokenEnv string `json:"token_env,omitempty"`

	// Headers are sent verbatim (API keys, tenant selectors). Excluded
	// from JSON because an API key is as sensitive as a token.
	Headers map[string]string `json:"-"`
	// Basic is user:password.
	Basic string `json:"-"`

	ClientID        string `json:"client_id,omitempty"`
	ClientSecret    string `json:"-"`
	ClientSecretEnv string `json:"client_secret_env,omitempty"`

	ClientMetadataURL string     `json:"client_metadata_url,omitempty"`
	Scope             string     `json:"scope,omitempty"`
	Params            url.Values `json:"params,omitempty"`
	TokenURL          string     `json:"token_url,omitempty"`
	AuthURL           string     `json:"auth_url,omitempty"`
	Resource          string     `json:"resource,omitempty"`
	RedirectPort      int        `json:"redirect_port,omitempty"`
	TokenAuthMethod   string     `json:"token_auth_method,omitempty"`
}

// PolicySpec decides what may be invoked, and which discovered endpoints
// scout is willing to talk to.
type PolicySpec struct {
	AllowMutations   bool     `json:"allow_mutations,omitempty"`
	AllowDestructive bool     `json:"allow_destructive,omitempty"`
	Only             []string `json:"only,omitempty"`
	Deny             []string `json:"deny,omitempty"`
	// ToolArgs overrides generated arguments, keyed by tool then field.
	ToolArgs map[string]map[string]any `json:"tool_args,omitempty"`

	// The three insecure_* fields each switch off a check that exists
	// because everything past the operator's own endpoint is chosen by the
	// server under test.
	AllowPlaintextAuth    bool `json:"insecure_allow_http_auth,omitempty"`
	AllowPrivateHosts     bool `json:"insecure_allow_private_hosts,omitempty"`
	AllowResourceMismatch bool `json:"allow_resource_mismatch,omitempty"`
	SkipEraCheck          bool `json:"skip_era_check,omitempty"`
}

// PacingSpec bounds the load a run places on the server.
type PacingSpec struct {
	Samples      int           `json:"samples,omitempty"`
	Concurrency  int           `json:"concurrency,omitempty"`
	RPS          float64       `json:"rps"`
	CallTimeout  time.Duration `json:"call_timeout_ns,omitempty"`
	Seed         uint64        `json:"seed,omitempty"`
	FillOptional bool          `json:"fill_optional,omitempty"`
	AllowLoad    bool          `json:"allow_load,omitempty"`
	MaxResources int           `json:"max_resources,omitempty"`
	MaxPrompts   int           `json:"max_prompts,omitempty"`
}

// PhaseSpec selects phases by name.
type PhaseSpec struct {
	Only []string `json:"only,omitempty"`
	Skip []string `json:"skip,omitempty"`
}

// Format is an output rendering.
type Format string

// The formats a report can be rendered as.
const (
	FormatText   Format = "text"
	FormatJSON   Format = "json"
	FormatMD     Format = "md"
	FormatNDJSON Format = "ndjson"
	// FormatHTML is one self-contained document: the file somebody opens
	// offline, the page scout serve shows, and — through its print
	// stylesheet — the PDF a board reads.
	FormatHTML Format = "html"
	// FormatSARIF and FormatJUnit exist so a run reaches the place a team
	// already looks: code scanning for one, the test panel for the other.
	// Neither is a richer report than JSON; both are the same findings in
	// the shape a specific reader will not accept a substitute for.
	FormatSARIF Format = "sarif"
	FormatJUnit Format = "junit"
)

// Formats lists every valid output format.
var Formats = []Format{FormatText, FormatJSON, FormatMD, FormatNDJSON, FormatHTML, FormatSARIF, FormatJUnit}

// Valid reports whether f is a format scout can render.
func (f Format) Valid() bool {
	for _, v := range Formats {
		if f == v {
			return true
		}
	}
	return false
}

// OutputSpec says what to produce.
type OutputSpec struct {
	Format Format `json:"format,omitempty"`
	// ReportDir, when set, receives report.{txt,md,json} plus telemetry.
	ReportDir string `json:"report_dir,omitempty"`
	// CaptureBodies records request and response bodies, redacted and
	// capped, in the telemetry.
	CaptureBodies bool `json:"capture_bodies,omitempty"`
	// WithEvents embeds every telemetry event in the JSON report.
	WithEvents bool `json:"with_events,omitempty"`
	// Verbose shows evidence references and info findings.
	Verbose bool `json:"verbose,omitempty"`
	// NoColor disables ANSI colour in the text rendering.
	NoColor bool `json:"no_color,omitempty"`
	// Interactive asks the surface to choose tools before the run. The
	// chosen names land in Policy.Only, so a run is reproducible from the
	// spec afterwards without asking again.
	Interactive bool `json:"interactive,omitempty"`
}

// Defaults are the values a run takes when a spec leaves them unset. They
// live here rather than in flag definitions so every surface gets the same
// ones, including a web client that sent a half-filled spec.
const (
	DefaultSamples      = 5
	DefaultConcurrency  = 4
	DefaultRPS          = 2
	DefaultCallTimeout  = 30 * time.Second
	DefaultSeed         = 1
	DefaultMaxResources = 25
	DefaultMaxPrompts   = 25
	DefaultRedirectPort = 8976
)

// WithDefaults returns a copy of s with unset values filled in.
func (s RunSpec) WithDefaults() RunSpec {
	if s.Creds.Mode == "" {
		s.Creds.Mode = string(creds.ModeAuto)
	}
	if s.Creds.RedirectPort == 0 {
		s.Creds.RedirectPort = DefaultRedirectPort
	}
	if s.Pacing.Samples == 0 {
		s.Pacing.Samples = DefaultSamples
	}
	if s.Pacing.Concurrency == 0 {
		s.Pacing.Concurrency = DefaultConcurrency
	}
	if s.Pacing.CallTimeout == 0 {
		s.Pacing.CallTimeout = DefaultCallTimeout
	}
	if s.Pacing.Seed == 0 {
		s.Pacing.Seed = DefaultSeed
	}
	if s.Pacing.MaxResources == 0 {
		s.Pacing.MaxResources = DefaultMaxResources
	}
	if s.Pacing.MaxPrompts == 0 {
		s.Pacing.MaxPrompts = DefaultMaxPrompts
	}
	if s.Output.Format == "" {
		s.Output.Format = FormatText
	}
	return s
}

// Validate reports why a spec cannot be run. It is the same check for
// every surface, so a web client gets the CLI's error rather than a
// different one.
func (s RunSpec) Validate() error {
	if strings.TrimSpace(s.Target.Endpoint) == "" {
		return errors.New("an endpoint is required")
	}
	u, err := url.Parse(s.Target.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("endpoint %q is not an absolute URL", s.Target.Endpoint)
	}
	if !s.Output.Format.Valid() {
		return fmt.Errorf("output format %q is not one of %v", s.Output.Format, Formats)
	}
	for _, name := range append(append([]string{}, s.Phases.Only...), s.Phases.Skip...) {
		if !knownPhase(name) {
			return fmt.Errorf("unknown phase %q; known phases are %s", name, strings.Join(probe.PhaseNames(), ", "))
		}
	}
	return s.credentials().Validate()
}

func knownPhase(name string) bool {
	for _, p := range probe.PhaseNames() {
		if p == name {
			return true
		}
	}
	return false
}

// credentials turns the spec's credential half into the resolved model,
// reading any environment variables it names.
func (s RunSpec) credentials() *creds.Credentials {
	c := &creds.Credentials{
		Mode:    creds.Mode(s.Creds.Mode),
		Headers: map[string]string{},
		Params:  url.Values{},
		Sources: map[string]string{},
	}
	c.SetFromFlag("token", s.Creds.Token)
	c.SetFromFlag("client-id", s.Creds.ClientID)
	c.SetFromFlag("client-secret", s.Creds.ClientSecret)
	c.SetFromFlag("basic", s.Creds.Basic)
	// An environment reference that names a missing variable is an error,
	// but Validate reports it; resolution here is best-effort so the
	// caller sees one error rather than two.
	_ = c.SetFromEnvName("token", s.Creds.TokenEnv)
	_ = c.SetFromEnvName("client-secret", s.Creds.ClientSecretEnv)
	c.FromEnv()
	for k, v := range s.Creds.Headers {
		c.Headers[k] = v
		c.Sources["header "+k] = "spec"
	}
	for k, vs := range s.Creds.Params {
		for _, v := range vs {
			c.Params.Add(k, v)
		}
	}
	c.ClientMetadataURL = s.Creds.ClientMetadataURL
	c.Scope = s.Creds.Scope
	c.TokenURL = s.Creds.TokenURL
	c.AuthURL = s.Creds.AuthURL
	c.Resource = s.Creds.Resource
	c.RedirectPort = s.Creds.RedirectPort
	c.TokenAuthMethod = s.Creds.TokenAuthMethod
	return c
}

// Credentials resolves the spec's credentials, reporting a named
// environment variable that is not set.
func (s RunSpec) Credentials() (*creds.Credentials, error) {
	c := s.credentials()
	if err := c.SetFromEnvName("token", s.Creds.TokenEnv); err != nil {
		return nil, err
	}
	if err := c.SetFromEnvName("client-secret", s.Creds.ClientSecretEnv); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// diagnosticsPolicy is the tool-invocation half of the policy.
func (s RunSpec) diagnosticsPolicy() diagnostics.Policy {
	return diagnostics.Policy{
		AllowMutations:   s.Policy.AllowMutations,
		AllowDestructive: s.Policy.AllowDestructive,
		Only:             s.Policy.Only,
		Deny:             s.Policy.Deny,
	}
}

// urlPolicy is the discovered-endpoint half.
func (s RunSpec) urlPolicy() auth.URLPolicy {
	return auth.URLPolicy{
		AllowHTTP:    s.Policy.AllowPlaintextAuth,
		AllowPrivate: s.Policy.AllowPrivateHosts,
	}
}

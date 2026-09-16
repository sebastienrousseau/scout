// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package report turns a probe session into a document: a scored,
// step-by-step account of what was observed, with the telemetry attached.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/internal/probe"
	"github.com/sebastienrousseau/scout/internal/telemetry"
)

// Report is the full diagnostic output.
type Report struct {
	Scout    Meta      `json:"scout"`
	Target   Target    `json:"target"`
	Started  time.Time    `json:"started"`
	Duration probe.Millis `json:"duration_ms"`
	TraceID  string `json:"trace_id"`

	Auth      AuthSummary         `json:"auth"`
	Server    *ServerInfo         `json:"server,omitempty"`
	Phases    []probe.PhaseResult `json:"phases"`
	Catalog   Catalog             `json:"catalog"`
	Execution Execution           `json:"execution"`
	Perf      *probe.PerfResult   `json:"performance,omitempty"`
	Score     Score               `json:"score"`
	// Blocked is the reason later phases were skipped, when the run did
	// not get through every phase.
	Blocked   string            `json:"blocked,omitempty"`
	Counts    Counts            `json:"counts"`
	Telemetry telemetry.Summary `json:"telemetry"`
	Events    []telemetry.Event `json:"events,omitempty"`
	Files     []string          `json:"files,omitempty"`
}

// SchemaVersion is the version of the JSON report format. It changes when a
// field is removed or its meaning changes, never when one is added, so a
// consumer can pin a major version and keep reading.
//
// 1: initial published format. Every *_ms field holds milliseconds.
const SchemaVersion = 1

// Meta identifies the scout build and the report format.
type Meta struct {
	Version string `json:"version"`
	// SchemaVersion lets a consumer detect a format it cannot read.
	SchemaVersion int `json:"schema_version"`
}

// Target is the server under test (secrets stripped).
type Target struct {
	Endpoint string `json:"endpoint"`
	Host     string `json:"host"`
	Scheme   string `json:"scheme"`
}

// AuthSummary is the credential and discovery summary.
type AuthSummary struct {
	Mode    string            `json:"mode"`
	Sources map[string]string `json:"sources,omitempty"`
	Reached bool              `json:"reached"`
	// Params are the extra token/authorization parameters sent (values are
	// operator-chosen identifiers, not secrets); Headers lists header names.
	Params       []string         `json:"params,omitempty"`
	Headers      []string         `json:"headers,omitempty"`
	Required     bool             `json:"required"`
	Issuer       string           `json:"issuer,omitempty"`
	PRMSource    string           `json:"prm_source,omitempty"`
	Registration string           `json:"registration,omitempty"`
	ClientID     string           `json:"client_id,omitempty"`
	Resource     string           `json:"resource,omitempty"`
	Token        *probe.TokenInfo `json:"token,omitempty"`
}

// ServerInfo is what initialize returned.
type ServerInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Protocol     string   `json:"protocol_version"`
	Capabilities []string `json:"capabilities"`
	Instructions int      `json:"instructions_chars"`
	Session      bool     `json:"session_id"`
}

// ToolSummary is one catalog row.
type ToolSummary struct {
	Name         string   `json:"name"`
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description"`
	ReadOnly     bool     `json:"read_only"`
	Destructive  bool     `json:"destructive"`
	Annotated    bool     `json:"annotated"`
	OutputSchema bool     `json:"output_schema"`
	Required     []string `json:"required_args,omitempty"`
}

// Catalog lists what the server exposes.
type Catalog struct {
	Tools     []ToolSummary            `json:"tools"`
	Resources []scout.Resource         `json:"resources,omitempty"`
	Templates []scout.ResourceTemplate `json:"resource_templates,omitempty"`
	Prompts   []scout.Prompt           `json:"prompts,omitempty"`
}

// Execution collects invocation results.
type Execution struct {
	Tools     []probe.ToolResult     `json:"tools"`
	Resources []probe.ResourceResult `json:"resources,omitempty"`
	Prompts   []probe.PromptResult   `json:"prompts,omitempty"`
}

// Counts totals findings by status.
type Counts struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
	Info int `json:"info"`
}

// Build assembles a report from a finished session.
func Build(s *probe.Session, version string, includeEvents bool) *Report {
	r := &Report{
		Scout:    Meta{Version: version, SchemaVersion: SchemaVersion},
		Target:   Target{Endpoint: s.Opts.Recorder.Redactor.URL(s.Opts.Endpoint), Host: s.URL.Host, Scheme: s.URL.Scheme},
		Started:  s.Started,
		Duration: probe.Millis(time.Since(s.Started)),
		TraceID:  s.TraceID,
		Phases:   s.Results,
		Perf:     s.Perf,
	}
	r.Auth = AuthSummary{Mode: string(s.Opts.Creds.Effective()), Sources: s.Opts.Creds.Sources, Reached: s.Reached, Required: s.RequiresAuth, Token: s.Token}
	r.Blocked = s.Blocked()
	for k, vs := range s.Opts.Creds.Params {
		for _, v := range vs {
			r.Auth.Params = append(r.Auth.Params, k+"="+v)
		}
	}
	sort.Strings(r.Auth.Params)
	for k := range s.Opts.Creds.Headers {
		r.Auth.Headers = append(r.Auth.Headers, k)
	}
	sort.Strings(r.Auth.Headers)
	if d := s.Discovery; d != nil {
		if d.Server != nil {
			r.Auth.Issuer = d.Server.Issuer
		}
		r.Auth.PRMSource = d.PRMSource
		r.Auth.Resource = d.Resource
		if d.Registration != nil {
			r.Auth.Registration = d.Registration.Method
			r.Auth.ClientID = d.Registration.ClientID
		}
	}
	if s.Init != nil {
		si := &ServerInfo{Name: s.Init.ServerInfo.Name, Version: s.Init.ServerInfo.Version, Protocol: s.Init.ProtocolVersion, Instructions: len(s.Init.Instructions), Session: s.SessionID}
		if s.Init.Capabilities.Tools != nil {
			si.Capabilities = append(si.Capabilities, "tools")
		}
		if s.Init.Capabilities.Resources != nil {
			si.Capabilities = append(si.Capabilities, "resources")
		}
		if s.Init.Capabilities.Prompts != nil {
			si.Capabilities = append(si.Capabilities, "prompts")
		}
		if s.Init.Capabilities.Logging != nil {
			si.Capabilities = append(si.Capabilities, "logging")
		}
		r.Server = si
	}
	for _, t := range s.Tools {
		r.Catalog.Tools = append(r.Catalog.Tools, ToolSummary{Name: t.Name, Title: t.Title, Description: t.Description, ReadOnly: t.IsReadOnly(), Destructive: t.IsDestructive(), Annotated: t.Annotations != nil, OutputSchema: len(t.OutputSchema) > 0, Required: required(t)})
	}
	r.Catalog.Resources, r.Catalog.Templates, r.Catalog.Prompts = s.Resources, s.Templates, s.Prompts
	r.Execution = Execution{Tools: s.ToolResults, Resources: s.ResourceResults, Prompts: s.PromptResults}
	for _, p := range s.Results {
		for _, f := range p.Findings {
			switch f.Status {
			case probe.Pass:
				r.Counts.Pass++
			case probe.Warn:
				r.Counts.Warn++
			case probe.Fail:
				r.Counts.Fail++
			case probe.Skip:
				r.Counts.Skip++
			case probe.Info:
				r.Counts.Info++
			}
		}
	}
	r.Score = ComputeScore(s.Results)
	r.Telemetry = s.Opts.Recorder.Summary()
	if includeEvents {
		r.Events = s.Opts.Recorder.Events()
	}
	return r
}

func required(t scout.Tool) []string {
	var s struct {
		Required []string `json:"required"`
	}
	_ = jsonUnmarshal(t.InputSchema, &s)
	sort.Strings(s.Required)
	return s.Required
}

// Failures returns fail findings ordered by severity, for the summary.
func (r *Report) Failures() []probe.Finding {
	var out []probe.Finding
	for _, p := range r.Phases {
		for _, f := range p.Findings {
			if f.Status == probe.Fail {
				out = append(out, f)
			}
		}
	}
	rank := map[probe.Severity]int{probe.Critical: 0, probe.Major: 1, probe.Minor: 2, probe.Note: 3}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Severity] < rank[out[j].Severity] })
	return out
}

// Warnings returns warn findings.
func (r *Report) Warnings() []probe.Finding {
	var out []probe.Finding
	for _, p := range r.Phases {
		for _, f := range p.Findings {
			if f.Status == probe.Warn {
				out = append(out, f)
			}
		}
	}
	return out
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// fmtMS renders a duration for a human. It is generic over time.Duration
// and probe.Millis, which share an underlying int64 of nanoseconds.
func fmtMS[T ~int64](v T) string {
	d := time.Duration(v)
	if d == 0 {
		return "-"
	}
	if d >= time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
}

// NextSteps returns the advice attached to failed and warned findings,
// failures first by severity, each once, so a reader knows what to change
// before anything else.
func (r *Report) NextSteps() []probe.Finding {
	seen := map[string]bool{}
	var out []probe.Finding
	for _, f := range append(r.Failures(), r.Warnings()...) {
		if f.Advice == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		out = append(out, f)
	}
	return out
}

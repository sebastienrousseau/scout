// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package otlp exports a finished run as OpenTelemetry traces.
//
// It speaks OTLP/HTTP with the JSON encoding rather than protobuf. The wire
// format is specified either way, and JSON is reachable from encoding/json,
// so a tool whose argument is an auditable dependency graph does not take on
// the protobuf runtime and the OpenTelemetry SDK to emit spans it already
// has every field for. Every collector that accepts OTLP/HTTP accepts JSON
// on the same endpoint.
//
// The shape of the trace is the shape of the run: a root span for the whole
// diagnostic, a span per phase, and a span per HTTP request parented to the
// phase that made it. Findings become span events on their phase, so a
// failure is visible in the trace at the point it was observed rather than
// only in the report.
package otlp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout/internal/probe"
	"github.com/sebastienrousseau/scout/internal/telemetry"
)

// Exporter posts a run's spans to an OTLP/HTTP endpoint.
type Exporter struct {
	// Endpoint is the collector's trace endpoint. A URL with no path is
	// given the conventional /v1/traces, because that is what every
	// collector serves and getting it wrong produces a 404 nobody reads.
	Endpoint string
	// Headers are sent on the export request, for a collector that wants
	// an API key or a tenant id.
	Headers map[string]string
	// Client is the HTTP client to use; nil means a client with Timeout.
	Client *http.Client
	// Timeout bounds the export when Client is nil.
	Timeout time.Duration
	// ServiceName identifies scout to the collector.
	ServiceName string
	// ServiceVersion is scout's version.
	ServiceVersion string
}

// Run is the subset of a finished run the exporter needs.
//
// It takes this rather than *report.Report so that internal/report does not
// become a dependency of every future consumer of traces, and so the
// exporter can be tested without building a whole report.
type Run struct {
	TraceID    string
	Endpoint   string
	Host       string
	Started    time.Time
	Duration   time.Duration
	Phases     []probe.PhaseResult
	Events     []telemetry.Event
	Failed     int
	Warned     int
	ScoreTotal float64
}

// Export sends the run. A collector that is unreachable or unhappy returns
// an error; it is the caller's decision whether that should fail a run, and
// scout's answer is that it must not — a diagnostic that cannot reach its
// telemetry backend still produced a diagnostic.
func (e Exporter) Export(ctx context.Context, r Run) error {
	if e.Endpoint == "" {
		return nil
	}
	payload, err := json.Marshal(e.build(r))
	if err != nil {
		return fmt.Errorf("encoding spans: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tracesURL(e.Endpoint), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.Headers {
		req.Header.Set(k, v)
	}

	cl := e.Client
	if cl == nil {
		to := e.Timeout
		if to <= 0 {
			to = 10 * time.Second
		}
		cl = &http.Client{Timeout: to}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("exporting spans: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("collector returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// tracesURL appends the conventional path when the caller gave only a host.
func tracesURL(endpoint string) string {
	trimmed := strings.TrimRight(endpoint, "/")
	if strings.Contains(strings.TrimPrefix(strings.TrimPrefix(trimmed, "https://"), "http://"), "/") {
		return endpoint
	}
	return trimmed + "/v1/traces"
}

// build assembles the OTLP payload.
func (e Exporter) build(r Run) exportTraceServiceRequest {
	traceID := normaliseTraceID(r.TraceID)
	rootID := newSpanID()

	end := r.Started.Add(r.Duration)
	spans := []span{{
		TraceID:           traceID,
		SpanID:            rootID,
		Name:              "scout check",
		Kind:              spanKindClient,
		StartTimeUnixNano: unixNano(r.Started),
		EndTimeUnixNano:   unixNano(end),
		Attributes: attrs(
			str("scout.endpoint", r.Endpoint),
			str("server.address", r.Host),
			integer("scout.findings.failed", int64(r.Failed)),
			integer("scout.findings.warned", int64(r.Warned)),
			double("scout.score", r.ScoreTotal),
		),
		Status: statusFor(r.Failed == 0, fmt.Sprintf("%d failing finding(s)", r.Failed)),
	}}

	// A phase span per phase, and the id kept so its requests can point at
	// it. A request whose phase is unknown hangs off the root rather than
	// being dropped: an unattributed request is still a request that was
	// made, and losing it would make the trace quietly incomplete.
	phaseSpan := map[string]string{}
	for _, p := range r.Phases {
		id := newSpanID()
		phaseSpan[p.Name] = id
		start := p.Started
		if start.IsZero() {
			start = r.Started
		}
		sp := span{
			TraceID:           traceID,
			SpanID:            id,
			ParentSpanID:      rootID,
			Name:              "phase " + p.Name,
			Kind:              spanKindInternal,
			StartTimeUnixNano: unixNano(start),
			EndTimeUnixNano:   unixNano(start.Add(time.Duration(p.Duration))),
			Attributes: attrs(
				str("scout.phase", p.Name),
				str("scout.phase.title", p.Title),
				str("scout.phase.status", string(p.Status)),
				str("scout.phase.summary", p.Summary),
			),
			Status: statusFor(p.Status != probe.Fail, p.Summary),
		}
		sp.Attributes = append(sp.Attributes, attrs(str("scout.phase.skipped", p.Skipped))...)
		for _, f := range p.Findings {
			sp.Events = append(sp.Events, spanEvent{
				TimeUnixNano: unixNano(start),
				Name:         f.ID,
				Attributes: attrs(
					str("scout.finding.status", string(f.Status)),
					str("scout.finding.severity", string(f.Severity)),
					str("scout.finding.title", f.Title),
					str("scout.finding.detail", f.Detail),
					str("scout.finding.advice", f.Advice),
					str("scout.finding.doc_url", f.DocURL),
					str("scout.finding.evidence", strings.Join(f.Evidence, ",")),
				),
			})
		}
		spans = append(spans, sp)
	}

	for _, ev := range r.Events {
		parent := phaseSpan[ev.Phase]
		if parent == "" {
			parent = rootID
		}
		start := ev.Time
		sp := span{
			TraceID:           traceID,
			SpanID:            newSpanID(),
			ParentSpanID:      parent,
			Name:              ev.Method + " " + ev.URL,
			Kind:              spanKindClient,
			StartTimeUnixNano: unixNano(start),
			EndTimeUnixNano:   unixNano(start.Add(ev.Timings.Total)),
			Attributes: attrs(
				str("http.request.method", ev.Method),
				str("url.full", ev.URL),
				integer("http.response.status_code", int64(ev.Status)),
				str("server.address", ev.Remote),
				str("scout.phase", ev.Phase),
				str("scout.label", ev.Label),
				boolean("http.connection.reused", ev.ReusedConn),
				integer("http.request.body.size", ev.RequestBytes),
				integer("http.response.body.size", ev.ResponseBytes),
				durationMs("scout.timing.dns_ms", ev.Timings.DNS),
				durationMs("scout.timing.connect_ms", ev.Timings.Connect),
				durationMs("scout.timing.tls_ms", ev.Timings.TLS),
				durationMs("scout.timing.ttfb_ms", ev.Timings.TTFB),
			),
			Status: statusFor(ev.Error == "" && ev.Status < 400, ev.Error),
		}
		if ev.RPC != nil {
			sp.Attributes = append(sp.Attributes, attrs(
				str("rpc.method", ev.RPC.Method),
				integer("rpc.jsonrpc.error_code", int64(ev.RPC.ErrorCode)),
				str("rpc.jsonrpc.error_message", ev.RPC.ErrorMessage),
			)...)
		}
		spans = append(spans, sp)
	}

	return exportTraceServiceRequest{ResourceSpans: []resourceSpans{{
		Resource: resource{Attributes: attrs(
			str("service.name", e.serviceName()),
			str("service.version", e.ServiceVersion),
			str("telemetry.sdk.name", "scout"),
			str("telemetry.sdk.language", "go"),
		)},
		ScopeSpans: []scopeSpans{{
			Scope: scope{Name: "github.com/sebastienrousseau/scout", Version: e.ServiceVersion},
			Spans: spans,
		}},
	}}}
}

func (e Exporter) serviceName() string {
	if e.ServiceName != "" {
		return e.ServiceName
	}
	return "scout"
}

// normaliseTraceID returns id as the 32 lowercase hex characters OTLP
// requires. scout's own trace ids are already that; anything else is padded
// or replaced rather than sent as a value a collector will reject outright.
func normaliseTraceID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if len(id) == 32 {
		if _, err := hex.DecodeString(id); err == nil {
			return id
		}
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.Repeat("0", 32)
	}
	return hex.EncodeToString(b[:])
}

func newSpanID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.Repeat("0", 16)
	}
	return hex.EncodeToString(b[:])
}

// unixNano renders an instant the way proto3 JSON renders a uint64: as a
// decimal string. A number would lose precision in any consumer that parses
// JSON numbers as float64, which is most of them.
func unixNano(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return strconv.FormatInt(t.UnixNano(), 10)
}

func statusFor(ok bool, message string) *status {
	if ok {
		return &status{Code: statusCodeOK}
	}
	return &status{Code: statusCodeError, Message: message}
}

// attrs drops the attributes whose value was never set, so a span does not
// carry a dozen empty strings.
func attrs(in ...*keyValue) []keyValue {
	out := make([]keyValue, 0, len(in))
	for _, kv := range in {
		if kv != nil {
			out = append(out, *kv)
		}
	}
	return out
}

func str(k, v string) *keyValue {
	if v == "" {
		return nil
	}
	return &keyValue{Key: k, Value: anyValue{StringValue: &v}}
}

func integer(k string, v int64) *keyValue {
	if v == 0 {
		return nil
	}
	s := strconv.FormatInt(v, 10)
	return &keyValue{Key: k, Value: anyValue{IntValue: &s}}
}

func double(k string, v float64) *keyValue {
	return &keyValue{Key: k, Value: anyValue{DoubleValue: &v}}
}

func boolean(k string, v bool) *keyValue {
	return &keyValue{Key: k, Value: anyValue{BoolValue: &v}}
}

func durationMs(k string, d time.Duration) *keyValue {
	if d == 0 {
		return nil
	}
	return double(k, float64(d)/float64(time.Millisecond))
}

// --- the OTLP/HTTP JSON shape ---------------------------------------------
//
// Hand-written for the same reason the SARIF model is: this is a small,
// stable corner of a large schema, and generating it would put a report
// format behind somebody else's release cadence.
//
// The proto3 JSON mapping is what makes the types look odd: a uint64 is a
// decimal string, an int64 attribute value is a string, and a trace id is
// lowercase hex rather than base64 because the OTLP JSON specification says
// so explicitly.

type exportTraceServiceRequest struct {
	ResourceSpans []resourceSpans `json:"resourceSpans"`
}

type resourceSpans struct {
	Resource   resource     `json:"resource"`
	ScopeSpans []scopeSpans `json:"scopeSpans"`
}

type resource struct {
	Attributes []keyValue `json:"attributes,omitempty"`
}

type scopeSpans struct {
	Scope scope  `json:"scope"`
	Spans []span `json:"spans"`
}

type scope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

const (
	spanKindInternal = 1
	spanKindClient   = 3

	statusCodeOK    = 1
	statusCodeError = 2
)

type span struct {
	TraceID           string      `json:"traceId"`
	SpanID            string      `json:"spanId"`
	ParentSpanID      string      `json:"parentSpanId,omitempty"`
	Name              string      `json:"name"`
	Kind              int         `json:"kind"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
	Attributes        []keyValue  `json:"attributes,omitempty"`
	Events            []spanEvent `json:"events,omitempty"`
	Status            *status     `json:"status,omitempty"`
}

type spanEvent struct {
	TimeUnixNano string     `json:"timeUnixNano"`
	Name         string     `json:"name"`
	Attributes   []keyValue `json:"attributes,omitempty"`
}

type status struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type anyValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

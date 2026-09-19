// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package attest turns a finished report into a signed-able, portable
// statement about one MCP server.
//
// The reason this exists rather than "just use the JSON report" is that the
// report is a document and this is a claim. A document is read by a person; a
// claim is verified by a machine that was not present when the run happened —
// a gateway deciding whether to route to a server, a registry deciding what
// to display, an auditor deciding whether a control was met in March.
//
// It is an in-toto statement, so it slots into a pipeline enterprises are
// already being forced to build: the same envelope as SLSA provenance and an
// SBOM, the same signing, the same verification. Owning a predicate type is
// more durable than owning a product category, because formats outlive both.
//
// Four properties are deliberate, and each is a constraint rather than a
// nice-to-have:
//
//   - It states what it was judged against. A verdict with no basis has no
//     shelf life: "82/100" means nothing in 2029 unless the rubric and the
//     specification revision are attached to it.
//   - The subject digest is over a target descriptor, and the statement says
//     so. A digest that looked like an artifact hash while covering a URL
//     would be the kind of lie that survives review.
//   - It carries the verdict for every check that ran, not only the failures.
//     A consumer cannot otherwise tell "checked and fine" from "not checked",
//     which for a conformance tool is the whole difference.
//   - It verifies offline. A gateway must never have to call scout to trust a
//     statement scout produced.
package attest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout/internal/probe"
	"github.com/sebastienrousseau/scout/internal/report"
)

// StatementType is the in-toto statement envelope this produces.
const StatementType = "https://in-toto.io/Statement/v1"

// PredicateType identifies a scout MCP evaluation.
//
// It is versioned in the URL rather than in a field, which is the in-toto
// convention: a consumer that does not recognise the type must not guess at
// the contents, and a field would invite exactly that guess.
const PredicateType = "https://scoutmcp.io/attestation/mcp-evaluation/v1"

// SubjectKindDescriptor is what the subject digest covers.
//
// An MCP server is a running service, not a file, so there is no artifact to
// hash. The digest is over a canonical descriptor of the target — the
// transport and the address or command — which identifies *which server this
// statement is about* and nothing more. Recording that in the predicate is
// the difference between a useful identifier and a misleading one.
const SubjectKindDescriptor = "mcp-target-descriptor"

// Statement is the in-toto envelope.
type Statement struct {
	Type          string     `json:"_type"`
	Subject       []Subject  `json:"subject"`
	PredicateType string     `json:"predicateType"`
	Predicate     Evaluation `json:"predicate"`
}

// Subject is what the statement is about.
type Subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// Evaluation is the scout predicate.
type Evaluation struct {
	// SubjectKind says what Subject.Digest covers. See
	// SubjectKindDescriptor: without this a reader would reasonably assume
	// an artifact hash.
	SubjectKind string `json:"subjectKind"`
	// Target is the server that was evaluated.
	Target Target `json:"target"`
	// JudgedAgainst is what the verdicts mean. A statement without it is
	// not interpretable later, so Validate refuses one.
	JudgedAgainst Basis `json:"judgedAgainst"`
	// Instrument is what produced the statement.
	Instrument Instrument `json:"instrument"`
	// RanAt is when the run started, and Took how long it lasted. A
	// verdict about a live service is a verdict about a moment.
	RanAt time.Time `json:"ranAt"`
	Took  string    `json:"took"`
	// Verdicts is every check that ran, passes included.
	Verdicts []Verdict `json:"verdicts"`
	// Counts and Score are the summary a policy engine gates on.
	Counts Counts `json:"counts"`
	Score  *Score `json:"score,omitempty"`
	// Blocked is why the run stopped early, when it did. A statement from a
	// blocked run covers less than a complete one, and a consumer has to be
	// able to tell.
	Blocked string `json:"blocked,omitempty"`
	// TraceID ties the statement to the telemetry the run recorded, for
	// anyone who kept it.
	TraceID string `json:"traceId,omitempty"`
}

// Target identifies the evaluated server without disclosing credentials.
type Target struct {
	// Transport is "http" or "stdio".
	Transport string `json:"transport"`
	// Endpoint is the URL, or the command line for a child process.
	Endpoint string `json:"endpoint"`
	// Server is what the server said it was, when it said anything.
	Server *ServerIdentity `json:"server,omitempty"`
}

// ServerIdentity is the server's own claim about itself.
type ServerIdentity struct {
	Name     string `json:"name,omitempty"`
	Version  string `json:"version,omitempty"`
	Protocol string `json:"protocolVersion,omitempty"`
}

// Basis is what the verdicts were judged against.
type Basis struct {
	// SpecRevision is the MCP revision the server negotiated.
	SpecRevision string `json:"specRevision,omitempty"`
	// Rubric is the version of the scoring rubric behind Score. It is
	// required whenever Score is present: a number with no rubric is not
	// comparable to any other number.
	Rubric string `json:"rubric,omitempty"`
	// CheckInventory is the version of the check catalogue the ids come
	// from, so an id that is later renamed can still be resolved.
	CheckInventory string `json:"checkInventory,omitempty"`
}

// Instrument is the tool that produced the statement.
type Instrument struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// SchemaVersion is the report format the verdicts were derived from.
	SchemaVersion int `json:"reportSchemaVersion"`
}

// Verdict is one check's outcome.
type Verdict struct {
	ID       string `json:"id"`
	Phase    string `json:"phase"`
	Status   string `json:"status"`
	Severity string `json:"severity,omitempty"`
	// Evidence are the recorded-request references that produced the
	// verdict, carried verbatim from the report.
	Evidence []string `json:"evidence,omitempty"`
	// Doc addresses the check in the published inventory, so a consumer can
	// explain a verdict without shipping scout's prose.
	Doc string `json:"doc,omitempty"`
}

// Counts totals the verdicts by status.
type Counts struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
	Info int `json:"info"`
}

// Score is the rating, carried only with the rubric that produced it.
type Score struct {
	Total    float64            `json:"total"`
	Grade    string             `json:"grade"`
	Assessed int                `json:"categoriesAssessed"`
	Of       int                `json:"categoriesTotal"`
	By       map[string]float64 `json:"byCategory,omitempty"`
}

// From builds a statement from a finished report.
//
// It reads and never writes: the report is the run's output and this is a
// second rendering of it, so a statement can always be regenerated from a
// kept report and compared.
func From(r *report.Report) (*Statement, error) {
	if r == nil {
		return nil, fmt.Errorf("attest: no report")
	}

	t := Target{
		Transport: transportOf(r),
		Endpoint:  r.Target.Endpoint,
	}
	if r.Server != nil {
		t.Server = &ServerIdentity{
			Name:     r.Server.Name,
			Version:  r.Server.Version,
			Protocol: r.Server.Protocol,
		}
	}

	ev := Evaluation{
		SubjectKind: SubjectKindDescriptor,
		Target:      t,
		JudgedAgainst: Basis{
			SpecRevision:   protocolOf(r),
			Rubric:         report.RubricVersion,
			CheckInventory: report.CheckInventoryVersion,
		},
		Instrument: Instrument{
			Name:          "scout",
			Version:       r.Scout.Version,
			SchemaVersion: r.Scout.SchemaVersion,
		},
		RanAt:   r.Started,
		Took:    time.Duration(r.Duration).Round(time.Millisecond).String(),
		Blocked: r.Blocked,
		TraceID: r.TraceID,
		Counts: Counts{
			Pass: r.Counts.Pass, Warn: r.Counts.Warn, Fail: r.Counts.Fail,
			Skip: r.Counts.Skip, Info: r.Counts.Info,
		},
	}

	for _, p := range r.Phases {
		for _, f := range p.Findings {
			ev.Verdicts = append(ev.Verdicts, Verdict{
				ID:       f.ID,
				Phase:    phaseOf(f, p),
				Status:   string(f.Status),
				Severity: string(f.Severity),
				Evidence: f.Evidence,
				Doc:      f.DocURL,
			})
		}
	}
	// Sorted by id, so two statements about the same run are byte-identical
	// and a diff between two runs is a diff about the server.
	sort.SliceStable(ev.Verdicts, func(i, j int) bool { return ev.Verdicts[i].ID < ev.Verdicts[j].ID })

	// The score travels only when a category was actually assessed. A run
	// that reached nothing has no rating, and publishing 0/100 for it would
	// read as a verdict rather than an absence.
	if r.Score.Assessed > 0 {
		s := &Score{
			Total: r.Score.Total, Grade: r.Score.Grade,
			Assessed: r.Score.Assessed, Of: r.Score.Of,
			By: map[string]float64{},
		}
		for _, c := range r.Score.Categories {
			if c.Assessed {
				s.By[c.Name] = c.Score
			}
		}
		ev.Score = s
	}

	name := descriptor(t)
	st := &Statement{
		Type:          StatementType,
		PredicateType: PredicateType,
		Predicate:     ev,
		Subject: []Subject{{
			Name:   t.Endpoint,
			Digest: map[string]string{"sha256": digest(name)},
		}},
	}
	if err := st.Validate(); err != nil {
		return nil, err
	}
	return st, nil
}

// descriptor is the canonical string the subject digest covers.
//
// Canonical means two runs against the same server produce the same digest
// and two different servers never collide, which is the whole job: it is an
// identifier, not an integrity check over bytes nobody has.
func descriptor(t Target) string {
	return t.Transport + "\n" + strings.TrimSpace(t.Endpoint)
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// transportOf derives the transport from the report.
//
// It reads the scheme rather than a dedicated field because the report does
// not carry one yet — the stdio work adds Target.Transport, and when that
// lands this becomes a direct read with the scheme as the fallback for a
// statement generated from an older report. Deriving it is not optional: an
// HTTP run and a stdio run do not contain the same checks, so a consumer
// comparing two statements has to be told which kind each one is.
func transportOf(r *report.Report) string {
	switch {
	case r.Target.Scheme == "stdio":
		return "stdio"
	case r.Target.Scheme != "":
		return "http"
	default:
		return ""
	}
}

func protocolOf(r *report.Report) string {
	if r.Server != nil {
		return r.Server.Protocol
	}
	return ""
}

// phaseOf prefers the finding's own phase and falls back to the phase that
// contains it, because a finding assembled by hand in a test may not have one.
func phaseOf(f probe.Finding, p probe.PhaseResult) string {
	if f.Phase != "" {
		return f.Phase
	}
	return p.Name
}

// Marshal renders the statement as the JSON that gets signed.
//
// Indented on purpose: an attestation is read by people during an incident
// far more often than anyone expects, and the signature covers the bytes
// either way.
func (s *Statement) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

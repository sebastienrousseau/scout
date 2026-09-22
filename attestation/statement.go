// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: Apache-2.0

// Package attestation is the scout MCP evaluation attestation: the in-toto
// statement a run produces about one MCP server, and everything needed to
// read and verify one offline.
//
// It is licensed Apache-2.0, unlike the rest of scout, and imports only the
// standard library, so a gateway, registry or CI system can embed it to
// verify a statement without taking on the engine's GPL (ADR 0011). The
// JSON Schema in spec/attestation is generated from these types. Producing a
// statement from a report is scout's job and lives in the engine; checking
// one is anybody's.
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
package attestation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// StatementType is the in-toto statement envelope.
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

// SubjectFor returns the one subject a statement about t carries: its
// endpoint as the name and the digest of its canonical descriptor.
//
// Exported so that anything producing a statement names and digests the
// target exactly as Validate and Covers recompute it; a producer that
// derived the digest its own way would write statements that do not verify.
func SubjectFor(t Target) Subject {
	return Subject{
		Name:   t.Endpoint,
		Digest: map[string]string{"sha256": digest(descriptor(t))},
	}
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

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: Apache-2.0

package attestation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests use only this package and the standard library, so they move
// with it when it leaves scout's repository. The statements are written by
// hand for that reason: building one from a report is the engine's job.

func valid() *Statement {
	t := Target{Transport: "http", Endpoint: "https://mcp.example.com/mcp"}
	return &Statement{
		Type:          StatementType,
		PredicateType: PredicateType,
		Subject:       []Subject{SubjectFor(t)},
		Predicate: Evaluation{
			SubjectKind:   SubjectKindDescriptor,
			Target:        t,
			JudgedAgainst: Basis{SpecRevision: "2026-07-28", Rubric: "1", CheckInventory: "1"},
			Instrument:    Instrument{Name: "scout", Version: "0.0.3", SchemaVersion: 1},
			RanAt:         time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC),
			Took:          "3.2s",
			Verdicts: []Verdict{
				{ID: "auth.unauthenticated_tools", Phase: "auth", Status: "pass"},
				{ID: "protocol.origin", Phase: "protocol", Status: "fail", Severity: "major", Evidence: []string{"req#12"}},
			},
			Counts: Counts{Pass: 1, Fail: 1},
			Score:  &Score{Total: 88, Grade: "B", Assessed: 6, Of: 6},
		},
	}
}

func TestAWellFormedStatementValidates(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestValidateNamesEveryProblem. Each mutation breaks one property the
// format guarantees; the error must say which.
func TestValidateNamesEveryProblem(t *testing.T) {
	for want, mutate := range map[string]func(s *Statement){
		"_type is":                  func(s *Statement) { s.Type = "x" },
		"predicateType is":          func(s *Statement) { s.PredicateType = "x" },
		"no subject":                func(s *Statement) { s.Subject = nil },
		"2 subjects":                func(s *Statement) { s.Subject = append(s.Subject, s.Subject[0]) },
		"subject has no name":       func(s *Statement) { s.Subject[0].Name = " " },
		"named":                     func(s *Statement) { s.Subject[0].Name = "https://trusted.example.com/mcp" },
		"no sha256 digest":          func(s *Statement) { s.Subject[0].Digest = map[string]string{} },
		"does not cover the target": func(s *Statement) { s.Subject[0].Digest["sha256"] = strings.Repeat("0", 64) },
		"subjectKind is":            func(s *Statement) { s.Predicate.SubjectKind = "artifact" },
		"no endpoint":               func(s *Statement) { s.Predicate.Target.Endpoint = "" },
		"no transport":              func(s *Statement) { s.Predicate.Target.Transport = "" },
		"unknown transport":         func(s *Statement) { s.Predicate.Target.Transport = "carrier-pigeon" },
		"not identified":            func(s *Statement) { s.Predicate.Instrument.Version = "" },
		"no run time":               func(s *Statement) { s.Predicate.RanAt = time.Time{} },
		"no verdicts":               func(s *Statement) { s.Predicate.Verdicts = nil; s.Predicate.Counts = Counts{} },
		"checkInventory is empty":   func(s *Statement) { s.Predicate.JudgedAgainst.CheckInventory = "" },
		"no rubric version":         func(s *Statement) { s.Predicate.JudgedAgainst.Rubric = "" },
		"has no id":                 func(s *Statement) { s.Predicate.Verdicts[0].ID = "" },
		"has status":                func(s *Statement) { s.Predicate.Verdicts[0].Status = "probably" },
		"counts disagree":           func(s *Statement) { s.Predicate.Counts.Pass = 7 },
	} {
		t.Run(want, func(t *testing.T) {
			s := valid()
			mutate(s)
			err := s.Validate()
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want it to mention %q", err, want)
			}
		})
	}
}

// TestEveryStatusIsCounted. Counts are what a policy gates on, so each
// status has to land in its own bucket.
func TestEveryStatusIsCounted(t *testing.T) {
	s := valid()
	s.Predicate.Verdicts = []Verdict{
		{ID: "a", Status: "pass"}, {ID: "b", Status: "warn"}, {ID: "c", Status: "fail"},
		{ID: "d", Status: "skip"}, {ID: "e", Status: "info"},
	}
	s.Predicate.Counts = Counts{Pass: 1, Warn: 1, Fail: 1, Skip: 1, Info: 1}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAStatementWithoutAScoreNeedsNoRubric(t *testing.T) {
	s := valid()
	s.Predicate.Score = nil
	s.Predicate.JudgedAgainst.Rubric = ""
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestParseRoundTripsWhatMarshalWrites(t *testing.T) {
	b, err := valid().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "\n") || !strings.Contains(string(b), "\n  \"subject\"") {
		t.Errorf("not indented and newline-terminated:\n%s", b)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Predicate.Target.Endpoint != "https://mcp.example.com/mcp" || len(got.Predicate.Verdicts) != 2 {
		t.Errorf("round trip lost data: %+v", got.Predicate)
	}
}

func TestParseRefusesWhatIsNotAValidStatement(t *testing.T) {
	if _, err := Parse([]byte("{not json")); err == nil || !strings.Contains(err.Error(), "not a statement") {
		t.Errorf("malformed JSON: %v", err)
	}
	b, _ := json.Marshal(map[string]string{"_type": StatementType})
	if _, err := Parse(b); err == nil {
		t.Error("an empty statement parsed")
	}
}

func TestVerdictForSeparatesFailedFromNeverMeasured(t *testing.T) {
	s := valid()
	v, err := s.VerdictFor("protocol.origin")
	if err != nil || v.Status != "fail" {
		t.Fatalf("VerdictFor = %+v, %v", v, err)
	}
	if _, err := s.VerdictFor("never.measured"); !errors.Is(err, ErrNoSuchCheck) {
		t.Errorf("err = %v, want ErrNoSuchCheck", err)
	}
}

// TestCoversRecomputesRatherThanCompares. A gateway asks "is this
// statement about the server I am routing to"; the answer must come from
// the digest, so a rewritten predicate cannot pass.
func TestCoversRecomputesRatherThanCompares(t *testing.T) {
	s := valid()
	if !s.Covers("http", "https://mcp.example.com/mcp") {
		t.Error("the statement does not cover its own target")
	}
	if !s.Covers("http", "  https://mcp.example.com/mcp  ") {
		t.Error("surrounding whitespace changed the answer")
	}
	if s.Covers("stdio", "https://mcp.example.com/mcp") {
		t.Error("another transport was covered")
	}
	if s.Covers("http", "https://other.example.com/mcp") {
		t.Error("another server was covered")
	}
	s.Subject = nil
	if s.Covers("http", "https://mcp.example.com/mcp") {
		t.Error("a statement with no subject covers something")
	}
}

func TestSubjectForIsStableAndDistinct(t *testing.T) {
	a := SubjectFor(Target{Transport: "http", Endpoint: "https://a.example/mcp"})
	b := SubjectFor(Target{Transport: "http", Endpoint: "https://a.example/mcp"})
	c := SubjectFor(Target{Transport: "stdio", Endpoint: "https://a.example/mcp"})
	if a.Digest["sha256"] != b.Digest["sha256"] {
		t.Error("one target, two digests")
	}
	if a.Digest["sha256"] == c.Digest["sha256"] {
		t.Error("two transports, one digest")
	}
	if len(a.Digest["sha256"]) != 64 {
		t.Errorf("digest %q is not hex SHA-256", a.Digest["sha256"])
	}
}

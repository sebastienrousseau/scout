// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package attest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Parse reads a statement and validates its structure.
//
// It is strict about the envelope and forgiving about nothing, because the
// caller is deciding whether to trust a claim about a server it cannot see.
func Parse(b []byte) (*Statement, error) {
	var s Statement
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("attest: not a statement: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate reports every way a statement is not interpretable.
//
// The rules are the four properties the package exists to guarantee, turned
// into refusals. A statement that cannot say what it was judged against, or
// whose digest does not cover the target it names, is worse than no statement:
// it looks like evidence.
func (s *Statement) Validate() error {
	var problems []string
	note := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	if s.Type != StatementType {
		note("_type is %q, want %q", s.Type, StatementType)
	}
	if s.PredicateType != PredicateType {
		note("predicateType is %q, want %q", s.PredicateType, PredicateType)
	}

	switch len(s.Subject) {
	case 1:
		sub := s.Subject[0]
		if strings.TrimSpace(sub.Name) == "" {
			note("the subject has no name")
		}
		got := sub.Digest["sha256"]
		switch {
		case got == "":
			note("the subject has no sha256 digest")
		case got != digest(descriptor(s.Predicate.Target)):
			// The digest is what ties the statement to a target. One that
			// does not recompute means the predicate was edited after the
			// digest was taken, which is precisely what an attestation is
			// for detecting.
			note("the subject digest does not cover the target it names: a statement whose predicate was edited after the fact")
		}
	case 0:
		note("no subject: the statement is about nothing")
	default:
		note("%d subjects: a scout evaluation is about one server", len(s.Subject))
	}

	p := s.Predicate
	if p.SubjectKind != SubjectKindDescriptor {
		note("subjectKind is %q, want %q — a reader would otherwise take the digest for an artifact hash",
			p.SubjectKind, SubjectKindDescriptor)
	}
	if strings.TrimSpace(p.Target.Endpoint) == "" {
		note("the target has no endpoint")
	}
	switch p.Target.Transport {
	case "http", "stdio":
	case "":
		note("the target has no transport, and an HTTP run and a stdio run do not contain the same checks")
	default:
		note("unknown transport %q", p.Target.Transport)
	}
	if p.Instrument.Name == "" || p.Instrument.Version == "" {
		note("the instrument is not identified")
	}
	if p.RanAt.IsZero() {
		note("no run time: a verdict about a live service is a verdict about a moment")
	}
	if len(p.Verdicts) == 0 {
		note("no verdicts")
	}

	// The basis is the property that gives a verdict a shelf life.
	if p.JudgedAgainst.CheckInventory == "" {
		note("judgedAgainst.checkInventory is empty: the ids cannot be resolved to a catalogue")
	}
	if p.Score != nil && p.JudgedAgainst.Rubric == "" {
		note("a score with no rubric version, which is a number nothing can be compared to")
	}

	counted := Counts{}
	for i, v := range p.Verdicts {
		if v.ID == "" {
			note("verdict %d has no id", i)
			continue
		}
		// A duplicate id is not an error: a family id produces one verdict
		// per observed value, and the same id can be reached from two
		// branches of a phase. VerdictFor returns the first, which is why
		// the verdicts are canonically ordered.
		switch v.Status {
		case "pass":
			counted.Pass++
		case "warn":
			counted.Warn++
		case "fail":
			counted.Fail++
		case "skip":
			counted.Skip++
		case "info":
			counted.Info++
		default:
			note("verdict %s has status %q", v.ID, v.Status)
		}
	}
	if len(p.Verdicts) > 0 && counted != p.Counts {
		note("the counts disagree with the verdicts: says %+v, the verdicts are %+v", p.Counts, counted)
	}

	if len(problems) > 0 {
		return fmt.Errorf("attest: %s", strings.Join(problems, "; "))
	}
	return nil
}

// ErrNoSuchCheck is returned when a policy names a check the statement does
// not carry. It is its own error because "the server failed this" and "this
// was never measured" are different answers and a policy engine must not
// conflate them.
var ErrNoSuchCheck = errors.New("attest: the statement carries no verdict for that check")

// VerdictFor returns the outcome recorded for a check id.
func (s *Statement) VerdictFor(id string) (Verdict, error) {
	for _, v := range s.Predicate.Verdicts {
		if v.ID == id {
			return v, nil
		}
	}
	return Verdict{}, fmt.Errorf("%w: %s", ErrNoSuchCheck, id)
}

// Covers reports whether the statement is about the given target, by
// recomputing the descriptor digest rather than comparing strings.
//
// A gateway holding a statement and an address needs to know they match. A
// string comparison would answer "no" for a URL that differs only in its
// trailing slash, and "yes" for a statement whose predicate was rewritten.
func (s *Statement) Covers(transport, endpoint string) bool {
	if len(s.Subject) != 1 {
		return false
	}
	want := digest(descriptor(Target{Transport: transport, Endpoint: endpoint}))
	return s.Subject[0].Digest["sha256"] == want
}

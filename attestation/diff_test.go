// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: Apache-2.0

package attestation

import "testing"

func withVerdicts(vs ...Verdict) *Statement {
	s := valid()
	s.Predicate.Verdicts = vs
	return s
}

func ids(cs []Change) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func sameIDs(t *testing.T, name string, got []Change, want ...string) {
	t.Helper()
	g := ids(got)
	if len(g) != len(want) {
		t.Errorf("%s = %v, want %v", name, g, want)
		return
	}
	for i := range g {
		if g[i] != want[i] {
			t.Errorf("%s = %v, want %v", name, g, want)
			return
		}
	}
}

// TestCompareSortsEveryKindOfChange. One statement pair carries each kind,
// so a change landing in the wrong bucket shows up as two errors at once.
func TestCompareSortsEveryKindOfChange(t *testing.T) {
	before := withVerdicts(
		Verdict{ID: "a.regressed", Status: "pass"},
		Verdict{ID: "b.improved", Status: "fail", Severity: "major"},
		Verdict{ID: "c.worse_warn", Status: "warn"},
		Verdict{ID: "d.severity", Status: "fail", Severity: "minor"},
		Verdict{ID: "e.lost", Status: "pass"},
		Verdict{ID: "f.gone", Status: "fail", Severity: "major"},
		Verdict{ID: "g.same", Status: "pass"},
		Verdict{ID: "h.was_skipped", Status: "skip"},
		Verdict{ID: "i.info_to_pass", Status: "info"},
	)
	after := withVerdicts(
		Verdict{ID: "a.regressed", Status: "fail", Severity: "critical"},
		Verdict{ID: "b.improved", Status: "pass"},
		Verdict{ID: "c.worse_warn", Status: "fail", Severity: "minor"},
		Verdict{ID: "d.severity", Status: "fail", Severity: "critical"},
		Verdict{ID: "e.lost", Status: "skip"},
		Verdict{ID: "g.same", Status: "pass"},
		Verdict{ID: "h.was_skipped", Status: "warn"},
		Verdict{ID: "i.info_to_pass", Status: "pass"},
		Verdict{ID: "j.new", Status: "fail", Severity: "major"},
	)
	d := Compare(before, after)
	sameIDs(t, "Regressed", d.Regressed, "a.regressed", "c.worse_warn")
	sameIDs(t, "Improved", d.Improved, "b.improved")
	sameIDs(t, "SeverityChanged", d.SeverityChanged, "d.severity")
	sameIDs(t, "Unassessed", d.Unassessed, "e.lost", "f.gone")
	sameIDs(t, "Added", d.Added, "h.was_skipped", "j.new")
	if !d.Regression() {
		t.Error("two regressions and Regression() is false")
	}
	if !d.Comparable || d.ScoreFrom == nil || *d.ScoreFrom != 88 {
		t.Errorf("comparable delta without its scores: %+v", d)
	}
	if d.Regressed[0].From != "pass" || d.Regressed[0].To != "fail" || d.Regressed[0].ToSeverity != "critical" {
		t.Errorf("the change does not say what it was: %+v", d.Regressed[0])
	}
}

func TestAStatementComparedWithItselfHasNoDelta(t *testing.T) {
	d := Compare(valid(), valid())
	if d.Regression() || len(d.Improved)+len(d.Added)+len(d.Unassessed)+len(d.SeverityChanged) != 0 {
		t.Errorf("identical statements differ: %+v", d)
	}
}

// TestDifferentRubricsCompareVerdictsNotScores. Ids are stable within an
// inventory, so verdicts still compare; a score under another rubric does
// not.
func TestDifferentRubricsCompareVerdictsNotScores(t *testing.T) {
	later := valid()
	later.Predicate.JudgedAgainst.Rubric = "2"
	d := Compare(valid(), later)
	if d.Comparable || d.ScoreFrom != nil || d.ScoreTo != nil {
		t.Errorf("scores compared across rubrics: %+v", d)
	}
}

// TestTheFirstVerdictPerIDIsCompared, matching VerdictFor.
func TestTheFirstVerdictPerIDIsCompared(t *testing.T) {
	before := withVerdicts(Verdict{ID: "x", Status: "pass"}, Verdict{ID: "x", Status: "fail"})
	after := withVerdicts(Verdict{ID: "x", Status: "pass"})
	if d := Compare(before, after); d.Regression() || len(d.Improved) != 0 {
		t.Errorf("a family's later verdict was compared: %+v", d)
	}
}

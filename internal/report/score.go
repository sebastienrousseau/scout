// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package report

import (
	"fmt"

	"github.com/sebastienrousseau/scout/internal/probe"
)

// RubricVersion is the version of the scoring rubric.
//
// It exists because a score is only comparable to another score computed the
// same way. "82/100" in a report from 2027 and "82/100" from 2029 are the
// same number about possibly different things, and an attestation that
// carries the total without the rubric behind it is publishing a figure
// nothing can be compared to.
//
// Increment it when the weights change, when a category is added or removed,
// or when the deduction model changes — anything that would move a score
// without the server moving. Never increment it for a new check inside an
// existing category: that is the rubric working, not changing.
//
// 1: six weighted categories — connectivity 10, authorization 20, protocol
// 20, catalog 15, execution 20, performance 15 — scored over the phases each
// names, with unassessed categories excluded from the weighting rather than
// counted as zero.
const RubricVersion = "1"

// CheckInventoryVersion is the version of the check catalogue the ids in a
// report belong to.
//
// It follows the same rule as SchemaVersion, for the same reason: increment it
// when an id is renamed, removed, or changes what it asserts, and never when
// one is added. A consumer holding an attestation from two years ago needs to
// know whether `catalog.tools.output_schema` still means what it meant then,
// and the generated inventory cannot tell it that on its own — docs/checks.md
// describes the current catalogue, not the one the statement was written
// against.
//
// 1: the catalogue as generated from the call sites, ids in the
// <phase>.<check> form with auth.source.* as the one computed family.
const CheckInventoryVersion = "1"

// Category groups phases for scoring.
type Category struct {
	Name       string   `json:"name"`
	Weight     int      `json:"weight"`
	Phases     []string `json:"phases"`
	Assessed   bool     `json:"assessed"`
	Score      float64  `json:"score"`
	Deductions []string `json:"deductions,omitempty"`
}

// Score is the weighted rating with its full derivation.
type Score struct {
	Total      float64    `json:"total"`
	Assessed   int        `json:"categories_assessed"`
	Of         int        `json:"categories_total"`
	Categories []Category `json:"categories"`
	// Grade is a coarse letter for dashboards: A ≥ 90, B ≥ 75, C ≥ 60, D ≥ 40, F below.
	Grade string `json:"grade"`
}

var categories = []Category{
	{Name: "connectivity", Weight: 10, Phases: []string{"net"}},
	{Name: "authorization", Weight: 20, Phases: []string{"discovery", "auth"}},
	{Name: "protocol", Weight: 20, Phases: []string{"handshake", "protocol", "resilience"}},
	{Name: "catalog", Weight: 15, Phases: []string{"catalog"}},
	{Name: "execution", Weight: 20, Phases: []string{"execution"}},
	{Name: "performance", Weight: 15, Phases: []string{"performance"}},
}

// Deduction points per severity. A category cannot go below zero.
var deduction = map[probe.Severity]float64{probe.Critical: 100, probe.Major: 40, probe.Minor: 15}

// ComputeScore derives the score from findings. Only categories whose
// phases actually ran count; the report says how many were assessed so a
// high score on a partial run cannot be mistaken for a full one.
func ComputeScore(phases []probe.PhaseResult) Score {
	byName := map[string]probe.PhaseResult{}
	for _, p := range phases {
		byName[p.Name] = p
	}
	sc := Score{Of: len(categories)}
	var weighted, weights float64
	for _, cat := range categories {
		c := cat
		c.Score = 100
		for _, pn := range c.Phases {
			p, ok := byName[pn]
			if !ok || p.Status == probe.Skip {
				continue
			}
			c.Assessed = true
			for _, f := range p.Findings {
				switch f.Status {
				case probe.Fail:
					d := deduction[f.Severity]
					if d == 0 {
						d = 15
					}
					c.Score -= d
					c.Deductions = append(c.Deductions, fmt.Sprintf("-%.0f %s: %s", d, f.ID, f.Detail))
				case probe.Warn:
					c.Score -= 5
					c.Deductions = append(c.Deductions, fmt.Sprintf("-5 %s: %s", f.ID, f.Detail))
				}
			}
		}
		if c.Score < 0 {
			c.Score = 0
		}
		if c.Assessed {
			sc.Assessed++
			weighted += c.Score * float64(c.Weight)
			weights += float64(c.Weight)
		}
		sc.Categories = append(sc.Categories, c)
	}
	if weights > 0 {
		sc.Total = weighted / weights
	}
	switch {
	case sc.Assessed == 0:
		sc.Grade = "n/a"
	case sc.Total >= 90:
		sc.Grade = "A"
	case sc.Total >= 75:
		sc.Grade = "B"
	case sc.Total >= 60:
		sc.Grade = "C"
	case sc.Total >= 40:
		sc.Grade = "D"
	default:
		sc.Grade = "F"
	}
	return sc
}

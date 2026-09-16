// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package report

import (
	"fmt"

	"github.com/sebastienrousseau/scout/internal/probe"
)

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

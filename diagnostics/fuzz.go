// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package diagnostics

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
)

// Generator builds representative arguments from a JSON Schema so a tool
// can be invoked without prior knowledge of its contract. It prefers
// concrete hints in the schema (const, enum, default, examples) over
// random values, and only fills required properties unless FillOptional is
// set.
type Generator struct {
	Rand         *rand.Rand
	FillOptional bool
	MaxDepth     int

	res *resolver
}

// NewGenerator returns a deterministic generator for seed.
func NewGenerator(seed uint64) *Generator {
	// #nosec G404 -- deliberately deterministic: a run must be reproducible
	// from its --seed, and these values are tool arguments, not secrets.
	return &Generator{Rand: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), MaxDepth: 6}
}

// Arguments generates arguments for a tool input schema. A nil or empty
// schema yields an empty object.
func (g *Generator) Arguments(schema json.RawMessage) (map[string]any, error) {
	if len(schema) == 0 || string(schema) == "null" {
		return map[string]any{}, nil
	}
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, fmt.Errorf("diagnostics: parse input schema: %w", err)
	}
	g.res = newResolver(s)
	v := g.value(s, 0)
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	return map[string]any{}, nil
}

func (g *Generator) value(s map[string]any, depth int) any {
	if g.MaxDepth > 0 && depth > g.MaxDepth {
		return nil
	}
	if depth > MaxSchemaDepth {
		return nil
	}
	s = g.res.deref(s, depth)
	if c, ok := s["const"]; ok {
		return c
	}
	if e, ok := s["enum"].([]any); ok && len(e) > 0 {
		return e[g.Rand.IntN(len(e))]
	}
	if d, ok := s["default"]; ok && d != nil {
		return d
	}
	if ex, ok := s["examples"].([]any); ok && len(ex) > 0 {
		return ex[g.Rand.IntN(len(ex))]
	}
	for _, k := range []string{"oneOf", "anyOf", "allOf"} {
		if alts, ok := s[k].([]any); ok && len(alts) > 0 {
			if sub, ok := alts[0].(map[string]any); ok {
				merged := map[string]any{}
				for kk, vv := range s {
					if kk != k {
						merged[kk] = vv
					}
				}
				for kk, vv := range sub {
					merged[kk] = vv
				}
				return g.value(merged, depth+1)
			}
		}
	}
	switch typeOf(s) {
	case "object":
		out := map[string]any{}
		props, _ := s["properties"].(map[string]any)
		req := stringSlice(s["required"])
		for name, raw := range props {
			ps, _ := raw.(map[string]any)
			if ps == nil {
				continue
			}
			if !g.FillOptional && !contains(req, name) {
				continue
			}
			out[name] = g.value(ps, depth+1)
		}
		return out
	case "array":
		items, _ := s["items"].(map[string]any)
		n := 1
		if mn, ok := num(s["minItems"]); ok && mn > 0 {
			n = int(math.Min(mn, maxGeneratedItems))
		}
		arr := make([]any, 0, n)
		for i := 0; i < n; i++ {
			if items == nil {
				arr = append(arr, "item")
			} else {
				arr = append(arr, g.value(items, depth+1))
			}
		}
		return arr
	case "integer":
		return g.number(s, true)
	case "number":
		return g.number(s, false)
	case "boolean":
		return g.Rand.IntN(2) == 0
	case "null":
		return nil
	default:
		return g.str(s)
	}
}

// safeIntBound is the largest magnitude an integer bound is honoured at.
// Beyond 2^53 a float64 no longer represents consecutive integers, and the
// int64 conversion of a bound like 1e19 overflows. Schemas do carry such
// values — generated code emits them for "unbounded" fields — so they are
// clamped rather than trusted.
const safeIntBound = 1 << 53

// maxGeneratedString caps a string produced to satisfy minLength, and
// maxGeneratedItems an array produced to satisfy minItems.
const (
	maxGeneratedString = 4096
	maxGeneratedItems  = 256
)

func clampBound(f float64) (float64, bool) {
	switch {
	case math.IsNaN(f):
		return 0, false
	case math.IsInf(f, 1) || f > safeIntBound:
		return safeIntBound, true
	case math.IsInf(f, -1) || f < -safeIntBound:
		return -safeIntBound, true
	}
	return f, true
}

func (g *Generator) number(s map[string]any, integer bool) any {
	lo, hasLo := num(s["minimum"])
	hi, hasHi := num(s["maximum"])
	if v, ok := num(s["exclusiveMinimum"]); ok {
		lo, hasLo = v+1, true
	}
	if v, ok := num(s["exclusiveMaximum"]); ok {
		hi, hasHi = v-1, true
	}
	if hasLo {
		lo, hasLo = clampBound(lo)
	}
	if hasHi {
		hi, hasHi = clampBound(hi)
	}
	switch {
	case hasLo && hasHi && hi >= lo:
		if integer {
			span := int64(hi) - int64(lo)
			if span < 0 {
				span = 0
			}
			return int64(lo) + g.Rand.Int64N(span+1)
		}
		return lo + g.Rand.Float64()*(hi-lo)
	case hasLo:
		if integer {
			return int64(lo) + g.Rand.Int64N(10)
		}
		return lo + g.Rand.Float64()*10
	case hasHi:
		if integer {
			return int64(hi) - g.Rand.Int64N(10)
		}
		return hi - g.Rand.Float64()*10
	}
	if integer {
		return g.Rand.Int64N(100) + 1
	}
	return g.Rand.Float64() * 100
}

func (g *Generator) str(s map[string]any) string {
	format, _ := s["format"].(string)
	switch format {
	case "email":
		return "probe@example.com"
	case "uri", "url":
		return "https://example.com/probe"
	case "date":
		return "2026-01-15"
	case "date-time":
		return "2026-01-15T10:30:00Z"
	case "time":
		return "10:30:00Z"
	case "uuid":
		return "123e4567-e89b-12d3-a456-426614174000"
	case "ipv4":
		return "192.0.2.1"
	case "hostname":
		return "example.com"
	}
	if p, ok := s["pattern"].(string); ok && p != "" {
		// A pattern we cannot satisfy generically; use something short and
		// let the server's validation surface in the report.
		return "probe"
	}
	desc, _ := s["description"].(string)
	base := "probe"
	switch {
	case strings.Contains(strings.ToLower(desc), "query"), strings.Contains(strings.ToLower(desc), "search"):
		base = "example"
	case strings.Contains(strings.ToLower(desc), "name"):
		base = "Example"
	}
	if mn, ok := num(s["minLength"]); ok && mn > float64(len(base)) {
		// A hostile or careless minLength must not turn into a gigabyte of
		// request body.
		n := int(math.Min(mn, maxGeneratedString))
		base = strings.Repeat("x", n)
	}
	if mx, ok := num(s["maxLength"]); ok && int(mx) < len(base) && mx > 0 {
		base = base[:int(mx)]
	}
	return base
}

func typeOf(s map[string]any) string {
	switch t := s["type"].(type) {
	case string:
		return t
	case []any:
		for _, v := range t {
			if str, ok := v.(string); ok && str != "null" {
				return str
			}
		}
	}
	if _, ok := s["properties"]; ok {
		return "object"
	}
	if _, ok := s["items"]; ok {
		return "array"
	}
	return "string"
}

func num(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func stringSlice(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, a := range arr {
		if s, ok := a.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

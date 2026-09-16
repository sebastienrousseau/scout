// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package diagnostics

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Validate checks value against a JSON Schema and returns human-readable
// violations. It implements the structural core used by MCP tool schemas:
// type, properties, required, additionalProperties=false, items, enum,
// const, minimum/maximum, minLength/maxLength, minItems/maxItems,
// oneOf/anyOf/allOf, and local "$ref"/"$defs" pointers. It does not check
// patterns or formats, and a $ref pointing outside the document is reported
// as unchecked rather than passed over in silence.
func Validate(schema, value json.RawMessage) []string {
	if len(schema) == 0 {
		return nil
	}
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		return []string{"schema is not valid JSON: " + err.Error()}
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(string(value)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return []string{"value is not valid JSON: " + err.Error()}
	}
	var out []string
	r := newResolver(s)
	v8 := &validator{r: r}
	v8.validate(s, v, "$", 0, &out)
	out = append(out, r.externalRefIssues()...)
	return out
}

type validator struct{ r *resolver }

func (x *validator) validate(s map[string]any, v any, path string, depth int, out *[]string) {
	if depth > MaxSchemaDepth {
		*out = append(*out, fmt.Sprintf("%s: schema nests deeper than %d levels; not checked further", path, MaxSchemaDepth))
		return
	}
	s = x.r.deref(s, depth)
	validateNode(x, s, v, path, depth, out)
}

func validateNode(x *validator, s map[string]any, v any, path string, depth int, out *[]string) {
	if c, ok := s["const"]; ok && !jsonEqual(c, v) {
		*out = append(*out, fmt.Sprintf("%s: expected const %v", path, c))
	}
	if e, ok := s["enum"].([]any); ok {
		found := false
		for _, cand := range e {
			if jsonEqual(cand, v) {
				found = true
				break
			}
		}
		if !found {
			*out = append(*out, fmt.Sprintf("%s: value not in enum", path))
		}
	}
	for _, k := range []string{"allOf"} {
		if alts, ok := s[k].([]any); ok {
			for i, a := range alts {
				if sub, ok := a.(map[string]any); ok {
					x.validate(sub, v, fmt.Sprintf("%s(allOf[%d])", path, i), depth+1, out)
				}
			}
		}
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if alts, ok := s[k].([]any); ok && len(alts) > 0 {
			matches := 0
			for _, a := range alts {
				sub, ok := a.(map[string]any)
				if !ok {
					continue
				}
				var tmp []string
				x.validate(sub, v, path, depth+1, &tmp)
				if len(tmp) == 0 {
					matches++
				}
			}
			if matches == 0 || (k == "oneOf" && matches > 1) {
				*out = append(*out, fmt.Sprintf("%s: matches %d of %s alternatives", path, matches, k))
			}
		}
	}
	if !typeMatches(s["type"], v) {
		*out = append(*out, fmt.Sprintf("%s: expected type %v, got %s", path, s["type"], jsonTypeName(v)))
		return
	}
	switch val := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		for _, r := range stringSlice(s["required"]) {
			if _, ok := val[r]; !ok {
				*out = append(*out, fmt.Sprintf("%s: missing required property %q", path, r))
			}
		}
		for name, pv := range val {
			if ps, ok := props[name].(map[string]any); ok {
				x.validate(ps, pv, path+"."+name, depth+1, out)
				continue
			}
			if ap, ok := s["additionalProperties"]; ok {
				switch a := ap.(type) {
				case bool:
					if !a {
						*out = append(*out, fmt.Sprintf("%s: unexpected property %q", path, name))
					}
				case map[string]any:
					x.validate(a, pv, path+"."+name, depth+1, out)
				}
			}
		}
	case []any:
		if mn, ok := num(s["minItems"]); ok && float64(len(val)) < mn {
			*out = append(*out, fmt.Sprintf("%s: fewer than %v items", path, mn))
		}
		if mx, ok := num(s["maxItems"]); ok && float64(len(val)) > mx {
			*out = append(*out, fmt.Sprintf("%s: more than %v items", path, mx))
		}
		if items, ok := s["items"].(map[string]any); ok {
			for i, iv := range val {
				x.validate(items, iv, fmt.Sprintf("%s[%d]", path, i), depth+1, out)
			}
		}
	case string:
		if mn, ok := num(s["minLength"]); ok && float64(len(val)) < mn {
			*out = append(*out, fmt.Sprintf("%s: shorter than minLength %v", path, mn))
		}
		if mx, ok := num(s["maxLength"]); ok && float64(len(val)) > mx {
			*out = append(*out, fmt.Sprintf("%s: longer than maxLength %v", path, mx))
		}
	case json.Number:
		f, _ := val.Float64()
		if mn, ok := num(s["minimum"]); ok && f < mn {
			*out = append(*out, fmt.Sprintf("%s: below minimum %v", path, mn))
		}
		if mx, ok := num(s["maximum"]); ok && f > mx {
			*out = append(*out, fmt.Sprintf("%s: above maximum %v", path, mx))
		}
	}
}

func typeMatches(t any, v any) bool {
	switch tt := t.(type) {
	case nil:
		return true
	case string:
		return typeIs(tt, v)
	case []any:
		for _, x := range tt {
			if s, ok := x.(string); ok && typeIs(s, v) {
				return true
			}
		}
		return false
	}
	return true
}

func typeIs(t string, v any) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		f, err := n.Float64()
		return err == nil && f == math.Trunc(f)
	}
	return true
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	case json.Number:
		return "number"
	}
	return fmt.Sprintf("%T", v)
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

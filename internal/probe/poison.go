// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/diagnostics"
)

// catalogText is every string in a catalog that reaches the model.
//
// The list is the point. A reviewer reads tool descriptions; the model also
// reads titles, resource descriptions, prompt argument descriptions, and
// every `description` inside an inputSchema — and the schema is where
// published poisoning has most often been found, because it is the part
// nobody renders.
func catalogText(tools []scout.Tool, res []scout.Resource, prompts []scout.Prompt) []textField {
	var fields []textField
	for _, t := range tools {
		fields = append(fields,
			textField{where: fmt.Sprintf("tool %q description", t.Name), text: t.Description},
			textField{where: fmt.Sprintf("tool %q title", t.Name), text: t.Title},
		)
		fields = append(fields, schemaText(fmt.Sprintf("tool %q inputSchema", t.Name), t.InputSchema)...)
		fields = append(fields, schemaText(fmt.Sprintf("tool %q outputSchema", t.Name), t.OutputSchema)...)
	}
	for _, r := range res {
		fields = append(fields,
			textField{where: fmt.Sprintf("resource %q description", r.Name), text: r.Description},
			textField{where: fmt.Sprintf("resource %q title", r.Name), text: r.Title},
		)
	}
	for _, p := range prompts {
		fields = append(fields,
			textField{where: fmt.Sprintf("prompt %q description", p.Name), text: p.Description},
			textField{where: fmt.Sprintf("prompt %q title", p.Name), text: p.Title},
		)
		for _, a := range p.Arguments {
			fields = append(fields, textField{
				where: fmt.Sprintf("prompt %q argument %q description", p.Name, a.Name),
				text:  a.Description,
			})
		}
	}
	return fields
}

type textField struct {
	where string
	text  string
}

// schemaDepthLimit bounds the walk. A schema deep enough to exceed it is
// already a finding of its own in the validator; this is here so a
// self-referential or hostile schema cannot make the scan run forever.
const schemaDepthLimit = 12

// schemaText collects every description and title inside a JSON Schema.
func schemaText(where string, raw json.RawMessage) []textField {
	if len(raw) == 0 {
		return nil
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	var out []textField
	var walk func(node any, path string, depth int)
	walk = func(node any, path string, depth int) {
		if depth > schemaDepthLimit {
			return
		}
		switch n := node.(type) {
		case map[string]any:
			// Sorted, because a map is not, and a report whose findings
			// reorder between two runs over the same server is a diff with
			// no change in it.
			keys := make([]string, 0, len(n))
			for k := range n {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := n[k]
				if s, ok := v.(string); ok && (k == "description" || k == "title") {
					out = append(out, textField{where: path + "." + k, text: s})
					continue
				}
				walk(v, path+"."+k, depth+1)
			}
		case []any:
			for i, v := range n {
				walk(v, fmt.Sprintf("%s[%d]", path, i), depth+1)
			}
		}
	}
	walk(doc, where, 0)
	return out
}

// scanCatalog reports what the catalog says to the model that it does not
// say to the person reading it.
func scanCatalog(s *Session, res []scout.Resource, prompts []scout.Prompt) []Finding {
	var sigs []diagnostics.Signal
	for _, f := range catalogText(s.Tools, res, prompts) {
		sigs = append(sigs, diagnostics.ScanText(f.where, f.text)...)
	}
	for _, t := range s.Tools {
		sigs = append(sigs, diagnostics.ScanName(fmt.Sprintf("tool %q name", t.Name), t.Name)...)
	}
	for _, p := range prompts {
		sigs = append(sigs, diagnostics.ScanName(fmt.Sprintf("prompt %q name", p.Name), p.Name)...)
	}

	byKind := map[diagnostics.SignalKind][]diagnostics.Signal{}
	for _, sig := range sigs {
		byKind[sig.Kind] = append(byKind[sig.Kind], sig)
	}

	// One check per kind, so a reader can tell "there is a hidden
	// character somewhere" from "a description is addressed to the model".
	// They are different problems with different answers.
	// Each entry opens its own check with literal arguments rather than
	// passing the id through a variable. scripts/checkinventory parses
	// (*Session).check call sites from the source, so an id built at run
	// time is an id the published inventory cannot list — and the
	// inventory gate caught exactly that when this table held strings.
	checks := []struct {
		kind  diagnostics.SignalKind
		open  func(*Session) *check
		clean string
		fix   string
	}{
		{diagnostics.SignalHidden,
			func(s *Session) *check {
				return s.check("catalog.text.hidden", "Catalog text has nothing hidden in it")
			},
			"no invisible or bidirectional characters",
			"remove the characters; a description that needs them is a description a reviewer cannot check"},
		{diagnostics.SignalComment,
			func(s *Session) *check {
				return s.check("catalog.text.comments", "Catalog text carries no hidden comments")
			},
			"no HTML comments",
			"move the content into the description itself, or delete it: a rendered catalog hides a comment and the model does not"},
		{diagnostics.SignalInstruction,
			func(s *Session) *check {
				return s.check("catalog.text.instructions", "Catalog text describes rather than instructs")
			},
			"no text addressed to the model",
			"describe what the tool does; an instruction aimed at the model is indistinguishable from one an attacker planted"},
		{diagnostics.SignalSecretPath,
			func(s *Session) *check {
				return s.check("catalog.text.secret_paths", "Catalog text names no credential locations")
			},
			"no credential paths named",
			"if the tool genuinely reads these, say so in prose an operator approves rather than in a schema field"},
		{diagnostics.SignalConfusable,
			func(s *Session) *check {
				return s.check("catalog.names.confusable", "Names use a single script")
			},
			"every name is single-script",
			"use one script per name; a mixed-script name exists to render like a name the user already trusts"},
	}

	var out []Finding
	for _, spec := range checks {
		c := spec.open(s)
		found := byKind[spec.kind]
		if len(found) == 0 {
			out = append(out, c.pass(spec.clean))
			continue
		}
		worst, _ := diagnostics.Worst(found)
		detail := describeSignals(found)
		switch worst {
		case diagnostics.SeverityCritical:
			out = append(out, c.fail(Critical, detail, spec.fix))
		case diagnostics.SeverityMajor:
			out = append(out, c.fail(Major, detail, spec.fix))
		default:
			out = append(out, c.warn(detail, spec.fix))
		}
	}
	return out
}

// describeSignals renders the findings a maintainer has to act on.
//
// It names at most three: a catalog with forty poisoned descriptions has
// one problem, not forty, and a finding that scrolls is a finding nobody
// reads to the end of.
func describeSignals(sigs []diagnostics.Signal) string {
	sort.SliceStable(sigs, func(i, j int) bool {
		return severityRank(sigs[i].Severity) < severityRank(sigs[j].Severity)
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%d occurrence(s); ", len(sigs))
	shown := min(len(sigs), 3)
	for i := range shown {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s %s — %q", sigs[i].Where, sigs[i].Detail, sigs[i].Excerpt)
	}
	if len(sigs) > shown {
		fmt.Fprintf(&b, "; and %d more", len(sigs)-shown)
	}
	return b.String()
}

func severityRank(s diagnostics.SignalSeverity) int {
	switch s {
	case diagnostics.SeverityCritical:
		return 0
	case diagnostics.SeverityMajor:
		return 1
	default:
		return 2
	}
}

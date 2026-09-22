// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/sebastienrousseau/scout"
)

// An annotation is a claim, and this is the check that reads the claim
// against the rest of what the tool says about itself.
//
// It matters here more than it would in another tool, because scout acts on
// the answer. diagnostics.Policy invokes tools with readOnlyHint: true and
// nothing else (ADR-0004), so a server that annotates delete_project as
// read-only is not merely mis-documented: it has found the way to make
// scout perform the deletion, on a run the operator authorised precisely
// because it was supposed to be safe. The annotation is the load-bearing
// part of scout's own safety story, and an unchecked claim is not a safety
// story at all.
//
// What can be established from a catalogue alone is a contradiction: the
// annotation says the tool reads, and the tool's own name or its own first
// sentence say it writes. That is the check. It is not a proof that the
// tool mutates — only calling it and inspecting the world would be, and
// scout will not call a tool it suspects of writing in order to find out.
// A contradiction is enough, because whichever half is wrong, the catalogue
// is unsafe to act on until somebody fixes it.

// mutationVerbs are verbs whose ordinary reading is a change of state on
// the server's side of the wire.
//
// The bar for the list is that the verb has no common read-only sense in a
// tool name. "get", "list", "search", "read", "fetch" and "find" are
// obviously absent. So are "open", "close" and "set": "open a file for
// reading", "close a connection" and "set the page size" are all ordinary
// read-path language, and including them was what made the first pass
// flag half of an honest catalogue.
var mutationVerbs = map[string]bool{
	"add": true, "append": true, "archive": true, "assign": true,
	"cancel": true, "clear": true, "commit": true, "create": true,
	"delete": true, "deploy": true, "destroy": true, "disable": true,
	"drop": true, "edit": true, "enable": true, "execute": true,
	"grant": true, "insert": true, "install": true, "kill": true,
	"merge": true, "modify": true, "move": true, "patch": true,
	"post": true, "publish": true, "purge": true, "push": true,
	"put": true, "remove": true, "rename": true, "reset": true,
	"restore": true, "revert": true, "revoke": true, "run": true,
	"send": true, "terminate": true, "truncate": true, "uninstall": true,
	"update": true, "upload": true, "wipe": true, "write": true,
}

// mutationVerb reports whether word is a mutation verb, accepting the
// third-person and gerund forms a description is written in: a name says
// "delete_issue", a description says "Deletes the issue" or "Deleting an
// issue removes it permanently".
func mutationVerb(word string) (string, bool) {
	w := strings.ToLower(strings.Trim(word, ".,;:!?()[]\"'`"))
	if w == "" {
		return "", false
	}
	if mutationVerbs[w] {
		return w, true
	}
	// Deletes -> delete, pushes -> push, copies -> copy is not in the list
	// so needs no y-rule, and "creates" -> "create" falls out of the "s"
	// trim once "create" is checked with the trailing e restored.
	for _, suffix := range []string{"es", "s", "ing", "ed"} {
		if !strings.HasSuffix(w, suffix) {
			continue
		}
		stem := strings.TrimSuffix(w, suffix)
		if mutationVerbs[stem] {
			return stem, true
		}
		// Trimming the suffix can leave a stem with its final "e" gone --
		// "creates" and "deleting" both do it -- so putting the "e" back
		// is tried before giving up.
		if mutationVerbs[stem+"e"] {
			return stem + "e", true
		}
		// Or with the final consonant doubled, as "committing" does.
		if n := len(stem); n >= 2 && stem[n-1] == stem[n-2] && mutationVerbs[stem[:n-1]] {
			return stem[:n-1], true
		}
	}
	return "", false
}

// nameVerb returns the leading verb of a tool name, for either of the two
// conventions in the wild: delete_issue and deleteIssue.
func nameVerb(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if i := strings.IndexAny(name, "_-."); i > 0 {
		return name[:i]
	}
	// camelCase: take the run up to the first upper-case rune.
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			return name[:i]
		}
	}
	return name
}

// leadVerb returns the verb a description leads with.
//
// A description conventionally opens by saying what the tool does — "Delete
// a file", "Creates a new issue" — so the opening verb is the one sentence
// element worth reading against the annotation. Later sentences are where
// the caveats live ("...will fail if the file was deleted"), and reading
// them the same way is how this check would start crying wolf.
func leadVerb(desc string) string {
	first := desc
	if i := strings.IndexAny(first, ".!?\n"); i > 0 {
		first = first[:i]
	}
	fields := strings.Fields(first)
	// Skip the openers that delay the verb without changing it.
	skip := map[string]bool{
		"a": true, "an": true, "the": true, "this": true, "tool": true,
		"that": true, "which": true, "it": true, "will": true, "can": true,
		"to": true, "and": true, "or": true, "also": true,
	}
	for _, f := range fields {
		w := strings.ToLower(strings.Trim(f, ".,;:!?()[]\"'`"))
		if w == "" || skip[w] {
			continue
		}
		return w
	}
	return ""
}

// dishonestAnnotation is one tool whose annotation and self-description
// disagree.
type dishonestAnnotation struct {
	tool  string
	verb  string
	from  string // "name" or "description"
	quote string
}

// findDishonestAnnotations reports read-only tools that describe writing.
func findDishonestAnnotations(tools []scout.Tool) []dishonestAnnotation {
	var out []dishonestAnnotation
	for _, t := range tools {
		if !t.IsReadOnly() {
			continue
		}
		if verb, ok := mutationVerb(nameVerb(t.Name)); ok {
			out = append(out, dishonestAnnotation{
				tool: t.Name, verb: verb, from: "name", quote: t.Name,
			})
			continue
		}
		desc := strings.TrimSpace(t.Description)
		if desc == "" {
			continue
		}
		if verb, ok := mutationVerb(leadVerb(desc)); ok {
			out = append(out, dishonestAnnotation{
				tool: t.Name, verb: verb, from: "description", quote: excerpt(desc, 0),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].tool < out[j].tool })
	return out
}

// checkAnnotationHonesty reads readOnlyHint against what the tool says it
// does.
func checkAnnotationHonesty(s *Session) Finding {
	c := s.check("catalog.tools.annotation_honesty", "readOnlyHint agrees with what the tool says it does")

	var readOnly int
	for _, t := range s.Tools {
		if t.IsReadOnly() {
			readOnly++
		}
	}
	if readOnly == 0 {
		return c.info("no tool declares readOnlyHint: true")
	}

	bad := findDishonestAnnotations(s.Tools)
	if len(bad) == 0 {
		return c.pass(fmt.Sprintf("%d read-only tool(s), none of which describe writing", readOnly))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d read-only tool(s) describe a change of state; ", len(bad), readOnly)
	shown := min(len(bad), 3)
	for i := range shown {
		if i > 0 {
			b.WriteString("; ")
		}
		d := bad[i]
		fmt.Fprintf(&b, "%q is annotated readOnlyHint: true and its %s says %q — %q",
			d.tool, d.from, d.verb, d.quote)
	}
	if len(bad) > shown {
		fmt.Fprintf(&b, "; and %d more", len(bad)-shown)
	}

	return c.fail(Critical, b.String(),
		"correct whichever half is wrong. A client that honours readOnlyHint — scout included — "+
			"invokes these tools without asking, so an annotation that understates what a tool "+
			"does is how a server gets a cautious client to perform the write for it")
}

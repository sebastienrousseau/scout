// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build ignore

// ecosystem generates the family table in docs/ecosystem.md and the
// ecosystem.json that repositories outside this one read, and with -check
// fails when either has drifted from the manifest.
//
// REPO-STANDARD requires a CI-checked table of which repository has what, so
// a multi-repository family cannot silently drift. AGENTS.md forbids
// hand-writing a redundant copy of anything that has a primary definition.
// Together those two rules mean the table in the manual cannot be typed by a
// human, which is what this program is for.
//
// It is the same arrangement as scripts/checkinventory, for the same reason,
// and deliberately so: a contributor who has met one has met both.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sebastienrousseau/scout/internal/ecosystem"
)

// The generated region of the manual. Everything between these markers is
// this program's; everything outside is prose, and prose is not generated.
const (
	begin = "<!-- BEGIN generated family table — run `make ecosystem`; do not edit by hand -->"
	end   = "<!-- END generated family table -->"
)

const (
	docPath  = "docs/ecosystem.md"
	jsonPath = "ecosystem.json"
)

func main() {
	check := flag.Bool("check", false, "verify the committed files match the manifest instead of writing them")
	flag.Parse()

	if errs := ecosystem.Validate(); len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "ecosystem: %v\n", err)
		}
		fmt.Fprintln(os.Stderr, "\nThe manifest contradicts itself; nothing generated from it would be trustworthy.")
		os.Exit(1)
	}

	doc, err := os.ReadFile(docPath)
	if err != nil {
		fail(err)
	}
	updated, err := replaceRegion(string(doc), renderTable())
	if err != nil {
		fail(err)
	}
	manifest, err := renderJSON()
	if err != nil {
		fail(err)
	}

	if *check {
		var stale []string
		if updated != string(doc) {
			stale = append(stale, docPath)
		}
		if committed, err := os.ReadFile(jsonPath); err != nil || !bytes.Equal(committed, manifest) {
			stale = append(stale, jsonPath)
		}
		if len(stale) > 0 {
			fmt.Fprintf(os.Stderr, "ecosystem: %s is stale — the manifest in internal/ecosystem says something else.\nRun: make ecosystem\n", strings.Join(stale, " and "))
			os.Exit(1)
		}
		fmt.Printf("ecosystem: %s and %s match the manifest (%d repositories)\n", docPath, jsonPath, len(ecosystem.Family))
		return
	}

	if err := os.WriteFile(docPath, []byte(updated), 0o644); err != nil { //nolint:gosec // documentation, not a secret
		fail(err)
	}
	if err := os.WriteFile(jsonPath, manifest, 0o644); err != nil { //nolint:gosec // a published manifest
		fail(err)
	}
	fmt.Printf("ecosystem: wrote %s and %s (%d repositories: %d shipping, %d planned, %d rejected)\n",
		docPath, jsonPath, len(ecosystem.Family),
		len(ecosystem.ByStatus(ecosystem.Shipping)),
		len(ecosystem.ByStatus(ecosystem.Planned)),
		len(ecosystem.ByStatus(ecosystem.Rejected)))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "ecosystem: %v\n", err)
	os.Exit(1)
}

// replaceRegion swaps the generated region for body, leaving the prose alone.
func replaceRegion(doc, body string) (string, error) {
	i := strings.Index(doc, begin)
	j := strings.Index(doc, end)
	if i < 0 || j < 0 || j < i {
		return "", fmt.Errorf("%s has no generated region; it needs the BEGIN and END markers", docPath)
	}
	return doc[:i+len(begin)] + "\n\n" + body + "\n" + doc[j:], nil
}

// renderTable writes the three tables the manual carries: what exists, what
// is planned, and what was rejected. The third is not padding — a rejection
// with a recorded reason is the only kind that stays rejected.
func renderTable() string {
	var b strings.Builder

	b.WriteString("### Shipping\n\n")
	b.WriteString("| Repository | Licence | Lockstep | What it owns |\n|---|---|---|---|\n")
	for _, r := range ecosystem.ByStatus(ecosystem.Shipping) {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", r.Name, r.Licence, yesNo(r.Lockstep), r.Role)
	}

	if planned := ecosystem.ByStatus(ecosystem.Planned); len(planned) > 0 {
		b.WriteString("\n### Planned\n\n")
		b.WriteString("**These do not exist yet.** They are recorded so the layout cannot drift\n")
		b.WriteString("silently once they do, and so nobody goes looking for them. Every row states\n")
		b.WriteString("the boundary that forces a separate repository and the criterion for\n")
		b.WriteString("archiving it.\n\n")
		for _, r := range planned {
			fmt.Fprintf(&b, "#### `%s`\n\n", r.Name)
			fmt.Fprintf(&b, "%s\n\n", r.Role)
			fmt.Fprintf(&b, "- **Licence** %s · **%s** · **Lockstep** %s\n", r.Licence, r.Language, yesNo(r.Lockstep))
			fmt.Fprintf(&b, "- **Why separate** %s\n", r.Boundary)
			fmt.Fprintf(&b, "- **Archive when** %s\n\n", r.Kill)
		}
	}

	if rejected := ecosystem.ByStatus(ecosystem.Rejected); len(rejected) > 0 {
		b.WriteString("### Considered and rejected\n\n")
		b.WriteString("| Repository | Why not |\n|---|---|\n")
		for _, r := range rejected {
			fmt.Fprintf(&b, "| `%s` | %s |\n", r.Name, r.Reason)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// jsonRepo is the published shape. It is separate from the Go type on
// purpose: this one is a contract with repositories that are not written in
// Go, so it changes only deliberately.
type jsonRepo struct {
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Role      string   `json:"role"`
	Language  string   `json:"language"`
	Licence   string   `json:"license,omitempty"`
	Boundary  string   `json:"boundary,omitempty"`
	Lockstep  bool     `json:"lockstep"`
	Artefacts []string `json:"artifacts,omitempty"`
	Kill      string   `json:"archive_when,omitempty"`
	Reason    string   `json:"rejected_because,omitempty"`
}

func renderJSON() ([]byte, error) {
	out := struct {
		Comment       string     `json:"_comment"`
		SchemaVersion int        `json:"schema_version"`
		Family        string     `json:"family"`
		Standard      string     `json:"standard"`
		Repositories  []jsonRepo `json:"repositories"`
	}{
		Comment: "Generated from internal/ecosystem by scripts/ecosystem. Do not edit by hand. " +
			"Every repository in the family verifies its own row against this file in CI.",
		SchemaVersion: 1,
		Family:        "scout",
		Standard:      "REPO-STANDARD.md",
	}
	for _, r := range ecosystem.Family {
		jr := jsonRepo{
			Name: r.Name, Status: string(r.Status), Role: r.Role, Language: r.Language,
			Licence: r.Licence, Boundary: r.Boundary, Lockstep: r.Lockstep,
			Kill: r.Kill, Reason: r.Reason,
		}
		for _, a := range r.Artefacts {
			jr.Artefacts = append(jr.Artefacts, string(a))
		}
		out.Repositories = append(out.Repositories, jr)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

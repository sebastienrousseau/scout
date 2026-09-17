// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build ignore

// checkinventory writes docs/CHECKS.md from the source, and with -check
// fails when the committed file has drifted.
//
// scout publishes a number — "76 checks" — on its own front page. A figure
// a reader is asked to trust, produced by counting by hand, is the same
// class of claim scout exists to disbelieve. This reads the call sites.
//
// Every check is created through (*Session).check(id, title). Parsing that
// one call is the whole inventory; a check that does not go through it does
// not exist as far as the report is concerned.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type entry struct {
	ID    string
	Title string
	Sites int
	// Family marks a check whose id is built at run time, so one entry
	// here stands for one check per value the run encounters.
	Family bool
}

func main() {
	verify := len(os.Args) > 1 && os.Args[1] == "-check"

	found, err := inventory("internal/probe")
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-inventory:", err)
		os.Exit(1)
	}
	if len(found) == 0 {
		fmt.Fprintln(os.Stderr, "check-inventory: no checks found; the parser is looking in the wrong place")
		os.Exit(1)
	}

	want := render(found)
	const path = "docs/CHECKS.md"

	if !verify {
		if err := os.WriteFile(path, want, 0o644); err != nil { //nolint:gosec // documentation, not a secret
			fmt.Fprintln(os.Stderr, "check-inventory:", err)
			os.Exit(1)
		}
		fmt.Printf("check-inventory: wrote %s with %d checks across %d phases\n", path, len(found), phaseCount(found))
		return
	}

	got, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-inventory: %v\nRun: make checks\n", err)
		os.Exit(1)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		fmt.Fprintf(os.Stderr, "check-inventory: %s is stale — the source declares %d checks.\nRun: make checks\n", path, len(found))
		os.Exit(1)
	}
	fmt.Printf("check-inventory: %s matches the source (%d checks, %d phases)\n", path, len(found), phaseCount(found))
}

// inventory walks the package and records every (*Session).check(id, title).
func inventory(dir string) ([]entry, error) {
	fset := token.NewFileSet()
	byID := map[string]*entry{}

	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "check" || len(call.Args) != 2 {
				return true
			}
			id, ok1 := literal(call.Args[0])
			title, ok2 := literal(call.Args[1])
			// A computed id is a family of checks, not a missing one:
			// auth.source.<field> emits one per credential source in play.
			// Recording the prefix keeps the inventory honest — dropping it
			// is how a hand count and a generated one disagree, which is
			// exactly what this tool exists to stop.
			if !ok1 {
				prefix, isFamily := literalPrefix(call.Args[0])
				if !isFamily {
					return true
				}
				id = prefix + "*"
				if !ok2 {
					title = ""
				}
				if e, seen := byID[id]; seen {
					e.Sites++
					return true
				}
				byID[id] = &entry{ID: id, Title: title, Sites: 1, Family: true}
				return true
			}
			if e, seen := byID[id]; seen {
				e.Sites++
				if e.Title == "" && ok2 {
					e.Title = title
				}
				return true
			}
			byID[id] = &entry{ID: id, Title: title, Sites: 1}
			return true
		})
	}

	out := make([]entry, 0, len(byID))
	for _, e := range byID {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// literalPrefix pulls the fixed prefix out of `"auth.source." + field`.
func literalPrefix(e ast.Expr) (string, bool) {
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return "", false
	}
	return literal(bin.X)
}

func literal(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func phaseOf(id string) string {
	if i := strings.IndexByte(id, '.'); i > 0 {
		return id[:i]
	}
	return "other"
}

func phaseCount(es []entry) int {
	seen := map[string]bool{}
	for _, e := range es {
		seen[phaseOf(e.ID)] = true
	}
	return len(seen)
}

func render(es []entry) []byte {
	var b bytes.Buffer
	// Front matter carries the page's own meta description, so this page
	// does not share the site-wide one with every other manual page.
	b.WriteString("---\n")
	b.WriteString("# SPDX-License-Identifier: GPL-3.0-only\n")
	b.WriteString("description: >-\n")
	b.WriteString("  Every check scout runs against an MCP server, listed by phase with its id and what it asserts. Generated from the source, so it cannot drift.\n")
	b.WriteString("---\n\n")
	b.WriteString("<!-- Generated by scripts/checkinventory. Do not edit; run `make checks`. -->\n\n")
	b.WriteString("# The check inventory\n\n")
	fixed, families := 0, 0
	for _, e := range es {
		if e.Family {
			families++
		} else {
			fixed++
		}
	}
	fmt.Fprintf(&b, "scout runs **%d checks** across **%d phases**.\n\n", len(es), phaseCount(es))
	if families > 0 {
		fmt.Fprintf(&b, "%d of those are fixed, and %d is a family whose id is built at run time —\n"+
			"one check per value the run encounters, marked `*` below.\n\n", fixed, families)
	}
	b.WriteString("This file is generated from the source: every check is created through\n")
	b.WriteString("`(*Session).check(id, title)`, and this table is those call sites. CI fails\n")
	b.WriteString("when it drifts, so the figure scout publishes is the figure it implements.\n\n")
	b.WriteString("A check may be reached from more than one branch — a handshake can fail in\n")
	b.WriteString("several ways and report the same id. The count is of distinct ids, because\n")
	b.WriteString("that is what a reader sees in a report.\n\n")

	order := []string{"net", "discovery", "auth", "handshake", "protocol", "catalog", "execution", "performance", "resilience"}
	grouped := map[string][]entry{}
	for _, e := range es {
		p := phaseOf(e.ID)
		grouped[p] = append(grouped[p], e)
	}
	var rest []string
	for p := range grouped {
		known := false
		for _, o := range order {
			if o == p {
				known = true
				break
			}
		}
		if !known {
			rest = append(rest, p)
		}
	}
	sort.Strings(rest)

	for _, p := range append(order, rest...) {
		in := grouped[p]
		if len(in) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s — %d checks\n\n", p, len(in))
		b.WriteString("| Check | What it looks for |\n|---|---|\n")
		for _, e := range in {
			title := e.Title
			if title == "" {
				title = "_(title computed at run time)_"
			}
			if e.Family {
				title += " — one per value encountered"
			}
			fmt.Fprintf(&b, "| `%s` | %s |\n", e.ID, title)
		}
		b.WriteString("\n")
	}
	// markdownlint gates this file like any other. Ending with exactly one
	// newline and no run of blanks keeps the generator's output clean
	// rather than making the linter carry an exception for it.
	return append(bytes.TrimRight(b.Bytes(), "\n"), '\n')
}

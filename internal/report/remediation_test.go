// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package report

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// checkIDs reads the ids out of the generated inventory, which is itself
// generated from the call sites and gated. Going through it means this test
// cannot drift from the source without check-inventory failing first.
func checkIDs(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile("../../docs/checks.md")
	if err != nil {
		t.Fatalf("reading the inventory: %v", err)
	}
	re := regexp.MustCompile("<span id=\"check-[a-z0-9_-]+\"></span>`([^`]+)`")
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatal("no check ids found; the inventory format changed and this test did not")
	}
	return out
}

// TestRemediationKeysAreRealChecks is the guard that matters.
//
// Guidance filed under an id no check produces is guidance nobody will ever
// see, and it fails silently — the report simply renders without it. A typo
// here is invisible at run time, which is exactly the kind of mistake that
// survives review.
func TestRemediationKeysAreRealChecks(t *testing.T) {
	ids := checkIDs(t)
	for key := range remediations {
		// A family is keyed by its prefix; the inventory lists it as "*".
		lookup := key
		if strings.HasSuffix(key, ".") {
			lookup = key + "*"
		}
		if !ids[lookup] {
			t.Errorf("remediation %q matches no check in the inventory", key)
		}
	}
}

// TestRemediationsAreUsable: an entry that explains without saying what to
// change is the thing this file exists to replace.
func TestRemediationsAreUsable(t *testing.T) {
	for id, r := range remediations {
		if strings.TrimSpace(r.Means) == "" {
			t.Errorf("%s: no Means", id)
		}
		if len(r.Steps) == 0 {
			t.Errorf("%s: no Steps — an explanation with no action is the status quo", id)
		}
		for i, s := range r.Steps {
			if strings.TrimSpace(s.Title) == "" || strings.TrimSpace(s.Body) == "" {
				t.Errorf("%s: step %d is incomplete", id, i)
			}
		}
		// Long enough to have said something. The shortest honest Means in
		// the set runs well past this; the bound is here to catch a
		// placeholder, not to police prose.
		if len(r.Means) < 120 {
			t.Errorf("%s: Means is %d characters, which is a stub", id, len(r.Means))
		}
	}
}

func TestRemediationForResolvesFamilies(t *testing.T) {
	// A concrete id wins outright.
	if _, ok := RemediationFor("catalog.tools.output_schema"); !ok {
		t.Error("a documented check resolved to nothing")
	}
	// An undocumented one is absent rather than wrong.
	if _, ok := RemediationFor("net.dns"); ok {
		t.Error("an undocumented check resolved to something")
	}
	// Families resolve through their prefix, if any are registered.
	for key := range remediations {
		if !strings.HasSuffix(key, ".") {
			continue
		}
		if _, ok := RemediationFor(key + "an_observed_value"); !ok {
			t.Errorf("family %q did not resolve for an instance", key)
		}
	}
}

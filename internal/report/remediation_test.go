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
	re := regexp.MustCompile("<span id=\"check-[a-z0-9_-]+\" data-can-fail=\"(true|false)\"></span>`([^`]+)`")
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		out[m[2]] = true
	}
	if len(out) == 0 {
		t.Fatal("no check ids found; the inventory format changed and this test did not")
	}
	return out
}

// failableIDs are the checks the generator detected as able to reach a fail
// or a warn — the ones that can appear in a report's "what to fix first".
//
// It is a lower bound. A check opened through a helper rather than directly
// is not detected, which is why catalog.text.* reads false despite failing
// for real. Erring that way means the gate below can under-demand, never
// over-demand, and an undetected check simply goes unguarded rather than
// failing the build for no reason.
func failableIDs(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("../../docs/checks.md")
	if err != nil {
		t.Fatalf("reading the inventory: %v", err)
	}
	re := regexp.MustCompile("data-can-fail=\"true\"></span>`([^`]+)`")
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatal("no failable checks found; the inventory format changed and this test did not")
	}
	return out
}

// TestEveryFailableCheckHasGuidance is the thoroughness gate.
//
// A check that can appear in "what to fix first" and has no guidance
// renders as a bare one-line advice string — which is where this whole
// catalogue started. Adding a failing check without writing for it should
// stop the build, not quietly regress the report.
func TestEveryFailableCheckHasGuidance(t *testing.T) {
	var missing []string
	for _, id := range failableIDs(t) {
		if _, ok := RemediationFor(id); !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d check(s) can fail with no remediation written: %s",
			len(missing), strings.Join(missing, ", "))
	}
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
	// An id nothing produces is absent rather than wrong. Using a made-up id
	// rather than a real-but-undocumented one, because the set of documented
	// checks is meant to grow and this assertion should not need revisiting
	// every time it does — which it just did.
	if _, ok := RemediationFor("not.a.real.check"); ok {
		t.Error("an unknown id resolved to guidance")
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

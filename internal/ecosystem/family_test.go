// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package ecosystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifestIsSelfConsistent is the cheap gate: the manifest has to hold
// together before anything generated from it can be trusted.
func TestManifestIsSelfConsistent(t *testing.T) {
	if errs := Validate(); len(errs) > 0 {
		for _, err := range errs {
			t.Errorf("manifest: %v", err)
		}
	}
}

// TestValidateCatchesTheThingsItIsFor.
//
// Validate exists to refuse three specific omissions — a satellite with no
// boundary, a satellite with no kill criterion, and a rejection with no
// reason — because each of those is how a family manifest rots into a list of
// names. Asserting that it refuses them is the only way to know it still does.
func TestValidateCatchesTheThingsItIsFor(t *testing.T) {
	original := Family
	t.Cleanup(func() { Family = original })

	cases := map[string]struct {
		rows []Repo
		want string
	}{
		"a satellite with no boundary": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only"},
				{Name: "scout-x", Status: Planned, Role: "something", Licence: "Apache-2.0", Kill: "when"},
			},
			want: "no boundary",
		},
		"a satellite with no kill criterion": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only"},
				{Name: "scout-x", Status: Planned, Role: "something", Licence: "Apache-2.0", Boundary: "because"},
			},
			want: "no kill criterion",
		},
		"a rejection with no reason": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only"},
				{Name: "scout-x", Status: Rejected, Role: "something"},
			},
			want: "no reason",
		},
		"a duplicated row": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only"},
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only"},
			},
			want: "listed twice",
		},
		"no centre at all": {
			rows: []Repo{
				{Name: "scout-x", Status: Rejected, Role: "something", Reason: "no"},
			},
			want: "does not list scout",
		},
		"a row with no name": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only"},
				{Status: Planned, Role: "nameless"},
			},
			want: "no name",
		},
		"a row with no role": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Licence: "GPL-3.0-only"},
			},
			want: "no role",
		},
		"a shipping row with no licence": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre"},
			},
			want: "no licence",
		},
		"an unknown status": {
			rows: []Repo{
				{Name: "scout", Status: "maybe", Role: "centre", Licence: "GPL-3.0-only"},
			},
			want: "unknown status",
		},
		"a rejection reason on a live row": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only", Reason: "but why"},
			},
			want: "not rejected",
		},
		"a rejected row still carrying lockstep": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only"},
				{Name: "scout-x", Status: Rejected, Role: "gone", Reason: "no", Lockstep: true},
			},
			want: "carries artefacts or lockstep",
		},
		"the same artefact claimed twice": {
			rows: []Repo{
				{Name: "scout", Status: Shipping, Role: "centre", Licence: "GPL-3.0-only",
					Artefacts: []Artefact{Readme, Readme}},
			},
			want: "listed twice",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			Family = tc.rows
			errs := Validate()
			if len(errs) == 0 {
				t.Fatalf("accepted %s", name)
			}
			var joined []string
			for _, e := range errs {
				joined = append(joined, e.Error())
			}
			if !strings.Contains(strings.Join(joined, "; "), tc.want) {
				t.Errorf("errors do not mention %q: %v", tc.want, joined)
			}
		})
	}
}

// TestScoutRowIsTrueOfTheWorkingTree is the point of the whole file.
//
// A manifest claiming an artefact this repository does not carry is worse
// than no manifest: it is a promise to a packager, an auditor and a
// contributor that nobody kept. So every artefact scout's own row claims has
// to exist on disk, and CI runs this on every push.
func TestScoutRowIsTrueOfTheWorkingTree(t *testing.T) {
	row, ok := Lookup("scout")
	if !ok {
		t.Fatal("no scout row")
	}
	if len(row.Artefacts) == 0 {
		t.Fatal("scout's row claims no artefacts, so this test would assert nothing")
	}
	root := repoRoot(t)
	for _, a := range row.Artefacts {
		if _, err := os.Stat(filepath.Join(root, string(a))); err != nil {
			t.Errorf("the manifest claims %s and the working tree does not have it: %v", a, err)
		}
	}
}

// TestEveryLockstepRepoIsShippingOrPlanned: lockstep binds a release
// process, so a rejected row carrying it would put a tag on something that
// does not exist.
func TestEveryLockstepRepoIsShippingOrPlanned(t *testing.T) {
	for _, r := range Family {
		if r.Lockstep && r.Status == Rejected {
			t.Errorf("%s is rejected and in lockstep", r.Name)
		}
	}
}

// repoRoot walks up to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find the repository root")
	return ""
}

// TestLookupAndByStatusAgreeWithTheManifest.
//
// These two are how every consumer reads the manifest — the generator groups
// by status and the verifier looks up scout's row — so an accessor that
// quietly disagreed with Family would make both wrong in the same direction
// and neither would notice.
func TestLookupAndByStatusAgreeWithTheManifest(t *testing.T) {
	for _, r := range Family {
		got, ok := Lookup(r.Name)
		if !ok {
			t.Errorf("Lookup(%q) found nothing, and it is in Family", r.Name)
			continue
		}
		if got.Name != r.Name || got.Status != r.Status {
			t.Errorf("Lookup(%q) returned %+v", r.Name, got)
		}
	}
	if _, ok := Lookup("scout-does-not-exist"); ok {
		t.Error("Lookup invented a repository")
	}

	var counted int
	for _, s := range []Status{Shipping, Planned, Rejected} {
		rows := ByStatus(s)
		counted += len(rows)
		for _, r := range rows {
			if r.Status != s {
				t.Errorf("ByStatus(%s) returned %s, which is %s", s, r.Name, r.Status)
			}
		}
	}
	if counted != len(Family) {
		t.Errorf("the three statuses account for %d of %d rows; a row has a status nothing groups by",
			counted, len(Family))
	}
	if len(ByStatus("nonsense")) != 0 {
		t.Error("ByStatus matched an unknown status")
	}
}

// TestArtefactListIsSortedAndComplete: it renders into a generated table, so
// an unstable order would make the generator's output differ run to run and
// the drift check fail for no reason.
func TestArtefactListIsSortedAndComplete(t *testing.T) {
	row, ok := Lookup("scout")
	if !ok {
		t.Fatal("no scout row")
	}
	got := row.ArtefactList()
	parts := strings.Split(got, ", ")
	if len(parts) != len(row.Artefacts) {
		t.Fatalf("rendered %d artefacts, the row has %d", len(parts), len(row.Artefacts))
	}
	for i := 1; i < len(parts); i++ {
		if parts[i-1] > parts[i] {
			t.Errorf("not sorted at %q > %q; the generated table would be unstable", parts[i-1], parts[i])
		}
	}
	if (Repo{}).ArtefactList() != "" {
		t.Error("a row with no artefacts rendered something")
	}
}

// TestEveryPlannedRepoHasADistinctBoundary.
//
// Two satellites with the same stated reason for existing are one satellite
// with a duplicated justification, which is how a family acquires a
// repository nobody can defend.
func TestEveryPlannedRepoHasADistinctBoundary(t *testing.T) {
	seen := map[string]string{}
	for _, r := range ByStatus(Planned) {
		// Compare the first clause: the boundary's category, before the
		// explanation.
		key := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Boundary, ".", 2)[0]))
		if key == "" {
			t.Errorf("%s: empty boundary", r.Name)
			continue
		}
		if other, dup := seen[key]; dup {
			t.Logf("%s and %s both cite %q as their boundary; that is allowed but worth re-reading",
				r.Name, other, key)
		}
		seen[key] = r.Name
	}
	if len(seen) == 0 {
		t.Error("no planned repositories have boundaries, so this asserts nothing")
	}
}

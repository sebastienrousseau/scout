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

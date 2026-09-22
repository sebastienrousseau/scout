// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package supply

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildFixture compiles a tiny Go program and returns its path.
//
// A real binary rather than a golden file: the whole point is reading what
// the toolchain actually embedded, and a checked-in fixture would be
// reading what some past toolchain embedded once.
func buildFixture(t *testing.T, dirty bool) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no Go toolchain to build a fixture with")
	}
	dir := t.TempDir()

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture.example/server\n\ngo 1.24\n")
	write("main.go", "package main\n\nfunc main() {}\n")

	// A git checkout, so the toolchain stamps a revision. Without one
	// there is no provenance to read and half of this package is untested.
	if _, err := exec.LookPath("git"); err == nil {
		run := func(args ...string) {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
				"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
				"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			)
			_ = cmd.Run()
		}
		run("init", "-q")
		run("add", ".")
		run("-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture")
		if dirty {
			// An uncommitted change is the whole of the dirty case.
			write("main.go", "package main\n\nfunc main() { _ = 1 }\n")
		}
	}

	out := filepath.Join(dir, "server")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not build a fixture binary: %v\n%s", err, b)
	}
	return out
}

// TestInspectReadsTheModuleGraph is the claim the milestone rests on: a Go
// binary carries its own inventory.
func TestInspectReadsTheModuleGraph(t *testing.T) {
	b, err := Inspect(buildFixture(t, false))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if b.GoVersion == "" {
		t.Error("no toolchain version")
	}
	if b.Main.Path != "fixture.example/server" {
		t.Errorf("main module = %q", b.Main.Path)
	}
	if b.GOOS != runtime.GOOS || b.GOARCH != runtime.GOARCH {
		t.Errorf("built for %s/%s, want %s/%s", b.GOOS, b.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if !strings.Contains(b.Summary(), "fixture.example/server") {
		t.Errorf("the summary does not name the module: %q", b.Summary())
	}
}

// TestInspectFlagsADirtyBuild is the milestone's own proof. A binary built
// from a tree with uncommitted changes cannot be traced to any commit.
func TestInspectFlagsADirtyBuild(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git, so there is no version-control stamp to read")
	}

	clean, err := Inspect(buildFixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if !clean.HasVCS {
		t.Skip("the toolchain did not stamp a revision here")
	}
	if clean.Dirty {
		t.Errorf("a clean checkout was reported dirty at %s", clean.Revision)
	}
	if clean.Revision == "" {
		t.Error("a clean checkout recorded no revision")
	}

	dirty, err := Inspect(buildFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if !dirty.Dirty {
		t.Error("a build from a modified tree was not flagged")
	}
}

// TestInspectRejectsSomethingThatIsNotAGoBinary, with an error a caller
// can tell apart from a missing file.
func TestInspectRejectsSomethingThatIsNotAGoBinary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notgo")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho hi\n"), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	_, err := Inspect(p)
	if !errors.Is(err, ErrNotGo) {
		t.Fatalf("want ErrNotGo, got %v", err)
	}
}

// TestInspectResolvesThroughPATH, because the operator wrote what they
// would have typed.
func TestInspectResolvesThroughPATH(t *testing.T) {
	bin := buildFixture(t, false)
	dir, name := filepath.Split(bin)
	t.Setenv("PATH", strings.TrimSuffix(dir, string(filepath.Separator)))

	if _, err := Inspect(name); err != nil {
		t.Errorf("a program on PATH was not found: %v", err)
	}
}

// TestInspectOnNothing reports rather than panicking.
func TestInspectOnNothing(t *testing.T) {
	if _, err := Inspect("   "); err == nil {
		t.Error("an empty command should be an error")
	}
	if _, err := Inspect("scout-no-such-program-anywhere"); err == nil {
		t.Error("a missing program should be an error")
	}
}

// TestUnpinnedFindsModulesWithNoChecksum. A module with no h1: sum did not
// come through the proxy and cannot be verified afterwards.
func TestUnpinnedFindsModulesWithNoChecksum(t *testing.T) {
	b := &Build{Deps: []Module{
		{Path: "example.com/a", Version: "v1.0.0", Sum: "h1:abc="},
		{Path: "example.com/b", Version: "v1.0.0"},
		{Path: "example.com/c", Version: "v1.0.0", Sum: "  "},
	}}
	got := b.Unpinned()
	if len(got) != 2 || got[0] != "example.com/b" || got[1] != "example.com/c" {
		t.Errorf("Unpinned = %v, want b and c", got)
	}
	if len((&Build{}).Unpinned()) != 0 {
		t.Error("a binary with no dependencies reported unpinned ones")
	}
}

// TestSummaryReadsAsASentence, since it is what lands in the report.
func TestSummaryReadsAsASentence(t *testing.T) {
	b := &Build{
		GoVersion: "go1.27.1",
		Main:      Module{Path: "example.com/srv", Version: "v1.2.3"},
		GOOS:      "linux", GOARCH: "amd64",
		Deps: []Module{{Path: "a"}, {Path: "b"}},
	}
	got := b.Summary()
	for _, want := range []string{"example.com/srv v1.2.3", "go1.27.1", "linux/amd64", "2 direct"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q does not contain %q", got, want)
		}
	}

	// A development build has no meaningful version, and printing
	// "(devel)" as though it were one helps nobody.
	devel := &Build{GoVersion: "go1.27.1", Main: Module{Path: "x", Version: "(devel)"}}
	if strings.Contains(devel.Summary(), "(devel)") {
		t.Errorf("summary shows a placeholder version: %q", devel.Summary())
	}
}

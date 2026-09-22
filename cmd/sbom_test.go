// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sebastienrousseau/scout/internal/supply"
)

// The test binary is itself a Go binary built by the toolchain under test,
// which makes it the honest fixture for this: a real module graph, with
// real checksums, that nobody wrote by hand.
func selfPath(t *testing.T) string {
	t.Helper()
	p, err := os.Executable()
	if err != nil {
		t.Skipf("cannot find the test binary: %v", err)
	}
	return p
}

// TestSBOMEmitsADocumentAScannerCanRead.
func TestSBOMEmitsADocumentAScannerCanRead(t *testing.T) {
	out, code := run(t, "sbom", selfPath(t))
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var doc struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Serial      string `json:"serialNumber"`
		Metadata    struct {
			Tools []struct{ Name, Version string } `json:"tools"`
		} `json:"metadata"`
		Components []struct {
			Name, PURL string
			Hashes     []struct{ Alg, Content string }
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("what was written is not JSON: %v\n%s", err, out)
	}
	if doc.BOMFormat != "CycloneDX" || doc.SpecVersion != "1.6" {
		t.Errorf("format = %q %q", doc.BOMFormat, doc.SpecVersion)
	}
	if !strings.HasPrefix(doc.Serial, "urn:uuid:") {
		t.Errorf("serialNumber = %q", doc.Serial)
	}
	if len(doc.Metadata.Tools) != 1 || doc.Metadata.Tools[0].Version != Version {
		t.Errorf("the document does not carry scout's version: %+v", doc.Metadata.Tools)
	}
	// This binary links cobra, so a graph with nothing in it means the
	// command wrote a document about nothing and exited zero.
	if len(doc.Components) == 0 {
		t.Fatal("no components, from a binary that has dependencies")
	}
	var withHash int
	for _, c := range doc.Components {
		if c.PURL == "" || !strings.HasPrefix(c.PURL, "pkg:golang/") {
			t.Errorf("component %q has no usable purl: %q", c.Name, c.PURL)
		}
		if len(c.Hashes) > 0 {
			withHash++
		}
	}
	if withHash == 0 {
		t.Error("not one dependency carried a checksum, so the document cannot be verified")
	}
}

// TestSBOMRefusesSomethingThatIsNotAGoBinary. The failure mode this guards
// against is a pipeline that ingests an empty document and goes green: for
// a Python server there is nothing to read, and saying so beats writing a
// bill of materials with no materials in it.
func TestSBOMRefusesSomethingThatIsNotAGoBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		// LookPath resolves by extension there, so a script with no
		// recognised suffix is not executable and the error is a
		// different one.
		t.Skip("PATH resolution on Windows keys on the extension")
	}
	p := filepath.Join(t.TempDir(), "server.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { // #nosec G306 -- it must be executable
		t.Fatal(err)
	}

	out, code := run(t, "sbom", p)
	if code == 0 {
		t.Fatalf("a shell script produced a bill of materials:\n%s", out)
	}
	if out != "" {
		t.Errorf("something was written to stdout for a program it could not read:\n%s", out)
	}

	// The exit code alone does not say which failure this was, and the
	// final message goes to the process's own stderr rather than through
	// anything the harness captures. Asked of the command directly, so the
	// reason is the one under test and not "could not open the file".
	err := sbomCmd.RunE(sbomCmd, []string{p})
	if err == nil {
		t.Fatal("no error from a program that is not a Go binary")
	}
	if !errors.Is(err, supply.ErrNotGo) {
		t.Errorf("the reason is not the one being tested: %v", err)
	}
	if !strings.Contains(err.Error(), "not implemented yet") {
		t.Errorf("the message does not say the limit is scout's: %v", err)
	}
}

// TestSBOMReportsAMissingProgram rather than writing an empty document.
func TestSBOMReportsAMissingProgram(t *testing.T) {
	out, code := run(t, "sbom", filepath.Join(t.TempDir(), "absent"))
	if code == 0 {
		t.Fatalf("a program that does not exist produced a document:\n%s", out)
	}
}

// TestSBOMNeedsExactlyOneProgram: a bare `scout sbom` that read something
// from the environment would be a surprise in a pipeline.
func TestSBOMNeedsExactlyOneProgram(t *testing.T) {
	if _, code := run(t, "sbom"); code == 0 {
		t.Error("a bare sbom command succeeded")
	}
}

// TestBuildTimestampHonoursSourceDateEpoch. The serial is already derived
// from what is described, so the clock is the only field left that would
// make two descriptions of one binary differ.
func TestBuildTimestampHonoursSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	if got := buildTimestamp(); !got.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Errorf("buildTimestamp = %s, want the epoch it was given", got)
	}

	// A malformed value is somebody asking for reproducibility and getting
	// it wrong. Falling back is right; doing so silently is not, and the
	// note goes to stderr because stdout carries the document.
	t.Setenv("SOURCE_DATE_EPOCH", "the ides of march")
	before := time.Now().Add(-time.Minute)
	if got := buildTimestamp(); got.Before(before) {
		t.Errorf("a malformed epoch was used anyway: %s", got)
	}

	t.Setenv("SOURCE_DATE_EPOCH", "")
	if got := buildTimestamp(); got.Before(before) {
		t.Errorf("with nothing set, buildTimestamp = %s", got)
	}
}

// TestSBOMIsReproducible end to end, which is the claim the help text
// makes and the one a pipeline diffing two documents depends on.
func TestSBOMIsReproducible(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	self := selfPath(t)

	first, code := run(t, "sbom", self)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, first)
	}
	second, _ := run(t, "sbom", self)
	if first != second {
		t.Error("the same binary described twice gave different bytes")
	}
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: Apache-2.0

package attestation

import (
	"bytes"
	"os"
	"testing"
)

// TestTheExampleFixtureIsCurrent keeps testdata/statement.json equal to
// what valid() marshals, so the Example reads a statement this package
// would itself accept. Regenerate with SCOUT_UPDATE_FIXTURES=1.
func TestTheExampleFixtureIsCurrent(t *testing.T) {
	want, err := valid().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/statement.json"
	if os.Getenv("SCOUT_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil { // #nosec G306 -- a test fixture
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; run SCOUT_UPDATE_FIXTURES=1 go test ./attestation/", path)
	}
}

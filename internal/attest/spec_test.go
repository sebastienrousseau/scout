// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package attest

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sebastienrousseau/scout/diagnostics"
)

// The published schema is generated from these types, and ADR 0011 offers
// it to anyone who wants to verify a statement without scout. A schema that
// rejected what scout actually writes — or accepted what it never would —
// is the failure worth a test: it would be adopted and then be wrong.

func publishedSchema(t *testing.T) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("../../spec/attestation/mcp-evaluation-v1.schema.json")
	if err != nil {
		t.Fatalf("the published schema is missing; run make spec: %v", err)
	}
	return b
}

func TestAStatementScoutWritesSatisfiesThePublishedSchema(t *testing.T) {
	st, err := From(sample())
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if errs := diagnostics.Validate(publishedSchema(t), b); len(errs) > 0 {
		t.Fatalf("scout's own statement fails its published schema:\n%s", strings.Join(errs, "\n"))
	}
}

func TestThePublishedSchemaRefusesWhatScoutNeverWrites(t *testing.T) {
	st, err := From(sample())
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	pred := doc["predicate"].(map[string]any)

	for name, mutate := range map[string]func(){
		"a status outside the vocabulary": func() {
			pred["verdicts"].([]any)[0].(map[string]any)["status"] = "passed-probably"
		},
		"another predicate type":    func() { doc["predicateType"] = "https://example.com/other/v1" },
		"no basis for the verdicts": func() { delete(pred, "judgedAgainst") },
	} {
		t.Run(name, func(t *testing.T) {
			saved, _ := json.Marshal(doc)
			mutate()
			bad, _ := json.Marshal(doc)
			if errs := diagnostics.Validate(publishedSchema(t), bad); len(errs) == 0 {
				t.Error("accepted")
			}
			_ = json.Unmarshal(saved, &doc)
			pred = doc["predicate"].(map[string]any)
		})
	}
}

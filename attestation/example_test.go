// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: Apache-2.0

package attestation_test

import (
	"fmt"
	"os"

	"github.com/sebastienrousseau/scout/attestation"
)

// A gateway deciding whether to route to a server: parse the statement it
// was handed, confirm it is about that server, and gate on one check.
// Verifying the envelope's signature is the job of the tool that signed it.
func Example() {
	b, err := os.ReadFile("testdata/statement.json")
	if err != nil {
		fmt.Println(err)
		return
	}
	st, err := attestation.Parse(b)
	if err != nil {
		fmt.Println("refuse:", err)
		return
	}
	if !st.Covers("http", "https://mcp.example.com/mcp") {
		fmt.Println("refuse: the statement is about another server")
		return
	}
	v, err := st.VerdictFor("protocol.origin")
	if err != nil {
		fmt.Println("refuse:", err)
		return
	}
	fmt.Println(v.ID, v.Status, v.Severity)
	// Output: protocol.origin fail major
}

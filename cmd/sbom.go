// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sebastienrousseau/scout/internal/supply"
	"github.com/spf13/cobra"
)

// The supply phase already reads a Go server's module graph out of the
// binary, and until now that graph only ever became two findings. This is
// the same read, emitted in the format the rest of a platform team's
// tooling already consumes.
//
// It is a separate command rather than a field on the report because the
// two documents answer to different readers. A report is read by whoever
// ran it; a bill of materials is ingested by a scanner, a registry, or an
// artifact store, and those want CycloneDX and nothing else in the file.
//
// No network, and nothing is executed: the program named is opened and
// read, not run.

// sbomCmd emits a CycloneDX document for a server binary.
var sbomCmd = &cobra.Command{
	Use:   "sbom <program>",
	Short: "Emit a CycloneDX bill of materials for a Go server binary.",
	Long: `Read a Go program's embedded module graph and write it as CycloneDX 1.6.

  scout sbom ./mcp-server > bom.json
  scout sbom mcp-server | grep unverifiable

The program is resolved through PATH, the way a shell would resolve it. It
is opened and read, never executed.

Every dependency the toolchain linked in is emitted as a component with its
package URL and its h1: module checksum, so the document is an inventory
somebody can verify rather than a list somebody can edit. A dependency that
carries no checksum did not come through the module proxy, and is marked
scout:unverifiable rather than left silently short of a hash.

The toolchain, the target platform, the commit and whether the tree was
dirty when it was built travel as metadata properties, because CycloneDX
has no field for them and a bill of materials that dropped them would say
less than the binary does.

This only works on a Go binary. Most MCP servers are Python or TypeScript,
and for those the command says so and exits non-zero rather than writing a
document with nothing in it.

The document is reproducible: the serial number is derived from what is
being described rather than generated at random, and SOURCE_DATE_EPOCH, if
set, fixes the timestamp. The same binary described twice gives the same
bytes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		build, err := supply.Inspect(args[0])
		if errors.Is(err, supply.ErrNotGo) {
			// Named as the limit it is. A caller piping this into a
			// scanner needs to know the difference between "nothing was
			// found" and "this was never going to work here".
			return fmt.Errorf("%w; only a Go binary carries an embedded module graph, "+
				"and a manifest-based inventory for other ecosystems is not implemented yet", err)
		}
		if err != nil {
			return err
		}
		return build.WriteCycloneDX(os.Stdout, Version, buildTimestamp())
	},
}

// buildTimestamp honours SOURCE_DATE_EPOCH.
//
// The serial number is already derived from what is described rather than
// generated, so the timestamp is the only field left that would make two
// descriptions of one binary differ. A pipeline that diffs yesterday's
// document against today's should see dependency changes, not a clock.
func buildTimestamp() time.Time {
	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC()
		}
		// A malformed value is the caller asking for reproducibility and
		// getting it wrong, which is worth saying out loud rather than
		// silently answering with the wall clock.
		fmt.Fprintf(os.Stderr, "scout: ignoring SOURCE_DATE_EPOCH=%q: not an integer\n", v)
	}
	return time.Now().UTC()
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/sebastienrousseau/scout/internal/diag"
	"github.com/sebastienrousseau/scout/internal/web"
	"github.com/spf13/cobra"
)

var (
	serveAddr        string
	serveAllowRemote bool
	serveNoOpen      bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run scout's web interface on this machine.",
	Long: `Start scout's web interface: the same diagnostic as ` + "`scout check`" + `, driven
from a browser.

The run happens here. The endpoint you test, the credentials it uses and
the report it produces never leave this machine, which is the difference
between this and a hosted tester: one of those asks you to hand a working
credential to somebody else's server.

The listener binds loopback, validates Origin, and requires the token in
the URL it prints. Those are the same rules scout tells every MCP server
to follow.

Examples:
  scout serve
  scout serve --addr 127.0.0.1:8080
  scout serve --no-open`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, err := web.New(web.Options{
			Addr:        serveAddr,
			AllowRemote: serveAllowRemote,
			Version:     Version,
			Open: func(url string) {
				fmt.Fprintf(os.Stderr, "\n  scout is running at\n\n    %s\n\n", url)
				fmt.Fprintf(os.Stderr, "  The token in that URL is what authorises the page. Anyone with it\n")
				fmt.Fprintf(os.Stderr, "  can start runs from this machine, so treat it like a password.\n")
				fmt.Fprintf(os.Stderr, "  Press Ctrl-C to stop.\n\n")
				if !serveNoOpen {
					openBrowser(cmd.Context(), url)
				}
			},
		})
		if err != nil {
			return err
		}
		return srv.Serve(cmd.Context())
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", "127.0.0.1:0", "listen address; a port of 0 picks a free one")
	serveCmd.Flags().BoolVar(&serveAllowRemote, "allow-remote", false, "allow a non-loopback bind (this listener can start authenticated runs)")
	serveCmd.Flags().BoolVar(&serveNoOpen, "no-open", false, "do not open a browser")
}

// openBrowser is best-effort: failing to open one is not a reason to fail
// the command, because the URL is already on screen.
func openBrowser(ctx context.Context, url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	path, err := exec.LookPath(cmd)
	if err != nil {
		return
	}
	// #nosec G204 -- cmd is one of three literals above; url is scout's own
	// loopback address with a token it just minted.
	if err := exec.CommandContext(ctx, path, append(args, url)...).Start(); err != nil {
		diag.Debugf("could not open a browser: %v", err)
	}
}

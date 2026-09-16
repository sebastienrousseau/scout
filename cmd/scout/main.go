// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Command scout is the entry point for the scout CLI.
package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/sebastienrousseau/scout/cmd"
)

// execute is indirected so tests can run main without the real CLI
// parsing the test binary's arguments.
var execute = cmd.ExecuteContext

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	execute(ctx)
}

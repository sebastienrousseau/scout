<!-- SPDX-License-Identifier: GPL-3.0-only -->

# Getting started

## Install

```bash
go install github.com/sebastienrousseau/scout/cmd/scout@latest
```

A binary built this way reports `scout version dev`; the release
pipeline stamps the real version with `-ldflags`. Release archives,
`.deb`/`.rpm` packages and the Homebrew formula are described in the
repository's `pkg/` directory.

From source:

```bash
git clone https://github.com/sebastienrousseau/scout && cd scout
make build            # build/scout
make install          # /usr/local/bin/scout, manpages, completions
```

## First run

Against an open server:

```bash
scout check https://mcp.example.com/mcp
```

Against a server that gave you a bearer token:

```bash
export MCP_TOKEN=…
scout check https://mcp.example.com/mcp --token-env MCP_TOKEN
```

The text report lists the nine phases in order, each finding with its
status, what was observed and what to do about it, then the catalog,
execution and performance tables, the score with every deduction, and a
telemetry summary. Add `--report-dir ./out` to keep every format plus the
raw telemetry.

## Commands

| Command | Does |
|---|---|
| `scout check <endpoint>` | the full nine-phase diagnostic |
| `scout connect <endpoint>` | net, discovery, auth and handshake only |
| `scout tools <endpoint>` | the connection phases plus the catalog audit, no invocations |
| `scout call <endpoint> <tool>` | one tool invocation with `--arg field=value` or `--json` |
| `scout login <endpoint>` | authorize as a user in the browser and store the token |
| `scout config init\|validate\|show` | the configuration file |
| `scout version` | print the version |

An endpoint can be replaced by `--profile <name>` when the config file
names one; see [Configuration](configuration.md).

## Exit status

| Code | Meaning |
|---|---|
| 0 | the run completed and no finding failed |
| 2 | the run completed and at least one finding failed |
| 1 | scout itself could not run: bad flags, unreachable config, an internal error |

Warnings do not change the exit status. A CI job that should fail on a
warning can read the JSON report's `counts.warn` instead.

## Where output goes

Results go to stdout in the format `--output` selects. Diagnostics, what
scout is doing and why something was skipped, go to stderr under
`--log-level` (`error`, `warn`, `info`, `debug`), or `SCOUT_LOG_LEVEL`
for a whole shell session. `--output json` therefore stays pipeable no
matter how noisy the run is.

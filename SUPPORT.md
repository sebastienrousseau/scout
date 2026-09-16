<!-- SPDX-License-Identifier: GPL-3.0-only -->

# Support

Where to take a question, in the order most likely to get you an answer.

## Before opening anything

Run the built-in diagnostics — most reports are answered by them:

```sh
scout version
scout connect <endpoint> --log-level debug
scout check <endpoint> --report-dir ./scout-out --capture-bodies
```

`connect` runs only the network, discovery, credential and handshake
phases, so it isolates "cannot get in" from "got in and something is
wrong". `--log-level debug` puts the reasoning on stderr while leaving
stdout parseable. `--report-dir` writes the full report and the redacted
telemetry (`telemetry.ndjson`, `telemetry.har`) that a bug report needs.

## Documentation

| You want | Read |
|---|---|
| Install, flags, usage | [README.md](README.md) |
| Package reference | <https://pkg.go.dev/github.com/sebastienrousseau/scout> |
| How it works internally | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| Why it works that way | [docs/adr/](docs/adr/README.md) |
| Contributing and toolchain | [DEVELOPMENT.md](DEVELOPMENT.md) |
| Security posture | [docs/security-model.md](docs/security-model.md) |
| Packaging for a distro | [docs/packaging.md](docs/packaging.md) |

Once installed, `man scout` is available offline, and every subcommand has
its own page (`man scout-check`).

## Questions and discussion

[GitHub Discussions](https://github.com/sebastienrousseau/scout/discussions)
— for "how do I", "should scout do X", "is this finding right", and
anything open-ended.

## Bugs and feature requests

[GitHub Issues](https://github.com/sebastienrousseau/scout/issues), using
the templates. A good report includes:

- `scout version`
- The exact command, with flags
- The `report.json` and `telemetry.ndjson` from `--report-dir`, or the
  stderr output at `--log-level debug`
- What you expected and what happened
- OS, and whether the server is one you can share a URL for

Please check the files before attaching them. scout redacts tokens, client
secrets, API keys and OAuth codes at the recorder, but a server's own
response content is yours to judge, and a pasted shell transcript may still
contain a credential you typed.

## Security

**Do not open a public issue for a vulnerability.** Follow the private
reporting process in [SECURITY.md](SECURITY.md).

## Response expectations

scout is maintained by one person alongside other work. Issues are read
within a week; security reports are prioritised per the SLA in
SECURITY.md. There is no commercial support offering.

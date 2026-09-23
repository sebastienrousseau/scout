---
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  For distribution maintainers: how to build scout reproducibly, what it depends on, and the guarantees each release artefact carries.
---

# Packaging scout

Written for distribution maintainers. Everything a packager needs to
decide whether scout fits their archive, and to build it without asking.

Maintainer contact: <sebastian.rousseau@gmail.com> · issues at
<https://github.com/sebastienrousseau/scout/issues>.

## What it is

`scout`: a CLI that connects to a remote Model Context Protocol server
with the credentials an operator was given, runs a nine-phase diagnostic
(network, OAuth discovery, credentials, handshake, protocol conformance,
catalog, safe execution, performance, resilience) and writes a scored
report with every request's telemetry. One static binary, no runtime
data files beyond the manpages and completions.

## Licence

**GPL-3.0-only**, SPDX-identified. `LICENSE` at the repository root is the
full text, and every source file carries an SPDX header, verified in CI by
`make spdx-check`, so the tree is machine-readable for REUSE-style tooling.

Dependencies are all permissive (Apache-2.0, BSD-3-Clause, MIT): cobra,
pflag, go-isatty, mousetrap and golang.org/x/sys. A CycloneDX SBOM is
attached to every release archive.

## Minimum toolchain policy

The floor is the `go` directive in `go.mod`. It is deliberately not
restated anywhere else, so it cannot disagree with itself; CI sets
`GOTOOLCHAIN=auto` and lets `go.mod` decide.

**When it may rise:** on any release, if a standard-library fix or language
feature is worth it. scout makes no distro-LTS compatibility promise. If
your archive pins an older Go, check `go.mod` before packaging a new
version rather than assuming the floor held.

The reason for each rise is recorded in the CHANGELOG entry for the release
that raises it.

## Building

No code generation at build time, no vendored tree, no CGO.

```sh
make build                     # or: go build -trimpath ./cmd/scout
make DESTDIR=$PWD/stage PREFIX=/usr install
```

`make install` honours `PREFIX` (default `/usr/local`) and `DESTDIR`, and
produces:

```text
$PREFIX/bin/scout
$PREFIX/share/man/man1/scout.1
$PREFIX/share/man/man1/scout-<subcommand>.1
$PREFIX/share/bash-completion/completions/scout
$PREFIX/share/zsh/site-functions/_scout
$PREFIX/share/fish/vendor_completions.d/scout.fish
$PREFIX/share/doc/scout/{README.md,CHANGELOG.md,LICENSE,SECURITY.md}
```

`make uninstall` removes exactly that set. Both are exercised in CI by
`make install-smoke`, which stages an install on a clean runner and asserts
the tree.

**Manpages and completions are generated, not committed.** `make install`
runs the generator; if you build without it, run `make docs` first. They
are rendered from the cobra command tree so they cannot drift from
`--help`.

## Dependency pin model

`go.mod` and `go.sum` are committed and authoritative; CI builds with
`-mod=readonly`, the Go default, so a build that would need to change
either fails instead. Builds are reproducible:

- `-trimpath`: no build-machine paths in the binary
- `mod_timestamp` pinned to the commit
- `CGO_ENABLED=0`: static, no libc coupling

Two builds of the same commit should be byte-identical. This has not yet
been verified by building twice and comparing, so it is stated as the
intent rather than a guarantee; please report it if it does not hold.

## Offline builds

Vendor the dependencies once, then build with no network:

```sh
go mod vendor
go build -mod=vendor -trimpath ./cmd/scout
go test -mod=vendor ./...
```

The test suite makes **no network calls**: every MCP and authorization
server it talks to is an `httptest` server in the same process. It is safe
in a sealed build environment.

## Runtime dependencies

None. A CA bundle is needed to verify TLS on the servers scout tests,
which every distribution's `ca-certificates` package provides.

No daemon, no system user, no configuration file required to run. The
optional config file lives at `$XDG_CONFIG_HOME/scout/config.json` and
the token store `scout login` writes at `$XDG_CONFIG_HOME/scout/tokens.json`
with mode 0600.

## Signature verification

Every release carries:

| Artefact | What it is |
|---|---|
| `checksums.txt` | SHA-256 of every asset |
| `checksums.txt.sigstore.json` | Keyless cosign signature (Sigstore bundle) |
| `checksums.txt.intoto.jsonl` | SLSA build provenance |
| `*.cdx.sbom.json` | CycloneDX SBOM per archive |
| `PKGBUILD` | the generated AUR recipe |

Verify a download:

```console
$ cosign verify-blob checksums.txt \
    --bundle checksums.txt.sigstore.json \
    --certificate-identity-regexp 'https://github.com/sebastienrousseau/scout/.*' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com

$ sha256sum -c checksums.txt --ignore-missing
```

Or, with the GitHub CLI:

```sh
gh attestation verify scout_Linux_x86_64.tar.gz --owner sebastienrousseau
```

There is no long-lived signing key: signing is keyless via OIDC, so there
is no `KEYS.asc` to import and no key rotation for you to track.

## Pre-built packages

The release pipeline publishes `.deb` and `.rpm` (via nfpm), a Homebrew
formula, and an AUR `PKGBUILD` attached to the release. If you are
packaging for an archive that prefers to build from source, ignore those
and use `make install` above; they exist for users, not to forestall
distribution packaging.

## What to watch when updating

- The version must have a heading in `CHANGELOG.md`; the release
  workflow refuses a tag without one.
- Manpage filenames follow the command tree. A new subcommand adds
  `scout-<name>.1`; a glob (`scout*.1`) is safer than a fixed list.
- `go.mod`'s `go` directive is the toolchain floor. Check it on every bump.

<!-- SPDX-License-Identifier: GPL-3.0-only -->

# Security Policy

scout is a Go command-line tool that connects to MCP servers with
credentials the operator supplies. Its security posture is about keeping
those credentials, and the servers' responses, out of places they should
not reach.

## Reporting a Vulnerability

Report security issues through [GitHub's private vulnerability reporting](https://github.com/sebastienrousseau/scout/security/advisories/new). Do not open a public issue.

You will receive an acknowledgement within **72 hours**. A confirmed
vulnerability is fixed and released within **90 days** of the report, or
sooner when a fix is straightforward; if the window cannot be met you will
be told why and given a revised date.

## Supported Versions

Only the latest release on the `main` branch is supported.

## Security Measures

Each item below names the workflow, file or test that enforces it, so the
claim can be checked rather than taken on trust. Nothing is listed here
that is not mechanically verified — a policy that overstates its controls
is worse than one that omits them.

### Credentials and server output

- **Secrets are redacted at the recorder, not at the print site.** Every
  HTTP exchange scout makes passes through `internal/telemetry`, whose
  `Redactor` masks `Authorization`, `Cookie` and any header whose name
  contains `key`, `token` or `secret` by name; masks `code`, `state`,
  `client_secret`, `access_token` and similar parameters in URLs and form
  bodies by name; and masks the JSON keys `access_token`,
  `refresh_token`, `id_token`, `client_secret`, `code`, `code_verifier`
  and `password` structurally, registering their values so a token
  issued in the middle of a run is masked everywhere it appears
  afterwards. Content types are not trusted: a body that starts with `{`
  is treated as JSON. Verified by `internal/telemetry/recorder_test.go`
  and by `TestFullRunClientCredentials` in `internal/probe`, which
  asserts that no operator-supplied secret, dynamically-registered client
  secret or issued token appears anywhere in the recorded events.
- **The report records where each credential came from, never its
  value.** `creds.Credentials.Describe` and `Sources` are what the report
  and the stderr banner print; both are covered by
  `internal/creds/creds_test.go`, which asserts the description leaks no
  secret.
- **The token store is written `0600`** in a directory created `0700`,
  atomically via a temporary file and rename, for both `Put` and `Delete`.
  A store whose mode has widened to group- or world-readable is refused
  rather than read: it holds refresh tokens and client secrets, which are
  password-equivalent. Concurrent writes are serialised so two logins
  cannot drop one another's token. Verified by
  `internal/creds/store_hardening_test.go`.
- **Credentials are bound to the origin the operator named.** An
  `http.RoundTripper` runs below `http.Client`'s redirect handling, so one
  that attaches a credential unconditionally re-attaches it on every hop of
  a redirect chain — Go's own stripping of sensitive headers on a
  cross-domain redirect does not apply to a header the transport adds
  itself. `auth.Transport` and `auth.HeaderTransport` consult an
  `auth.OriginSet`, and every client scout builds carries
  `auth.CheckRedirect`. Verified by
  `TestBearerDoesNotFollowRedirectOffOrigin` and
  `TestAPIKeyDoesNotFollowRedirectOffOrigin` in `auth/origin_test.go`, and
  end to end by `TestRedirectLeakIsRefused` in `internal/hostile`.
- **Endpoints learned from the server are validated before they are
  contacted.** `authorization_servers`, `token_endpoint`,
  `authorization_endpoint`, `registration_endpoint` and the
  `resource_metadata` challenge hint are all chosen by the server under
  test. `auth.URLPolicy` requires HTTPS and a public host for each, so a
  resource cannot direct a client secret to a plaintext endpoint or to an
  address inside the network scout is running in (cloud metadata included).
  Loopback is exempt so local development needs no flags; the strict check
  is relaxed only by `--insecure-allow-http-auth` or
  `--insecure-allow-private-hosts`. Every entry in `authorization_servers`
  is checked, not only the first. Verified by `auth/policy_test.go`.
- **Protected-resource metadata must identify the endpoint it describes**
  (RFC 9728), and the authorization response must carry the expected `iss`
  (RFC 9207). Both are refusals, not warnings; `--allow-resource-mismatch`
  is the deliberate override. Verified by `TestConnectErrorPaths` in
  `client_more_test.go`.
- **Requests that must arrive unauthenticated do.** A second, bare
  transport carries no token, header or basic credential, so the first
  contact and the invalid-token probe cannot be silently upgraded by the
  bearer round-tripper. See
  [ADR-0001](docs/adr/0001-bare-transport-for-unauthenticated-probes.md).

### Surviving a server that misbehaves

- **A hostile server gets a finding, not a crash.** `internal/hostile`
  starts MCP servers that deviate on purpose — acknowledging an id-bearing
  request with `202`, redirecting mid-session to another origin, streaming
  events without end, returning a body larger than memory, advertising a
  schema whose bounds overflow `int64` or whose `$ref` refers to itself —
  and `TestProbeSurvivesHostileServers` asserts that scout answers each with
  a report or a typed error, never a panic, a hang or an unbounded
  allocation. Response bodies and event streams are bounded by
  `transport.MaxResponseBytes`, `MaxStreamBytes` and `MaxStreamEvents`;
  generated arguments are bounded by clamped schema limits; and the
  telemetry recording is bounded by `telemetry.Recorder.MaxEvents`.

### What scout sends to a server

- **Only tools declaring `readOnlyHint: true` are invoked by default.**
  A tool without annotations is destructive under the MCP specification's
  defaults and is skipped; `--allow-mutations` and `--allow-destructive`
  are explicit opt-ins. Enforced by `diagnostics.Policy` and covered by
  `TestPolicyDefaults`; the probe suite's fake server panics if its
  destructive tool is ever called under the default policy.
- **Requests are throttled** to `--rps` (default 2) including the
  parallel burst, unless `--allow-load` is passed.
- **One deliberately invalid bearer token** is sent to check the server
  rejects it. Nothing else adversarial is sent: no fuzzing of server
  inputs beyond a single malformed JSON body and an unknown method name,
  both of which any JSON-RPC server must handle.

### Supply chain

- **Every GitHub Action is pinned to an immutable commit SHA**, never a
  mutable tag. Verified by OpenSSF Scorecard's Pinned-Dependencies check
  (`.github/workflows/scorecard.yml`).
- **Dependabot watches Go modules, GitHub Actions and the container base
  image** (`.github/dependabot.yml`).
- **`SBOM.md` lists every direct dependency** and is checked against
  `go.mod` by `make sbom` in CI.
- **No third-party code is vendored or embedded.** Dependencies resolve
  through Go modules with checksums recorded in `go.sum`. There is no
  CGO anywhere; released binaries are static.

### Release integrity

- **Releases are signed with keyless Sigstore/cosign** and carry SLSA
  build provenance, published as release assets so the build can be
  verified by anyone holding the artefact
  (`.github/workflows/release.yml`).
- **A CycloneDX SBOM is attached to every release.**

### Code and secrets

- **Gitleaks scans the full commit history** on every push and pull
  request (`.github/workflows/secret-scan.yml`).
- **`govulncheck`, `gosec` and `staticcheck` run on every pull request**
  (`.github/workflows/security.yml`, `.github/workflows/ci.yml`).
- **The parsing boundaries are fuzzed** (`.github/workflows/fuzz.yml`):
  `FuzzParseWWWAuthenticate` in `auth`, `FuzzReadSSE` in `transport`,
  `FuzzValidate` and `FuzzArguments` in `diagnostics`. Every byte of a
  `WWW-Authenticate` header, an SSE stream or a tool schema comes from
  the server under test.
- **Tests run with the race detector and randomised ordering**
  (`make test-race`).
- **Commits are cryptographically signed and carry a DCO trailer.**

Full software bill of materials in [SBOM.md](SBOM.md). The threat model,
including what is explicitly out of scope, is in
[docs/security-model.md](docs/security-model.md).

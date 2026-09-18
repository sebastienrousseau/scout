<!-- SPDX-License-Identifier: GPL-3.0-only -->

# Changelog

All notable changes to scout are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions are
[Semantic Versioning](https://semver.org/) shaped.

**Versions increment by 0.0.1 a release, whatever the release contains.**
The sequence runs `v0.0.1`, `v0.0.2`, … and reaches `v0.1.0` only after
`v0.0.999`. A new transport, a new output format or a new phase is still a
patch bump. The slow climb is deliberate: it lets maturity be earned over
many releases rather than declared, and a version number is not where this
project announces that a change felt big.

## [Unreleased]

Nothing yet.

## [0.0.2] — 2026-09-18

Versions here increment by 0.0.1 a release, whatever is in them. This one
carries a second transport, two output formats, five checks and an export
path, and it is still a patch bump: the slow climb is the point, and a
version number is not where a project announces that something felt big.

### Added

- **stdio transport.** `transport.Stdio` speaks newline-delimited JSON-RPC
  to a server running as a child process, which is what most MCP servers
  in the field actually are. It is not yet reachable from `scout check` —
  the client holds a concrete `*transport.Streamable` and the protocol
  phase probes at the HTTP level, so the seam is its own change.

  The lifecycle is the substance. `Close` closes stdin first, because that
  is how a well-behaved MCP server is told to stop, and kills what ignores
  it after a grace period; it always returns having reaped the process. A
  cancelled call ends the process, because the read is blocked on a pipe
  only the process can release. A line longer than `MaxLine` ends the
  connection. stderr is drained continuously — a server whose stderr fills
  the pipe buffer stops answering, which presents as a hang with no
  explanation — and kept, because a server that dies says why there and
  nowhere else.

  The environment is constructed, not inherited. `os/exec` treats a nil
  `Env` as "give the child everything", which for a tool that starts a
  program in order to find out what it does means handing over every
  exported credential. A nil `Env` means `BaseEnv` — PATH, HOME, TMPDIR,
  locale — plus whatever `PassEnv` names.

- **`--output sarif`** writes SARIF 2.1.0, which GitHub code scanning reads
  directly. Passing checks are included, as `kind: "pass"` with
  `level: "none"`: SARIF models a pass deliberately, and dropping them
  would leave a consumer unable to tell "checked and fine" from "not
  checked", which for a conformance tool is the whole difference. The
  location is the endpoint as an absolute URI — scout tests a running
  server, not a checkout, and inventing a repository path would be lying
  about where the problem is.

- **`--output junit`** writes JUnit XML, so a run appears beside the unit
  tests in whatever panel CI already has. A warning becomes a `<failure>`
  typed `warning`: JUnit has no third state, and a deviation reported as a
  pass stops being read.

- **`--otlp-endpoint`** exports the finished run as OpenTelemetry traces —
  a root span for the run, a span per phase, a span per request parented to
  its phase, and each finding as a span event. A request made outside any
  phase is parented to the root rather than dropped. It speaks OTLP/HTTP's
  JSON encoding rather than protobuf, so traces cost the binary no new
  dependency. The export never changes the verdict: a collector being
  unreachable is not a finding about the server under test.

- **`--log-format json`** writes scout's own diagnostics as one JSON object
  per line through `log/slog`, each carrying the run's `trace_id` — the
  same id on the report and on the exported spans.

- **Five checks that read the catalog for intent rather than shape.**
  `catalog.text.hidden`, `.comments`, `.instructions`, `.secret_paths` and
  `catalog.names.confusable`. A tool description is not documentation: it
  is input the model reads before deciding what to call, with the same
  standing as the user's own words. They read every string that reaches the
  model, including every `description` inside an `inputSchema` — the part
  nobody renders, and where published poisoning has most often been found.

  Severity is calibrated so the checks stay worth reading: `critical` is
  reserved for text with no honest reading at all. A scanner people learn
  to ignore is worse than none, so the first test written was a corpus of
  descriptions a careful author actually writes, several containing rule
  words in their ordinary sense, none of which may signal.

- **`doc_url` on every finding**, addressing that check's row in the
  published inventory. A report is normally read long after the run that
  produced it, often by somebody who was not there, and
  `protocol.malformed_json` is only self-explanatory to a reader who
  already knows what it means. A test fails the build when the generator's
  anchors and the runtime URLs disagree, so a link inside an archived
  report keeps resolving.

- **Per-page meta descriptions across the manual**, and `/manual/ci/`, a
  page on running scout in a pipeline: the exit-code contract, gating with
  `jq`, and what not to put on a cron against production.

### Fixed

- **0.0.1 published without its SLSA provenance.** The release workflow
  carried the Homebrew artefact from `dist/homebrew/Formula/scout.rb`,
  where the deprecated `brews:` wrote; `homebrew_casks` writes to `Casks/`.
  With `if-no-files-found: error` that step failed and took the attestation
  behind it down with it. Two further defects sat on the same path:
  `find dist/aur -name PKGBUILD` matched nothing because goreleaser names
  the file `scout-bin.pkgbuild`, and the tap job asserted `class Scout`,
  which a cask does not contain.

  Attestation and the release uploads now run before any packaging step, so
  a package host or a renamed artefact costs the tap pull request and
  nothing else. 0.0.1 is left as published: reusing a tag whose checksums
  are already in the sigstore transparency log is the supply-chain defect
  this tool exists to find.

- **The TUI and the blocked-run summary counted phases and called them
  checks.** A user saw "4 of 9 checks" from a tool that runs 81.

### Changed

- The check inventory gate now verifies the figure quoted on the site, in
  the local app, the manual index and the README — not only the generated
  table. `docs/checks.md` could never drift; the headline number could, and
  it is the one number a reader checks scout against.

- `docs/CHECKS.md`, `ARCHITECTURE.md` and `ECOSYSTEM.md` are lowercase, and
  the manual's URLs lost a redundant `/manual/` segment. Both were done
  before anything could link to the old addresses.

## [0.0.1] — 2026-09-17

The first release. Everything below was written before scout had ever been
tagged, so this entry is the whole of it rather than a diff against
something earlier.

### Security

- **Credentials no longer follow a redirect off the origin they were meant
  for.** `auth.Transport` and `auth.HeaderTransport` attach their headers
  inside `RoundTrip`, which sits below `http.Client`'s redirect handling, so
  Go's own stripping of sensitive headers on a cross-domain redirect did not
  apply: the header was re-added on every hop. A single
  `307 Location: https://attacker.example/` from a server scout was pointed
  at collected the operator's bearer token, API key or HTTP basic
  credentials. Both transports now consult an `auth.OriginSet` and every
  client scout builds carries an `auth.CheckRedirect` policy. A transport
  built without an explicit allow-list pins the first origin it carries.
- **Discovered OAuth endpoints are validated before they are contacted.**
  Everything after the operator's own endpoint is chosen by the server under
  test: `authorization_servers` comes from the resource, `token_endpoint`
  and `registration_endpoint` from the authorization server, and the
  `resource_metadata` hint from a response header. scout followed them
  unchecked, so a resource could send a client secret to a plaintext host,
  or aim the request at `169.254.169.254` and other addresses reachable from
  the CI runner. The new `auth.URLPolicy` requires HTTPS and a public host
  for every discovered URL, with `--insecure-allow-http-auth` and
  `--insecure-allow-private-hosts` as deliberate overrides. Loopback stays
  allowed so local development needs no flags, and an endpoint that is
  itself on a private address relaxes the check for its own network.
- **Protected-resource metadata must identify the endpoint it came from.**
  RFC 9728 requires the client to verify this binding; scout reported a
  mismatch as a warning and carried on requesting tokens anyway, which is
  the shape of a token mix-up. It is now refused, with
  `--allow-resource-mismatch` to override.
- **RFC 9207 issuer validation.** `CompleteAuthorizationFrom` checks the
  `iss` on the redirect against the issuer the code was requested from, and
  refuses a missing `iss` from a server that advertises support for it.
- **The token store refuses to be group- or world-readable.** It holds
  refresh tokens and client secrets; a store at anything other than 0600 is
  now an error rather than something to read anyway. `Delete` writes
  atomically like `Put` (it previously rewrote in place, so a crash mid-write
  lost every other endpoint's token), and both are serialised so two
  concurrent logins cannot drop one another's token.
- The `scout login` callback server, which listens on the operator's machine
  for the length of a browser flow, now sets read, header, write and idle
  timeouts.
- A body too large to parse as JSON is redacted by key name rather than
  passed through, so a token in an oversized response cannot reach a report.

### Fixed

- **Crash on a server that acknowledges a request.** `transport.Streamable`
  returned no response for `202 Accepted` and `204 No Content` — correct for
  a notification — and `Call` dereferenced it unconditionally. A server that
  answered an id-bearing request that way panicked scout with a stack trace
  instead of producing the finding that says so. It now returns
  `transport.ErrNoResponse`.
- **Crash on a schema with a large numeric bound.** `{"type":"integer",
  "maximum":1e19}` overflowed the `int64` conversion and reached
  `rand.Int64N` with a non-positive argument. Bounds are now clamped to the
  range a float64 represents exactly, and `minLength`/`minItems` are capped
  so a schema cannot turn a probe into a payload.
- **Every `*_ms` field in the JSON report held nanoseconds.** They were bare
  `time.Duration` values tagged `duration_ms`, so anything consuming scout's
  JSON was wrong by a factor of a million. A new `probe.Millis` type
  marshals as fractional milliseconds. `report.Report.Duration` and
  `diagnostics.Report.Duration`, which carried no JSON tag at all, are now
  tagged.
- **`$ref` and `$defs` are resolved.** pydantic, zod and the official SDKs
  all emit them for any nested model, and the validator ignored them
  entirely: those schemas produced no violations at all, and the argument
  generator could not build a value for them. Local JSON pointers are now
  followed, with cycle detection and a depth cap; a `$ref` pointing outside
  the document is reported as unchecked rather than passed over.
- Only the first entry of `authorization_servers` was checked for HTTPS,
  while discovery iterated all of them.
- A data race in the telemetry body recorder: `Read` mutated the byte count
  and capture buffer outside the mutex `finish` holds.
- The body capture cap was not enforced — a read landing one byte under the
  cap wrote its whole chunk — and the truncation note counted the retained
  bytes rather than the real size.
- `diagnostics.Limiter.Wait` leaked a timer per cancelled wait.
- `auth.AuthorizationCodeFlow` guards its PKCE verifier and state, and
  compares the state in constant time.

### Added

- **Protocol-era detection, and the transport seam for `2026-07-28`.** The
  current revision removed the `initialize` handshake and `Mcp-Session-Id`,
  moved every request's protocol version, client identity and capabilities
  into `_meta`, and mirrors the method and target name into `Mcp-Method` and
  `Mcp-Name` so a gateway can route without parsing the body. A new
  `transport.Dialect` puts that difference behind an interface:
  `transport.Sessioned` is the existing behaviour, extracted unchanged, and
  `transport.Stateless` implements the new binding, including the Base64
  sentinel encoding for header values, `resultType`, the Multi Round-Trip
  Request `input_required` result, and the `-32020`/`-32021`/`-32022` error
  codes. `Client.Negotiate` decides which generation a server speaks by
  trying a stateless request first and reading the body of a `400` before
  concluding anything, exactly as the specification's backward-compatibility
  rule requires.

  `scout check` now reports the answer as `handshake.protocol_era`: a server
  still on a handshake revision gets a warning naming what changed, and a
  server on the current revision is recognised even when it refuses the
  handshake-era first contact.

  The nine-phase diagnostic runs on both generations. The era is settled on
  the credential-free transport before first contact, because the shape of
  that first request depends on the answer, and the phases that assumed a
  handshake now branch:

  - `handshake` reports `handshake.server_info` from `server/discover` in
    place of `initialize`, and warns when a server implements neither.
  - `protocol.routing_headers` checks that a request whose `Mcp-Method`
    disagrees with its body is refused with `-32020`. Mirrored headers only
    buy a gateway anything if the server validates them; one that does not
    lets the gateway and the server act on different requests.
  - `protocol.get_stream` expects `405` on this revision, and warns when a
    server still serves the standalone stream it removed.
  - `resilience.stateless` replaces the session-recovery check: it sends the
    same request over two independent connections and compares the answers,
    because independence from the connection is what lets the server sit
    behind a round-robin load balancer.
  - The raw conformance probes are built through the active dialect, so they
    are well-formed requests of whichever generation is being tested. Before
    this they were rejected for missing `_meta` on a stateless server, and
    every protocol finding described the probe rather than the server.

  `--skip-era-check` suppresses the single `server/discover` that settles the
  generation. This project treats a new unavoidable request to the server
  under test as a breaking change, and that is the opt-out.
- `internal/hostile`: MCP servers that misbehave on purpose, and the table
  test asserting scout answers every one of them with a finding or a typed
  error rather than a panic, a hang, or an unbounded allocation. The rest of
  the suite exercises scout against a server that follows the rules, which
  is the wrong shape for a tool whose job is to be pointed at servers that
  do not — every crash fixed in this release passed the old suite.
- `telemetry.Recorder.MaxEvents` bounds the recording (default 5000);
  dropped events are still counted in the summary, and `Count()` stays
  monotonic so a finding's evidence range keeps its meaning.
- `diagnostics.Options.Concurrency` exercises tools in parallel under the
  same rate limit, so a large catalog is not serialised behind it.
- `report.SchemaVersion` in the JSON output, so a consumer can pin a format.
- `gosec`, `bodyclose`, `errorlint`, `nilerr`, `copyloopvar`, `makezero`,
  `rowserrcheck` and `noctx` in the lint gate.
- Response and event-stream size limits in `transport`
  (`MaxResponseBytes`, `MaxStreamBytes`, `MaxStreamEvents`).
- `scout check`: nine-phase diagnostic (net, discovery, auth, handshake,
  protocol, catalog, execution, performance, resilience) with a scored
  report in text, Markdown, JSON or NDJSON, and a report directory holding
  `report.{txt,md,json}`, `telemetry.ndjson` and `telemetry.har`.
- `scout connect`, `scout tools`, `scout call`, `scout login`, `scout config`.
- Credentials from flags, `SCOUT_*` environment variables, or config
  profiles: bearer token, API-key headers, HTTP basic, OAuth client
  credentials, and the interactive PKCE flow with a 0600 token store.
- Telemetry recorder with httptrace timings, TLS details, and redaction of
  every secret, including tokens issued during the run.
- Library packages `scout`, `auth`, `transport`, `diagnostics`, `trace`.
- Branded `--help`, in the same house style as the sibling CLIs: a solid
  braille flame with a coral gradient, the wordmark and tagline, a version
  line, and coral UPPERCASE sections (USAGE, EXAMPLES, COMMANDS, FLAGS,
  ENVIRONMENT). Piped, cobra's plain help is kept. `SCOUT_SHOW_LOGO=0`
  drops the flame.
- Plain-language report, printed to the terminal's own scrollback: a verdict
  ("Ready for agents" / "Ready, with room to improve" / "Not ready for
  agents" / "Couldn't finish the check"), a score out of 100 with a
  five-dot meter per area, and a numbered "What to improve" list — each a
  plain problem and a one-line fix. `--verbose` adds the per-check detail
  and `req#N` evidence for developers.
- Calm live view while a check runs, on Bubble Tea: the wordmark, the
  endpoint and a checklist of the nine checks with a spinner on the one in
  flight and a plain result as each finishes. No borders, no viewport to
  scroll; the finished checklist stays above the printed report. `q` stops
  the run; piped output prints one `✓ [PASS] phase: …` line per check.
- `scout check -i` opens a tool selector before the run, with corralctl's
  keybindings and slash commands.
- Manpages and bash/zsh/fish/PowerShell completions generated from the
  command tree (`make docs`), installed by `make install` under `PREFIX`
  and `DESTDIR`, with `make install-smoke` asserting the staged tree.
- Fuzz targets for the challenge parser, SSE reader, schema validator and
  argument generator, with committed seed corpora; benchmarks smoke-run in
  CI; `make api-check` against the last release tag.
- Release pipeline: goreleaser archives for Linux, macOS and Windows,
  cosign signatures, SLSA provenance, CycloneDX SBOM, deb/rpm, a Homebrew
  cask, AUR PKGBUILD, distroless container image pinned by digest.
- Governance and policy set: security policy with private reporting,
  contributing guide, code of conduct, governance, support, maintainers,
  citation, AI-agent invariants, architecture decision records, migration
  guides, rendered manual (MkDocs).
- The manual at <https://scoutmcp.io/manual/>: twenty-five pages covering
  the nine phases, credentials, the stateless revision, reading the
  evidence behind a finding, running scout in CI, reports and telemetry,
  library use, the security model and packaging. `docs/checks.md` is
  generated from the `(*Session).check(id, title)` call sites with
  `go/ast` and gated by `make checks-verify`, so the published inventory
  cannot claim a check the binary does not run: 75 fixed checks, one
  computed family, 76 across 9 phases.
- The site at <https://scoutmcp.io>, including a sample report produced by
  the binary built from the same commit rather than a screenshot.

[Unreleased]: https://github.com/sebastienrousseau/scout/compare/v0.0.2...HEAD
[0.0.2]: https://github.com/sebastienrousseau/scout/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/sebastienrousseau/scout/releases/tag/v0.0.1

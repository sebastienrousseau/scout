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

### Added

- **The server's own binary now says what it is made of.** scout scored
  how a server behaves on the wire and said nothing about the artifact
  behind it, which for a platform team is the first question they are
  asked.

  For a Go server it needs no new dependency and no network: a Go binary
  carries its own module graph — every dependency with its version and
  `h1:` checksum, the toolchain, the target platform, and, when it was
  built from a checkout, the commit and whether the tree was clean.
  `supply.buildinfo` reports the inventory; `supply.provenance` reports
  whether it can be traced to a commit.

  **A dirty build is the finding worth having.** `vcs.modified=true`
  means the source it was made from does not exist in the repository, so
  no review of that repository describes what is running. A build from a
  source archive carries no stamp at all, which is an observation rather
  than a dirty build, and the two are not conflated.

  Read out of the file, so it is one of the few things in the report the
  server cannot influence by answering differently. stdio only: an
  endpoint is a URL, and a URL is not a file scout can open. A server
  that is not a Go binary is reported as such — most are Python or
  TypeScript, and a manifest-based inventory for those is separate work.

- **`scout check --plant-canaries` says whether the server went looking
  for credentials it was never given.** The egress witness says where a
  server went; it cannot say what it took. The thing worth taking sits in
  the same place on almost every developer machine.

  Because scout starts the process, it decides where `HOME` points.
  Pointing it at a scratch directory holding a decoy `.ssh/id_rsa`,
  `.aws/credentials`, `.env` and `.netrc` turns "did it go looking" into
  a question with an answer, and costs a well-behaved server nothing.
  Each decoy carries a marker that exists nowhere else, so a marker
  seen leaving is not a suspicion — it is the file, in transit, labelled.

  `fs.credential_probe` reports the decoys being opened.
  `fs.canary_exfiltrated` reports their contents leaving, in a plain
  request body, on the server's own stderr, or handed back to scout in a
  result.

  **The instrument measures itself.** The obvious witness for "was this
  read" is the access time moving, and it is unreliable in a way that
  matters: macOS on APFS does not update it on an ordinary read at all,
  and Linux mounted `noatime` never does. So `Seed` writes a probe file,
  backdates it, reads it back and looks — and where the answer is no, the
  check reports that it **cannot tell** rather than that nothing was
  found. A security check that reported clean on a machine where it was
  incapable of reporting anything else would be worse than no check.

- **`scout check --watch-egress` says where the server went.** A server
  that quietly posts your tool arguments to a third host passes every
  other check: the catalogue is clean, the schemas validate, the
  annotations are honest, and the destination appears in no document
  anywhere, because not appearing is the point.

  You do not need a packet capture to see where a subprocess dials — you
  need to be the thing it dials through. scout runs a loopback proxy and
  points the child's `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` at it.
  `CONNECT` hands over the hostname in clear text before any handshake,
  so there is no certificate authority, no interception, and nothing read
  that the server sent.

  `egress.hosts` inventories the destinations and does not judge them: a
  GitHub server talks to GitHub, and scout cannot know which host is
  legitimate for a server it was handed five seconds ago. Supply
  `--expect-egress` and the same evidence becomes a gate —
  `egress.undeclared_host` fails on anything the operator did not name, a
  leading dot matching subdomains.

  stdio only, because it works by setting the child's environment. Two
  blind spots, documented rather than discovered later: a destination on
  the same machine is not seen, since almost every runtime refuses to
  proxy loopback, and a client that ignores the proxy environment is not
  seen either.

- **`catalog.cache_hints` asks whether the catalogue can be cached.**
  The budget check says what a catalogue costs to look at; this says
  whether the server did anything about it. Every client fetches the
  catalogue again on every session and then pays for it in context on
  every call, and the 2026-07-28 revision answers the first half of that
  directly: any list result may carry `ttlMs`, how long a client may keep
  it, and `cacheScope`, whether that cache may be shared.

  Both fields are optional, so silence warns only on a catalogue large
  enough for the re-fetch to cost something — the budget check's own warn
  threshold, so the two cannot disagree — and is an observation
  otherwise. It is skipped before 2026-07-28, where the fields do not
  exist: reporting their absence there would be a finding about the
  revision the operator runs rather than about the server.

  `Client.ListToolsWithHints` exposes them. `ListTools` is unchanged.

- **`scout badge` turns a report into a shields.io endpoint.** A score in
  a CI log is read once, by whoever ran it; the same score in a README is
  read by everyone deciding whether to point an agent at the server.

  ```sh
  scout check "$URL" --output json > report.json
  scout badge report.json > badge.json
  ```

  An endpoint document rather than an image, so there is no service to
  run and nothing to render. The colour follows the report's own grade
  instead of re-reading the number, so the badge and the document it came
  from cannot disagree about where a boundary is. A run that never
  reached a verdict renders as an error rather than as a low score: a
  badge reading `0/100` because the endpoint was unreachable would repeat
  exactly the mistake the exit-code contract exists to prevent.

- **`scout check --baseline` says what changed since you approved it.**
  A check tells you a server was sound when you ran it. It cannot tell
  you the server is still the one you reviewed, and that gap is the whole
  of the rug-pull threat: the server that passes review and edits its
  tool descriptions the following week is the one that gets through.

  `--baseline .scout/baseline.json` compares the catalogue against an
  approved snapshot; `--approve` writes the catalogue this run saw as the
  new one, after the report, so the person approving has just read what
  they are approving. The snapshot content-addresses the catalogue, so
  the common answer is one string comparison — which is what will make a
  watcher cheap enough to run on a schedule.

  **Severity is by kind, never by count.** A `readOnlyHint` becoming true
  after approval is critical, because it is the flip that makes a
  cautious client — scout included — start invoking a tool it previously
  refused. A description that gains text aimed at the model is critical,
  and is distinguished from one that was always odd, so an approved
  quirk is not re-reported every run. A dropped `required` argument is
  serious, because a widened schema accepts calls the approved one
  refused. A new optional property is reported as information: a gate
  that cries wolf over one is a gate somebody switches off.

  Re-formatting is not a change. A server that minifies its schemas one
  week and pretty-prints them the next has changed nothing a model can
  see, and the digest says so.

- **The interactive tool selector, in the browser.** `-i` was the last
  capability one surface had and the others did not, and it was
  surface-bound for no better reason than where the listing code was
  defined: connecting, listing the catalogue and classifying each tool
  sat in `cmd`, so only the CLI and the TUI could reach it.

  It moves to `engine.ListToolChoices`, and `scout serve` grows
  `POST /api/tools`, which takes the same `RunSpec` a run does and
  answers with the same rows the terminal draws — name, class, whether
  the current policy would invoke it, and the server's own description.
  Choosing a tool stays an explicit opt-in: the browser sends back the
  names, and `SelectTools` widens the policy for exactly those, the same
  call the CLI makes.

  The new route shares one admission gate with `POST /api/runs` rather
  than carrying a copy. Listing dials whatever endpoint the body names,
  with whatever credentials it carries, so a read-only convenience with
  its own nearly-identical checks would have been a second and quieter
  version of the path [ADR-0005](docs/adr/0005-public-mode-is-the-same-binary.md)
  closed. Public mode refuses credentials and off-allowlist endpoints on
  the selector exactly as it does on a run, and a program is refused on
  both.

- **Process custody over stdio.** The child is started in its own process
  group, so scout owns the tree rather than the one pid it was handed.
  Shutdown escalates — stdin closed, then `SIGTERM` to the group, then
  `SIGKILL` — and `Close` still returns having reaped everything. Putting
  the child in its own group also detaches it from scout's terminal, so
  Ctrl-C reaches scout alone and the server is shut down the documented
  way instead of the terminal signalling both and racing.

  Two checks come out of it, and neither is answerable until the process
  is gone, so they run after the phases and the resilience phase adopts
  them. `stdio.clean_exit` asks whether closing stdin was enough, because
  that is how a host ends a session and a server that ignores it is one
  that accumulates, a process per session, until something runs out.
  `stdio.no_zombie` asks whether the server's process group was empty once
  it exited — the question no other diagnostic asks, because no other
  diagnostic owns the process. A worker that outlives its server still
  holds what it was given and nothing is left that knows how to stop it;
  scout kills it, and says so, because a host will not.

  After a forced kill `stdio.no_zombie` skips rather than guessing: the
  group was ended to stop the server, so what it left behind cannot be
  told apart from what the signal stopped. Platforms without POSIX process
  groups skip it too, with the reason, rather than claiming the tree was
  clean.

- **Three checks that read the catalogue for intent rather than for
  quality.** The existing text checks ask whether a description is
  well-formed. These ask what it is trying to do.

  `catalog.text.shadowing` finds text that governs a tool other than the
  one it describes — "when calling `send_email`, always BCC…". The model
  reads every description with equal authority and no notion of which
  server each one came from, so a server being evaluated can rewrite the
  behaviour of a server already trusted without ever being called. A
  target this server does not itself list is reported as critical, because
  that is the cross-server shape. Constructions that merely point at a
  sibling — "use `list_directory` to find the path" — are passed over:
  that is what good documentation looks like, and a check that flags it is
  one people mute.

  `catalog.tools.annotation_honesty` reads `readOnlyHint: true` against
  the tool's own name and opening sentence. scout has a stake in this one:
  `diagnostics.Policy` invokes read-only tools and nothing else, so a
  server that annotates `delete_project` as read-only has found the way to
  make scout perform the deletion on a run the operator authorised
  precisely because it was supposed to be safe.

  `execution.error_guidance` grades what a rejected call said. The caller
  is a model and the error string is its entire recovery path, so "path
  must be absolute; use `list_directory` to find it" is a retry away from
  working and "invalid input" is not. A returned stack trace fails it
  twice over — unusable to the caller, and a disclosure of file layout,
  framework and versions to anyone who can call the tool. Returning
  `isError` is not itself counted against a server: scout calls tools with
  generated arguments and a correct server rejects some of them, so only
  the wording is graded, and a run with no rejection skips rather than
  passing.

- **`scout check --stdio -- <command>`.** The stdio transport that shipped
  in 0.0.2 was unreachable from the CLI, which meant scout could not
  diagnose most MCP servers: most of them are programs a host starts, not
  URLs it fetches. Now it can.

  ```bash
  scout check --stdio -- npx -y @modelcontextprotocol/server-everything stdio
  scout check --stdio --stdio-env GITHUB_TOKEN -- docker run -i --rm ghcr.io/example/mcp
  ```

  `connect`, `tools` and `call` take it too; for `call`, which has two
  operands, the tool comes before `--` and the server after it.

  Everything after `--` belongs to the server, including its own flags.
  There is no `--stdio-command "npx -y thing"` form, because splitting a
  command string means quoting rules and quoting rules mean a shell. The
  process is started when the run starts and reaped when it ends, on every
  path out. `--stdio-env NAME` forwards one variable by name, `--stdio-set
  NAME=value` replaces the environment outright, `--stdio-dir` sets the
  working directory. Credentials are refused rather than ignored: a pipe has
  no origin to authorize against, and silently dropping a `--token` would
  produce a report that reads as a test of an authenticated server.

- **Five checks that only a child-process run can make.** `stdio.process`
  (it started and is still running), `stdio.environment` (what scout handed
  it), `stdio.alive` (it survived the run), `stdio.stderr` (what it logged),
  and `stdio.stdout_clean`.

  The last one is the one that earns its place. Over stdio, stdout *is* the
  wire: every byte on it is parsed as protocol framing. One startup banner,
  one `print` left in a handler, one progress bar, and the stream is corrupt
  — and what a host reports is a hang, or a parse error naming a line nobody
  wrote. It never names the cause. This does, and quotes the line.

  These run even when an earlier phase blocked the rest of the run, because
  a blocked run is exactly when "the process exited" is the finding that
  explains all the others.

- **Every check a stdio run cannot make is reported as skipped, by id, with
  the reason.** Two phases — authorization discovery and credentials — plus
  `protocol.accept_header`, `protocol.get_stream`,
  `protocol.bogus_session`, `protocol.version_header` and
  `handshake.session`. None is left out.

  This is the part that took the most care. A run that silently contained
  fewer checks than the documentation promises reads as a better result than
  it is, and nothing downstream could detect the difference. `doc_url` still
  resolves for each of them, so a reader can see what was not done and why
  it could not be.

- **The four protocol probes that are not about HTTP now run over a pipe
  too.** An unknown method, a mismatched response id, a truncated body and
  a `tools/call` with no name are JSON-RPC questions. Skipping them over
  stdio would have been laziness dressed as honesty, so `transport.Stdio`
  gained `Exchange` — the pipe's counterpart to `Streamable.Do` — for a
  probe that has to send what a client library would refuse to build.

- **`transport.StdioConfig.Observe`** reports every exchange, and the probe
  layer wires it to the recorder through `telemetry.Recorder.RecordPipe`. A
  stdio finding cites `req#7` the way an HTTP one does; without it the
  evidence section of a stdio report was empty, and the report read as less
  rigorous for a reason that had nothing to do with the server.

### Changed

- **A timed-out call no longer kills a stdio server.** This is a behaviour
  change from 0.0.2, where it did — the read happened inline under a lock,
  so killing the process was the only way to free a goroutine blocked on a
  descriptor. It meant one slow tool ended a whole run, while the same
  timeout over HTTP costs a single finding.

  The connection now has one reader that matches replies to requests by id.
  An abandoned call is abandoned, the late reply is discarded rather than
  handed to whoever asks next, and concurrent calls are concurrent — which
  is also what lets the performance phase measure the server rather than
  scout's own mutex. Custody did not move: `Close` still owns the process,
  and every caller defers one.

- **A line on stdout that is not a JSON-RPC message ends the connection and
  is recorded.** It was already fatal to the call in flight; what is new is
  that the line is kept, so the report can name the cause instead of a
  timeout.

- **`Report.Target` carries `transport` and, for a stdio run, `command`.** A
  consumer comparing two reports has to be able to tell which kind of run it
  is reading: they do not contain the same checks, and the difference is not
  the server's. The command is redacted like any other field — an
  `--api-key=…` in an argument is ordinary, and a report is the one place it
  must not be.

- **`scout serve` refuses a run that names a program** unless started with
  `--allow-stdio`, and always refuses one in `--public` mode. The engine can
  do it and the CLI does, but "diagnose the URL in this field" and "run this
  command on the machine scout is running on" are not the same permission,
  and the token in the page's URL is not a credential anybody should be able
  to trade for the second.

- **`scout.New` starts a process when `Config.Stdio` is set**, and the
  client gained `Close`, `Stdio()` and `NewStdio(ctx, cfg)`. A pipe cannot
  exist before the process on the other end of it, so this is the one
  configuration where `New` does something a context belongs on.

- **A finding's detail is collapsed to one line.** Some of what goes into
  one comes from the server, and a validation library that returns a
  pretty-printed array put its newlines straight through the terminal
  layout, the Markdown table and the JUnit message.

- **The published check count is 86**, five of which are the stdio ones. The
  generator no longer counts `stdio.*` as a tenth phase — it briefly said
  "across 10 phases" while scout ran nine, which is the drift a generated
  page exists to prevent — and a new test fails when a group in the
  inventory is neither a phase nor a recorded exception.

### Fixed

- **A stdio server was reported as running for as long as anything it
  started.** `os/exec` copies a plain `io.Writer` stderr on a goroutine
  that `Wait` joins, so `Wait` returned when the last holder of the stderr
  descriptor let go rather than when the server exited — and a server that
  forks a worker leaves that worker holding it. Measured against a fixture
  that exits immediately and leaves a `sleep 5` behind: the shell was gone
  at 0.5s and `Exited()` still said false at 5.0s. Every check that asks
  whether the server is still running read that, `stdio.alive` included,
  and the shutdown grace was being counted against a process that had
  already gone. stderr now has a pipe of its own, drained by scout, so the
  process's exit is what ends the wait.

- `server/discover` results that carry the server's identity in `_meta`
  under `io.modelcontextprotocol/serverInfo`, where the 2026-07-28 revision
  and the reference SDKs put it, are now read. Such servers were reported as
  not implementing `server/discover`, and because the answer was discarded
  every capability they declared was then reported as undeclared by the
  catalog phase (#49). A discover answer with no identity anywhere is its
  own, smaller finding, and its capabilities are kept.

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

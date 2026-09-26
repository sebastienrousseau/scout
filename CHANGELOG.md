<!-- SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com> -->
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

### Fixed

- **The Arch install line installed someone else's program.** The
  README said `yay -S scout-bin`, and `scout-bin` on the AUR is an
  unrelated project. scout is now published to the AUR as `scout`, built
  from the release's source with its manpages and completions, beside
  `scout-mcp-bin` and `scout-agentgateway-extmcp`. The release no longer
  attaches a generated `scout-bin` PKGBUILD that could not be published
  under that name.

### Added

- **`scripts/aur-bump.sh` moves the three AUR packages to a release.** It
  recomputes every checksum from what the release published, then builds,
  installs and lints each package with `makepkg` and `namcap` in an Arch
  Linux container before anything is pushed; `--push` publishes and reads
  each package page back.

### Changed

- **The README follows the portfolio template.** The sections are the
  template's, in its order; scout's own sections are kept, each under the
  template section it belongs to, and nothing was dropped. Three sections
  are new, each backed by a page in the manual: Capabilities at a glance,
  Ecosystem comparison ([`docs/COMPARISON.md`](docs/COMPARISON.md), built
  from the migration guides) and Benchmarks
  ([`docs/BENCHMARKS.md`](docs/BENCHMARKS.md)). The benchmark numbers were
  re-measured; JSON rendering makes 7 allocations, not the 15 the reports
  page said, and that page now links to the one table instead of keeping a
  copy.
- **CI checks the README against the template.** `scripts/readme-check.sh`
  (`make readme-check`, and a step in the docs-lint workflow) fails on a
  heading out of the template's order and on an unresolved
  `{{VARIABLE}}` outside code, as AGENTS.md §7.3 requires.

## [0.0.6] — 2026-09-25

### Added

- **[`scout-mcp`](https://github.com/sebastienrousseau/scout-mcp) ships.**
  An MCP server over stdio that exposes scout as three read-only tools:
  `scout_check` evaluates an MCP server and returns its score and every
  failing check with its guidance, `scout_verify_attestation` checks a
  statement offline, and `scout_version` reports both versions. It
  evaluates loopback endpoints only unless the operator allowlists more,
  never sends credentials, and runs scout with an empty configuration so
  no local profile can switch on mutations. scout scores it 95 (A). The
  family manifest marks it shipping, and the release dispatch reaches it.

### Changed

- **ADR 0007 draws the line "never in the data path" means.** scout is
  in the data path when an agent request fails or waits because
  something scout operates is down, or when a request's bytes pass
  through code scout operates; neither is allowed. Code a gateway
  operator runs, such as the verifier and the agentgateway processor,
  admission at registration time, and an asynchronous feed to a
  gateway's control plane are inside the line. A dated section, so the
  original decision stays as written.

### Fixed

- **`go install …/cmd/scout@vX.Y.Z` reports its version.** Without the
  release pipeline's `-ldflags`, scout reported `dev`, and every
  attestation a CI job produced from the documented install named
  `scout dev` as its instrument, which nobody can reproduce. scout now
  falls back to the module version the Go toolchain embeds; a local
  build reports its pseudo-version, and `dev` remains only when the
  toolchain recorded nothing. An injected version still wins.
- **`protocol.get_stream` no longer waits out the call timeout.** A
  server that answers the GET with an event stream may hold it open and
  idle, and scout read the body until the 30-second timeout ended it,
  on every run, for a check that needs only the status and content
  type. It now stops at the headers. Against the reference `everything`
  server the protocol phase drops from about a minute to milliseconds,
  with the same verdicts and score.

## [0.0.5] — 2026-09-24

### Changed

- **`make api-check` compares against v0.0.4 with nothing excused.**
  The acceptance for the attestation package's aliases is gone: the
  baseline already carries them, so an entry would hide something new
  rather than excuse something known. `.api-check-accepted` stays, empty,
  as the place the next justified exception goes.

- **The stale v0.1.0 is retracted in `go.mod`.** The module proxy still
  serves a v0.1.0 whose tag was deleted early on, so `go install
  …/cmd/scout@latest` resolves to it rather than to the current release.
  Go honours a retraction only from a version above the one it retracts,
  and this project moves by 0.0.1 a release, so the directive is recorded
  and inert. Every `go install` snippet in the README, the manual and the
  site now pins the release instead of `@latest`, and
  `scripts/verify-release-versions.sh` fails a release whose snippets name
  anything else.

## [0.0.4] — 2026-09-24

### Added

- **`--soak N` asks whether the server's memory settles.** The fastest
  tool that succeeded is called `N` more times in sequence, and the
  resident memory of the server's process group is read from `/proc`
  after each. The first tenth of the samples is dropped as warm-up and a
  least-squares line is fitted through the rest; `resilience.soak_memory`
  fails as Major when the line explains most of the variation, adds up
  to at least sixteen mebibytes and a quarter of where it started, and
  is still rising over the last quarter of the window, or when the
  server stops answering or exits part way through. A collector's
  sawtooth has a slope and no fit; a warm-up has a fit and no slope
  after the cut; a heap that is settling flattens before the end; and a
  rise of a few mebibytes is what a runtime does on its way to its first
  collection, so it is reported and not judged. The measurement is the
  slope, never the peak.

  Stdio only, since the memory read is of a process scout started, and
  read on Linux; elsewhere the check is skipped by name, never passed.
  It needs at least 100 calls, is paced by `--rps` like every other
  call, and is recorded in the attestation's plan so `--reproduce`
  repeats it. 120 checks.

### Changed

- **The attestation verifier is its own module.** The `attestation`
  package moved unchanged to
  [`github.com/sebastienrousseau/scout-reporting`](https://github.com/sebastienrousseau/scout-reporting),
  the family's Apache-2.0 repository, and scout imports it back. It
  moved because importing a package from this module pulls every one of
  scout's dependencies into a consumer's `go.sum`, terminal UI included,
  which is not what a gateway signs up for when it adds a verifier. The
  predicate's JSON Schema moved with the types it is generated from;
  `spec/` here keeps the rubric, which is generated from the scorer.
  `scout/attestation` remains as a deprecated forwarder, so an existing
  importer keeps compiling and accepts exactly the same statements. One
  new direct dependency, first-party and standard-library only.
  `make api-check` reports the aliases as an identity change, which is
  what apidiff sees and not a break; `.api-check-accepted` names the
  package and the reason for this release, and fails on anything else.

## [0.0.3] — 2026-09-23

### Added

- **`protocol.origin` asks whether a web page can drive the server.** The
  Streamable HTTP transport requires servers to validate `Origin`,
  because DNS rebinding lets any site a user opens point a hostname at
  `127.0.0.1` and reach a local server from their browser. scout has
  told servers to do this since the web UI shipped, and until now never
  checked that they did.

  One ping, carrying the operator's credentials and a session, with
  `Origin: https://scout-origin-probe.invalid` — so a refusal can only
  be about the origin. 403 passes; another 4xx passes and says the
  specification asks for 403. A server that answers fails as Major on
  loopback or a private address, which is what rebinding reaches, and
  warns on a public one, where the rule still applies and the attack
  mostly does not. Over stdio it is skipped by name: a pipe has no
  headers and nothing to rebind.

  It came out of running the official conformance suite against a
  correct, minimal server, where it was the one scenario that measured
  something every server should do. Most of the rest need the suite's
  own fixture tools, which is why scout does not wrap it as a phase.

- **`scout sbom` emits a CycloneDX bill of materials.** The supply phase
  already reads a Go server's module graph out of the binary, and until
  now that graph only ever became two findings. This is the same read,
  written in the format a scanner, a registry or an artifact store
  already ingests.

  ```sh
  scout sbom ./mcp-server > bom.json
  ```

  Every dependency travels with its package URL and its `h1:` module
  checksum, which is what makes the document an inventory somebody can
  verify rather than a list somebody can edit. A dependency that carries
  no checksum is marked `scout:unverifiable` rather than left silently
  short of a hash: an absent one is indistinguishable from an oversight.
  The toolchain, the platform, the commit and whether the tree was dirty
  travel as properties, because CycloneDX has no field for them and a
  document that dropped them would say less than the binary does.

  The same binary described twice gives the same bytes. The serial
  number is derived from what is being described rather than generated
  at random, and `SOURCE_DATE_EPOCH` fixes the timestamp, so a pipeline
  diffing yesterday's document against today's sees dependency changes
  rather than a clock.

  For a TypeScript, Python or Rust server, name the project directory
  and its lockfiles are read instead: `package-lock.json`, `uv.lock`,
  `Cargo.lock` and `requirements.txt`, each package with the hash its
  package manager recorded. A requirement with no pin or no `--hash`, and
  a git or local-path source, is marked `scout:unverifiable` with the
  reason. A lockfile is what was declared rather than what is running,
  and the document says so in a `scout:evidence` property.

  ```sh
  scout sbom ./my-ts-server > bom.json
  ```

  A program that is not a Go binary, or a directory with no lockfile,
  exits non-zero — a pipeline that ingested an empty bill of materials
  and went green is the failure this exists to avoid. No network, and
  the program is opened and read, never executed. No new dependency
  either: the document is a few hundred lines of struct tags against a
  schema that has been stable for years, and taking a library for it
  would mean adding a dependency to the binary a security team has to
  approve in order to describe the dependencies in somebody else's.

- **A stdio run on Linux reports what the server did after its
  handshake.** Three `stdio.post_init_*` checks sample the server's
  process group under `/proc` while the other phases run: a socket to a
  non-loopback address that bypassed scout's proxy, a file open for
  writing outside the working directory, and processes started. Whatever
  was open when the handshake completed is bootstrap and is ignored.
  Sampling misses what opens and closes between samples, so seeing
  nothing is information, never a pass; off Linux the checks are skipped
  by name. This replaces the roadmap's "sandbox that snaps shut", which
  cannot be built from outside: Landlock and seccomp are restrictions a
  process applies to itself.

- **`--fault-upstream` asks what a tool does when its dependency is
  down.** At the end of a stdio run, the egress proxy holds every new
  connection open without answering, as a hung upstream would, and up to
  three tools that succeeded are called again. `resilience.upstream_down`
  passes a call that comes back with an error, `isError` or a cached
  answer, and fails one that does not come back within the call timeout,
  or a server that exits. Only calls during which the server tried to
  connect count. Off by default and stdio only; asked for over HTTP it is
  refused rather than skipped.

- **Discovery reads DPoP and Enterprise-Managed Authorization.**
  `discovery.dpop` fails a proof algorithm RFC 9449 forbids (`none`, an
  `HS*` MAC) and warns when a resource requires bound tokens that its
  authorization server or its 401 gives a client no way to learn about.
  `discovery.enterprise_managed` fails an authorization server that
  advertises the ID-JAG grant profile without the JWT bearer grant it
  needs. Both read metadata discovery already fetched and send nothing:
  scout holds no bound token and is not an identity provider. A server
  without DPoP is recorded as info, not marked down, while MCP's DPoP
  profile (SEP-1932) is still a draft.

- **`scout overlap` finds where servers used together interfere.** It
  compares the catalogues in two or more saved reports, offline, for the
  two failures a single-server run cannot see: the same tool name exposed
  by two servers, and one server's text attaching a rule to a tool
  another server owns. It reuses `catalog.text.shadowing`'s narrow
  matcher, so recommending a sibling tool is not reported, and it refuses
  a report with no catalogue rather than comparing it as clean.

  ```sh
  scout overlap mail.json weather.json
  ```

- **`catalog.tools.idempotency` reports what the catalogue says about
  retries.** An agent repeats calls after timeouts; `idempotentHint` is
  how a tool says a repeat is harmless. The check lists which
  state-changing tools declare it, and which leave it at the
  specification's default of "not safe to repeat" — information, since
  that default is the cautious answer. It warns only on a contradiction:
  a read-only tool declaring it is not idempotent. scout does not call a
  mutating tool twice to test this, because the effect of a call is not
  something it can observe from outside.

- **`scout explain` writes explanations beside a saved report.** It lists
  the report's failures and warnings, most severe first, with scout's own
  guidance for each, and never writes to the report. With `--model` and an
  Anthropic API key it also asks that model to explain each finding, and
  prints the answer beside the guidance, attributed to it. The model
  cannot change a status, a severity or a check id; those are copied from
  the report. Only the findings and scout's guidance are sent, the
  destination is announced first, and a key already in the environment
  sends nothing without `--model`. ADR 0006 is amended to list the new
  destination. No dependency: the Messages API call is the standard
  library.

  ```sh
  scout explain report.json --model claude-sonnet-5 > explanations.md
  ```

- **`scout verify --reproduce` repeats the recorded run.** A statement
  from `scout check` now carries its plan: the run specification with
  every secret value removed, the credential mode, the names of anything
  given by value, and the OS, architecture and kernel. `--reproduce`
  makes the same measurement again and gates on what got worse, as
  `--against` does. It contacts only a target named with `--endpoint`
  that the statement covers, takes credentials and permissions from its
  own command line and never from the statement, and refuses a plan that
  asks for permissions it was not given or names a different target from
  the subject.

  ```sh
  scout verify approved.json --reproduce --endpoint https://mcp.example.com/mcp
  ```

- **`scout verify --against` gates on drift.** Two statements about the
  same target are compared check by check, and the gate fails when any
  check got worse — pass or info to warn or fail, or warn to fail — even
  if the score did not move. Improvements, severity changes, checks the
  later run did not assess and checks it newly measured are listed; the
  score delta only when both were judged under the same rubric and
  inventory. Statements about different targets are refused. The
  comparison is `attestation.Compare`, in the Apache-2.0 package, so a
  gateway can run it without scout.

  ```sh
  scout verify today.json --against approved.json
  ```

- **The Tasks extension is checked.** For a server advertising
  `io.modelcontextprotocol/tasks`, four `protocol.tasks.*` checks ask
  what the extension requires: an unknown task id is refused with
  -32602; a client that did not declare the extension gets -32021; no
  task is returned to a call that did not ask for one; and a task scout
  creates — by calling a read-only tool it has already called — is
  retrievable at once, carries the required fields, reaches a terminal
  state within 30 seconds and keeps it. scout honours `pollIntervalMs`
  between 250 ms and 5 s, and cancels any task it does not see finish.
  A server that answers synchronously is not faulted; one on a handshake
  revision, or not advertising the extension, is skipped by name.

  Supporting it, the stateless dialect now keeps capabilities a caller
  declares on one request, and sets `Mcp-Name` to the task id on
  `tasks/*` requests, as the extension requires of a client.

- **`attestation` is a public, Apache-2.0 package for verifying a
  statement.** The statement types and the offline verifier — `Parse`,
  `Validate`, `Covers`, `VerdictFor`, and `SubjectFor` for producers —
  moved out of `internal/attest` into a package that imports only the
  standard library, so a gateway can embed it without taking on scout's
  GPL. `internal/attest` keeps the report-to-statement builder and
  re-exports the rest, and the published schema is unchanged. Error
  messages from the package now begin `attestation:`.

- **The attestation format is published under Apache-2.0** (ADR 0011).
  `spec/` holds a JSON Schema for the in-toto statement and its
  `mcp-evaluation/v1` predicate, and the scoring rubric as data — weights,
  deductions, grade bands and rules — so a gateway, registry or CI system
  can implement verification without taking on the engine's GPL. Both are
  generated from the code (`make spec`), CI fails when they drift, a test
  validates a real statement against the schema and refuses tampered ones,
  and another checks every published deduction against the scorer. The
  engine stays GPL-3.0; extracting the verifier into an Apache-2.0 package
  is the next step, on its own.

- **`scout sbom --osv` says which of those dependencies are known to be
  broken.** Every component from a public registry is looked up in OSV,
  and the advisories that affect it are added to the document as
  CycloneDX vulnerabilities, with aliases, CVSS vectors and severity. A
  Go binary's document now lists its standard library as a component,
  because that is where most Go advisories are.

  ```sh
  scout sbom ./my-ts-server --osv > bom.json
  ```

  It is the only network access the command makes and it is off by
  default. What is sent is announced on stderr first, and it is package
  URLs only; a component from a local path, a git URL or a private Go
  module is never sent. `--osv-url` points at a mirror for teams whose
  package names cannot leave the network. A failed lookup fails the
  command rather than writing a document that reads as clean. ADR 0006
  carries an amendment recording the new destination.

- **`execution.payload_size` measures what an answer costs the caller.**
  The catalogue budget measures what a server costs to look at; this
  measures what it costs to use. A tool result is not a file somebody
  downloads — it goes into the model's context, whole, on the call that
  asked for it, and the caller cannot refuse delivery: by the time the
  size is known the answer has already arrived.

  What separates a large answer from a broken one is whether the server
  knows it is large. A result that paginates, truncates, or says it was
  cut is reported as an observation, because the size is then a choice
  somebody made. Only a large result with no sign of being bounded is
  worth acting on.

  It costs no extra request: the execution phase already made these
  calls and already counted the bytes.

- **Performance is a gate now, not an adjective.** "Fast" is
  unfalsifiable; budgets with a red build are the only version of a
  performance claim that survives a year of commits.

  CI enforces two: the binary stays under 18 MiB (roughly 13 today,
  because a static binary a security team can approve in an afternoon is
  the product and the way that stops being true is one dependency at a
  time), and each renderer stays under an allocation ceiling, with a
  check that rendering scales linearly with the number of findings. An
  accidental quadratic passes every correctness test in the suite and is
  unusable on the catalogue sizes that make a diagnostic worth running.

  **Wall-clock budgets are published rather than gated**, which is a
  deliberate departure from the roadmap. A time limit on a shared runner
  is a flaky gate, and a flaky gate teaches people to re-run the build
  until it goes green — the same outcome as no gate, reached more slowly
  and with less trust. `docs/reports.md` now carries the measured render
  costs and the machine they came from.

- **`scout watch` is the part that does the remembering.** `--baseline`
  closes the drift gap for anyone who runs it again. This runs it again.

  ```sh
  scout watch "$URL" --baseline .scout/baseline.json
  ```

  A pulse is deliberately small — connect, list the catalogue, hash it,
  compare — because a watcher that re-ran nine phases on a loop would be
  the abusive client scout warns everyone else about. Two requests and a
  string comparison, which is what the content address in a snapshot was
  for, and a timer in between rather than a polling loop. Intervals under
  30 seconds are refused with the reason.

  `--once` takes a single pulse and exits on the same contract `scout
  check` uses: 2 when the catalogue is not the approved one, 0 when it
  is, 1 when scout never got an answer. **An unreachable server is a 1,
  never a 2** — a network blip is not a rug pull, and a gate that
  conflated them is one people switch off. `--output ndjson` emits one
  event per line; `--approve` promotes what the watch saw.

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

- **The performance phase repeats at most ten tools.** The repeat pass is
  serial and throttled, so its cost grew with the catalogue: fifty tools
  at five samples was a two-minute floor. It now repeats every tool up to
  ten, and beyond that the five slowest plus five picked by a seed from
  the target, so two runs against one server repeat the same tools and
  their figures compare. The finding says when it sampled.

  Two other performance ideas were measured and not built. A path that
  skips the recorder would save nothing visible — within noise for a
  200-byte answer, about 1.2 ms at 500 KB on loopback — and would leave
  latency findings with no request to cite. A profile-guided build
  changed no renderer beyond noise and raised Markdown allocations by
  21%.

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

- **Provenance is reported, not verified** (ADR 0010). `supply.provenance`
  and `scout sbom` report what a binary records about itself; signature
  verification is left to `cosign` and `gh attestation verify`, and the
  reports manual now shows how. A hand-written Sigstore verifier was the
  alternative, and a subtle bug in one produces a false "verified".

- **A line on stdout that is not a JSON-RPC message ends the connection and
  is recorded.** It was already fatal to the call in flight; what is new is
  that the line is kept, so the report can name the cause instead of a
  timeout.

- **Token counts stay named estimates** (ADR 0009). No tokenizer
  vocabulary is embedded: model families tokenize differently and several
  tokenizers are unpublished, so an exact count against one public
  vocabulary would be a precise answer about the wrong model. The
  catalogue-budget guidance now says so; bytes remain exact.

- **`Report.Target` carries `transport` and, for a stdio run, `command`.** A
  consumer comparing two reports has to be able to tell which kind of run it
  is reading: they do not contain the same checks, and the difference is not
  the server's. The command is redacted like any other field — an
  `--api-key=…` in an argument is ordinary, and a report is the one place it
  must not be.

- **scout has no adversarial mode, and will not grow one** (ADR 0008).
  The plan proposed exploit probes behind a separate command gated on a
  statement of ownership. That is closed rather than deferred: a binary
  that contains an exploit mode has to be reviewed as one, a confirmation
  prompt stops nobody, and the evidence such a probe needs is the harm
  itself. Protocol conformance probes and policy-permitted tool calls are
  unchanged.

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

- **The published check count is 119**, the stdio ones among them. The
  generator no longer counts `stdio.*` as a tenth phase — it briefly said
  "across 10 phases" while scout ran nine, which is the drift a generated
  page exists to prevent — and a new test fails when a group in the
  inventory is neither a phase nor a recorded exception.

### Fixed

- **The manual told you to sign a statement with `cosign attest-blob`.**
  That command wraps a predicate in a new statement about a file, so it
  would have nested scout's statement inside another and replaced its
  subject — the server — with the digest of a JSON file. A statement is
  signed as a blob with `cosign sign-blob`; the new
  [Signing attestations](docs/signing.md) page has the keyless workflow,
  an offline form that uploads nothing, and the order to verify in.

- **`inputRequests` was read in a shape the specification does not
  define.** On the 2026-07-28 revision a server that needs client input
  answers with `inputRequests` as an object keyed by request id, and/or a
  `requestState`. scout decoded a list, so a correct server's
  `input_required` failed to decode and was misread as an ordinary
  result, while a server sending the non-standard list passed. The
  transport now reads the object form (`InputRequiredResult` gains
  `RequestState` and `ListForm`) and `protocol.mrtr` judges by the
  specification's rules: a list fails; so does asking for elicitation,
  sampling or roots from a client that did not declare them — which,
  since scout declares none, means any such request — and any other
  method; a retry carrying only `requestState` passes. The test fakes
  and the hostile harness had the same misreading.

- **`resilience.stateless` compared list sizes, not lists.** It asks for
  `tools/list` on two independent connections and passed whenever both
  returned the same number of tools, so two different catalogues of the
  same size passed as "the same answer". It now compares every tool's
  definition, ignoring key order, and names the tools that differ. On
  the stateless revision lists are cacheable, so a client may serve one
  connection's answer to another; a difference is a wrong answer for
  somebody.

- **`protocol.extensions` read extensions from the wrong place.** The
  2026-07-28 schema puts a server's extensions in
  `capabilities.extensions`, keyed by identifier; scout read a top-level
  list no specification defines, so a correct server advertising Tasks was
  reported as advertising nothing. scout now reads
  `capabilities.extensions` (`DiscoverResult.ExtensionIDs`), and a server
  that advertises only in the top-level list gets a warning, because no
  client following the specification will see those extensions.
  `DiscoverResult.ExtensionSettings` is new; it is not on
  `ServerCapabilities`, which must stay comparable. The test fake had the same
  misreading, which is why the tests never caught it.

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

[Unreleased]: https://github.com/sebastienrousseau/scout/compare/v0.0.6...HEAD
[0.0.6]: https://github.com/sebastienrousseau/scout/compare/v0.0.5...v0.0.6
[0.0.5]: https://github.com/sebastienrousseau/scout/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/sebastienrousseau/scout/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/sebastienrousseau/scout/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/sebastienrousseau/scout/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/sebastienrousseau/scout/releases/tag/v0.0.1

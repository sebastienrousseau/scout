<!-- SPDX-License-Identifier: GPL-3.0-only -->

<p align="center">
  <img src="https://raw.githubusercontent.com/sebastienrousseau/scout/main/.github/logo.svg" alt="scout logo" width="128" />
</p>

<h1 align="center"><a id="scout"></a>scout</h1>

<p align="center">
  Test any Model Context Protocol server and find out, in plain language, whether it is ready for your agents — and exactly what to fix if it is not.
</p>

<p align="center">
  <a href="https://github.com/sebastienrousseau/scout/actions"><img src="https://img.shields.io/github/actions/workflow/status/sebastienrousseau/scout/ci.yml?style=for-the-badge&logo=github" alt="Build Status" /></a>
  <a href="https://pkg.go.dev/github.com/sebastienrousseau/scout"><img src="https://img.shields.io/badge/go.dev-reference-007d9c?style=for-the-badge&logo=go&logoColor=white" alt="Go Reference" /></a>
  <a href="https://golangci-lint.run/"><img src="https://img.shields.io/badge/lint-golangci--lint-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="golangci-lint" /></a>
  <a href="https://codecov.io/gh/sebastienrousseau/scout"><img src="https://img.shields.io/codecov/c/github/sebastienrousseau/scout?style=for-the-badge&logo=codecov" alt="Code Coverage" /></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/sebastienrousseau/scout"><img src="https://img.shields.io/ossf-scorecard/github.com/sebastienrousseau/scout?style=for-the-badge&label=OpenSSF%20Scorecard&logo=openssf" alt="OpenSSF Scorecard" /></a>
  <a href="https://sebastienrousseau.github.io/scout"><img src="https://img.shields.io/badge/docs-manual-brightgreen?style=for-the-badge&logo=github" alt="Documentation" /></a>
  <a href="https://github.com/sebastienrousseau/scout/releases/latest"><img src="https://img.shields.io/github/v/release/sebastienrousseau/scout?style=for-the-badge" alt="Release Version" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-GPL--3.0-blue?style=for-the-badge" alt="License" /></a>
</p>

---

## Contents

**Getting started**

- [Install](#install) — mise, Homebrew, Arch, Nix, Go, or from source
- [Quick Start](#quick-start) — diagnose a server in one command

**Features & Capabilities**

- [Features](#features) — nine phases, real credentials, honest scoring
- [Architecture](#architecture) — end-to-end flow from first contact to report
- [The nine phases](#the-nine-phases) — what each step sends and what it looks for
- [Interactive TUI Mode](#interactive-tui-mode) — the live checklist and the tool selector
- [Credentials](#credentials) — bearer, API key, basic, client credentials, user login
- [Safety](#safety) — read-only by default, throttled, nothing adversarial
- [Understanding your report](#understanding-your-report) — the verdict, the score, what to fix
- [Scoring](#scoring) — six weighted categories, every deduction named
- [Library use](#library-use) — the public packages the CLI is built on

**Reference & Operational**

- [Usage & Flags](#usage--flags) — complete CLI parameter reference
- [Configuration file](#configuration-file) — defaults and profiles keyed by flag name
- [Coming from another tool](#coming-from-another-tool) — Inspector, curl scripts, playgrounds
- [Examples](#examples) — index of runnable programmatic examples
- [Troubleshooting](#troubleshooting) — quick solutions to common errors
- [Frequently Asked Questions](#frequently-asked-questions) — design decisions

**Project**

- [Documentation](#documentation) — manual, API reference, developer docs
- [When not to use scout](#when-not-to-use-scout) — honest limits
- [Requirements & toolchain policy](#requirements--toolchain-policy) — the Go floor and when it moves
- [Stability guarantees](#stability-guarantees) — what a breaking change means here
- [Security & hardening](#security--hardening) — reporting, posture, fuzzing
- [License](#license)

---

## Install

### mise (macOS / Linux)

```bash
mise use -g github:sebastienrousseau/scout
```

This installs the latest released `scout` binary and keeps it managed with
the rest of your mise tools.

### Homebrew (macOS / Linux)

```bash
brew install sebastienrousseau/tap/scout
```

The tap ships a formula, so the same line works with Homebrew on Linux and
installs the manpages and completions alongside the binary. The `.deb`/`.rpm`
packages and the tarballs attached to each
[release](https://github.com/sebastienrousseau/scout/releases/latest) are the
alternatives, as are [mise](#mise-macos--linux) and the
[Go toolchain](#go-toolchain).

### Arch Linux (AUR)

```bash
yay -S scout-bin    # or: paru -S scout-bin
```

### Nix (any platform)

```sh
nix run github:sebastienrousseau/scout -- --help   # run without installing
nix profile install github:sebastienrousseau/scout # install
```

The flake ships the binary with its manpages and shell completions, and
`nix develop` gives a shell with every tool the project's CI gates need.

### Go toolchain

```bash
go install github.com/sebastienrousseau/scout/cmd/scout@latest
```

Installs into `$(go env GOPATH)/bin` (or `$GOBIN` when set). Note that a
binary built this way reports `scout version dev`: the real version is
stamped by the release pipeline through `-ldflags`, which `go install` does
not apply. Use a release artefact if you need `version` to be meaningful.

### Build from source

Requires Go 1.26.8+:

```bash
git clone https://github.com/sebastienrousseau/scout.git
cd scout
make install            # installs /usr/local/bin/scout, manpages, completions
```

`make install PREFIX=$HOME/.local` for a home-directory install.

### Platform Prerequisites

<details>
<summary><b>macOS</b></summary>

```bash
brew install go
```

</details>

<details>
<summary><b>Ubuntu / Debian / WSL2</b></summary>

```bash
sudo apt install golang
```

</details>

<details>
<summary><b>Fedora / RHEL</b></summary>

```bash
sudo dnf install golang
```

</details>

---

## Quick Start

Point `scout check` at a server's Streamable HTTP endpoint. It runs nine
checks, shows a calm live checklist while it works, and then prints a
plain-language verdict you can read at a glance:

```bash
# An open server
scout check https://mcp.example.com/mcp

# A server that handed you a bearer token
MCP_TOKEN=… scout check https://mcp.example.com/mcp --token-env MCP_TOKEN

# A server that handed you OAuth client credentials and a tenant parameter
scout check https://mcp.example.com/mcp --auth client-credentials \
    --client-id acme --client-secret-env ACME_SECRET --param profile_id=tenant-1

# Keep everything: text, Markdown, JSON, NDJSON events and a HAR archive
scout check https://mcp.example.com/mcp --report-dir ./scout-out
```

A finished run reads like this:

```text
  http://mcp.example.com/mcp
  acme-mcp 1.4.0 · MCP 2025-11-25 · open, no sign-in

  Ready, with room to improve   98 / 100   Excellent

  Agents can connect to this server and use its tools. 3 things are worth
  improving, but none of them blocks adoption.

What to improve
  1  Unknown method returns -32601
     HTTP 400 instead of a JSON-RPC error
     → answer 200 with a JSON-RPC error object
  2  Tools declare outputSchema
     None declare outputSchema
     → add outputSchema and return structuredContent so results are machine-checkable
  3  Tool latency profile
     search and find_symbol take up to 17s at p95
     → slow tools make agents time out or retry; cache or bound the work

How it scores
  connectivity    ●●●●●   100
  authorization   ●●●●●   100
  protocol        ●●●●●    95
  catalog         ●●●●●    95
  execution       ●●●●●   100
  performance     ●●●●●    95
  6 of 6 areas tested
```

The headline is for anyone — a plain verdict, a score out of 100, and the
things worth fixing in order. `--verbose` adds the per-check detail for
developers; `--report-dir DIR` saves the full report and every request.

One binary, one base command, and every operation is a subcommand of it:

| Command | Does |
| :--- | :--- |
| `scout check <endpoint>` | Run all nine phases and write the scored report |
| `scout connect <endpoint>` | Only net, discovery, auth and handshake — is it reachable and do the credentials work |
| `scout tools <endpoint>` | Connect and audit the tool, resource and prompt catalog without invoking anything |
| `scout call <endpoint> <tool>` | Invoke one tool with `--arg field=value` or `--json` and time it |
| `scout login <endpoint>` | Authorize as a user in the browser (PKCE) and store the token for later runs |
| `scout config init` | Write a commented configuration file listing every setting |

The exit status is 0 when nothing failed, 2 when any finding failed, and 1
on a scout error, so a pipeline can gate on it.

A run against a local server looks like this:

```text
ok    Network and TLS  [0.7ms]
      info  Endpoint uses HTTPS: plain http to loopback 127.0.0.1 (acceptable for local servers)
      ok    Hostname resolves
            1 address(es) in 0.0ms
      ok    TCP connection
            connected in 0.5ms

ok    Authorization discovery  [2.7ms]
      info  Unauthenticated initialize: 200 OK without credentials: the server is open

warn  Protocol conformance  [2.7ms]
      ok    ping
      warn  Unknown method returns -32601
            HTTP 400 instead of a JSON-RPC error
            → answer 200 with a JSON-RPC error object
      ok    Response id matches request id
…
Score
      connectivity   100.0  (weight 10)
      authorization  100.0  (weight 20)
      protocol        95.0  (weight 20)
            -5 protocol.unknown_method: HTTP 400 instead of a JSON-RPC error
      total           97.5  grade A · 6 of 6 categories assessed
```

---

## Features

| Feature | Description |
| :--- | :--- |
| **Nine phases, in order** | Network and TLS, OAuth discovery, credentials, the initialize handshake, protocol conformance, catalog audit, safe execution with content validation, latency and concurrency, session and token recovery. |
| **Your credentials, every kind** | Bearer token, API-key headers, HTTP basic, OAuth 2.1 client credentials with extra parameters, pinned endpoints for servers without discovery, or an interactive user login with PKCE. |
| **Evidence-backed findings** | Every finding cites the requests that produced it as `req#N`, matching the `seq` in the NDJSON telemetry and the entry in the HAR file. Nothing passes without a request that showed it. |
| **Full telemetry** | DNS, connect, TLS and time-to-first-byte per request from `httptrace`, TLS version and cipher, certificate expiry, redacted headers, byte counts, JSON-RPC method and error code. |
| **Structural redaction** | Tokens, client secrets, API keys, OAuth codes and cookies are masked by name in headers, URLs, forms and JSON bodies, and a token issued during the run is masked wherever it appears afterwards. |
| **Read-only by default** | Only tools that declare `readOnlyHint` are invoked. Unannotated tools are destructive by the MCP specification's default and are skipped; mutations are an explicit opt-in. |
| **Honest scoring** | Six weighted categories, each deduction named with the finding behind it, and the report says how many categories were actually assessed. |
| **Zero configuration** | Flags cover everything; a config file with profiles is there when you test the same servers repeatedly. |

---

## Architecture

A single run builds one HTTP client whose transport is wrapped by the
telemetry recorder, then wraps it again for the MCP client with tracing,
fixed headers and the bearer transport. A second, bare transport carries
nothing but the trace header, so the requests that must arrive
unauthenticated — first contact and the invalid-token probe — really do.
Phases run in order; a phase that finds the server unreachable, or that
credentials are missing for a protected server, marks every later phase as
skipped with that reason.

```mermaid
graph TD
    A[User Shell] --> B{scout check}
    B --> C[Resolve credentials<br/>flags › profile › defaults › env]
    C --> D[net: DNS, TCP, TLS]
    D -- unreachable --> Z[Report: later phases skipped]
    D --> E[discovery: unauthenticated initialize]
    E -- 200 --> H
    E -- 401 --> F[PRM → AS metadata → registration path]
    F --> G[auth: token with your credentials<br/>+ invalid-token probe]
    G -- rejected --> Z
    G --> H[handshake: initialize, capabilities, session]
    H --> I[protocol: ping, -32601, id echo,<br/>malformed JSON, bogus session]
    I --> J[catalog: tools, resources, prompts audit]
    J --> K{Policy}
    K -- readOnlyHint --> L[execution: generated or supplied args,<br/>outputSchema validation, missing-arg test]
    K -- unannotated / destructive --> M[skipped, reported]
    L --> N[performance: p50/p95, cold vs warm,<br/>bounded burst, 429 + Retry-After]
    N --> O[resilience: lost session, invalidated token]
    O --> P[Score + report<br/>text · md · json · ndjson · har]
    M --> N
```

Every request, whichever phase made it, passes through the same recorder,
so the report's telemetry section and the HAR file are complete by
construction.

---

## The nine phases

| Phase | What happens | Examples of findings |
| :--- | :--- | :--- |
| **net** | URL scheme, DNS, TCP, TLS handshake | plain HTTP to a public host, TLS 1.2 only, certificate expiring in 9 days |
| **discovery** | unauthenticated first contact; on 401, the challenge, RFC 9728 protected-resource metadata (hint, then path-aware and root well-known), RFC 8414/OIDC server metadata, PKCE, grants, registration path | no `WWW-Authenticate`, PRM resource differs from endpoint, S256 not advertised, no DCR or CIMD |
| **auth** | token acquisition with your credentials (static, CIMD, or dynamic registration), token shape, and whether the server rejects a made-up token | no `expires_in`, granted scope narrower than requested, **server accepts any bearer token** |
| **handshake** | `initialize`: protocol version, `serverInfo`, capabilities, instructions, session id | empty `serverInfo.version`, no capabilities declared |
| **protocol** | `ping`, unknown method, id echo, malformed JSON, invalid params, unknown tool, missing `Accept`, GET stream, bogus session id, bad protocol-version header | unknown method answered with HTTP 400, unknown session accepted |
| **catalog** | tools, resources, templates, prompts: unique names, descriptions, `inputSchema` shape, annotations, `outputSchema`, absolute URIs; capabilities vs what actually lists | 3 tools unannotated, tools listed without the capability |
| **execution** | invoke what the policy allows with generated or supplied arguments; validate `structuredContent` against `outputSchema`; omit a required argument and expect rejection; read resources; render prompts | `search` returns `hits` as a string, `lax` accepts a call with `x` missing |
| **performance** | ping baseline, repeat-call p50/p95/max per tool, cold vs warm, bounded parallel burst, 429 and `Retry-After` | p95 above 2s, errors under 4 workers |
| **resilience** | lose the session and recover, invalidate the token and recover | server ignores unknown session ids |

Run a subset with `--phases net,discovery,auth` or `--skip-phases
performance`. `scout connect` and `scout tools` are shorthands for the
connection phases and the catalog.

---

## Interactive TUI Mode

On a terminal, `scout check` shows a calm live checklist while it works —
the wordmark, the endpoint, and each of the nine checks with a spinner on
the one in flight and a plain result as it finishes:

```text
  scout
  http://mcp.example.com/mcp
  no credentials

  ✓  Connectivity    reachable over TLS   4ms
  ✓  Authorization   open server         92ms
  –  Credentials     not needed
  ✓  Handshake       acme-mcp 1.4.0      21ms
  ⠋  Protocol        checking…
  ○  Catalog
  ○  Execution
  ○  Performance
  ○  Resilience

  4 of 9 checks   ·   press q to stop
```

Press `q` at any time to stop. When the run finishes, the checklist stays
put and the report is printed below it, in your terminal's own scrollback.
There is nothing to scroll inside — it is just text you can select, copy
and page through as usual. Piped or redirected, the same run prints one
`✓ [PASS] net: 3 ok` line per check instead, so logs stay greppable.

By passing the `-i` or `--interactive` flag, you can pick the tools to
exercise before anything runs:

```bash
scout check -i https://mcp.example.com/mcp --token-env MCP_TOKEN
```

The selector connects, lists the tools with their kind (read-only,
mutating, destructive) and whether the default policy would run them, and
preselects the ones it would. Selecting a mutating or destructive tool is
an explicit opt-in for that tool only.

### Keybindings

- `[space]` — Toggle selection of the current tool.
- `[ctrl+a]` — Select all currently filtered tools.
- `[ctrl+n]` — Deselect all currently filtered tools.
- `[/]` — Enter command / filter mode.
- `[enter]` — Confirm selection and start the run.
- `[esc]` — Exit without running.

### In-Session Commands

Press `/` inside the TUI to enter Command Mode. Commands support
prefix-based autocompletion (press `[tab]` or `[right-arrow]` to
autocomplete):

- `/sort <field>` — Sort tools. Fields:
  - `name` — Alphabetical sort by tool name.
  - `kind` — Sort by kind (destructive, mutating, read-only).
  - `policy` — Sort by whether the default policy allows the tool.
  - `read-only` / `mutating` / `destructive` — Bring that kind to the top.
- `/all` — Select all filtered tools.
- `/none` — Deselect all filtered tools.
- `/exit` / `/quit` — Cancel and exit silently.
- `/help` — Display the in-session help panel overlay.

`SCOUT_SHOW_LOGO=0` replaces the flame with a plain title in both views.

---

## Credentials

Whatever you were handed, there is a flag for it, an environment variable,
and a config-profile setting with the same name.

| You were given | Use |
| :--- | :--- |
| a bearer token | `--token`, `--token-env NAME`, or `SCOUT_TOKEN` |
| an API key or tenant header | `--header "X-API-Key: …"` (repeatable) |
| a username and password | `--basic user:pass` or `SCOUT_BASIC` |
| an OAuth client id and secret | `--auth client-credentials --client-id … --client-secret-env NAME` (or `SCOUT_CLIENT_ID`, `SCOUT_CLIENT_SECRET`) |
| extra token parameters (tenant, profile, audience) | `--param key=value` (repeatable) |
| a scope | `--scope "a b"` (default: what the server's challenge asks for) |
| a token endpoint but no discovery | `--token-url … [--auth-url …] [--resource …]` |
| a hosted Client ID Metadata Document | `--client-metadata-url https://…` |
| a user login | `scout login <endpoint>` then `--auth authorization-code` |

`--auth auto` (the default) picks the mode from what is set. An explicit
`--client-id` wins over dynamic registration: an operator who was handed a
client id chose it deliberately. The report records where each credential
came from — flag, environment variable, profile — never its value.

---

## Safety

- **Read-only by default.** Only tools with `readOnlyHint: true` are
  invoked. Tools without annotations are destructive by the MCP
  specification's default and are skipped; the catalog phase warns about
  them so the server author can fix the annotations.
- **Mutations are opt-in.** `--allow-mutations` also runs tools that mutate
  but declare `destructiveHint: false`. `--allow-destructive` runs
  everything and is meant for a tenant you are willing to lose.
- **Narrow the set.** `--only NAME` and `--deny NAME` pick tools;
  `--arg tool.field=value` supplies real arguments instead of generated
  ones, and a tool given real arguments is not put through the
  missing-argument test.
- **Throttled.** Requests are capped at `--rps` (default 2). The parallel
  burst respects it unless you pass `--allow-load` to test the server's
  rate limiting for real.
- **One adversarial request.** The invalid-token probe sends a single
  request with an obviously made-up bearer token. Nothing else adversarial
  is sent, and nothing is fuzzed against a server you do not own.

### Safety in the other direction

Those rules protect the server. These protect you, because the server you
point scout at is by definition one you have not vetted yet.

- **Credentials are bound to the origin you named.** A bearer token, API-key
  header or HTTP basic credential is sent to that origin and nowhere else.
  A redirect leaving it is refused, not followed: an HTTP round-tripper sits
  below the redirect handler, so a client that attaches credentials without
  this check re-attaches them on every hop, and one `307` is enough to
  collect them.
- **Discovered endpoints are checked before they are contacted.** Everything
  after your own endpoint is chosen by the server: `authorization_servers`
  comes from the resource, `token_endpoint` and `registration_endpoint` from
  the authorization server, and the `resource_metadata` hint from a response
  header. Each must be HTTPS and must resolve to a public address, so a
  server cannot aim your client secret at a plaintext host or at an address
  inside the network your CI runner sits in. Loopback is always allowed, and
  an endpoint that is itself private relaxes the check for its own network.
- **The metadata must be about your endpoint.** A protected-resource
  document naming a different resource is refused: RFC 9728 requires that
  binding, and a mismatch is the shape of a token mix-up.
- **The authorization response must come from the right issuer.** The RFC
  9207 `iss` on the redirect is checked against the issuer the code was
  requested from.
- **A misbehaving server gets a finding, not a crash.** scout is tested
  against servers that misbehave on purpose — acknowledging a request with
  `202`, redirecting mid-session, streaming without end, advertising a
  schema that overflows an integer — and every one must produce a finding or
  a typed error rather than a panic, a hang, or an unbounded allocation.

---

## Understanding your report

Every run answers one question first: **is this server ready for agents?**

- **Ready for agents** — everything works. Ship it.
- **Ready, with room to improve** — agents can use it today; the listed
  items make it better, none blocks adoption.
- **Not ready for agents** — an agent will hit a real problem here. Fix
  the numbered items before you roll it out to your fleet.
- **Couldn't finish the check** — scout could not reach or sign in to the
  server. The one thing to fix is shown; nothing else was tested.

The **score out of 100** is a weighted read across six areas —
connectivity, authorization, protocol, catalog, execution and performance.
The five-dot meter beside each area shows where the points went. Only areas
that were actually tested count, and the report says how many, so a high
score on a partial run can't be mistaken for a clean bill of health.

**What to improve** lists each issue as a plain problem and a one-line fix,
worst first. That is the whole executive summary. Developers add
`--verbose` for the per-check detail: every check, the tools and resources
that were called, the speed measurements, and a `req#N` reference tying
each finding to a recorded request.

### Formats and telemetry

`--output text` (the default, coloured on a terminal) prints the report
above to your terminal's scrollback, so you scroll it the normal way.
`--output md` is a shareable Markdown document, `--output json` the full
structured report (add `--events` to embed every request), and
`--output ndjson` a stream you can pipe into `jq`.

`--report-dir DIR` writes all of them plus:

- `telemetry.ndjson`, one line per request: phase, label, method, URL,
  status, DNS/connect/TLS/TTFB/total timings, TLS version and cipher,
  certificate expiry, redacted headers, byte counts, JSON-RPC method and
  error, and with `--capture-bodies` the redacted bodies.
- `telemetry.har`, the same as an HTTP Archive 1.2 you can open in any
  browser's devtools.

Findings cite requests as `req#N`; `N` is the `seq` field in the NDJSON
and the entry index in the HAR.

Redaction is structural, not best-effort: `Authorization`, cookies and
key-like headers are masked by name; `code`, `state`, `client_secret` and
friends are masked in URLs and forms; `access_token`, `refresh_token`,
`client_secret` and similar are masked inside JSON bodies and their values
registered, so a token issued mid-run is masked wherever it appears
afterwards. Content types are not trusted — a body that starts with `{` is
treated as JSON, because token endpoints answer `text/plain` often enough.

---

## Scoring

Six categories, weighted: connectivity 10, authorization 20, protocol 20,
catalog 15, execution 20, performance 15. Each starts at 100; a critical
failure zeroes it, a major one costs 40, a minor one 15, and every warning
5. The total is the weighted mean over the categories whose phases actually
ran, and the report says how many that was, so a partial run cannot pass
for a full one. Every deduction is listed with the finding that caused it.

Grades are coarse labels for dashboards: A at 90 and above, B at 75, C at
60, D at 40, F below.

---

## Library use

The CLI is built on public packages you can use directly:

```go
client, _ := scout.New(scout.Config{
    Endpoint: "https://mcp.example.com/mcp",
    Auth:     scout.AuthConfig{Mode: scout.AuthClientCredentials, Extra: url.Values{"profile_id": {"t1"}}},
})
res, err := client.Connect(ctx)      // 401 → discovery → registration → token → initialize
tools, _ := client.ListTools(ctx)
out, _ := client.CallTool(ctx, "search", map[string]any{"q": "invoices"})
```

`auth` exposes discovery, registration and the token sources individually;
`transport` is the Streamable HTTP layer with raw access for conformance
probes; `diagnostics` holds the safety policy, the schema-driven argument
generator, the validator and a standalone read-only runner; `trace` carries
the run's trace id.

---

## Usage & Flags

### Positional Arguments

```bash
scout check <endpoint>
```

- `<endpoint>` — The server's Streamable HTTP URL (Required, unless
  `--profile` supplies it).

### Credential Options

| Option | Default | Description |
| :--- | :--- | :--- |
| `--auth` | `auto` | Credential mode: `auto`, `none`, `bearer`, `client-credentials`, `authorization-code` |
| `--token` | — | Pre-issued bearer token (or `SCOUT_TOKEN`) |
| `--token-env` | — | Read the bearer token from this environment variable |
| `--header` | — | Extra header sent on every request, `"Name: value"` (repeatable) |
| `--basic` | — | HTTP basic credentials as `user:password` (or `SCOUT_BASIC`) |
| `--client-id` | — | OAuth client id (or `SCOUT_CLIENT_ID`) |
| `--client-secret` | — | OAuth client secret (or `SCOUT_CLIENT_SECRET`; prefer `--client-secret-env`) |
| `--client-secret-env` | — | Read the client secret from this environment variable |
| `--client-metadata-url` | — | `https` URL of a Client ID Metadata Document to use as client id |
| `--scope` | — | Scope to request (default: what the server challenge asks for) |
| `--param` | — | Extra token/authorization request parameter `key=value` (repeatable) |
| `--token-url` | — | Token endpoint, bypassing discovery |
| `--auth-url` | — | Authorization endpoint, bypassing discovery (with `--token-url`) |
| `--resource` | — | RFC 8707 resource indicator override |
| `--redirect-port` | `8976` | Loopback port for the authorization-code redirect |
| `--token-auth-method` | — | Token endpoint auth: `client_secret_basic`, `client_secret_post` or `none` |

### Policy Options

| Option | Default | Description |
| :--- | :--- | :--- |
| `--allow-mutations` | off | Also invoke tools that mutate but are not destructive |
| `--allow-destructive` | off | Also invoke destructive and unannotated tools (dangerous) |
| `--only` | — | Restrict execution to this tool (repeatable) |
| `--deny` | — | Never invoke this tool (repeatable) |
| `--arg` | — | Argument override `tool.field=value` (repeatable; JSON parsed when possible) |
| `--insecure-allow-http-auth` | off | Allow a discovered OAuth endpoint served over plain `http` |
| `--insecure-allow-private-hosts` | off | Allow a discovered OAuth endpoint that resolves inside your network |
| `--allow-resource-mismatch` | off | Continue when the protected-resource metadata names a different endpoint |

The three overrides above each switch off a check that exists because
everything past your own endpoint is chosen by the server under test. Leave
them off unless you know why you are turning one on; see
[Safety](#safety).

### Pacing Options

| Option | Default | Description |
| :--- | :--- | :--- |
| `--samples` | `5` | Repeat calls per tool in the performance phase |
| `--concurrency` | `4` | Workers in the parallel burst (`0` disables) |
| `--rps` | `2` | Max requests per second; `0` or negative disables throttling |
| `--timeout` | `30s` | Per-call timeout |
| `--seed` | `1` | Seed for generated arguments |
| `--fill-optional` | off | Also populate optional schema properties |
| `--allow-load` | off | Run the burst unthrottled to test the server's rate limiting |
| `--max-resources` | `25` | Max resources to read |
| `--max-prompts` | `25` | Max prompts to render |

### Output Options

| Option | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--interactive` | `-i` | off | Pick the tools to exercise in the selector before the run |
| `--output` | — | `text` | Output format: `text`, `json`, `md`, `ndjson` |
| `--report-dir` | — | — | Write `report.{txt,md,json}`, `telemetry.ndjson` and `telemetry.har` here |
| `--capture-bodies` | — | off | Record request/response bodies in telemetry (redacted, capped) |
| `--events` | — | off | Embed every telemetry event in JSON output |
| `--verbose` | `-v` | off | Show evidence references and full info findings |
| `--no-color` | — | off | Disable ANSI colour |
| `--phases` | — | all | Run only these phases (comma-separated) |
| `--skip-phases` | — | — | Skip these phases (comma-separated) |
| `--config` | — | `~/.config/scout/config.json` | Configuration file |
| `--profile` | — | — | Profile from the configuration file supplying the endpoint and settings |
| `--log-level` | — | `info` | Diagnostic verbosity on stderr: `error`, `warn`, `info`, `debug` |

### Diagnostics

Results go to stdout in the format `--output` selects. Diagnostics — which
phase is running, what failed, why something was skipped — go to stderr, so
`--output json` stays pipeable no matter how noisy the run is.

`--log-level` controls how much of that stderr you get. `SCOUT_LOG_LEVEL`
sets the same thing for a whole shell session. `SCOUT_SHOW_LOGO=0` drops
the flame from the selector and `version`.

```bash
# Why was that tool skipped? Turn the detail up.
scout check https://mcp.example.com/mcp --log-level debug

# Machine-readable results, quiet stderr, both at once.
scout check https://mcp.example.com/mcp --output json --log-level error > report.json

# For a bug report: full detail, everything captured.
SCOUT_LOG_LEVEL=debug scout check https://mcp.example.com/mcp --report-dir ./out 2> diagnostics.log
```

### Operational Commands

```bash
scout connect https://mcp.example.com/mcp --token-env MCP_TOKEN
scout tools https://mcp.example.com/mcp --output md
scout call https://mcp.example.com/mcp search --arg q=invoices --arg limit=5
scout login https://mcp.example.com/mcp
scout config validate
```

`connect` stops after the handshake, `tools` after the catalog audit,
`call` invokes one tool and prints its result with timing, and `login`
runs the PKCE flow and stores the token (0600) under
`~/.config/scout/tokens.json` for `--auth authorization-code` runs.

---

## Configuration file

`~/.config/scout/config.json` (or `$XDG_CONFIG_HOME/scout/config.json`, or
`SCOUT_CONFIG`). Keys are flag names, so the file has no schema of its own
and picks up new flags automatically. `scout config init` writes a
commented template listing every setting.

```json
{
  "defaults": { "rps": 4, "report-dir": "./scout-reports" },
  "profiles": {
    "prod": {
      "endpoint": "https://mcp.example.com/mcp",
      "settings": {
        "auth": "client-credentials",
        "client-id": "acme",
        "client-secret-env": "ACME_SECRET",
        "param": ["profile_id=tenant-1"],
        "deny": ["send_email"]
      }
    }
  }
}
```

```bash
scout check --profile prod
```

Precedence, highest first: an explicit flag, the selected profile, the
defaults block, the flag's built-in default. Environment variables are
consulted for secrets only. Keep secrets in the environment and reference
them with the `*-env` settings. A setting that names no flag is an error,
not a silent no-op.

---

## Coming from another tool

Migration guides live in [`docs/migrating/`](docs/migrating/README.md):
from [MCP Inspector](docs/migrating/from-mcp-inspector.md), from
[a curl script](docs/migrating/from-a-curl-script.md), or from
[an online playground](docs/migrating/from-an-online-playground.md).

Each says what carries over, what is genuinely different, and what scout
will not do — nothing there touches the server, and `scout connect` shows
you the handshake before anything else runs.

---

## Examples

To inspect the package layout and programmatically drive scout modules,
see the self-contained, copy-pasteable Go code examples in the
[examples](examples/) directory:

1. **[Connect and call](examples/connect_and_call.go)** — Connect with
   OAuth client credentials through the public `scout` and `auth` packages
   and invoke one tool.
2. **[Safe diagnostics](examples/safe_diagnostics.go)** — Run the
   library's read-only `diagnostics` runner against an open server and
   print the quality score with its deductions.

---

## Troubleshooting

| Error Message | Cause | Solution |
| :--- | :--- | :--- |
| `server requires authorization and no credentials were supplied` | The server answered 401 and `--auth` resolved to `none`. | Pass `--token-env`, `--client-id`/`--client-secret-env`, or run `scout login`. |
| `--auth client-credentials needs --client-id` | Client-credentials mode with nothing to identify the client. | Supply `--client-id`, `--client-metadata-url`, or `SCOUT_CLIENT_ID`. |
| `token endpoint invalid_target` | The authorization server rejected the RFC 8707 resource indicator. | Pass `--resource` with the value the server expects. |
| `no stored token for this endpoint` | `--auth authorization-code` without a prior login. | Run `scout login <endpoint>` first. |
| `credentials rejected at initialize` | The token was issued but the MCP server did not accept it. | Check audience/resource, scope and expiry; `--log-level debug` shows the challenge. |
| `unknown setting "rsp"` | A config key does not match any flag name. | Settings are named after flags; see `scout check --help`. |

---

## Frequently Asked Questions

- **Does it test stdio servers?**  
  No. scout speaks Streamable HTTP. Put a stdio-to-HTTP bridge in front of
  a stdio server, or run it in HTTP mode if it has one.
- **Why were my tools skipped?**  
  They declare no annotations, or `destructiveHint` is true. The MCP
  specification's default for an unannotated tool is destructive, and scout
  honours it. Add `readOnlyHint: true` to the tools that are, or opt in with
  `--allow-mutations` / `--allow-destructive` on a tenant you control.
- **Why did a tool return `isError` and count as a warning, not a failure?**  
  Generated arguments are representative, not real; a server that rejects
  `"probe"` as a repository name is behaving correctly. Give it real values
  with `--arg tool.field=value` and the call becomes a proper test.
- **Can I run it inside cron or CI?**  
  Yes. The command is non-interactive, stdout carries the report in the
  format you chose, and the exit status is 2 when any finding failed.
- **Where do the secrets go?**  
  Nowhere. They are registered with the redactor before the first request
  and masked in every event, body and report. The token store is written
  with mode 0600, and a store that is readable by anyone else is refused
  rather than read.
- **Can a server under test steal my token?**  
  Not by asking for it. Credentials are bound to the origin you named, so a
  redirect pointing somewhere else is refused rather than followed, and the
  OAuth endpoints a server advertises are checked for HTTPS and for pointing
  at a public host before scout will talk to them. That is what
  `--insecure-allow-http-auth` and `--insecure-allow-private-hosts` turn
  off, which is why they say `insecure`.
- **Are the `*_ms` fields in the JSON milliseconds?**  
  Yes. `scout.schema_version` in the report says which format version you
  are reading; pin it if you build on the JSON.

---

**THE ARCHITECT** ᛫ [Sebastien Rousseau](https://sebastienrousseau.com)  
**THE ENGINE** ᛞ [EUXIS](https://euxis.co) ᛫ Enterprise Unified Execution Intelligence System

---

## Documentation

| Resource | Where |
|---|---|
| **User manual** | <https://sebastienrousseau.github.io/scout> |
| **API reference** | <https://pkg.go.dev/github.com/sebastienrousseau/scout> |
| **Developer docs** | [DEVELOPMENT.md](DEVELOPMENT.md) — toolchain and every CI gate reproduced locally |
| **Architecture** | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| **Decision records** | [docs/adr/](docs/adr/) |
| **Security model** | [docs/security-model.md](docs/security-model.md) |
| **Packaging** | [docs/packaging.md](docs/packaging.md) — for distribution maintainers |
| **Support** | [SUPPORT.md](SUPPORT.md) |

Once installed, `man scout` works offline, and every subcommand has its own
page (`man scout-check`).

---

## When not to use scout

scout is opinionated, and the opinions do not suit everyone.

- **You want a load test.** The parallel burst is bounded on purpose, and
  the default throttle is two requests per second. Use a load-testing tool
  for capacity questions; use scout to learn whether the server behaves
  under modest concurrency and rate-limits politely.
- **You need full JSON Schema validation.** The validator covers the
  structural core — types, required, properties, items, enum, const,
  bounds, oneOf/anyOf/allOf — and resolves local `$ref`/`$defs` pointers,
  which is what pydantic, zod and the official SDKs emit. It does not fetch
  a `$ref` that points at another document (it reports one as unchecked
  rather than passing it over), and it does not check `pattern` or `format`.
  A contract that leans on those needs a full validator.
- **Your server is stdio-only.** scout tests the Streamable HTTP
  transport. A bridge can expose a stdio server over HTTP, but the
  transport findings then describe the bridge.
- **You need the Tasks or Apps extensions checked.** scout diagnoses the
  core protocol on both the handshake revisions and the stateless
  `2026-07-28` one, but it does not yet exercise the optional extensions.
- **You want an agent to exercise the server.** scout's execution phase is
  deterministic: generated or supplied arguments, one call per tool. The
  `diagnostics.Model` interface exists for a model-driven probe, but no
  vendor adapter ships in this module.
- **You need Windows without WSL.** Binaries are published for Windows,
  but the experience is less tested than on macOS and Linux.

---

## Requirements & toolchain policy

| | |
|---|---|
| **Go** | The `go` directive in [`go.mod`](go.mod) — currently **1.26.8** |
| **Network** | Outbound HTTPS to the server under test and its authorization server |

The Go floor is stated in exactly one place, `go.mod`, and CI sets
`GOTOOLCHAIN=auto` so it cannot disagree with a workflow input.

**Policy for raising it.** The floor may rise in any release when a
standard-library fix or language feature justifies it, and the reason is
recorded in that release's CHANGELOG entry. scout makes **no distro-LTS
compatibility promise** — an aspirational claim without a table mapping
distro toolchains to the floor would be worse than none. Packagers should
check `go.mod` on every version bump rather than assume the floor held.

---

## Stability guarantees

scout is pre-1.0 and follows SemVer, with the patch digit moving for
everything until 1.0.

**The breaking axis is behaviour, not signatures.** For a tool whose output
is consumed by pipelines and whose requests reach other people's servers, a
change to what it *sends* or *reports* is breaking even when no flag or
function signature moves. Specifically, these are treated as breaking:

- A change to what `--output json` / `ndjson` emits, beyond added fields
- A change to a finding id, or to the status a given observation produces
- A change to the score weights or deductions
- A change to an exit code
- A safety refusal becoming permissive: any case where scout used to
  decline to invoke a tool and now proceeds
- A new request that reaches the server under test without a flag to
  disable it

Added fields, new flags with inert defaults, new findings, and new refusals
are **not** breaking.

**Deprecation window.** A deprecated flag keeps working for at least one
minor release after the release that announces it, and warns on stderr —
never on stdout, which carries the selected output format.

---

## Security & hardening

**Reporting.** Do not open a public issue. Follow the private process in
[SECURITY.md](SECURITY.md); the response SLA is stated there.

**Posture.** scout runs with credentials the operator supplied against
servers the operator chose, so the threat model is about *limiting blast
radius*, not crossing a privilege boundary. Full detail in
[docs/security-model.md](docs/security-model.md).

- **Secrets are redacted at the recorder, structurally.** Headers by name,
  URL and form parameters by name, JSON keys by name with their values
  registered, so nothing that was ever a secret reaches a report, a log or
  a HAR file. The token store is written with mode 0600.
- **Nothing destructive without a flag.** Only `readOnlyHint` tools run by
  default; unannotated tools are treated as destructive per the
  specification; mutations and destructive tools each need their own
  opt-in.
- **One adversarial request, and throttled.** The invalid-token probe is
  the only request designed to be rejected. Everything is capped at
  `--rps` unless `--allow-load` is given.
- **Unauthenticated probes are really unauthenticated.** A second, bare
  transport carries no credentials of any kind, so a server that accepts
  any token cannot hide behind the real one.
- **Memory safety** comes from Go; there is no CGO anywhere
  (`CGO_ENABLED=0`), so released binaries are static and free of libc
  coupling.

**Fuzzing.** Fuzz targets cover the parsing boundaries — the
`WWW-Authenticate` challenge parser, the SSE response reader, the JSON
Schema validator and the argument generator. A committed seed corpus under
each package's `testdata/fuzz/` replays on every `go test`, and the targets
run for a fixed duration on every push. scout is not enrolled in OSS-Fuzz.

**Supply chain.** Releases are signed with keyless cosign, carry SLSA build
provenance and a CycloneDX SBOM, and are built with `-trimpath` so two
builds of a commit are byte-identical. Every GitHub Action is pinned by
commit SHA and the container base by digest. `govulncheck` runs on every
push.

---

## License

Licensed under the **[GNU General Public License v3.0](LICENSE)**.

<p align="right"><a href="#scout">Back to Top</a></p>

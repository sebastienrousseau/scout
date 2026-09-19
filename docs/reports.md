---
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  scout's output formats — text, Markdown, JSON, NDJSON, HTML and the in-toto attestation — plus the report directory, the HAR export, and what telemetry holds.
---

# Reports and telemetry

## Output formats

| `--output` | What you get |
|---|---|
| `text` (default) | a plain-language verdict, a score out of 100 with a five-dot meter per area, and a numbered "What to improve" list. Printed to the terminal scrollback so you scroll it normally. Coloured on a TTY unless `--no-color`; `--verbose` adds the per-check detail, the tools and resources called, speed measurements and `req#N` evidence |
| `md` | the same as a shareable Markdown document, failures and warnings first |
| `json` | the full report; `--events` embeds every recorded request |
| `ndjson` | phases, findings and requests streamed as they happen, one JSON object per line, ending with the report |
| `html` | one self-contained document — the page `scout serve` shows, and the PDF its print stylesheet produces |
| `sarif` | SARIF 2.1.0, for GitHub code scanning and anything else that reads it |
| `junit` | JUnit XML, so a run appears beside the unit tests in the CI panel |
| `attestation` | not a report: an in-toto statement about the server, for signing and for `scout verify`. See [Attestations](#attestations) |

## The report directory

`--report-dir DIR` writes everything at once:

| File | Contents |
|---|---|
| `report.txt` | the verbose text report |
| `report.md` | the Markdown report |
| `report.json` | the full report with every event embedded |
| `report.sarif` | the SARIF 2.1.0 rendering |
| `report.junit.xml` | the JUnit rendering |
| `attestation.json` | the in-toto statement — the only file here a machine can act on without reading prose |
| `telemetry.ndjson` | one line per request |
| `telemetry.har` | the same as an HTTP Archive 1.2, which any browser's devtools can open |

## Attestations

Every format above is the findings arranged for a particular reader. An
attestation is a different thing: a **claim** about the server, in the
in-toto envelope a supply-chain pipeline already verifies, meant to be signed
and handed to a machine that was not present when the run happened — a
gateway deciding whether to route, a registry deciding what to display, an
auditor deciding in March whether a control was met.

```sh
scout check https://mcp.example.com/mcp --output attestation > attestation.json
```

Four properties make it worth more than the JSON report:

- **It says what it was judged against.** `82/100` means nothing in 2029
  without the rubric version and the MCP revision attached to it.
- **The subject digest covers a target descriptor, and the statement says
  so.** A digest that looked like an artifact hash while covering a URL
  would be the kind of lie that survives review.
- **It carries every verdict, passes included.** A consumer cannot otherwise
  tell "checked and fine" from "not checked".
- **It verifies offline.** A gateway must never have to call scout to trust
  a statement scout produced.

### Producing one in a later job

Signing usually is not the job that ran the diagnostic. Run scout where the
server credentials live, save the report, and attest somewhere with an OIDC
identity and no access to the server at all — the split SLSA provenance uses:

```sh
scout check "$URL" --output json > report.json   # has credentials
scout attest report.json > attestation.json      # has an identity
cosign attest-blob --predicate attestation.json --new-bundle-format ...
```

`scout attest` reads standard input when given no filename.

### Verifying and gating

`scout verify` answers one question by default: **can the statement be
believed?** It parses, its subject digest still covers the target it names,
its subject is named for that same target, and it records what the verdicts
were judged against.

That is not approval. A statement can be perfectly valid and describe a
server you would never route to, so approval is opt-in:

```sh
scout verify attestation.json \
  --endpoint https://mcp.example.com/mcp \
  --require auth.unauthenticated_tools \
  --max-fail 0
```

| Gate | Holds when |
|---|---|
| `--endpoint URL` | the statement is about that target. Add `--transport stdio` for a child process |
| `--require ID` | that check's verdict is a pass. **Absent is not a pass** — a statement that never ran the check cannot vouch for it |
| `--max-fail N` | at most N checks failed |
| `--min-score N` | the score is at least N. A run that assessed nothing carries no score, and a missing score never counts as zero |

A gate applies because the flag was given, not because of its value:
`--max-fail 0` is the strictest form of that gate, and omitting the flag asks
for no gate at all.

Exit status is the part a pipeline reads:

| Code | Meaning |
|---|---|
| `0` | valid, and every gate met |
| `2` | valid, and a gate was not met — the evidence is good and the answer is no |
| `1` | the statement cannot be believed at all — the evidence is unusable |

Treat `1` and `2` differently. They are different incidents.

Nothing in `scout verify` checks a signature. Verify the envelope with the
tool that produced it, then verify what is inside it with this.

## SARIF and JUnit

Both are the same findings in a shape one particular reader will not accept
a substitute for. Neither is richer than `json`; if you are writing your own
consumer, use `json`.

Two choices in the SARIF are worth knowing about, because the conventional
answer is different:

**Passing checks are emitted**, as `"kind": "pass"` with `"level": "none"`.
SARIF models a passing result deliberately, and dropping them would mean a
consumer cannot tell "scout checked this and it was fine" from "scout did
not check this" — which, for a conformance tool, is the whole difference.
Anything that shows only alerts filters on `level`, at no cost.

**The location is the endpoint**, as an absolute URI. There is no file to
point at: scout tests a running server, not a checkout. A tool that invents
a repository path so its results look like source findings is lying about
where the problem is. For a [stdio server](stdio.md) there is no URL either,
so the location is `stdio:` followed by the escaped command line, and the
fingerprint that lets a consumer track one alert across runs is built from
that command rather than from a hostname it does not have.

Each rule carries the check's `helpUri`, which is its `doc_url` — so an
alert in code scanning links to what the check asserts.

The JUnit mapping has one decision that surprises people: **a warning
becomes a `<failure>`**, typed `warning`. JUnit has no third state, and
reporting a deviation as a pass is how a warning stops being read. Filter on
the `type` attribute if you want to gate only on real failures.

## Which server a report is about

`target` says what was diagnosed, and a consumer that reads more than one
report has to look at `transport` before `endpoint`:

```json
{ "target": { "endpoint": "https://mcp.example.com/mcp", "host": "mcp.example.com",
              "scheme": "https", "transport": "http" } }
```

```json
{ "target": { "endpoint": "npx -y server-everything stdio", "host": "", "scheme": "stdio",
              "transport": "stdio", "command": ["npx", "-y", "server-everything", "stdio"] } }
```

`endpoint` is the target as scout was given it, which for a stdio run is a
command line rather than a URL — `transport` is how you tell, and `command`
carries the argument list unjoined so it can be re-run without guessing how
it was quoted. `schema_version` is unchanged at 1: nothing was removed, and
no report of a kind that existed before reads differently.

The two kinds of run do not contain the same checks. A stdio report carries
five that an HTTP one cannot make and reports several of the HTTP ones as
skipped, each with its reason — so comparing two scores across transports
compares different batteries. [Servers that are programs](stdio.md) lists
which.

## Traces and structured logs

Two flags put a run into the systems a platform team already watches.

`--otlp-endpoint URL` exports the finished run as OpenTelemetry traces over
OTLP/HTTP. The shape of the trace is the shape of the run:

```text
scout check                      root span: endpoint, score, failure counts
├── phase net                    span events: one per finding
│   ├── GET  https://…/mcp       DNS, connect, TLS and TTFB as attributes
│   └── POST https://…/mcp
├── phase handshake
└── …
```

A request scout made outside any phase is parented to the root rather than
dropped — an unattributed request is still a request that was made.
`--otlp-header "Name: value"` is repeatable, for a collector that wants an
API key or a tenant id.

**The export never changes the verdict.** A collector being unreachable is
not a finding about the server under test, so a failed export is a warning
on stderr and the exit code is whatever the run earned.

scout speaks the JSON encoding of OTLP rather than protobuf. Both are
specified, every collector accepts JSON on the same endpoint, and JSON is
reachable from the standard library — so emitting traces costs the binary no
new dependency.

`--log-format json` switches scout's own diagnostics on stderr from prefixed
lines to one JSON object each, through `log/slog`. Every line carries the
run's `trace_id`, which is the same id on the report and on the spans, so a
log line can be joined to the run it came from. The default stays human: a
person watching a single run wants a line they can read.

## How much a finding explains

A finding always carries a one-line `advice`. Sixty-five of the eighty-one
checks — every one that can fail or warn — also have a fuller explanation:
what the finding means in terms of the protocol, and numbered steps naming
the field, header or error code to change.

Where that appears depends on who is reading:

| Rendering | Carries the full guidance |
|---|---|
| `text` (default) | no — one-line advice only |
| `text --verbose` | yes |
| `md` | yes, always |
| `html` | yes, always |
| `json`, `ndjson` | on request — `--guidance` |

The default terminal output is read while the run is still fresh, by
somebody who wants to know what is wrong; five findings with three steps
each is sixty lines nobody asked for. The Markdown and HTML renderings are
the ones forwarded to people who cannot re-run the tool, so they carry
everything.

The JSON carries `advice` and `doc_url` by default. `--guidance` adds a
`guidance` object to the report:

```json
{
  "guidance": {
    "protocol.malformed_json": {
      "means": "A truncated or malformed request body was answered with…",
      "steps": [
        { "title": "Reject a body that does not parse", "body": "Return HTTP 400, or…" }
      ]
    }
  }
}
```

It is a dictionary keyed by check id, not a field on each finding, because
the prose is per-check and not per-occurrence: a catalog with forty poisoned
descriptions produces forty findings and one entry here. Join on the
finding's `id`.

Only the checks that failed or warned in this run appear. A report about one
server should not carry advice about checks that server passed.

It is off by default because most consumers have `doc_url` and want the
report small — it costs around 6 KB on a typical run. Turn it on for a
consumer that has to explain a finding somewhere scout cannot reach, such as
a bot writing a pull request comment.

A test fails the build when a check that can fail has no guidance written
for it, so this does not quietly regress as checks are added.

## What a finding holds

Every finding in the JSON report carries the same fields:

| Field | What it is |
|---|---|
| `id` | the check that produced it, e.g. `protocol.malformed_json` |
| `phase` | which of the nine phases it came from |
| `status` | `pass`, `warn`, `fail`, `skip` or `info` |
| `severity` | on a failure or warning: `critical`, `major` or `minor` |
| `detail` | what was observed, in plain language |
| `advice` | what to change, on anything that is not a pass |
| `evidence` | the recorded requests behind it, e.g. `["req#8"]` — see [Reading the evidence](evidence.md) |
| `doc_url` | this check's row in [the inventory](checks.md) |

`doc_url` exists because a report is usually read long after the run that
produced it, often by somebody who was not there. `protocol.malformed_json`
is only self-explanatory to a reader who already knows what it means; the
link is the difference between a finding you can act on and one you have to
ask about.

The fragment it points at is written into the inventory by the same
generator that counts the checks, and a test fails the build if the two ever
disagree — so a link that shipped inside an archived report keeps resolving.

## What a telemetry event holds

Every request scout makes, to the MCP server and to the authorization
server, is recorded with:

- sequence number, timestamp, trace id, the phase and label it belongs to;
- method, URL, status, or the transport error;
- whether the connection was reused, and the remote address;
- DNS, connect, TLS, time-to-first-byte and total durations from
  `net/http/httptrace`;
- TLS version, cipher suite, server name, certificate subject, issuer,
  expiry and days remaining;
- request and response headers, redacted;
- byte counts, and with `--capture-bodies`, the redacted bodies capped at
  64 KiB;
- the JSON-RPC method, id, and any error code and message.

Findings cite requests as `req#N`; `N` is the `seq` field in the NDJSON
and the entry index in the HAR. Every request also carries an
`X-MCP-Trace-ID` header with the run's trace id, so a server operator can
find the run in their own logs.

## Scoring

Six categories, weighted:

| Category | Weight | Phases |
|---|---|---|
| connectivity | 10 | net |
| authorization | 20 | discovery, auth |
| protocol | 20 | handshake, protocol, resilience |
| catalog | 15 | catalog |
| execution | 20 | execution |
| performance | 15 | performance |

Each category starts at 100. A critical failure zeroes it, a major one
costs 40, a minor one 15, and every warning 5; a category cannot go below
zero. The total is the weighted mean over the categories whose phases
actually ran, and the report states how many were assessed, so a high
score on a partial run cannot be mistaken for a full one. Every
deduction is listed with the finding that caused it. Grades: A at 90 and
above, B at 75, C at 60, D at 40, F below.

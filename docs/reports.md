---
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  scout's output formats — text, Markdown, JSON, NDJSON and HTML — plus the report directory, the HAR export, and what telemetry holds.
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

## The report directory

`--report-dir DIR` writes everything at once:

| File | Contents |
|---|---|
| `report.txt` | the verbose text report |
| `report.md` | the Markdown report |
| `report.json` | the full report with every event embedded |
| `report.sarif` | the SARIF 2.1.0 rendering |
| `report.junit.xml` | the JUnit rendering |
| `telemetry.ndjson` | one line per request |
| `telemetry.har` | the same as an HTTP Archive 1.2, which any browser's devtools can open |

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
where the problem is.

Each rule carries the check's `helpUri`, which is its `doc_url` — so an
alert in code scanning links to what the check asserts.

The JUnit mapping has one decision that surprises people: **a warning
becomes a `<failure>`**, typed `warning`. JUnit has no third state, and
reporting a deviation as a pass is how a warning stops being read. Filter on
the `type` attribute if you want to gate only on real failures.

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

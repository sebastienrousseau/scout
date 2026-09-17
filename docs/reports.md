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

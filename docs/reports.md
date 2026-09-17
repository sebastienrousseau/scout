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

## The report directory

`--report-dir DIR` writes everything at once:

| File | Contents |
|---|---|
| `report.txt` | the verbose text report |
| `report.md` | the Markdown report |
| `report.json` | the full report with every event embedded |
| `telemetry.ndjson` | one line per request |
| `telemetry.har` | the same as an HTTP Archive 1.2, which any browser's devtools can open |

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

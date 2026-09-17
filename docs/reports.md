---
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  scout's output formats — text, JSON, HTML, JUnit, SARIF — plus the wire log, the HAR export, and the telemetry it does not collect.
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

---
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
name: "scout"
short_name: "SC"
title: "scout — MCP server testing that never sees your credentials"
description: "Test any Model Context Protocol server against 76 checks in nine phases. Runs on your machine, so credentials never leave your trust boundary. Every finding cites the request that produced it."
keywords: "MCP server testing, test MCP server, MCP security scanner, Model Context Protocol validation, MCP Inspector alternative, MCP conformance testing, MCP compliance audit, MCP server security, agent tooling QA"
author: "Sebastien Rousseau"
date: "2026-09-17"
layout: "index"
language: "en-GB"
schema: "page"
changefreq: "weekly"
copyright_year: "2026"
form_origin: "'self'"
theme_style: "style-scout"
theme_colour: "#F9A12C"
brand_mark: "S"
footer_note: "A verdict is worth what its evidence is worth."

eyebrow: "Open source · GPL-3.0 · runs on your machine"
headline: "Know what your MCP server does before your agents find out."
lead: "scout connects the way an agent would, runs nine phases against the live server, and tells you in plain language whether it is ready — and exactly what to fix if it is not. Nothing is uploaded. Nothing is guessed."
cta_primary: "Install"
cta_secondary: "See a real report"

readout_kicker: "A real run"
readout_target: "https://mcp.example.com/mcp"
readout_verdict: "Ready, with room to improve"
readout_score: "84"
readout_grade: "Good"
readout_note: "Nine phases · 76 checks · 50 requests · every finding tied to one of them"

metric_one_label: "Checks per run"
metric_one_value: "76"
metric_two_label: "Protocol revisions"
metric_two_value: "Both"
metric_three_label: "Credentials uploaded"
metric_three_value: "None"
proof_note: "The check count is generated from the source and gated in CI — every one is listed in the manual, under Every check. The score above is from a real server; the endpoint is redacted."

why_eyebrow: "Why it runs here"
why_title: "A scanner you upload to is a scanner you trust twice."
why_lead: "Handing an endpoint to a hosted tester means handing over whatever reaches it. For a server behind OAuth, that is a working credential. scout is the same test, run from inside your own trust boundary."
why_one_title: "Your credentials stay put"
why_one_text: "scout authenticates from your machine with the token you already hold. Nothing is transmitted to a third party, so nothing has to be revoked afterwards — and no vendor ends up holding a key to your systems."
why_two_title: "Your network is the network"
why_two_text: "A server on a private address, behind a VPN or inside a CI runner is reachable exactly as your agents will reach it. A hosted scanner can only test what it can route to from the public internet."
why_three_title: "The evidence stays with you"
why_three_text: "Wire logs, headers and timings are written beside the report. You can read them, diff them, attach them to a ticket and keep them, rather than trusting a summary of a run you cannot inspect."

compare_eyebrow: "How it compares"
compare_title: "Three ways to test a server. They are not interchangeable."
compare_lead: "The official Inspector is for looking around by hand. Hosted scanners are for a quick public-endpoint opinion. scout is for the answer you have to stand behind."
compare_note: "Hosted-scanner figures are taken from the public descriptions of those tools at the time of writing. scout's are from its own suite and its runs against the official MCP registry, both reproducible with the commands below."

ledger_eyebrow: "Severity ledger"
ledger_title: "Every deduction named."
ledger_lead: "A grade with no arithmetic behind it is a brand, not a measurement. scout shows the categories, their weights and what each one lost — so a score you dispute is a score you can audit."

evidence_eyebrow: "Evidence"
evidence_title: "Nothing passes without a request that showed it."
evidence_lead: "An independent audit in 2026 put false positives from pattern-matching MCP scanners at roughly four in five. scout takes the other route: each finding carries the numbered exchange that produced it, so you follow a verdict back to the bytes on the wire instead of taking it on faith."

phases_eyebrow: "Method"
phases_title: "Nine phases, in the order an agent meets them."
phases_lead: "The sequence matters: a catalogue check means nothing if the handshake never completed, and a latency figure means nothing if half the calls failed."

enterprise_eyebrow: "For platform and security teams"
enterprise_title: "The evidence an approval actually needs."
enterprise_lead: "In May 2026 the NSA published security design guidance for MCP, describing authentication controls and prompt-injection defences as required mitigations rather than optional hardening. Gartner expects more than four in ten agentic AI projects to be cancelled by the end of 2027, inadequate risk controls among the reasons. Both are easier to answer with a record than with an assurance."
enterprise_one_title: "A gate, not a ritual"
enterprise_one_text: "scout exits non-zero on failure and writes JSON and NDJSON, so a pull request that regresses a server's tool schemas fails before it merges. The same binary runs on a laptop and in a pipeline, so the check that gates a release is the check an engineer already ran."
enterprise_two_title: "A record you can hand over"
enterprise_two_text: "Every run writes a timestamped report, the requests behind it, and a HAR archive. Print the report to PDF for a review board; keep the wire log for the auditor who asks how you know. Secrets are redacted structurally at the recorder, including tokens the server issued mid-run."
enterprise_three_title: "Supply chain you can verify"
enterprise_three_text: "Releases are signed with keyless cosign, carry SLSA build provenance and a CycloneDX SBOM, and the container base is pinned by digest. There is no telemetry, no account and no phone-home: scout is auditable the way it asks servers to be."

faq_eyebrow: "Questions"
faq_title: "The things people ask before installing."
faq_lead: "Short answers. Longer ones are in the manual."
faq_one_q: "How is this different from the official MCP Inspector?"
faq_one_a: "The Inspector is an excellent place to look around a server by hand — click a tool, read a response, debug an OAuth flow. scout is the non-interactive counterpart: it runs a fixed battery of 76 checks, scores the result, cites its evidence and returns an exit code. Most teams end up using both."
faq_two_q: "Why not just use a hosted MCP security scanner?"
faq_two_a: "For a public, unauthenticated endpoint, a hosted scanner is a reasonable quick opinion. The moment a server needs a credential, using one means giving a third party a working key to your systems — and it still cannot reach anything on a VPN, a private address or a CI runner. scout runs the same class of test without either problem."
faq_three_q: "Does it support the stateless 2026-07-28 revision?"
faq_three_a: "Yes, and all nine phases run on either generation. scout settles which revision a server speaks before first contact, because the shape of the first request depends on the answer. Routing headers, resultType, multi-round-trip requests and the -32020/-32021/-32022 error codes are all exercised."
faq_four_q: "Is it safe to run against production?"
faq_four_a: "It is designed for it. Only tools declaring readOnlyHint are invoked; a tool with no annotation is destructive by the specification's own default, so it is skipped and the report says so. Requests are throttled. One deliberately invalid token is sent to confirm the server rejects it, and nothing else adversarial. It is a diagnostic, not a penetration test."
faq_five_q: "Does it test stdio servers?"
faq_five_a: "Not yet. scout speaks Streamable HTTP, which is the transport for remote servers. A stdio server can be tested through a stdio-to-HTTP bridge, or in HTTP mode if it has one. Native stdio support is the next milestone and is tracked in the open."
faq_six_q: "What does it cost, and what does it send back to you?"
faq_six_a: "It is free and GPL-3.0 licensed. It sends nothing back: no telemetry, no account, no analytics. Once installed it runs entirely offline apart from the requests it makes to the server you named."

report_eyebrow: "The report"
report_title: "One document, two readers."
report_lead: "An executive summary sits above the fold — verdict, score, what to fix first. The full findings and their evidence follow. Terminal, JSON, Markdown, HTML, or printed to PDF for people who do not read terminals."
---

## Install

```sh
brew install sebastienrousseau/tap/scout     # macOS and Linux
mise use -g github:sebastienrousseau/scout   # or with mise
go install github.com/sebastienrousseau/scout/cmd/scout@latest
```

Then point it at a server:

```sh
scout check https://mcp.example.com/mcp
```

That is the whole first run. No account, no config file, no signup. Add a
credential when the server needs one:

```sh
scout check https://mcp.example.com/mcp --token-env MCP_TOKEN
```

Or open the same diagnostic in a browser, still running locally:

```sh
scout serve
```

## In a pipeline

scout exits non-zero when a server fails, so it gates a merge without any
extra plumbing:

```sh
scout check "$MCP_ENDPOINT" --token-env MCP_TOKEN \
  --output json --report-dir ./scout-report
```

The report directory holds the JSON verdict, the NDJSON event stream and a
HAR archive of every exchange. Attach it to the build and the question
"how do you know?" has an answer that outlives the person who ran it.

## What it checks

Connectivity and TLS. How the server asks to be authorized, and whether it
refuses a bad token. The MCP handshake, and which generation of the protocol
it speaks — scout supports both the handshake revisions and the stateless
`2026-07-28` one. Protocol behaviour on the edge cases an agent will hit.
The tool catalogue, as a model reads it: descriptions, schemas, annotations,
and whether `$ref` and `$defs` resolve. Safe calls, with results checked
against the contracts the tools declare. Latency under repeat and parallel
calls. Recovery from a lost session, or from a server that turns out not to
be as stateless as it claims.

## What it will not do

It calls only tools that declare `readOnlyHint` unless you say otherwise.
It throttles itself. It sends one deliberately invalid token to check the
server rejects it, and nothing else adversarial. It is a diagnostic, not a
penetration test, and the difference is deliberate.

It also will not phone home. There is no telemetry, no account and no
analytics — the only requests scout makes are to the server you named.

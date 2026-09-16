---
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
name: "scout"
short_name: "SC"
title: "scout — test any MCP server, from your own machine"
description: "A diagnostic for Model Context Protocol servers. Nine phases, 76 checks, every finding tied to the request that produced it."
keywords: "MCP server testing, Model Context Protocol, MCP diagnostics, MCP security scanner, MCP inspector"
author: "Sebastien Rousseau"
date: "2026-09-16"
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

eyebrow: "Open source · runs on your machine"
headline: "Test any MCP server. Nothing leaves your machine."
lead: "scout connects with your own credentials, runs nine phases against a live server, and tells you in plain language whether agents can use it — and exactly what to fix if they cannot."
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
proof_note: "Figures from scout's own test suite and its runs against the official MCP registry. The score above is from a real server; the endpoint is redacted."

why_eyebrow: "Why it runs here"
why_title: "A scanner you upload to is a scanner you trust twice."
why_lead: "Handing an endpoint to a hosted tester means handing over whatever reaches it. For a server behind OAuth, that is a working credential. scout is the same test, run from inside your own trust boundary."
why_one_title: "Your credentials stay put"
why_one_text: "scout authenticates from your machine with the token you already hold. Nothing is transmitted to a third party, so nothing has to be revoked afterwards."
why_two_title: "Your network is the network"
why_two_text: "A server on a private address, behind a VPN or inside a CI runner is reachable exactly as your agents will reach it. A hosted scanner can only test what it can route to."
why_three_title: "The evidence stays with you"
why_three_text: "Wire logs, headers and timings are written beside the report. You can read them, diff them and keep them, rather than trusting a summary of a run you cannot inspect."

ledger_eyebrow: "Severity ledger"
ledger_title: "Every deduction named."
ledger_lead: "A grade with no arithmetic behind it is a brand, not a measurement. scout shows the categories, their weights and what each one lost."

evidence_eyebrow: "Evidence"
evidence_title: "Nothing passes without a request that showed it."
evidence_lead: "Each finding carries the numbered exchange that produced it, so you can follow a verdict back to the bytes on the wire instead of taking it on faith."

phases_eyebrow: "Method"
phases_title: "Nine phases, in the order an agent meets them."
phases_lead: "The sequence matters: a catalogue check means nothing if the handshake never completed, and a latency figure means nothing if half the calls failed."

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

Or open the same diagnostic in a browser, still running locally:

```sh
scout serve
```

## What it checks

Connectivity and TLS. How the server asks to be authorized, and whether it
refuses a bad token. The MCP handshake, and which generation of the protocol
it speaks — scout supports both the handshake revisions and the stateless
`2026-07-28` one. Protocol behaviour on the edge cases an agent will hit.
The tool catalogue, as a model reads it. Safe calls, with results checked
against the contracts the tools declare. Latency under repeat and parallel
calls. Recovery from a lost session, or from a server that turns out not to
be as stateless as it claims.

## What it will not do

It calls only tools that declare `readOnlyHint` unless you say otherwise.
It throttles itself. It sends one deliberately invalid token to check the
server rejects it, and nothing else adversarial. It is a diagnostic, not a
penetration test, and the difference is deliberate.

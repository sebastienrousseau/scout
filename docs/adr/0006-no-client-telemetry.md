---
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  Why scout sends nothing home, why that is a product guarantee rather than a current fact, and what replaces the numbers telemetry would have produced.
---

# 0006 — scout never phones home

**Status:** Accepted · **Date:** 2026-09-19

## Context

scout's front page says *0 bytes uploaded* and *nothing here is uploaded*.
Today that is simply a description of the code: there is no client-side
callback anywhere in the binary, because nobody has written one.

A proposal was put forward to add one. Opt-out, anonymous, no personal
data: operating system, architecture, whether the run was in CI, how long
it took, the final score, and which phase failed. The reasoning is sound
and conventional — a developer tool with no usage data is a tool whose
maintainers are guessing, and an investor asking about adoption deserves a
number rather than an anecdote.

Three things make it the wrong trade for this particular tool.

**The tool is pointed at production.** An operator runs `scout check`
against a server holding their customers' data, with a credential that
works. The endpoint is often internal and its hostname is itself
sensitive. Even a payload that carries none of that has to be *audited* to
establish that it carries none of that, and the audit is a cost paid by
every security team that evaluates scout, forever.

**The guidance now runs the other way.** In May 2026 the NSA's Artificial
Intelligence Security Center published *Model Context Protocol (MCP):
Security Design Considerations for AI-Driven Automation*. Among its
recommendations: for sensitive data, prefer local MCP server instances
with **no vendor access or telemetry**. scout is not an MCP server, but
it is the tool an operator points at one, and the reasoning transfers
exactly. An enterprise reading that document and then finding a callback
in the diagnostic has been handed a reason to stop reading.

**The nearest competitor has just made the opposite choice.** The most
widely adopted MCP security scanner now requires an account and an API
token before it will scan anything. That is a gift, and it is only a gift
while scout needs neither.

## Decision

**scout contains no client-side telemetry, and will not.** No callback, no
ping, no opt-out beacon, no crash reporter.

*0 bytes uploaded* is promoted from a description of the current code to a
**product guarantee**, recorded in the stability-guarantees section of the
README. Adding a callback is therefore a breaking change under this
project's own rules, not a feature.

The guarantee is bounded and the bound is stated rather than implied: scout
makes requests to the server under test, to the authorization server that
server names, and to an OTLP collector or a report directory the operator
configures explicitly. Those are the run. (A fourth, asked for by flag, was
added on 2026-09-22; see the amendment below.) Nothing else leaves the machine,
and nothing at all goes to anywhere scout's authors control.

`scoutmcp.io` is a website and carries ordinary website analytics. A
website is not the tool.

## Consequences

The numbers have to come from somewhere else, and they do — from
deliberate publication rather than silent collection, which is a better
source and not merely an acceptable one:

| Signal | Why it is stronger than telemetry |
|---|---|
| Published attestations in a transparency log | Every entry is an operator choosing to publish. Attributable, countable, and auditable by a third party directly rather than on our word |
| GitHub Action usage count | Computed and published by GitHub, not by us |
| Badge impressions | The operator opted in by embedding it |
| Package and release download counts | Already collected by four distribution channels |
| Citations of the rubric | The only signal that measures whether scout is becoming a standard rather than a popular tool |

What is genuinely lost is the diagnostic feedback loop: we cannot see
which phase developers fail on most, or which checks confuse them. That
has to be bought with research instead — the census, issue triage, an
opt-in survey, and the coverage of the guidance catalogue. Slower, less
precise, and it does not require asking every user to trust us with a
callback they did not want.

## What would make this wrong

If the ecosystem's norms shift so far that a local-first posture stops
being a differentiator and becomes an obstacle — if operators start
*expecting* a diagnostic to report to a fleet service and treat one that
does not as unmanageable — then the answer is still not a callback in the
binary. It is a separate, explicitly configured exporter the operator
points at **their own** collector, which `--otlp-endpoint` already is.

The line that must not move: scout never sends anything to an endpoint
scout's authors control.

## Amendment — 2026-09-22: vulnerability lookup

`scout sbom --osv` asks OSV which advisories affect the packages in a bill
of materials. That is a destination the bound above did not list, so it is
recorded here rather than added quietly.

It stays inside the decision for the same reason `--otlp-endpoint` does:
the operator asks for it explicitly, by flag, on the run where it happens.
It is off by default, and nothing else in scout makes the request. It is
announced on stderr before anything is sent, and it is recorded in the
document it produced. What is sent is package URLs and nothing else: no
hashes, no paths, no project name, nothing about the server under test.
Components that did not come from a public registry are never sent, because
their names may be internal and no public database could match them.
`--osv-url` points the lookup at a mirror for an operator who cannot send
even public package names outside their network.

The OSV API is run by Google, not by scout's authors, so the line above
does not move. The bound now reads: the server under test, the
authorization server it names, an OTLP collector or report directory the
operator configures, and a vulnerability database the operator asks for by
flag.

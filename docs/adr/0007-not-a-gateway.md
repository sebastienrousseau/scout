---
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  Why scout measures MCP servers instead of sitting between agents and them, and why that is a positioning decision rather than a scoping one.
---

# 0007 — scout is an instrument, not a gateway

**Status:** Accepted · **Date:** 2026-09-19

## Context

The obvious commercial shape for a tool that understands MCP deeply is a
gateway: sit between the agent and the server, broker credentials, block
the tool call that should not happen, and charge for the control plane.
The proposal was specific — centralised token orchestration, blast-radius
controls on unannotated tools, interception of a mutating call an agent
attempted by mistake.

Two facts argue against it.

**That market is not merely competitive, it is settled.** As of mid-2026
there are published fourteen-vendor buyer's guides for MCP gateways. The
entrants include Kong, Solo.io's Agent Gateway, Obot, IBM ContextForge,
Amazon Bedrock AgentCore, Tyk, MintMCP, Lunar.dev, Arcade, TrueFoundry and
Docker's own MCP Gateway. Two of them are free and open source. One of them
is AWS. Entering it means competing on deployment surface, enterprise
integrations and brand — the three axes on which a single-maintainer Go
project is weakest — and abandoning evidence, protocol depth and
trustworthiness, which are the three on which it is strongest.

**A gateway is a production dependency.** The reason an operator is willing
to point scout at a live server is that scout touches nothing: it makes
requests, records what came back, and exits. A proxy has to be trusted with
traffic, kept available, patched on somebody else's schedule, and blamed
when latency moves. That is a different product with a different risk
profile sold to a different buyer, and it would cost the one property that
makes the current product easy to adopt.

There is also an asymmetry worth naming. Every one of those fourteen
gateways has to make an admission decision — route to this server, or do
not — and none of them has a defensible basis for it. They ship policy
engines with no measurement. scout is measurement with no policy engine.

## Decision

**scout is never in the data path.** It does not proxy, broker credentials,
or intercept a call between an agent and a server. There is no
`scout-gateway`, and the family manifest records it as considered and
rejected with this reasoning attached.

What scout sells instead is the input those gateways lack: a signed,
portable [attestation](../reports.md) that states what was measured, what
it was judged against, and what the evidence was — verifiable offline, so a
gateway never has to call scout to trust it.

The corollary is a policy and not just a scope boundary: **scout never
becomes a runtime dependency of anyone's production traffic.** It is
written down here so that it does not get re-litigated by the first
enterprise that asks for a proxy.

## Consequences

The commercial model rests on fleet, history and evidence rather than on
interception: policy gating in CI, drift between two attestations, the
inventory a security team is required to keep, retention for an audit, and
licensing the evaluation engine to the gateways themselves.

That last one is the point. A gateway integration is a partnership rather
than a competition, and it reaches distribution scout could not buy. The
first two should be pull requests into the two open-source gateways, where
there is no procurement and a merged change is the case study.

The cost is real. Interception is where the obvious money is, and declining
it means the revenue depends on an attestation format being adopted by
people who are under no obligation to adopt it. If nobody verifies it, the
strategy has no second act.

## What would make this wrong

If the attestation is not adopted — if twelve months after it is signed no
gateway, registry or auditor verifies one — then measurement alone is not a
business and the decision needs revisiting. The response is still not to
build a gateway from scratch; it is to be the evaluation engine inside
somebody else's, which is a licensing conversation rather than a product
rewrite.

The other thing that would make it wrong is a gateway shipping its own
behavioural evaluator of comparable depth. That is the risk the census and
the public rubric exist to make expensive: a competitor can copy the checks
in a quarter and cannot copy a longitudinal dataset that everyone cites.

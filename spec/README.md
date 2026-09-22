<!-- SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com> -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# The scout attestation format

This directory is the part of scout meant to be implemented by someone
else. It is licensed **Apache-2.0**, unlike the engine, which is
GPL-3.0-only. A gateway, registry or CI system can read, verify and gate on
a scout attestation — or produce a compatible one — without taking on the
engine's licence. [ADR 0011](../docs/adr/0011-attestation-format-is-apache.md)
records why.

Both JSON files are generated from the Go that implements them, by
`make spec`, and CI fails when they drift. Do not edit them by hand.

## Contents

| File | What it is |
|---|---|
| [`attestation/mcp-evaluation-v1.schema.json`](attestation/mcp-evaluation-v1.schema.json) | JSON Schema (2020-12) for an in-toto Statement carrying the `https://scoutmcp.io/attestation/mcp-evaluation/v1` predicate |
| [`rubric/rubric-v1.json`](rubric/rubric-v1.json) | The scoring rubric: category weights and the phases under each, the deduction per outcome, the grade bands, and the rules that are behaviour rather than numbers |

## The statement

An in-toto v1 Statement. Its one subject is the evaluated server, and its
digest is over a canonical descriptor of the target — transport plus
address or command — not over an artifact; `predicate.subjectKind` says so
(`mcp-target-descriptor`), and a verifier must check the subject's name
matches the predicate's target.

The predicate carries:

- **`target`** — transport (`http` or `stdio`), endpoint or command, and what
  the server said it was.
- **`judgedAgainst`** — the MCP revision negotiated, the rubric version
  and the check-inventory version. A score is only comparable with another
  computed under the same rubric, so a statement carrying a score without a
  rubric version is invalid.
- **`instrument`** — the tool and version that produced it.
- **`ranAt`** and **`took`** — a verdict about a live service is a verdict
  about a moment.
- **`verdicts`** — every check that ran, passes included, each with its
  status (`pass`, `warn`, `fail`, `skip`, `info`), severity, and the
  references of the recorded requests that produced it.
- **`counts`**, **`score`** — the summary a policy engine gates on.
- **`blocked`** — why a run stopped early, when it did.

## The rubric

Each category starts at 100. Every failing or warning finding in its phases
deducts the amount the rubric gives for that outcome, and a category cannot
go below zero. A category counts only if one of its phases ran; the total
is the weight-averaged score of those that did, so a partial run cannot
pass for a full one. The rubric's `version` changes whenever a weight, a
category or the deduction model changes — never for a new check inside an
existing category.

## Integrity

A verdict asserts only what the run observed. The rubric is public and
versioned so that anyone can recompute a score from the verdicts and get
the same number.

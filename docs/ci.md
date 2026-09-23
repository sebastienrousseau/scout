---
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  Run scout in GitHub Actions or GitLab CI: the exit-code contract, gating on a score with jq, keeping the report as an artifact, and what not to run against production.
---

# Running scout in CI

A diagnostic you run by hand tells you what was true the afternoon you ran
it. The point of putting scout in a pipeline is that the answer stays
current: the server changes, the tool catalog changes, the protocol
revision changes, and the build tells you before an agent does.

This page is the whole contract — what scout returns, what it writes, and
where the sharp edges are.

## The exit-code contract

`scout check` has three outcomes and they mean different things. Treat
them differently or CI will be useless in both directions.

| Exit | Meaning | What CI should do |
|---|---|---|
| `0` | The run completed and nothing failed. Warnings may still be present. | Pass. |
| `1` | scout could not do its job: bad flags, an endpoint it could not reach, an authorization step that did not complete. **No verdict was reached.** | Fail the job loudly. This is scout or the pipeline being broken, not the server. |
| `2` | The run completed and at least one finding has status `fail`. | This is the finding you came for. |

The distinction between `1` and `2` is the one that matters. A pipeline
that treats "could not connect" the same as "connected and found four
problems" will eventually go green because the endpoint was down.

Exit `2` is triggered by `counts.fail > 0` — a `warn` never fails a build
on its own. If you want warnings to fail yours, gate on the JSON instead;
see below, or use a policy, which replaces that rule with one you wrote
down.

## GitHub Actions

```yaml
name: MCP server diagnostic

on:
  schedule:
    - cron: "17 6 * * *"
  workflow_dispatch:

permissions:
  contents: read

jobs:
  scout:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"

      - name: Install scout
        run: go install github.com/sebastienrousseau/scout/cmd/scout@latest

      - name: Diagnose
        env:
          MCP_TOKEN: ${{ secrets.MCP_TOKEN }}
        run: |
          scout check https://mcp.example.com/mcp \
            --token-env MCP_TOKEN \
            --report-dir "$RUNNER_TEMP/scout" \
            --no-color \
            --rps 2

      - name: Keep the evidence
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: scout-report
          path: ${{ runner.temp }}/scout
```

Three things in there are deliberate.

`--token-env MCP_TOKEN` rather than `--token "$MCP_TOKEN"`. The value
never becomes a process argument, so it never appears in `ps`, in a shell
trace, or in the log line a runner prints when a step fails.

`if: always()` on the upload. The run you most want the HAR from is the
one that just failed the job.

`--rps 2` is the default and it is there to be seen. scout throttles
because a scheduled job pointed at a production server is a scheduled
load test if you let it be. Raise it only against something you own.

### Gating on a policy

`jq` gating works and it lives in your pipeline, which means the rule your
organisation actually enforces is a shell line in a YAML file. Past one
repository, put it in a file instead:

```bash
scout check https://mcp.example.com/mcp --token-env MCP_TOKEN --policy .scout/policy.json
```

The policy replaces the default "any failure fails" rule, so exit `2` now
means *the policy was not met*. It can require named checks, cap failures and
warnings, set a floor on the score or on one category — and it can carry
exceptions that have a reason, a ticket and an expiry date, which is the
difference between a gate a team keeps and a gate a team switches off. A
policy written for a later scout is refused rather than partly applied, so
exit `1` still means nothing was judged.

See [Acceptance policies](policy.md). The same file governs `scout verify`, so
a gateway checking a signed attestation months later applies what the pipeline
applied.

With `--report-dir`, the answer is written to `policy.json` beside the
evidence — which is the artefact to attach to a change request, because it
names every rule, every exemption, and which of them were used.

### Gating on a score, not just a verdict

Exit `2` fires on any failure at any severity. If you want a threshold
without a policy file, take the JSON:

```bash
scout check https://mcp.example.com/mcp \
  --token-env MCP_TOKEN --output json --no-color > report.json || true

jq -e '.score.total >= 85' report.json > /dev/null \
  || { echo "score $(jq .score.total report.json) is below 85"; exit 1; }
```

The `|| true` is load-bearing: without it `set -e` ends the step on exit
`2` before you ever read the file. Check the file for `.counts` first if
you need to tell "scored 40" apart from "never ran".

Other fields worth gating on:

```bash
jq '.counts'                                             # pass/warn/fail/skip/info
jq '[.phases[].findings[] | select(.status == "fail")]'  # every failure, with evidence
jq '[.phases[].findings[]
     | select(.status == "fail" and .severity == "critical")] | length'
jq '.score.categories[] | select(.assessed) | {name, score}'

# Every failure as a line somebody can act on, with a link to what it means.
jq -r '.phases[].findings[] | select(.status == "fail")
       | "\(.id)\t\(.detail)\t\(.doc_url)"'
```

Every failing finding carries `evidence`, an array like `["req#8"]`, which
indexes the wire log in the report directory. See
[Reading the evidence](evidence.md) for following one back to the bytes.

### Surfacing findings where people already look

`--output sarif` writes SARIF 2.1.0, which GitHub code scanning reads
directly. Each alert links to the check's entry in
[the inventory](checks.md), because every rule carries the finding's
`doc_url` as its `helpUri`.

```yaml
      - name: Diagnose
        env:
          MCP_TOKEN: ${{ secrets.MCP_TOKEN }}
        run: |
          scout check https://mcp.example.com/mcp \
            --token-env MCP_TOKEN --output sarif --no-color > scout.sarif || true

      - uses: github/codeql-action/upload-sarif@v3
        if: always()
        with:
          sarif_file: scout.sarif
          category: scout
```

That needs `security-events: write` in the job's `permissions`. The `|| true`
is the same story as before: exit `2` would end the step before the upload.

Passing checks are in the file too, as `"kind": "pass"` with
`"level": "none"` — code scanning shows only the alerts, and a consumer that
wants the full picture can tell "checked and fine" apart from "not checked".

`--output junit` writes JUnit XML, so a run lands beside the unit tests in
whatever panel your CI already has. A warning becomes a `<failure>` typed
`warning`, because JUnit has no third state and a deviation reported as a
pass stops being read; gate on the `type` attribute if you only want real
failures.

```yaml
      - run: scout check "$ENDPOINT" --token-env MCP_TOKEN --output junit > scout.xml || true
      - uses: mikepenz/action-junit-report@v5
        if: always()
        with:
          report_paths: scout.xml
```

For a human-readable summary on the run page itself, `--output md` is the
shareable report, failures and warnings first, and renders as-is:

```bash
scout check "$ENDPOINT" --token-env MCP_TOKEN --output md --no-color \
  >> "$GITHUB_STEP_SUMMARY" || true
```

## GitLab CI

```yaml
mcp-diagnostic:
  image: golang:1.26
  timeout: 10m
  script:
    - go install github.com/sebastienrousseau/scout/cmd/scout@latest
    - scout check "$MCP_ENDPOINT" --token-env MCP_TOKEN
        --report-dir scout-report --no-color
  artifacts:
    when: always
    paths:
      - scout-report/
    expire_in: 30 days
```

Set `MCP_TOKEN` as a masked, protected CI/CD variable. scout reads it
from the environment; it is never an argument.

## What not to run in a pipeline

**`--allow-mutations` and `--allow-destructive` against anything you did
not stand up for the run.** By default scout invokes only tools that
declare `readOnlyHint`, which is what makes it safe to point at a live
server — see [ADR 0004](adr/0004-read-only-by-default.md). Those two
flags remove that guarantee. In CI they belong against an ephemeral
instance, never against production, and never in a job any contributor
can trigger.

**`--allow-load`.** It runs the burst unthrottled. That is a load test,
and a load test on a cron against someone else's infrastructure is an
outage with a changelog entry.

**`--capture-bodies` without reading [the security model](security-model.md)
first.** Bodies are redacted structurally at the recorder and capped, but
the artifact is still the closest thing to a copy of your traffic that
scout produces. Decide who can download build artifacts before you turn
it on.

**A fork-triggered `pull_request` job with the credential attached.**
`pull_request` from a fork must not see repository secrets. If the
diagnostic needs a real token, run it on `schedule` or
`workflow_dispatch`, or on `push` to your own branches.

## Shipping the run to your telemetry backend

A scheduled diagnostic is worth more when it lands where the rest of your
signals already are:

```yaml
      - name: Diagnose
        env:
          MCP_TOKEN: ${{ secrets.MCP_TOKEN }}
          OTLP_KEY: ${{ secrets.OTLP_KEY }}
        run: |
          scout check https://mcp.example.com/mcp \
            --token-env MCP_TOKEN \
            --otlp-endpoint https://otlp.example.com \
            --otlp-header "Authorization: Bearer $OTLP_KEY" \
            --log-format json || true
```

The endpoint may be a bare host; scout appends `/v1/traces` when the URL has
no path of its own. A collector that is down produces a warning and nothing
else — the exit code is whatever the run earned, because a telemetry backend
being unreachable is not a finding about the server under test.

`--log-format json` makes scout's own stderr one JSON object per line, each
carrying the run's `trace_id` — the same id on the report and on the exported
spans, so a log line joins to the run that produced it.

## Pinning the version

`@latest` is fine for a scheduled job whose failure you will read. For a
build that gates a merge, pin it, so a new check in a new release does not
turn into a red build nobody changed anything to cause:

```bash
go install github.com/sebastienrousseau/scout/cmd/scout@v0.0.1   # a tag, not @latest
```

No version has been tagged yet, so `@latest` currently resolves to the tip
of the default branch. Until the first release, a build that must not move
under you should pin the commit.

The report records what ran, under `scout.version`, so an archived
`report.json` says which binary produced it.

## Interpreting a run that changed

Two runs against an unchanged server should produce the same findings.
The score is derived from findings, not asserted independently
([ADR 0002](adr/0002-findings-cite-requests.md)), so a moved score always
has a finding under it. Diff the ids rather than the number:

```bash
jq -r '[.phases[].findings[] | select(.status == "fail") | .id] | sort[]' \
  report.json > today.txt
diff yesterday.txt today.txt
```

What legitimately moves without the server changing: latency percentiles
in the performance phase, and the generated arguments if you change
`--seed`. Everything else moving is a real difference.

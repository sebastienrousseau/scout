---
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  Use scout's Go packages directly: the client and MCP types, OAuth discovery, the Streamable HTTP transport, and the schema validator.
---

# Library use

The CLI is built on public Go packages you can use directly. The module
is `github.com/sebastienrousseau/scout`.

## Connect and call

```go
import (
    "context"
    "net/url"

    "github.com/sebastienrousseau/scout"
)

client, err := scout.New(scout.Config{
    Endpoint: "https://mcp.example.com/mcp",
    Auth: scout.AuthConfig{
        Mode:  scout.AuthClientCredentials,
        Extra: url.Values{"profile_id": {"tenant-1"}},
    },
})
res, err := client.Connect(ctx)   // 401 → discovery → registration → token → initialize
tools, err := client.ListTools(ctx)
out, err := client.CallTool(ctx, "search", map[string]any{"q": "invoices"})
```

`Connect` runs the whole state machine. For the authorization-code flow
it returns `StatusAuthorizationRequired` with the URL to open, and
`CompleteAuthorization(ctx, code, state)` finishes it. `Resume` reconnects
from a stored token source without rediscovery. Each step is also
exported on its own: `Initialize`, `Discover`, `Register`,
`ClientCredentialsSource`, `StartAuthorization`, `SetTokenSource`.

The client also exposes `Ping`, `ListResources`, `ListResourceTemplates`,
`ReadResource`, `ListPrompts`, `GetPrompt`, and `Call` for any JSON-RPC
method on the current session.

## Packages

| Package | What it holds |
|---|---|
| `scout` | the client, its configuration, and the MCP types |
| `auth` | RFC 9110 challenge parsing, RFC 9728 and RFC 8414 discovery, registration, PKCE, token sources, and the bearer round-tripper that refreshes and steps up scope |
| `transport` | the Streamable HTTP transport (JSON-RPC over POST, SSE responses, sessions, raw access for conformance probes) and the stdio transport, which runs a server as a child process over newline-delimited JSON |
| `diagnostics` | the safety policy, the schema-driven argument generator, the structural JSON Schema validator, the token-bucket limiter, and a budgeted agent loop behind a vendor-neutral `Model` interface |
| `trace` | the per-run trace id in context and the `X-MCP-Trace-ID` header |
| `attestation` | the MCP evaluation attestation: the in-toto statement types, `Parse`, `Validate`, `Covers` and `VerdictFor`. **Apache-2.0**, standard library only, so a gateway or registry can embed it to check a statement offline ([ADR 0011](adr/0011-attestation-format-is-apache.md)) |

The `internal/` packages (probe, report, telemetry, creds, config, diag)
are the CLI's own and carry no compatibility promise.

## Safety policy

```go
policy := diagnostics.Policy{AllowMutations: false, Deny: []string{"send_email"}}
d := policy.Decide(tool)   // d.Execute, d.Reason
```

The default policy executes only tools with `readOnlyHint: true`. A tool
without annotations is destructive by the specification's default and is
refused.

## Validator limits

`diagnostics.Validate` covers the structural core of JSON Schema: types,
required, properties, additionalProperties, items, enum, const, numeric
and length bounds, oneOf/anyOf/allOf. It does not resolve `$ref`,
`pattern` or `format`. Swap in a full validator if your servers rely on
those.

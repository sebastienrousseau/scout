---
# SPDX-License-Identifier: GPL-3.0-only
description: >-
  MCP's 2026-07-28 stateless revision: what changed, how scout works out which revision a server speaks, and what it checks on each.
---

# The stateless revision

MCP has two generations in the field, and scout runs all nine phases on
either. This page is about the newer one — what changed, how scout works
out which you are speaking, and what it checks that the older revision has
no equivalent of.

You do not need to read this to use scout. You need it when a finding
mentions `_meta`, `Mcp-Method` or an error code in the `-320xx` range, and
you want to know whether the tool or the server is wrong.

## What changed

The `2026-07-28` revision removed the session.

| | Handshake revisions | `2026-07-28` |
|---|---|---|
| Opening exchange | `initialize`, then `notifications/initialized` | none — the first request is the work |
| Session | `Mcp-Session-Id` header, server-issued | gone |
| Protocol version | negotiated once | per request, in `_meta` |
| Client identity | sent once at `initialize` | per request, in `_meta` |
| Capabilities | negotiated once | per request, in `_meta` |
| Routing | parse the body | `Mcp-Method` and `Mcp-Name` headers mirror it |
| Liveness | `ping` | `ping` removed; `server/discover` |
| Server discovery | `initialize` result | `server/discover`, a MUST |

The point of it is that a server becomes an ordinary stateless HTTP
service. Two requests can land on two instances behind a round-robin load
balancer and neither needs to know about the other.

Every request carries what the handshake used to establish:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "tools/call",
  "params": {
    "name": "find_customer",
    "arguments": { "email": "a@example.com" },
    "_meta": {
      "io.modelcontextprotocol/protocolVersion": "2026-07-28",
      "io.modelcontextprotocol/clientCapabilities": {},
      "io.modelcontextprotocol/clientInfo": { "name": "scout", "version": "0.0.1" }
    }
  }
}
```

`protocolVersion` and `clientCapabilities` are required. `clientInfo` is a
SHOULD, and scout sends it.

## How scout decides which you speak

Before first contact, and on a transport carrying no credentials.

The shape of the very first request depends on the answer, so guessing
wrong means every protocol finding afterwards describes scout's probe
rather than your server. scout asks, then commits:

1. It tries `server/discover`, which the new revision requires.
2. A useful answer means the stateless revision, and `handshake.protocol_era`
   records it.
3. `-32601 Method not found` means the older generation — with one
   distinction that matters: **HTTP 404 means modern-but-absent, HTTP 200
   with `-32601` means legacy.** Conflating them misreports both.

Once settled, the era drives the whole run. `handshake.initialize` and
`handshake.session` only make sense on the older revision;
`handshake.stateless` and `protocol.get_stream` only on the newer.

## What scout checks that only exists here

### Routing headers are validated, not just present

`Mcp-Method` and `Mcp-Name` mirror the body so a gateway can route without
parsing JSON. Mirrored headers only buy a gateway anything **if the server
checks they agree with the body** — otherwise the gateway routes on the
header and the server acts on the body, and they can be different requests.

`protocol.routing_headers` sends a request whose header disagrees with its
body and expects `-32020 HeaderMismatch`. Refusing with a bare HTTP status
is a warning rather than a pass: a client cannot tell that apart from an
ordinary rejection.

### Statelessness is tested, not believed

`resilience.stateless` sends the same request over two independent
connections and compares the answers. A server that quietly keeps state
will pass every other check and then fail the first time it sits behind a
load balancer.

### `GET` should be refused

There is no event stream to open, so `protocol.get_stream` expects `405`.

### What was removed should be gone — or declared

`initialize` and `ping` are not part of this revision. A server that still
answers them is in one of two situations, and from outside they look
identical: either it serves both generations deliberately, or it is carrying
handlers no current client will ever call.

`supportedVersions` in the `server/discover` result is how a server says
which. `protocol.deprecated_features` reads it before deciding, so declared
compatibility is a **pass** — scout is not in the business of telling a
deliberately compatible server to break itself. Undeclared, it is a warning:
the code is there and nothing tells a client it may be used, so the callers
left are stale software and whoever is enumerating the endpoint.

Declaring a revision the server does not actually serve is also a warning,
and the worse of the two: a client that reads the declaration negotiates
down, and the failure lands on its first real call rather than at discovery.

### Extensions are enumerated

`server/discover` carries an `extensions` list, and everything on it is
interface. `protocol.extensions` reports what is advertised, separating the
specification's own `io.modelcontextprotocol/…` extensions from an author's,
because "this server speaks Tasks" and "this server speaks something only
its own client knows about" are different facts.

The identifiers are reverse-DNS for the same reason `_meta` keys are: it is
a global namespace with no registry behind it, and the domain is what stops
two authors meaning different things by the same word. A bare `billing` is
reported, and so is the same extension listed twice — a client that
deduplicates and one that does not will disagree about what the server
offers, and neither is wrong.

scout does not test an extension's semantics. Naming it is the point: the
rest of the report describes the base protocol, and an operator should know
what sits beside it.

### The error codes

| Code | Meaning | When |
|---|---|---|
| `-32020` | HeaderMismatch | `Mcp-Method`/`Mcp-Name` disagree with the body |
| `-32021` | MissingRequiredClientCapability | the request needs a capability `_meta` did not declare |
| `-32022` | UnsupportedProtocolVersion | the server does not speak the version in `_meta` |

### Header values that HTTP will not carry

RFC 9110 restricts a field value to visible ASCII. A tool name is only
SHOULD-constrained to that, and a resource URI is not constrained at all —
so a name with an accent in it cannot go in `Mcp-Name` as-is. The revision
defines a Base64 sentinel form, `=?base64?…?=`, and scout uses it
automatically. See
[`transport.EncodeHeaderValue`](https://pkg.go.dev/github.com/sebastienrousseau/scout/transport#EncodeHeaderValue).

## `ping` is gone

Worth stating plainly, because scout got this wrong once and it is the kind
of error that costs somebody a day.

The revision removed `ping`. A stateless server answering `-32601` to a
ping is behaving **correctly**. scout used to report that as a failure,
which meant telling a conformant implementation to break itself — worse
than no diagnostic at all. Liveness now uses `server/discover` on this
revision, and two registry servers that had been failed went from 83 and 81
to 90 and 88 once it was fixed.

If you see a tool reporting a missing `ping` as a fault on `2026-07-28`,
that is the tool.

## Forcing the era

scout settles this itself, and should be left to. If you need to override —
testing a gateway that speaks one revision to you and another upstream:

```sh
scout check https://mcp.example.com/mcp --skip-era-check
```

That skips detection and uses the handshake path. There is no flag to force
the stateless path, because a server that does not answer `server/discover`
is not speaking it.

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package report

import "strings"

// A finding tells somebody what is wrong. It does not tell them what to do,
// and the gap between those two is where a report stops being useful.
//
// "add outputSchema and return structuredContent" is correct and it assumes
// the reader already knows what changed, why it changed, and where in their
// code it lives. The person reading this report is often not the person who
// wrote the server, and is frequently reading it to decide whether to adopt
// the thing at all.
//
// So each entry here answers two questions in order: what does this mean,
// and what do I change. The steps are imperative and name the actual field,
// header or method — a step a reader cannot act on without a search engine
// is a step that has not been written yet.
//
// This is documentation, not observation. It is keyed by check id and
// attached at render time rather than carried on every Finding, because it
// is identical for every run and would otherwise be duplicated into every
// JSON report. remediation_test.go fails when a key here is not a real
// check id.

// Step is one imperative action, with the reason it is the action.
type Step struct {
	Title string
	Body  string
}

// Remediation is the guidance for one check.
type Remediation struct {
	// Means explains the finding in terms of the protocol, for a reader who
	// did not follow the specification change that produced it.
	Means string
	// Steps are what to change, in the order they are worth doing.
	Steps []Step
	// Note is the shortcut, where one exists — usually that an SDK upgrade
	// does most of this. Saying so is not undermining the advice; it is the
	// difference between a report somebody acts on and one they postpone.
	Note string
}

// remediations is keyed by check id. Families are keyed by their literal
// prefix with the trailing dot, matching probe.DocFamilies.
var remediations = map[string]Remediation{
	"handshake.protocol_era": {
		Means: "MCP has two generations in the field. The older one opens with an " +
			"`initialize` request, and the server answers with an `Mcp-Session-Id` " +
			"that both sides then carry for the life of the connection. The " +
			"2026-07-28 revision removes that entirely: there is no handshake and " +
			"no session. Every request instead carries its own context, so a " +
			"server can answer any request without remembering the one before it.",
		Steps: []Step{
			{"Stop depending on initialize",
				"Remove the code that expects an initialization step, and anything " +
					"that stores or looks up per-session state keyed by " +
					"`Mcp-Session-Id`. On the current revision neither arrives."},
			{"Read the context out of _meta",
				"Each request carries the protocol version, the client's identity " +
					"and its capabilities in a `_meta` object on the request. That is " +
					"where the values you used to take from the initialize result now " +
					"live."},
			{"Route on the headers, not the body",
				"`Mcp-Method` and `Mcp-Name` mirror the method and the target name " +
					"outside the JSON. A gateway can route on them without parsing " +
					"the payload — but only if your server validates that they agree " +
					"with the body, which `protocol.routing_headers` checks."},
		},
		Note: "If you build on an official SDK, updating the package usually " +
			"carries the whole shift for you.",
	},

	"catalog.tools.output_schema": {
		Means: "`inputSchema` tells a client what arguments a tool takes. " +
			"`outputSchema` tells it what comes back. Without one, a result is " +
			"unvalidated text: the client cannot check it, cannot type it, and " +
			"cannot tell a malformed answer from a correct one — so the model is " +
			"left to interpret whatever arrives.",
		Steps: []Step{
			{"Declare outputSchema on each tool",
				"Ordinary JSON Schema, the same as `inputSchema`, describing the " +
					"shape of what the tool returns."},
			{"Return structuredContent, not prose",
				"Alongside the human-readable `content`, return a " +
					"`structuredContent` object that validates against the schema you " +
					"just declared. Returning a flat string leaves the client nothing " +
					"to check."},
			{"Keep them honest",
				"scout validates `structuredContent` against `outputSchema` on every " +
					"call it makes; a schema that does not describe the real result is " +
					"worse than none, because it is now a promise."},
		},
		Note: "Updating the SDK gives you the type definitions, which will flag " +
			"every tool still missing either half.",
	},

	"catalog.tools.descriptions": {
		Means: "A description is not documentation for a person. It is the text " +
			"the model reads when deciding which tool to call, and it is the only " +
			"thing distinguishing two tools with similar names. A thin one gets " +
			"the tool called at the wrong moment, or not at all.",
		Steps: []Step{
			{"Say what it does and when to use it",
				"One or two sentences: the action, the inputs that matter, and the " +
					"situation it is for. \"Searches\" is not a description; " +
					"\"Search the customer directory by email or account number\" is."},
			{"Say what it returns",
				"A caller choosing between two tools is choosing between two " +
					"results."},
			{"Name the limits",
				"Rate limits, maximum page sizes and required permissions belong " +
					"here — the model has no other way to learn them."},
		},
	},

	"catalog.tools.annotations": {
		Means: "Annotations are how a tool declares whether calling it is safe. " +
			"`readOnlyHint` says it only reads; `destructiveHint` says it can " +
			"destroy something. A tool with no annotations is treated by the " +
			"specification as destructive, which is why scout will not call it " +
			"and why a cautious client will not either.",
		Steps: []Step{
			{"Annotate every read-only tool",
				"Set `readOnlyHint: true` on anything that only reads. This is the " +
					"single change that most increases what a client is willing to do " +
					"unattended."},
			{"Be explicit about the rest",
				"Set `destructiveHint` honestly on anything that deletes or " +
					"overwrites. An unannotated tool and a destructive one are the " +
					"same thing to a careful caller, so silence costs you nothing but " +
					"usage."},
		},
	},

	"execution.validation": {
		Means: "The tool accepted a call with a required argument missing. The " +
			"schema said the argument was required and the server did not enforce " +
			"it, which means the schema is describing an intention rather than a " +
			"contract. A model that gets a plausible answer to an incomplete call " +
			"has no way to learn it made a mistake.",
		Steps: []Step{
			{"Validate arguments against your own schema",
				"Before the tool body runs, check the arguments against the " +
					"`inputSchema` you published. Most SDKs will do this for you if " +
					"you let them."},
			{"Fail with -32602",
				"Return the JSON-RPC `Invalid params` error rather than a success " +
					"with a guess in it. An error the model can read is a correction; " +
					"a plausible wrong answer is not."},
		},
	},

	"protocol.malformed_json": {
		Means: "A truncated or malformed request body was answered with a success " +
			"status. A client cannot distinguish that from a real answer, and a " +
			"proxy or a retry that corrupts a body will look like it worked.",
		Steps: []Step{
			{"Reject a body that does not parse",
				"Return HTTP 400, or the JSON-RPC `Parse error` code -32700. Either " +
					"tells the caller what happened; 200 does not."},
			{"Check the framing before the handler",
				"This usually belongs in the transport layer rather than in any " +
					"single tool, which is why it is easy to miss."},
		},
	},

	"protocol.invalid_params": {
		Means: "A request missing a required parameter was answered as though it " +
			"were valid. The parameter list is part of the contract; not enforcing " +
			"it means the contract is advisory.",
		Steps: []Step{
			{"Return -32602 for bad params",
				"`Invalid params` is the specified answer. Include which parameter " +
					"and why in the error message — that message reaches the model."},
		},
	},

	"protocol.unknown_tool": {
		Means: "Calling a tool that does not exist returned success. A client has " +
			"no way to tell a typo from a working call, and a model that " +
			"hallucinates a tool name is told it was right.",
		Steps: []Step{
			{"Return -32601 for an unknown method or tool",
				"`Method not found`. A model that receives an error learns the tool " +
					"does not exist; one that receives success learns the opposite."},
		},
	},

	"catalog.text.instructions": {
		Means: "Somewhere in the catalog, text is addressed to the model rather " +
			"than describing a tool — an instruction to ignore what it was told, " +
			"to conceal something from the user, or to act as a different agent. " +
			"A description is read with the same standing as the user's own words, " +
			"so this is indistinguishable from an instruction an attacker planted.",
		Steps: []Step{
			{"Find the text scout quoted",
				"The finding names the field, including when it is a `description` " +
					"inside an `inputSchema` — which is where this is most often " +
					"found, because it is the part nobody renders."},
			{"Rewrite it as a description",
				"Say what the tool does. If it genuinely needs a precondition, that " +
					"belongs in the schema as a required argument, not in prose " +
					"aimed at the model."},
			{"Find out how it got there",
				"If nobody on the team wrote it, the question is no longer a " +
					"wording one."},
		},
	},

	"catalog.text.comments": {
		Means: "Catalog text contains an HTML comment. A catalog viewer renders " +
			"the description and hides the comment; the model receives the raw " +
			"string and reads both. That gap is the whole attraction — a comment " +
			"is the one place to put text a reviewer will not see and the model " +
			"will.",
		Steps: []Step{
			{"Read what the comment says",
				"scout quotes it in the finding. A leftover note and an instruction " +
					"aimed at the model look identical in a diff and are not the " +
					"same problem."},
			{"Move it or delete it",
				"If it belongs in the description, put it there, where a person " +
					"approving the tool will see it. If it does not, it does not " +
					"belong in text the model reads either."},
		},
	},

	"catalog.text.hidden": {
		Means: "The catalog contains characters a reviewer cannot see — " +
			"zero-width spaces, or bidirectional overrides that reorder how text " +
			"displays without changing what is read. The model reads the real " +
			"sequence. Anyone auditing the catalog reads the rendered one.",
		Steps: []Step{
			{"Strip the characters scout listed",
				"The finding gives their code points and shows them in context as " +
					"`<U+200B>`-style escapes."},
			{"Ask why they are there",
				"A bidirectional override in a tool description has no honest " +
					"explanation. Zero-width characters occasionally arrive by " +
					"accident through a copy-paste from a rich-text editor."},
		},
	},

	"catalog.text.secret_paths": {
		Means: "Catalog text names a place credentials live — an SSH key, an AWS " +
			"credentials file, a token environment variable. That may be accurate " +
			"and expected. It is also exactly what a poisoned description says " +
			"when it is trying to get one read and returned.",
		Steps: []Step{
			{"Confirm the tool really does this",
				"If it does, say so in prose an operator approves before installing, " +
					"not in a schema field."},
			{"If it does not, delete the reference",
				"A description that names a credential path it never touches is " +
					"either a leftover or a probe."},
		},
	},

	"catalog.names.confusable": {
		Means: "A name mixes scripts — Latin with Cyrillic or Greek letters that " +
			"render identically. That is not how a name is written by accident; " +
			"it is how one tool is made to look like another the user already " +
			"trusts.",
		Steps: []Step{
			{"Rename it in a single script",
				"The finding names which scripts were mixed."},
			{"Check what it resembles",
				"The interesting question is which existing tool the rendered name " +
					"matches."},
		},
	},

	"protocol.routing_headers": {
		Means: "`Mcp-Method` and `Mcp-Name` mirror the method and target outside " +
			"the JSON body so a gateway can route without parsing it. That only " +
			"buys anything if the server checks they agree with the body — " +
			"otherwise the gateway and the server can act on two different " +
			"requests, which is a confused-deputy waiting to happen.",
		Steps: []Step{
			{"Compare the headers to the body",
				"On every request, before dispatch."},
			{"Refuse a mismatch with -32020",
				"`HeaderMismatch` is the specified code. Refusing is the point: a " +
					"request whose envelope and body disagree has no correct " +
					"interpretation."},
		},
	},

	// --- net ---------------------------------------------------------------

	"net.dns": {
		Means: "The hostname did not resolve. Nothing else in the run could be " +
			"attempted, because there was no address to connect to — every later " +
			"phase is skipped rather than failed.",
		Steps: []Step{
			{"Check the name, then the resolver",
				"A typo and a missing DNS record look identical from here. If the " +
					"name is right, the record may be internal-only, in which case " +
					"the run needs to happen from inside that network."},
		},
	},

	"net.scheme": {
		Means: "The endpoint is plain HTTP. Everything the client sends — the " +
			"bearer token above all — crosses the network readable by anything on " +
			"the path.",
		Steps: []Step{
			{"Serve the endpoint over TLS",
				"Then move the client to the https URL. An http endpoint that " +
					"redirects to https still exposes the first request, including " +
					"its Authorization header."},
		},
		Note: "Loopback is exempt and scout treats it as such: a local development " +
			"server on http is not a finding.",
	},

	"net.tcp": {
		Means: "The address resolved but the connection was refused or timed out. " +
			"The server is not listening where DNS says it is, or something between " +
			"here and there is dropping the connection.",
		Steps: []Step{
			{"Confirm what is listening on that port",
				"From a machine that can reach it. A firewall that drops rather " +
					"than rejects presents as a timeout, not a refusal."},
		},
	},

	"net.tls": {
		Means: "The TLS handshake failed, or the certificate did not verify for " +
			"this hostname. scout does not skip verification — a diagnostic that " +
			"ignores a bad certificate is telling you the connection is fine when " +
			"it is not.",
		Steps: []Step{
			{"Fix the chain, or the name",
				"An incomplete chain and a certificate for the wrong hostname are " +
					"the two common causes. The finding says which."},
			{"Check the expiry while you are there",
				"scout records the certificate's remaining lifetime in the " +
					"telemetry for every connection it makes."},
		},
	},

	"net.tls.cert": {
		Means: "The certificate has expired, or is close enough to expiry to be " +
			"worth acting on now. An expired certificate does not degrade " +
			"gracefully: every client stops connecting at the same moment, and the " +
			"first anyone hears of it is an outage.",
		Steps: []Step{
			{"Renew it",
				"The finding gives the remaining lifetime in days, which is also " +
					"recorded per connection in the telemetry."},
			{"Automate the renewal if it is not already",
				"A certificate that needed a person to remember is a certificate " +
					"that will expire on a weekend."},
		},
	},

	"net.tls.version": {
		Means: "The connection negotiated TLS 1.1 or older. Those versions are " +
			"deprecated and are being removed from clients — a server on one of " +
			"them will stop being reachable rather than become insecure.",
		Steps: []Step{
			{"Enable TLS 1.3, require 1.2 as the floor",
				"Both are widely supported. The only thing older versions buy is " +
					"compatibility with clients that should not be connecting to a " +
					"credentialed endpoint anyway."},
		},
	},

	// --- discovery ---------------------------------------------------------

	"discovery.first_contact": {
		Means: "The endpoint did not answer a JSON-RPC POST. Before any " +
			"authorization is attempted, scout makes one unauthenticated request to " +
			"see how the server asks to be authenticated; this is that request " +
			"failing outright.",
		Steps: []Step{
			{"Accept a POST at the endpoint",
				"Even unauthenticated, the answer should be an HTTP-level refusal " +
					"with a challenge — not a connection error, a redirect to a login " +
					"page, or an HTML error document."},
		},
	},

	"discovery.challenge": {
		Means: "The server demands authorization but does not say how to obtain " +
			"it. RFC 9728 expects a `WWW-Authenticate` header naming the protected " +
			"resource metadata, which is how a client finds the authorization " +
			"server without being configured by hand.",
		Steps: []Step{
			{"Return a challenge on 401",
				"`WWW-Authenticate: Bearer resource_metadata=\"https://…/" +
					".well-known/oauth-protected-resource\"`. Without it every client " +
					"needs out-of-band setup."},
		},
	},

	"discovery.prm": {
		Means: "Protected resource metadata is the document that tells a client " +
			"which authorization server issues tokens for this endpoint. Without " +
			"it, discovery stops and the operator has to supply the token endpoint " +
			"by hand.",
		Steps: []Step{
			{"Serve the metadata document",
				"At `/.well-known/oauth-protected-resource`, listing " +
					"`authorization_servers`."},
		},
	},

	"discovery.prm.resource": {
		Means: "The metadata names a `resource` that is not this endpoint. RFC " +
			"9728 requires a client to verify that binding, because metadata that " +
			"can claim to speak for any endpoint is metadata that can redirect a " +
			"token to the wrong one. scout refuses rather than warns.",
		Steps: []Step{
			{"Make resource match the endpoint it describes",
				"Exactly. If one document legitimately serves several endpoints, " +
					"each needs its own."},
		},
	},

	"discovery.as": {
		Means: "The authorization server's metadata could not be fetched or " +
			"parsed. That document carries the token and authorization endpoints, " +
			"so nothing after it can proceed.",
		Steps: []Step{
			{"Serve RFC 8414 or OIDC metadata",
				"At `/.well-known/oauth-authorization-server` or " +
					"`/.well-known/openid-configuration` on the issuer named in the " +
					"protected resource metadata."},
		},
	},

	"discovery.as.https": {
		Means: "A discovered authorization endpoint is plain HTTP. Everything " +
			"after the first document is chosen by the server under test, so an " +
			"http URL here would send a client secret across the network in the " +
			"clear. scout refuses to follow it.",
		Steps: []Step{
			{"Serve every discovered endpoint over HTTPS",
				"`--insecure-allow-http-auth` exists for local development and is a " +
					"deliberate override, not a fix."},
		},
	},

	"discovery.as.pkce": {
		Means: "The authorization server does not advertise PKCE with S256. MCP " +
			"clients are required to use PKCE, so a server that does not advertise " +
			"it either does not support it or is not saying so — and a client " +
			"cannot tell which.",
		Steps: []Step{
			{"Advertise S256",
				"`code_challenge_methods_supported: [\"S256\"]` in the metadata. " +
					"`plain` is not sufficient."},
		},
	},

	"discovery.registration": {
		Means: "Dynamic client registration lets a client obtain its own " +
			"credentials rather than being configured with a shared one. Without " +
			"it, every client needs a secret provisioned by hand.",
		Steps: []Step{
			{"Advertise registration_endpoint",
				"Or accept that each client is registered manually — which is a " +
					"defensible choice, and one worth stating in your own " +
					"documentation."},
		},
	},

	"discovery.creds_unused": {
		Means: "Credentials were supplied and the server never asked for any. The " +
			"run succeeded, but the endpoint is open: anything that can reach it " +
			"can use it.",
		Steps: []Step{
			{"Confirm that is intended",
				"An endpoint meant to be public is fine. One that was meant to be " +
					"protected and is not is the most serious thing in this report, " +
					"whatever its severity says."},
		},
	},

	"discovery.assemble": {
		Means: "The discovered documents could not be assembled into a usable " +
			"authorization configuration — the pieces were each fetchable but do " +
			"not fit together.",
		Steps: []Step{
			{"Read the error, then the metadata",
				"The finding carries the specific inconsistency. It is most often " +
					"an issuer that disagrees between the two documents."},
		},
	},

	"discovery.override.build": {
		Means: "The endpoints supplied on the command line — `--token-url`, " +
			"`--auth-url` — could not be turned into a working configuration. This " +
			"is about the override, not the server.",
		Steps: []Step{
			{"Check the overriding flags",
				"They bypass discovery entirely, so a typo here is not corrected by " +
					"anything the server says."},
		},
	},

	// --- auth --------------------------------------------------------------

	"auth.token": {
		Means: "No token could be obtained, so every phase that needs one was " +
			"skipped. Either no credentials were supplied for a server that " +
			"requires them, or the exchange itself failed.",
		Steps: []Step{
			{"Supply what the server asked for",
				"The discovery phase above says which mode the server advertises. " +
					"`--token-env` for a pre-issued token, `--auth " +
					"client-credentials` with `--client-secret-env` for a machine " +
					"client."},
			{"Read the exchange in the wire log",
				"The token endpoint's own error message is in the recorded " +
					"requests, and is usually more specific than the finding."},
		},
	},

	"auth.registration": {
		Means: "Dynamic client registration was attempted and failed. scout " +
			"registers a client when the server advertises the endpoint and no " +
			"client id was supplied.",
		Steps: []Step{
			{"Check what the registration endpoint returned",
				"It is in the wire log. Servers commonly reject registration " +
					"because of a redirect URI policy, which the error names."},
		},
	},

	"auth.token.type": {
		Means: "The token endpoint returned a `token_type` other than Bearer. A " +
			"client that assumes Bearer will send the wrong Authorization scheme " +
			"and be refused.",
		Steps: []Step{
			{"Return token_type: Bearer",
				"Unless the endpoint genuinely issues another type, in which case " +
					"the clients that can use it are the ones written for it."},
		},
	},

	"auth.token.expiry": {
		Means: "The token endpoint did not return `expires_in`, so a client cannot " +
			"tell how long the token is good for. It has to wait for a 401 and " +
			"retry, which turns a refresh into a user-visible failure.",
		Steps: []Step{
			{"Return expires_in",
				"Seconds, alongside the token. Clients then refresh before expiry " +
					"rather than after."},
		},
	},

	"auth.token.scope": {
		Means: "The granted scope is narrower than the scope requested. Calls that " +
			"need the missing permission will fail later, at the point of use, " +
			"with an error that does not obviously point back here.",
		Steps: []Step{
			{"Grant the scope, or stop advertising it",
				"A client that asks for what the server advertises and receives " +
					"less has no way to discover which calls will now fail."},
		},
	},

	"auth.rejects_garbage": {
		Means: "The server accepted an obviously invalid token. Whatever it is " +
			"doing with the Authorization header, it is not verifying it — which " +
			"means the endpoint is effectively unauthenticated.",
		Steps: []Step{
			{"Verify the token before handling the request",
				"Signature, issuer, audience and expiry. This is the most serious " +
					"class of finding scout produces, because every other " +
					"authorization control is downstream of it."},
		},
	},

	// --- handshake ---------------------------------------------------------

	"handshake.initialize": {
		Means: "The `initialize` request failed on a server that speaks a " +
			"handshake revision. Nothing after it can run: the protocol version and " +
			"the capability set are both settled here.",
		Steps: []Step{
			{"Read the error in the wire log",
				"An initialize that fails outright is usually a version " +
					"negotiation refusal or a malformed capabilities object, both of " +
					"which the response body names."},
		},
	},

	"handshake.stateless": {
		Means: "On the 2026-07-28 revision there is no `initialize`; a server " +
			"identifies itself through `server/discover` instead. This one answered " +
			"neither, so scout has no server identity or capability set to work " +
			"from.",
		Steps: []Step{
			{"Implement server/discover",
				"It is the stateless revision's replacement for the initialize " +
					"result, and the specification makes it a MUST."},
		},
	},

	"handshake.server_info": {
		Means: "The server did not identify itself, or gave a version of empty " +
			"string. Clients report that name and version in their own diagnostics, " +
			"and an operator looking at a misbehaving agent has nothing to go on.",
		Steps: []Step{
			{"Set name and version in serverInfo",
				"The version especially: it is what turns \"this server is " +
					"misbehaving\" into \"this build of this server is " +
					"misbehaving\"."},
		},
	},

	"handshake.capabilities": {
		Means: "The capabilities the server advertised do not match what it " +
			"actually serves — most often a capability declared and then not " +
			"implemented, or implemented and never declared. A client uses this " +
			"object to decide what to try.",
		Steps: []Step{
			{"Declare exactly what you implement",
				"An undeclared capability is one no careful client will use. A " +
					"declared one that fails is worse: it is discovered at the point " +
					"of use, in front of a user."},
		},
	},

	// --- protocol ----------------------------------------------------------

	"protocol.unknown_method": {
		Means: "An unknown method did not return `-32601`. A client cannot tell an " +
			"unimplemented method from a broken one, and the model on the other " +
			"side learns nothing from the answer.",
		Steps: []Step{
			{"Return -32601 Method not found",
				"For any method you do not implement, including the optional ones."},
		},
	},

	"protocol.ping": {
		Means: "`ping` is how a client checks a connection is alive without " +
			"invoking anything. Without it, the only liveness signal is a real " +
			"call, which costs whatever that call costs.",
		Steps: []Step{
			{"Implement ping",
				"It returns an empty result. Note that the 2026-07-28 revision " +
					"removes it, so this applies to handshake-revision servers."},
		},
	},

	"protocol.id_echo": {
		Means: "A response came back with an id that does not match the request " +
			"that produced it. JSON-RPC uses that id to pair the two — a client " +
			"with several requests in flight will hand the wrong answer to the " +
			"wrong caller.",
		Steps: []Step{
			{"Echo the request id exactly",
				"Same value, same type. A numeric id must not come back as a " +
					"string."},
		},
	},

	"protocol.get_stream": {
		Means: "A GET on the MCP endpoint behaved unexpectedly for the revision " +
			"the server speaks. The handshake revisions serve a standalone event " +
			"stream there; 2026-07-28 removes it and expects `405`.",
		Steps: []Step{
			{"Match the revision you advertise",
				"Serving a stream a client no longer opens is harmless; refusing " +
					"one a client still needs is not."},
		},
	},

	"protocol.bogus_session": {
		Means: "The server answered a request carrying a session id it never " +
			"issued. A client that has lost its session cannot tell it has, so it " +
			"keeps sending a dead id instead of re-initializing.",
		Steps: []Step{
			{"Return 404 for an unknown session",
				"That is the signal a client uses to start a new one. Accepting an " +
					"unknown id silently is how a client gets stuck."},
		},
	},

	// --- catalog -----------------------------------------------------------

	"catalog.tools.list": {
		Means: "The server declared a tools capability and then failed to list " +
			"them. Nothing in the execution phase can run, and a client will meet " +
			"the same error at the point it tries to use the server at all.",
		Steps: []Step{
			{"Implement tools/list, or stop declaring the capability",
				"A declared capability is a promise the client will act on."},
		},
	},

	"catalog.resources.list": {
		Means: "Resources were advertised and could not be listed. The same shape " +
			"as the tools case: a declared capability is a promise the client acts " +
			"on, and this one is not kept — so a client meets the error at the " +
			"moment it tries to use the server.",
		Steps: []Step{
			{"Implement resources/list, or drop the capability",
				"Whichever is true. A server with no resources and no resources " +
					"capability is correct; one that advertises and then fails is a " +
					"broken promise."},
		},
	},

	"catalog.prompts.list": {
		Means: "Prompts were advertised and could not be listed. A client reads " +
			"the capability set to decide what to offer, so a prompts capability " +
			"that cannot be enumerated produces a menu entry leading nowhere.",
		Steps: []Step{
			{"Implement prompts/list, or drop the capability",
				"A capability nobody can enumerate is one nobody can use."},
		},
	},

	"catalog.tools.unique": {
		Means: "Two tools share a name. Which one a call reaches is undefined, and " +
			"a model choosing between them is choosing between two identical " +
			"labels.",
		Steps: []Step{
			{"Make names unique",
				"The finding lists the duplicates. If they come from different " +
					"upstreams merged into one server, prefix them."},
		},
	},

	"catalog.tools.input_schema": {
		Means: "An `inputSchema` is not a JSON Schema object. A client cannot " +
			"generate arguments for it, cannot validate them, and in most SDKs will " +
			"not offer the tool at all.",
		Steps: []Step{
			{"Make every inputSchema type: object",
				"Even a tool with no arguments has an object schema with no " +
					"properties. The finding names which tools are wrong."},
		},
	},

	"catalog.resources.uris": {
		Means: "A resource URI is relative. A client has nothing to resolve it " +
			"against — the resource list is not a web page and there is no base.",
		Steps: []Step{
			{"Make resource URIs absolute",
				"With a scheme. Custom schemes are fine; relative paths are not."},
		},
	},

	"catalog.prompts.descriptions": {
		Means: "Prompts, or their arguments, are undescribed. The same problem as " +
			"an undescribed tool: the description is what the model reads to decide " +
			"whether this is the prompt it wants.",
		Steps: []Step{
			{"Describe each prompt and each argument",
				"What it produces, and what the argument is for."},
		},
	},

	"catalog.tools.title": {
		Means: "Tools have no human-readable `title`. The name is an identifier " +
			"meant for matching; the title is what a person reads when picking " +
			"from a list, and what a client shows in a consent prompt before " +
			"letting an agent call something.",
		Steps: []Step{
			{"Add a title to each tool",
				"Short, in sentence case, describing the action rather than " +
					"restating the identifier."},
		},
	},

	"catalog.empty": {
		Means: "The server exposes no tools, no resources and no prompts. There is " +
			"nothing for an agent to use — whatever else is correct, the catalog is " +
			"the product.",
		Steps: []Step{
			{"Check the capability declaration and the listing",
				"An empty catalog is usually a server that failed to register its " +
					"tools at startup rather than one that has none."},
		},
	},

	// --- execution ---------------------------------------------------------

	"execution.tools": {
		Means: "Every tool scout called returned an error. The catalog is " +
			"well-formed and nothing in it works, which a client discovers only at " +
			"the point of use.",
		Steps: []Step{
			{"Read one failing call in the wire log",
				"Each finding cites the request. A single failing call usually " +
					"explains all of them — a missing upstream credential, most " +
					"often."},
		},
	},

	"execution.content": {
		Means: "A tool returned `structuredContent` that does not validate against " +
			"its own `outputSchema`. The schema is a promise to the client about " +
			"what it can rely on, and this one is not kept.",
		Steps: []Step{
			{"Validate before returning",
				"Against the schema you published. The finding gives the JSON " +
					"pointer to the part that did not match."},
			{"Fix whichever is wrong",
				"Sometimes it is the result. Sometimes the schema drifted from the " +
					"code and nobody noticed, which is the more common of the two."},
		},
	},

	"execution.resources": {
		Means: "A resource read returned no contents. A client cannot tell that " +
			"apart from a broken read, so it will either retry or present an empty " +
			"document as though it were the answer.",
		Steps: []Step{
			{"Return contents, or an error",
				"An empty success is the one answer that carries no information."},
		},
	},

	"execution.prompts": {
		Means: "A prompt rendered with no messages. There is nothing for the model " +
			"to receive, so the prompt is unusable however well it is described.",
		Steps: []Step{
			{"Return at least one message",
				"Or report an error if the arguments given cannot produce one."},
		},
	},

	// --- performance -------------------------------------------------------

	"performance.ping": {
		Means: "The round-trip baseline is the cheapest call the server answers, " +
			"measured repeatedly. It is the floor under every other latency number " +
			"in this report — a slow baseline makes every tool look slow.",
		Steps: []Step{
			{"Compare cold and warm",
				"A large gap points at connection setup or a cold cache rather than " +
					"at the handler."},
		},
	},

	"performance.tools": {
		Means: "Tool latency was measured across repeated calls. A model waits for " +
			"these synchronously, and a slow tool is a slow agent — the cost lands " +
			"on the person watching a cursor blink.",
		Steps: []Step{
			{"Look at p95, not the mean",
				"The report gives both. An agent making several calls meets the " +
					"tail on most runs."},
		},
	},

	"performance.concurrency": {
		Means: "Calls failed under a modest parallel burst that succeeded " +
			"serially. That pattern points at shared mutable state or connection " +
			"handling rather than at load — the server is not slow, it is " +
			"incorrect under concurrency.",
		Steps: []Step{
			{"Look for state shared between requests",
				"A cached client, a reused buffer, a handler that is not reentrant. " +
					"The burst here is small; real agent traffic is larger."},
			{"If the failures are 429, send Retry-After",
				"Rate limiting is a correct answer. Rate limiting without telling " +
					"the client how long to wait is not."},
		},
	},

	"performance.rate_limit": {
		Means: "An unthrottled burst produced no rate limiting at all. That is " +
			"fine for a server you own, and a liability for one serving several " +
			"tenants: one misbehaving agent can consume the whole thing.",
		Steps: []Step{
			{"Consider a per-client limit",
				"With `Retry-After` on the 429, so a well-behaved client backs off " +
					"correctly rather than guessing."},
		},
	},

	// --- resilience --------------------------------------------------------

	"resilience.session_reinit": {
		Means: "After its session was invalidated, the client could not recover. " +
			"Sessions end for ordinary reasons — a deploy, a timeout, a load " +
			"balancer moving the connection — and a client that cannot re-establish " +
			"one turns that into a user-visible failure.",
		Steps: []Step{
			{"Return 404 for a dead session",
				"That is the signal to re-initialize. Any other answer leaves the " +
					"client repeating a dead id."},
			{"Make re-initialization cheap",
				"It happens more often than the happy path suggests."},
		},
	},

	"resilience.token_refresh": {
		Means: "The token source could not renew. Tokens expire during long runs, " +
			"and a client that cannot refresh fails partway through work it has " +
			"already started.",
		Steps: []Step{
			{"Issue refresh tokens, or keep lifetimes long enough",
				"Either is defensible. What does not work is a short lifetime with " +
					"no way to renew."},
		},
	},

	"resilience.stateless": {
		Means: "The same request sent over two independent connections produced " +
			"different answers. Independence from the connection is what lets a " +
			"server sit behind a load balancer at all — without it, the second " +
			"request in a conversation may land on a machine that knows nothing " +
			"about the first.",
		Steps: []Step{
			{"Move per-connection state out of the process",
				"Anything remembered between requests belongs in a store both " +
					"replicas can reach, or nowhere."},
			{"Re-read the request context each time",
				"On the current revision every request carries what it needs in " +
					"`_meta`; there is no session to consult."},
		},
	},
}

// RemediationFor returns the guidance for a check id.
//
// A family instance resolves to its family, the same way probe.DocURL does,
// because the guidance is about the family and the instance is whatever this
// particular server happened to have.
func RemediationFor(id string) (Remediation, bool) {
	if r, ok := remediations[id]; ok {
		return r, true
	}
	for key := range remediations {
		if strings.HasSuffix(key, ".") && strings.HasPrefix(id, key) {
			return remediations[key], true
		}
	}
	return Remediation{}, false
}

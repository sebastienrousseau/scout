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
			{"Reject unparseable bodies",
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

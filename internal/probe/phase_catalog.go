// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/sebastienrousseau/scout/internal/telemetry"
)

// phaseCatalog lists tools, resources and prompts and audits their
// descriptions and schemas without invoking anything.
func phaseCatalog(ctx context.Context, s *Session) []Finding {
	var out []Finding
	caps := s.Init.Capabilities
	pctx := func(label string) context.Context { return telemetry.WithPhase(ctx, "catalog", label) }

	// ---- tools ----
	c := s.check("catalog.tools.list", "tools/list")
	tools, err := s.Client.ListTools(pctx("tools/list"))
	switch {
	case err != nil && caps.Tools != nil:
		out = append(out, c.fail(Critical, "capability advertised but listing failed: "+err.Error(), "implement tools/list"))
	case err != nil:
		out = append(out, c.info("not supported: "+err.Error()))
	case caps.Tools == nil && len(tools) > 0:
		out = append(out, c.warn(fmt.Sprintf("%d tools listed but the tools capability was not declared", len(tools)), "declare capabilities.tools on initialize"))
	default:
		out = append(out, c.pass(fmt.Sprintf("%d tools", len(tools))))
	}
	s.Tools = tools
	if len(tools) > 0 {
		names := map[string]int{}
		var noDesc, shortDesc, badSchema, noAnn, noOut, noTitle []string
		for _, t := range tools {
			names[t.Name]++
			d := strings.TrimSpace(t.Description)
			switch {
			case d == "":
				noDesc = append(noDesc, t.Name)
			case len(d) < 20:
				shortDesc = append(shortDesc, t.Name)
			}
			if !schemaIsObject(t.InputSchema) {
				badSchema = append(badSchema, t.Name)
			}
			if t.Annotations == nil {
				noAnn = append(noAnn, t.Name)
			}
			if len(t.OutputSchema) == 0 {
				noOut = append(noOut, t.Name)
			}
			if t.Title == "" && (t.Annotations == nil || t.Annotations.Title == "") {
				noTitle = append(noTitle, t.Name)
			}
		}
		var dups []string
		for n, k := range names {
			if k > 1 {
				dups = append(dups, n)
			}
		}
		sort.Strings(dups)
		c = s.check("catalog.tools.unique", "Tool names are unique")
		if len(dups) > 0 {
			out = append(out, c.fail(Major, "duplicates: "+strings.Join(dups, ", "), "tool names must be unique"))
		} else {
			out = append(out, c.pass("no duplicates"))
		}
		c = s.check("catalog.tools.descriptions", "Every tool has a useful description")
		switch {
		case len(noDesc) > 0:
			out = append(out, c.fail(Major, "missing: "+list(noDesc), "an agent cannot choose a tool it cannot read about"))
		case len(shortDesc) > 0:
			out = append(out, c.warn("under 20 characters: "+list(shortDesc), "describe what the tool does, when to use it, and what it returns"))
		default:
			out = append(out, c.pass("all described"))
		}
		c = s.check("catalog.tools.input_schema", "inputSchema is a JSON Schema object")
		if len(badSchema) > 0 {
			out = append(out, c.fail(Major, "not type:object: "+list(badSchema), "inputSchema must describe an object"))
		} else {
			out = append(out, c.pass("all object schemas"))
		}
		// Whether the annotations exist, and then whether they are true.
		// The second is the one scout has a stake in: it invokes what
		// readOnlyHint: true claims is safe.
		c = s.check("catalog.tools.annotations", "Tools declare behaviour annotations")
		if len(noAnn) > 0 {
			out = append(out, c.warn(fmt.Sprintf("%d of %d without annotations: %s", len(noAnn), len(tools), list(noAnn)), "add readOnlyHint/destructiveHint; unannotated tools are treated as destructive and skipped by cautious clients"))
		} else {
			out = append(out, c.pass("all annotated"))
		}
		out = append(out, checkAnnotationHonesty(s))
		c = s.check("catalog.tools.output_schema", "Tools declare outputSchema")
		if len(noOut) == len(tools) {
			out = append(out, c.warn("none declare outputSchema", "add outputSchema and return structuredContent so results are machine-checkable"))
		} else if len(noOut) > 0 {
			out = append(out, c.info(fmt.Sprintf("%d of %d without outputSchema: %s", len(noOut), len(tools), list(noOut))))
		} else {
			out = append(out, c.pass("all declared"))
		}
		if len(noTitle) > 0 {
			out = append(out, s.check("catalog.tools.title", "Tools have a human title").info(fmt.Sprintf("%d without title", len(noTitle))))
		}

		// What the catalogue costs to look at, and whether a model has
		// anything to reason with once it has. Both are properties the
		// specification does not require and an agent pays for anyway.
		out = append(out, checkCatalogueBudget(s)...)
		out = append(out, checkParameterAmbiguity(s)...)

		// And whether this is still the catalogue somebody signed off.
		out = append(out, checkBaseline(s)...)
	}

	// ---- resources ----
	c = s.check("catalog.resources.list", "resources/list")
	res, err := s.Client.ListResources(pctx("resources/list"))
	switch {
	case err != nil && caps.Resources != nil:
		out = append(out, c.fail(Major, "capability advertised but listing failed: "+err.Error(), "implement resources/list"))
	case err != nil:
		out = append(out, c.info("not supported"))
	case caps.Resources == nil && len(res) > 0:
		out = append(out, c.warn(fmt.Sprintf("%d resources listed without the capability declared", len(res)), "declare capabilities.resources"))
	default:
		out = append(out, c.pass(fmt.Sprintf("%d resources", len(res))))
	}
	s.Resources = res
	if len(res) > 0 {
		var badURI, noMime []string
		for _, r := range res {
			if u, err := url.Parse(r.URI); err != nil || u.Scheme == "" {
				badURI = append(badURI, r.URI)
			}
			if r.MimeType == "" {
				noMime = append(noMime, r.Name)
			}
		}
		c = s.check("catalog.resources.uris", "Resource URIs are absolute")
		if len(badURI) > 0 {
			out = append(out, c.fail(Minor, list(badURI), "use scheme://… URIs"))
		} else {
			out = append(out, c.pass("all absolute"))
		}
		if len(noMime) > 0 {
			out = append(out, s.check("catalog.resources.mime", "Resources declare mimeType").info(fmt.Sprintf("%d without mimeType", len(noMime))))
		}
	}
	if caps.Resources != nil {
		c = s.check("catalog.resources.templates", "resources/templates/list")
		tpl, err := s.Client.ListResourceTemplates(pctx("resources/templates/list"))
		if err != nil {
			out = append(out, c.info("not supported: "+truncate(err.Error(), 80)))
		} else {
			s.Templates = tpl
			out = append(out, c.pass(fmt.Sprintf("%d templates", len(tpl))))
		}
	}

	// ---- prompts ----
	c = s.check("catalog.prompts.list", "prompts/list")
	prompts, err := s.Client.ListPrompts(pctx("prompts/list"))
	switch {
	case err != nil && caps.Prompts != nil:
		out = append(out, c.fail(Major, "capability advertised but listing failed: "+err.Error(), "implement prompts/list"))
	case err != nil:
		out = append(out, c.info("not supported"))
	case caps.Prompts == nil && len(prompts) > 0:
		out = append(out, c.warn(fmt.Sprintf("%d prompts listed without the capability declared", len(prompts)), "declare capabilities.prompts"))
	default:
		out = append(out, c.pass(fmt.Sprintf("%d prompts", len(prompts))))
	}
	s.Prompts = prompts
	if len(prompts) > 0 {
		var noDesc []string
		for _, p := range prompts {
			if strings.TrimSpace(p.Description) == "" {
				noDesc = append(noDesc, p.Name)
			}
			for _, a := range p.Arguments {
				if strings.TrimSpace(a.Description) == "" {
					noDesc = append(noDesc, p.Name+"."+a.Name)
				}
			}
		}
		c = s.check("catalog.prompts.descriptions", "Prompts and arguments are described")
		if len(noDesc) > 0 {
			out = append(out, c.warn("undescribed: "+list(noDesc), "describe each prompt and argument"))
		} else {
			out = append(out, c.pass("all described"))
		}
	}

	// ---- what the catalog says to the model ----
	//
	// Everything above asks whether the catalog is well formed. This asks
	// whether it is honest, which is a different question and the one an
	// agent is exposed to: a description is not documentation, it is input
	// the model reads before deciding what to call.
	if len(tools) > 0 || len(res) > 0 || len(prompts) > 0 {
		out = append(out, scanCatalog(s, res, prompts)...)
	}

	if len(tools) == 0 && len(res) == 0 && len(prompts) == 0 {
		out = append(out, s.check("catalog.empty", "Server exposes something").fail(Critical, "no tools, resources or prompts", "an MCP server with an empty catalog has nothing for an agent to use"))
	}
	return out
}

func schemaIsObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var s struct {
		Type any `json:"type"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	switch t := s.Type.(type) {
	case string:
		return t == "object"
	case []any:
		for _, v := range t {
			if v == "object" {
				return true
			}
		}
	case nil:
		// Some servers omit type and only give properties; accept that.
		var p struct {
			Properties map[string]any `json:"properties"`
		}
		return json.Unmarshal(raw, &p) == nil && p.Properties != nil
	}
	return false
}

func list(ss []string) string {
	sort.Strings(ss)
	if len(ss) > 8 {
		return strings.Join(ss[:8], ", ") + fmt.Sprintf(" (+%d more)", len(ss)-8)
	}
	return strings.Join(ss, ", ")
}

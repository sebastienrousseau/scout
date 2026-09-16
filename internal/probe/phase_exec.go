// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/transport"
)

// ToolResult records one synthetic tool invocation.
type ToolResult struct {
	Name         string         `json:"name"`
	Executed     bool           `json:"executed"`
	SkipReason   string         `json:"skip_reason,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	ArgsSource   string         `json:"args_source,omitempty"`
	Duration     Millis         `json:"duration_ms,omitempty"`
	OK           bool           `json:"ok"`
	ToolError    string         `json:"tool_error,omitempty"`
	ProtoError   string         `json:"protocol_error,omitempty"`
	ContentTypes []string       `json:"content_types,omitempty"`
	TextBytes    int            `json:"text_bytes,omitempty"`
	Structured   bool           `json:"structured_content"`
	SchemaIssues []string       `json:"schema_issues,omitempty"`
	// NegativeTest is what happened when a required argument was omitted.
	NegativeTest string `json:"negative_test,omitempty"`
}

// ResourceResult records one resources/read.
type ResourceResult struct {
	URI      string        `json:"uri"`
	Duration Millis        `json:"duration_ms"`
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Bytes    int           `json:"bytes,omitempty"`
	MimeType string        `json:"mime_type,omitempty"`
	Items    int           `json:"items,omitempty"`
}

// PromptResult records one prompts/get.
type PromptResult struct {
	Name     string        `json:"name"`
	Duration Millis        `json:"duration_ms"`
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Messages int           `json:"messages,omitempty"`
}

// phaseExecution invokes what the policy allows and validates content.
func phaseExecution(ctx context.Context, s *Session) []Finding {
	var out []Finding
	pctx := func(label string) context.Context { return telemetry.WithPhase(ctx, "execution", label) }
	limiter := diagnostics.NewLimiter(s.Opts.RPS, 1)
	gen := diagnostics.NewGenerator(s.Opts.Seed)
	gen.FillOptional = s.Opts.FillOptional

	out = append(out, s.check("execution.policy", "Safety policy").info(policyDescribe(s.Opts.Policy)))

	var executed, okCount, toolErr, protoErr, schemaBad, negWeak int
	for _, t := range s.Tools {
		tr := ToolResult{Name: t.Name}
		d := s.Opts.Policy.Decide(t)
		if !d.Execute {
			tr.SkipReason = d.Reason
			s.ToolResults = append(s.ToolResults, tr)
			continue
		}
		if ov, ok := s.Opts.ToolArgs[t.Name]; ok {
			tr.Arguments, tr.ArgsSource = ov, "override"
		} else {
			args, err := gen.Arguments(t.InputSchema)
			if err != nil {
				tr.SkipReason = "inputSchema unparsable: " + err.Error()
				s.ToolResults = append(s.ToolResults, tr)
				continue
			}
			tr.Arguments, tr.ArgsSource = args, "generated"
		}
		tr.Executed = true
		executed++
		if err := limiter.Wait(ctx); err != nil {
			return out
		}
		cctx, cancel := context.WithTimeout(pctx("tools/call "+t.Name), s.Opts.CallTimeout)
		start := time.Now()
		res, err := s.Client.CallTool(cctx, t.Name, tr.Arguments)
		tr.Duration = Millis(time.Since(start))
		cancel()
		switch {
		case err != nil:
			protoErr++
			tr.ProtoError = err.Error()
			if errors.Is(err, context.DeadlineExceeded) {
				tr.ProtoError = "timeout after " + s.Opts.CallTimeout.String()
			}
		case res.IsError:
			toolErr++
			tr.ToolError = truncate(res.Text(), 200)
		default:
			okCount++
			tr.OK = true
			seen := map[string]bool{}
			for _, c := range res.Content {
				if !seen[c.Type] {
					seen[c.Type] = true
					tr.ContentTypes = append(tr.ContentTypes, c.Type)
				}
				tr.TextBytes += len(c.Text)
			}
			tr.Structured = len(res.StructuredContent) > 0
			if len(res.Content) == 0 && !tr.Structured {
				tr.SchemaIssues = append(tr.SchemaIssues, "empty result: no content and no structuredContent")
			}
			if len(t.OutputSchema) > 0 {
				if !tr.Structured {
					tr.SchemaIssues = append(tr.SchemaIssues, "outputSchema declared but structuredContent missing")
				} else {
					tr.SchemaIssues = append(tr.SchemaIssues, diagnostics.Validate(t.OutputSchema, res.StructuredContent)...)
				}
			}
			if len(tr.SchemaIssues) > 0 {
				schemaBad++
			}
		}
		// Negative test: omit a required argument and expect rejection.
		if req := requiredArgs(t.InputSchema); len(req) > 0 && tr.ArgsSource != "override" {
			if err := limiter.Wait(ctx); err != nil {
				return out
			}
			cctx, cancel := context.WithTimeout(pctx("tools/call "+t.Name+" (missing "+req[0]+")"), s.Opts.CallTimeout)
			bad := map[string]any{}
			for k, v := range tr.Arguments {
				if k != req[0] {
					bad[k] = v
				}
			}
			res, err := s.Client.CallTool(cctx, t.Name, bad)
			cancel()
			var rpc *transport.RPCError
			switch {
			case err != nil && asRPC(err, &rpc):
				tr.NegativeTest = fmt.Sprintf("rejected (JSON-RPC %d)", rpc.Code)
			case err != nil:
				tr.NegativeTest = "transport error: " + truncate(err.Error(), 80)
			case res.IsError:
				tr.NegativeTest = "rejected (isError)"
			default:
				tr.NegativeTest = "ACCEPTED without required " + req[0]
				negWeak++
			}
		}
		s.ToolResults = append(s.ToolResults, tr)
	}

	c := s.check("execution.tools", "Tool invocations")
	switch {
	case len(s.Tools) == 0:
		out = append(out, c.skip("no tools"))
	case executed == 0:
		out = append(out, c.warn(fmt.Sprintf("0 of %d tools executed: none permitted by policy", len(s.Tools)), "annotate read-only tools with readOnlyHint, or opt in with --allow-mutations"))
	default:
		detail := fmt.Sprintf("%d executed: %d ok, %d tool errors, %d protocol errors", executed, okCount, toolErr, protoErr)
		switch {
		case protoErr > 0:
			out = append(out, c.fail(Major, detail, "protocol errors and timeouts mean the call never completed"))
		case toolErr == executed:
			out = append(out, c.warn(detail+" (every call returned isError; generated arguments may not suit this server, use --arg to supply real ones)", ""))
		case toolErr > 0:
			out = append(out, c.info(detail+" (isError results are often correct rejections of generated arguments)"))
		default:
			out = append(out, c.pass(detail))
		}
	}
	if executed > 0 {
		c = s.check("execution.content", "Results validate against outputSchema")
		if schemaBad > 0 {
			out = append(out, c.fail(Major, fmt.Sprintf("%d tool(s) returned content that does not match their contract", schemaBad), "see per-tool schema issues"))
		} else {
			out = append(out, c.pass("no contract violations among successful calls"))
		}
		c = s.check("execution.validation", "Tools reject missing required arguments")
		if negWeak > 0 {
			out = append(out, c.fail(Minor, fmt.Sprintf("%d tool(s) accepted a call with a required argument omitted", negWeak), "validate arguments against inputSchema before executing"))
		} else {
			out = append(out, c.pass("all tested tools rejected the call"))
		}
	}

	// ---- resources ----
	if n := len(s.Resources); n > 0 {
		limit := min(n, s.Opts.MaxResources)
		var ok, failed, empty int
		for _, r := range s.Resources[:limit] {
			if err := limiter.Wait(ctx); err != nil {
				return out
			}
			rr := ResourceResult{URI: r.URI}
			cctx, cancel := context.WithTimeout(pctx("resources/read"), s.Opts.CallTimeout)
			start := time.Now()
			res, err := s.Client.ReadResource(cctx, r.URI)
			rr.Duration = Millis(time.Since(start))
			cancel()
			if err != nil {
				failed++
				rr.Error = truncate(err.Error(), 120)
			} else {
				ok++
				rr.OK = true
				rr.Items = len(res.Contents)
				for _, c := range res.Contents {
					rr.Bytes += len(c.Text) + len(c.Blob)
					if rr.MimeType == "" {
						rr.MimeType = c.MimeType
					}
				}
				if rr.Items == 0 {
					empty++
				}
			}
			s.ResourceResults = append(s.ResourceResults, rr)
		}
		c := s.check("execution.resources", "Resource reads")
		detail := fmt.Sprintf("%d of %d read: %d ok, %d failed, %d empty", limit, n, ok, failed, empty)
		switch {
		case failed > 0:
			out = append(out, c.fail(Major, detail, "every listed resource should be readable"))
		case empty > 0:
			out = append(out, c.warn(detail, "a read that returns no contents is indistinguishable from a broken one"))
		default:
			out = append(out, c.pass(detail))
		}
	}

	// ---- prompts ----
	if n := len(s.Prompts); n > 0 {
		limit := min(n, s.Opts.MaxPrompts)
		var ok, failed, empty int
		for _, p := range s.Prompts[:limit] {
			if err := limiter.Wait(ctx); err != nil {
				return out
			}
			args := map[string]string{}
			for _, a := range p.Arguments {
				if a.Required {
					args[a.Name] = "example"
				}
			}
			pr := PromptResult{Name: p.Name}
			cctx, cancel := context.WithTimeout(pctx("prompts/get "+p.Name), s.Opts.CallTimeout)
			start := time.Now()
			res, err := s.Client.GetPrompt(cctx, p.Name, args)
			pr.Duration = Millis(time.Since(start))
			cancel()
			if err != nil {
				failed++
				pr.Error = truncate(err.Error(), 120)
			} else {
				ok++
				pr.OK = true
				pr.Messages = len(res.Messages)
				if pr.Messages == 0 {
					empty++
				}
			}
			s.PromptResults = append(s.PromptResults, pr)
		}
		c := s.check("execution.prompts", "Prompt rendering")
		detail := fmt.Sprintf("%d of %d rendered: %d ok, %d failed, %d empty", limit, n, ok, failed, empty)
		switch {
		case failed > 0:
			out = append(out, c.fail(Major, detail, "prompts/get should succeed with the required arguments"))
		case empty > 0:
			out = append(out, c.warn(detail, "a prompt with no messages is unusable"))
		default:
			out = append(out, c.pass(detail))
		}
	}
	return out
}

func policyDescribe(p diagnostics.Policy) string {
	parts := []string{"read-only tools"}
	if p.AllowDestructive {
		parts = []string{"ALL tools including destructive"}
	} else if p.AllowMutations {
		parts = append(parts, "non-destructive mutations")
	}
	s := strings.Join(parts, " + ")
	if len(p.Only) > 0 {
		s += "; only " + strings.Join(p.Only, ",")
	}
	if len(p.Deny) > 0 {
		s += "; deny " + strings.Join(p.Deny, ",")
	}
	return s
}

func requiredArgs(schema json.RawMessage) []string {
	var s struct {
		Required []string `json:"required"`
	}
	_ = json.Unmarshal(schema, &s)
	return s.Required
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sebastienrousseau/scout/internal/probe"
)

// Markdown renders a shareable report.
func Markdown(w io.Writer, r *Report) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p("# MCP diagnostic: %s\n\n", r.Target.Endpoint)
	p("| | |\n|---|---|\n")
	p("| Scout | %s |\n| Started | %s |\n| Duration | %s |\n| Trace | `%s` |\n", r.Scout.Version, r.Started.Format(time.RFC3339), fmtMS(r.Duration), r.TraceID)
	if r.Server != nil {
		p("| Server | %s %s |\n| Protocol | %s |\n| Capabilities | %s |\n", r.Server.Name, r.Server.Version, r.Server.Protocol, strings.Join(r.Server.Capabilities, ", "))
	}
	p("| Auth | %s, required=%t |\n", r.Auth.Mode, r.Auth.Required)
	if r.Auth.Issuer != "" {
		p("| Issuer | %s |\n", r.Auth.Issuer)
	}
	if r.Blocked != "" {
		p("| **Score** | withheld: run stopped (%s) |\n\n", esc(r.Blocked))
	} else {
		p("| **Score** | **%.1f / 100 (grade %s)**, %d of %d categories assessed |\n\n", r.Score.Total, r.Score.Grade, r.Score.Assessed, r.Score.Of)
	}

	if fails := r.Failures(); len(fails) > 0 {
		p("## Failures\n\n")
		for _, f := range fails {
			p("- **%s** (%s, %s): %s", f.Title, f.Severity, f.Phase, f.Detail)
			if f.Advice != "" {
				p(" — *%s*", f.Advice)
			}
			p("\n")
		}
		p("\n")
	}
	if warns := r.Warnings(); len(warns) > 0 {
		p("## Warnings\n\n")
		for _, f := range warns {
			p("- **%s**: %s", f.Title, f.Detail)
			if f.Advice != "" {
				p(" — *%s*", f.Advice)
			}
			p("\n")
		}
		p("\n")
	}

	if steps := r.NextSteps(); len(steps) > 0 {
		p("## What to do next\n\n")
		for i, f := range steps {
			p("%d. **%s** — %s\n", i+1, f.Title, esc(f.Advice))
		}
		p("\n")
	}
	p("## Score breakdown\n\n| Category | Weight | Score | Deductions |\n|---|---|---|---|\n")
	for _, c := range r.Score.Categories {
		if !c.Assessed {
			p("| %s | %d | not assessed | |\n", c.Name, c.Weight)
			continue
		}
		p("| %s | %d | %.1f | %s |\n", c.Name, c.Weight, c.Score, strings.ReplaceAll(strings.Join(c.Deductions, "<br>"), "|", "\\|"))
	}
	p("\n## Step by step\n\n")
	var skipped []string
	reason := r.Blocked
	for _, ph := range r.Phases {
		if ph.Skipped != "" {
			skipped = append(skipped, ph.Name)
			if reason == "" {
				reason = ph.Skipped
			}
			continue
		}
		p("### %s — %s (%s)\n\n", ph.Title, strings.ToUpper(string(ph.Status)), fmtMS(ph.Duration))
		p("| Status | Check | Observed | Evidence |\n|---|---|---|---|\n")
		for _, f := range ph.Findings {
			p("| %s | %s | %s | %s |\n", string(f.Status), f.Title, esc(joinNonEmpty(f.Detail, adviceMD(f))), esc(strings.Join(f.Evidence, "; ")))
		}
		p("\n")
	}

	if len(skipped) > 0 {
		p("Not run (%s): %s\n\n", esc(reason), strings.Join(skipped, ", "))
	}
	if len(r.Catalog.Tools) > 0 {
		p("## Tools\n\n| Name | Read-only | Annotated | outputSchema | Required args | Description |\n|---|---|---|---|---|---|\n")
		for _, t := range r.Catalog.Tools {
			p("| `%s` | %s | %s | %s | %s | %s |\n", t.Name, yn(t.ReadOnly), yn(t.Annotated), yn(t.OutputSchema), strings.Join(t.Required, ", "), esc(trunc(t.Description, 120)))
		}
		p("\n")
	}
	if len(r.Execution.Tools) > 0 {
		p("## Execution\n\n| Tool | Result | Latency | Content | Missing-arg test |\n|---|---|---|---|---|\n")
		for _, t := range r.Execution.Tools {
			res := "skipped: " + t.SkipReason
			switch {
			case t.ProtoError != "":
				res = "protocol error: " + t.ProtoError
			case t.ToolError != "":
				res = "isError: " + firstLine(t.ToolError)
			case t.Executed:
				res = "ok"
				if len(t.SchemaIssues) > 0 {
					res = "schema: " + strings.Join(t.SchemaIssues, "; ")
				}
			}
			content := strings.Join(t.ContentTypes, "+")
			if t.Structured {
				content += " +structured"
			}
			p("| `%s` | %s | %s | %s | %s |\n", t.Name, esc(res), fmtMS(t.Duration), content, esc(t.NegativeTest))
		}
		p("\n")
	}
	if r.Perf != nil && len(r.Perf.Tools) > 0 {
		p("## Performance\n\n| Call | Samples | Cold | p50 | p95 | Max |\n|---|---|---|---|---|---|\n")
		if r.Perf.Ping != nil {
			t := r.Perf.Ping
			p("| ping | %d | %s | %s | %s | %s |\n", t.Samples-t.Errors, fmtMS(t.Cold), fmtMS(t.P50), fmtMS(t.P95), fmtMS(t.Max))
		}
		for _, t := range r.Perf.Tools {
			p("| `%s` | %d | %s | %s | %s | %s |\n", t.Name, t.Samples-t.Errors, fmtMS(t.Cold), fmtMS(t.P50), fmtMS(t.P95), fmtMS(t.Max))
		}
		if c := r.Perf.Concurrency; c != nil {
			p("\nBurst of %d workers × %d calls on `%s`: %d ok, %d errors, %d rate-limited, %.1f calls/s, p50 %s, p95 %s.\n", c.Workers, c.Calls/c.Workers, c.Tool, c.OK, c.Errors, c.RateLimited, c.Throughput, fmtMS(c.P50), fmtMS(c.P95))
		}
		p("\n")
	}
	t := r.Telemetry
	p("## Telemetry\n\n%d requests, %d errors, %s sent, %s received, %d new connections, wall %s.\n", t.Requests, t.Errors, humanBytes(t.BytesSent), humanBytes(t.BytesReceived), t.NewConns, fmtMS(t.Wall))
	if len(r.Files) > 0 {
		p("\nFiles: %s\n", strings.Join(r.Files, ", "))
	}
}

func adviceMD(f probe.Finding) string {
	if f.Advice == "" || (f.Status != probe.Fail && f.Status != probe.Warn) {
		return ""
	}
	return "→ " + f.Advice
}

func esc(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ") }

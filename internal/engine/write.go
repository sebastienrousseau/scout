// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package engine

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/sebastienrousseau/scout/internal/report"
	"github.com/sebastienrousseau/scout/internal/telemetry"
)

// Render writes the result to w in the spec's format.
//
// It lives here rather than in a surface so that "what a JSON report looks
// like" is one answer, whether it was asked for by a flag, a key press or
// an HTTP Accept header.
func (r *Result) Render(w io.Writer, spec RunSpec, termWidth int) error {
	if r.Report == nil {
		return nil
	}
	switch spec.Output.Format {
	case FormatJSON:
		rep := *r.Report
		if !spec.Output.WithEvents && spec.Output.ReportDir == "" {
			rep.Events = nil
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	case FormatMD:
		report.Markdown(w, r.Report)
		return nil
	case FormatHTML:
		return report.HTML(w, r.Report, report.HTMLOptions{Verbose: spec.Output.Verbose})
	case FormatNDJSON:
		// The events already streamed; this is the closing record.
		rep := *r.Report
		rep.Events = nil
		return json.NewEncoder(w).Encode(map[string]any{"type": "report", "report": rep})
	default:
		report.Text(w, r.Report, report.TextOptions{
			Color:   !spec.Output.NoColor,
			Verbose: spec.Output.Verbose,
			Width:   termWidth,
		})
		return nil
	}
}

// WriteDir saves the full report and telemetry under dir and records the
// files on the report. Every format is written, because a report directory
// is what somebody hands to another person and they should not have had to
// guess which rendering that person can open.
func (r *Result) WriteDir(dir, version string) ([]string, error) {
	if r.Report == nil {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	var files []string
	write := func(name string, fn func(io.Writer) error) error {
		p := filepath.Join(dir, name)
		f, err := os.Create(p) // #nosec G304 -- p is built from the caller's own report directory
		if err != nil {
			return err
		}
		if err := fn(f); err != nil {
			_ = f.Close()
			return err
		}
		files = append(files, p)
		return f.Close()
	}

	full := *r.Report
	if r.Recorder != nil {
		full.Events = r.Recorder.Events()
	}
	steps := []struct {
		name string
		fn   func(io.Writer) error
	}{
		{"report.json", func(w io.Writer) error {
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			return enc.Encode(full)
		}},
		{"report.md", func(w io.Writer) error { report.Markdown(w, r.Report); return nil }},
		{"report.html", func(w io.Writer) error {
			return report.HTML(w, r.Report, report.HTMLOptions{Verbose: true})
		}},
		{"report.txt", func(w io.Writer) error {
			report.Text(w, r.Report, report.TextOptions{Verbose: true})
			return nil
		}},
		{"telemetry.ndjson", func(w io.Writer) error { return r.recorder().WriteNDJSON(w) }},
		{"telemetry.har", func(w io.Writer) error { return r.recorder().WriteHAR(w, version) }},
	}
	for _, s := range steps {
		if err := write(s.name, s.fn); err != nil {
			return nil, err
		}
	}
	r.Report.Files = files
	return files, nil
}

// recorder is nil-safe so WriteDir works for a result assembled by hand.
func (r *Result) recorder() *telemetry.Recorder {
	if r.Recorder == nil {
		return telemetry.New()
	}
	return r.Recorder
}

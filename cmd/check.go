// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/auth"
	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/diag"
	"github.com/sebastienrousseau/scout/internal/probe"
	"github.com/sebastienrousseau/scout/internal/report"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/internal/tui"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check <endpoint>",
	Short: "Test an MCP server and explain, in plain language, whether it is ready for agents.",
	Long: `Test an MCP server end to end and report, in plain language, whether
agents can use it and what to improve.

scout runs nine checks: it reaches the server, learns how it wants to be
authorized, signs in with the credentials you give it, completes the MCP
handshake, probes protocol behaviour, reads the tool and resource catalog,
safely calls what it can, measures speed under load, and confirms recovery
after a dropped session.

Only read-only tools are called unless you pass --allow-mutations or
--allow-destructive. Requests are throttled to --rps.

Examples:
  scout check https://mcp.example.com/mcp
  scout check https://mcp.example.com/mcp --token-env MCP_TOKEN
  scout check https://mcp.example.com/mcp --auth client-credentials \
      --client-id acme --client-secret-env ACME_SECRET --param profile_id=tenant-1
  scout check https://mcp.example.com/mcp --report-dir ./out --verbose
  scout check --profile prod --output json > report.json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheck(cmd.Context(), args, nil)
	},
}

func init() {
	checkCmd.Flags().AddFlagSet(credFlags())
	checkCmd.Flags().AddFlagSet(policyFlags())
	checkCmd.Flags().AddFlagSet(paceFlags())
	checkCmd.Flags().AddFlagSet(outputFlags())
}

// runCheck is shared by check, connect and tools; only restricts phases.
func runCheck(ctx context.Context, args []string, only []string) error {
	endpoint, err := resolveEndpoint(args)
	if err != nil {
		return err
	}
	if !validOutput(output) {
		return errors.New("--output must be text, json, md or ndjson")
	}
	cr, err := buildCreds()
	if err != nil {
		return err
	}
	args2, err := parseToolArgs(toolArgs)
	if err != nil {
		return err
	}
	rec := telemetry.New()
	rec.CaptureBodies = captureBodies
	var ndjson *json.Encoder
	if output == "ndjson" {
		ndjson = json.NewEncoder(os.Stdout)
		rec.Sink = func(e telemetry.Event) { _ = ndjson.Encode(map[string]any{"type": "request", "event": e}) }
	}
	if len(only) > 0 {
		phasesOnly = only
	}

	diag.Debugf("scout %s → %s", Version, rec.Redactor.URL(endpoint))
	diag.Debugf("credentials: %s", cr.Describe())
	for f, src := range cr.Sources {
		diag.Debugf("credential %s from %s", f, src)
	}

	isTTY := output == "text" && stdoutIsTTY()
	policy := buildPolicy()

	// -i: pick the tools to exercise in the selector before anything runs.
	// A tool chosen here is an explicit opt-in, so the policy is widened to
	// what was selected.
	if interactive && isTTY {
		tui.Version = Version
		items, ok, err := runSelector(ctx, func() ([]tui.Item, error) {
			return listSelectorItems(ctx, endpoint, cr, rec, policy)
		})
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if len(items) == 0 {
			fmt.Println("No tools selected.")
			return nil
		}
		policy.Only = nil
		for _, it := range items {
			policy.Only = append(policy.Only, it.Name)
			switch it.Kind {
			case "mutating":
				policy.AllowMutations = true
			case "destructive":
				policy.AllowDestructive = true
			}
		}
	}

	// The live view: a calm checklist that updates in place while the run
	// proceeds, printed inline so the finished list stays above the report.
	// Quitting mid-run cancels the run. The report is printed afterwards to
	// the terminal's own scrollback, so scrolling is the terminal's own.
	var program *tea.Program
	var runView *tui.RunModel
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	if isTTY {
		tui.Version = Version
		var phases []tui.Phase
		for _, p := range probe.Phases {
			if (len(phasesOnly) == 0 || containsString(phasesOnly, p.Name)) && !containsString(phasesSkip, p.Name) {
				phases = append(phases, tui.Phase{Name: p.Name, Title: p.Title})
			}
		}
		runView = tui.NewRunModel(rec.Redactor.URL(endpoint), runSubtitle(cr), phases)
		runView.Cancel = cancelRun
		program = newProgram(runView)
	}

	opts := probe.Options{
		Endpoint: endpoint, Creds: cr, Store: &creds.Store{}, Recorder: rec, Version: Version,
		HTTPClient: &http.Client{Timeout: callTimeout + 30*time.Second},
		Policy:     policy,
		URLPolicy: auth.URLPolicy{AllowHTTP: allowPlaintextAuth, AllowPrivate: allowPrivateHosts},
		AllowResourceMismatch: allowResourceMismatch,
		SkipEraCheck:          skipEraCheck,
		Samples:    samples, Concurrency: concurrency, RPS: rps, CallTimeout: callTimeout, Seed: seed,
		FillOptional: fillOpt, AllowLoad: allowLoad, MaxResources: maxRes, MaxPrompts: maxPrompts,
		ToolArgs: args2, Only: phasesOnly, Skip: phasesSkip,
		PhaseDone: func(pr probe.PhaseResult) {
			var ok, warn, fail int
			for _, f := range pr.Findings {
				switch f.Status {
				case probe.Fail:
					fail++
				case probe.Warn:
					warn++
				case probe.Pass, probe.Info:
					ok++
				}
			}
			counts := fmt.Sprintf("%d ok", ok)
			if warn > 0 {
				counts += fmt.Sprintf(", %d warn", warn)
			}
			if fail > 0 {
				counts += fmt.Sprintf(", %d fail", fail)
			}
			action := strings.ToUpper(string(pr.Status))
			if pr.Skipped != "" {
				counts = "skipped: " + pr.Skipped
			} else if pr.Status == probe.Skip {
				for _, f := range pr.Findings {
					if f.Status == probe.Skip && f.Detail != "" {
						counts = f.Detail
						break
					}
				}
			}
			switch {
			case program != nil:
				program.Send(tui.PhaseDoneMsg{Name: pr.Name, Action: action, Duration: pr.Duration.Duration(), Message: phaseSummary(pr, warn, fail)})
			case output == "text":
				log.Printf("%s [%s] %s: %s", tui.ResultIcon(action), action, pr.Name, counts)
			default:
				diag.Infof("%-12s %-4s %7s  %s", pr.Name, action, pr.Duration.Duration().Round(time.Millisecond), counts)
			}
		},
		Progress: func(phase string, f *probe.Finding) {
			if f == nil {
				diag.Debugf("phase %s starting", phase)
				if program != nil {
					program.Send(tui.PhaseStartMsg{Name: phase})
				}
				if ndjson != nil {
					_ = ndjson.Encode(map[string]any{"type": "phase", "phase": phase})
				}
				return
			}
			diag.Debugf("%s %s: %s — %s", f.Status, f.ID, f.Title, f.Detail)
			if ndjson != nil {
				_ = ndjson.Encode(map[string]any{"type": "finding", "finding": f})
			}
		},
	}

	var sess *probe.Session
	var runErr error
	var rep *report.Report
	build := func() {
		if sess != nil {
			rep = report.Build(sess, Version, withEvents || reportDir != "")
		}
	}
	if program != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			sess, runErr = probe.Run(runCtx, opts)
			build()
			program.Send(tui.DoneMsg{})
		}()
		if _, err := program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		program.Kill()
		<-done
		if runView != nil && runView.Aborted() {
			return runErr
		}
	} else {
		sess, runErr = probe.Run(runCtx, opts)
		build()
	}
	if sess == nil || rep == nil {
		return runErr
	}

	if reportDir != "" {
		files, err := writeReportDir(reportDir, rep, rec)
		if err != nil {
			return err
		}
		rep.Files = files
	}
	switch output {
	case "json":
		if !withEvents && reportDir == "" {
			rep.Events = nil
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	case "md":
		report.Markdown(os.Stdout, rep)
	case "ndjson":
		rep.Events = nil
		_ = ndjson.Encode(map[string]any{"type": "report", "report": rep})
	default:
		report.Text(os.Stdout, rep, report.TextOptions{Color: !noColor && stdoutIsTTY(), Verbose: verbose, Width: termWidth()})
	}
	if runErr != nil {
		return runErr
	}
	if rep.Counts.Fail > 0 {
		osExit(2)
	}
	return nil
}

// runSubtitle is the one-line, secret-free description of the credentials,
// for the live view.
func runSubtitle(cr *creds.Credentials) string {
	switch cr.Effective() {
	case creds.ModeBearer:
		return "signing in with a bearer token"
	case creds.ModeClientCredentials:
		return "signing in with OAuth client credentials"
	case creds.ModeAuthorizationCode:
		return "signing in with your stored login"
	}
	return "no credentials"
}

// phaseSummary turns a finished phase into a short, human status line.
func phaseSummary(pr probe.PhaseResult, warn, fail int) string {
	switch {
	case pr.Skipped != "":
		return "not needed"
	case pr.Status == probe.Skip:
		for _, f := range pr.Findings {
			if f.Status == probe.Skip && f.Detail != "" {
				return f.Detail
			}
		}
		return "not needed"
	case fail == 1:
		return "1 problem"
	case fail > 1:
		return fmt.Sprintf("%d problems", fail)
	case warn == 1:
		return "1 thing to improve"
	case warn > 1:
		return fmt.Sprintf("%d things to improve", warn)
	default:
		for _, f := range pr.Findings {
			if (f.Status == probe.Pass || f.Status == probe.Info) && f.Detail != "" {
				return f.Detail
			}
		}
		return "looks good"
	}
}

// termWidth returns stdout's column count, clamped to a comfortable reading
// width, or 84 when stdout is not a terminal.
func termWidth() int {
	if f, ok := any(os.Stdout).(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		if w, _, err := term.GetSize(f.Fd()); err == nil && w > 40 {
			if w > 96 {
				return 96
			}
			return w
		}
	}
	return 84
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// listSelectorItems connects with the supplied credentials and lists the
// tools with their policy class, for the interactive selector.
func listSelectorItems(ctx context.Context, endpoint string, cr *creds.Credentials, rec *telemetry.Recorder, policy diagnostics.Policy) ([]tui.Item, error) {
	cfg := scout.Config{Endpoint: endpoint, HTTPClient: &http.Client{Timeout: 60 * time.Second, Transport: rec.Wrap(nil)}, ClientInfo: scout.Implementation{Name: "scout", Version: Version}}
	cr.Apply(&cfg)
	client, err := scout.New(cfg)
	if err != nil {
		return nil, err
	}
	if cr.Effective() == creds.ModeAuthorizationCode {
		st, err := (&creds.Store{}).Get(endpoint)
		if err != nil {
			return nil, err
		}
		if st == nil {
			return nil, fmt.Errorf("no stored token for %s; run `scout login %s`", endpoint, endpoint)
		}
		if _, err := client.Resume(ctx, st.Source(creds.HTTP{C: client.HTTPClient()})); err != nil {
			return nil, err
		}
	} else {
		res, err := client.Connect(ctx)
		if err != nil {
			return nil, err
		}
		if res.Status != scout.StatusConnected {
			return nil, errors.New("server requires a user login; run `scout login` first")
		}
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]tui.Item, 0, len(tools))
	for _, t := range tools {
		kind := "mutating"
		switch {
		case t.IsReadOnly():
			kind = "read-only"
		case t.IsDestructive():
			kind = "destructive"
		}
		pol := "opt-in"
		if policy.Decide(t).Execute {
			pol = "allowed"
		}
		items = append(items, tui.Item{Name: t.Name, Kind: kind, Policy: pol, Description: t.Description})
	}
	return items, nil
}

func writeReportDir(dir string, rep *report.Report, rec *telemetry.Recorder) ([]string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	var files []string
	write := func(name string, fn func(*os.File) error) error {
		p := filepath.Join(dir, name)
		f, err := os.Create(p) // #nosec G304 -- p is built from the operator's own --report-dir
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
	full := *rep
	full.Events = rec.Events()
	if err := write("report.json", func(f *os.File) error {
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(full)
	}); err != nil {
		return nil, err
	}
	if err := write("report.md", func(f *os.File) error { report.Markdown(f, rep); return nil }); err != nil {
		return nil, err
	}
	if err := write("report.txt", func(f *os.File) error {
		report.Text(f, rep, report.TextOptions{Verbose: true})
		return nil
	}); err != nil {
		return nil, err
	}
	if err := write("telemetry.ndjson", func(f *os.File) error { return rec.WriteNDJSON(f) }); err != nil {
		return nil, err
	}
	if err := write("telemetry.har", func(f *os.File) error { return rec.WriteHAR(f, Version) }); err != nil {
		return nil, err
	}
	diag.Infof("saved the full report and telemetry to %s", dir)
	return files, nil
}

// newProgram builds the live-view program; tests swap in a headless one.
// The view renders inline (no alt-screen) so the finished checklist stays
// in the scrollback above the printed report.
var newProgram = func(m tea.Model) *tea.Program { return tea.NewProgram(m) }

// runSelector is indirected so tests can drive the interactive path.
var runSelector = tui.RunSelector

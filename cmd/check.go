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
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/diag"
	"github.com/sebastienrousseau/scout/internal/engine"
	"github.com/sebastienrousseau/scout/internal/probe"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/internal/tui"
	"github.com/sebastienrousseau/scout/trace"
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
  scout check --profile prod --output json > report.json

Most MCP servers are programs rather than URLs. Point scout at one with
--stdio and the command after --; scout starts it, diagnoses it, and stops
it again:

  scout check --stdio -- npx -y @modelcontextprotocol/server-everything
  scout check --stdio --stdio-env GITHUB_TOKEN -- docker run -i --rm ghcr.io/example/mcp

The server gets a fixed base environment plus whatever --stdio-env names,
and nothing else: it is a program nobody has audited, and everything else
you have exported is somebody else's secret.`,
	// Not MaximumNArgs(1): with --stdio the command to run follows "--",
	// and the count is whatever that command needs. resolveTarget rejects
	// the combinations that are genuinely wrong, with a reason.
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheck(cmd, args, nil)
	},
}

func init() {
	checkCmd.Flags().AddFlagSet(targetFlags())
	checkCmd.Flags().AddFlagSet(credFlags())
	checkCmd.Flags().AddFlagSet(policyFlags())
	checkCmd.Flags().AddFlagSet(paceFlags())
	checkCmd.Flags().AddFlagSet(outputFlags())
}

// runCheck is shared by check, connect and tools; only restricts phases.
//
// Everything this function does after building the spec is presentation:
// the live view, the interactive selector, where the bytes go. The run
// itself belongs to the engine, which is what lets the TUI and the web UI
// start exactly the same run without reimplementing any of it.
func runCheck(cmd *cobra.Command, args []string, only []string) error {
	ctx := cmdContext(cmd)
	target, err := resolveTarget(cmd, args)
	if err != nil {
		return err
	}
	spec, err := buildSpec(target, only)
	if err != nil {
		return err
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	cr, err := spec.Credentials()
	if err != nil {
		return err
	}

	diag.Debugf("scout %s → %s", Version, spec.Target.Describe())
	diag.Debugf("credentials: %s", cr.Describe())
	for f, src := range cr.Sources {
		diag.Debugf("credential %s from %s", f, src)
	}

	isTTY := spec.Output.Format == engine.FormatText && stdoutIsTTY()

	// -i: pick the tools to exercise before anything runs. Drawing the
	// selector is this surface's job; what it produces is the engine's,
	// so the widening a deliberate choice implies happens in one place.
	if spec.Output.Interactive && isTTY {
		tui.Version = Version
		rec := telemetry.New()
		items, ok, err := runSelector(ctx, func() ([]tui.Item, error) {
			return listSelectorItems(ctx, spec.Target, cr, rec, spec.ToolPolicy())
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
		names := make([]string, 0, len(items))
		kinds := make(map[string]string, len(items))
		for _, it := range items {
			names = append(names, it.Name)
			kinds[it.Name] = it.Kind
		}
		spec.SelectTools(names, kinds)
	}

	// NDJSON streams events as they land, so it is wired to the sink
	// rather than printed at the end.
	var ndjson *json.Encoder
	if spec.Output.Format == engine.FormatNDJSON {
		ndjson = json.NewEncoder(os.Stdout)
	}

	// The live view: a calm checklist that updates in place while the run
	// proceeds, printed inline so the finished list stays above the report.
	// Quitting mid-run cancels the run.
	var program *tea.Program
	var runView *tui.RunModel
	runCtx, cancelRun := context.WithCancel(ctx)
	// Settle the run's trace id here rather than letting probe.Run generate
	// one, so the structured diagnostics carry the same id as the report
	// and the exported spans. trace.Ensure keeps an id the caller already
	// set, so nothing downstream changes.
	runCtx = trace.Ensure(runCtx)
	diag.SetRunID(trace.FromContext(runCtx))
	defer cancelRun()
	if isTTY {
		tui.Version = Version
		var phases []tui.Phase
		for _, name := range spec.PhaseNames() {
			phases = append(phases, tui.Phase{Name: name, Title: probe.PhaseTitle(name)})
		}
		runView = tui.NewRunModel(spec.Target.Describe(), runSubtitle(cr), phases)
		runView.Cancel = cancelRun
		program = newProgram(runView)
	}

	sink := engine.SinkFunc(func(e engine.Event) {
		switch e.Kind {
		case engine.EventPhaseStart:
			diag.Debugf("phase %s starting", e.Phase)
			if program != nil {
				program.Send(tui.PhaseStartMsg{Name: e.Phase})
			}
			if ndjson != nil {
				_ = ndjson.Encode(map[string]any{"type": "phase", "phase": e.Phase})
			}
		case engine.EventFinding:
			f := e.Finding
			diag.Debugf("%s %s: %s — %s", f.Status, f.ID, f.Title, f.Detail)
			if ndjson != nil {
				_ = ndjson.Encode(map[string]any{"type": "finding", "finding": f})
			}
		case engine.EventPhaseDone:
			reportPhase(*e.Result, program, spec.Output.Format)
		case engine.EventRequest:
			if ndjson != nil {
				_ = ndjson.Encode(map[string]any{"type": "request", "event": e.Request})
			}
		}
	})

	var res *engine.Result
	if program != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			res = engine.Run(runCtx, spec, sink)
			program.Send(tui.DoneMsg{})
		}()
		if _, err := program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		program.Kill()
		<-done
		if runView != nil && runView.Aborted() {
			if res != nil {
				return res.Err
			}
			return nil
		}
	} else {
		res = engine.Run(runCtx, spec, sink)
	}
	if res == nil || res.Report == nil {
		if res != nil {
			return res.Err
		}
		return nil
	}

	if spec.Output.ReportDir != "" {
		if _, err := res.WriteDir(spec, Version); err != nil {
			return err
		}
		diag.Infof("saved the full report and telemetry to %s", spec.Output.ReportDir)
	}
	if spec.Output.Format == engine.FormatNDJSON {
		rep := *res.Report
		rep.Events = nil
		_ = ndjson.Encode(map[string]any{"type": "report", "report": rep})
	} else {
		width := 0
		if spec.Output.Format == engine.FormatText {
			width = termWidth()
		}
		out := spec
		out.Output.NoColor = spec.Output.NoColor || !stdoutIsTTY()
		if err := res.Render(os.Stdout, out, width); err != nil {
			return err
		}
	}
	// After the report, and never fatal. A collector being unreachable is
	// not a finding about the server under test, and a run that produced a
	// verdict must not be reported as a run that failed to.
	if err := res.ExportTraces(runCtx, spec, Version); err != nil {
		diag.Warnf("OTLP export failed: %v", err)
	} else if spec.Output.OTLPEndpoint != "" {
		diag.Infof("exported the run to %s", spec.Output.OTLPEndpoint)
	}

	if res.Err != nil {
		return res.Err
	}
	if res.Failed() {
		osExit(2)
	}
	return nil
}

// reportPhase draws one finished phase, in whichever way this surface is
// currently speaking.
func reportPhase(pr probe.PhaseResult, program *tea.Program, format engine.Format) {
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
	case format == engine.FormatText:
		log.Printf("%s [%s] %s: %s", tui.ResultIcon(action), action, pr.Name, counts)
	default:
		diag.Infof("%-12s %-4s %7s  %s", pr.Name, action, pr.Duration.Duration().Round(time.Millisecond), counts)
	}
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

// listSelectorItems connects with the supplied credentials and lists the
// tools with their policy class, for the interactive selector.
func listSelectorItems(ctx context.Context, target engine.TargetSpec, cr *creds.Credentials, rec *telemetry.Recorder, policy diagnostics.Policy) ([]tui.Item, error) {
	endpoint := target.Endpoint
	cfg := scout.Config{Endpoint: endpoint, HTTPClient: &http.Client{Timeout: 60 * time.Second, Transport: rec.Wrap(nil)}, ClientInfo: scout.Implementation{Name: "scout", Version: Version}}
	// The selector starts its own connection, because it has to list the
	// tools before the run that would have listed them. Over stdio that
	// means a second process — briefly, and closed before the run starts
	// its own. Sharing one would mean the selector holding a server open
	// while somebody reads a list, which is the longer-lived mistake.
	if target.Stdio() {
		cfg = scout.Config{
			Stdio: &scout.StdioConfig{
				Command: target.Command, Args: target.Args,
				Dir: target.Dir, PassEnv: target.PassEnv, Env: target.Env,
			},
			ClientInfo: scout.Implementation{Name: "scout", Version: Version},
		}
	}
	cr.Apply(&cfg)
	client, err := scout.New(cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()
	if target.Stdio() {
		if _, err := client.Connect(ctx); err != nil {
			return nil, err
		}
		return selectorItems(ctx, client, policy)
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
	return selectorItems(ctx, client, policy)
}

// selectorItems lists the catalog as the selector shows it. Shared by both
// transports, so what the selector offers cannot depend on how the server
// was reached.
func selectorItems(ctx context.Context, client *scout.Client, policy diagnostics.Policy) ([]tui.Item, error) {
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

// newProgram builds the live-view program; tests swap in a headless one.
// The view renders inline (no alt-screen) so the finished checklist stays
// in the scrollback above the printed report.
var newProgram = func(m tea.Model) *tea.Program { return tea.NewProgram(m) }

// runSelector is indirected so tests can drive the interactive path.
var runSelector = tui.RunSelector

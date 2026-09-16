// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/creds"
	"github.com/sebastienrousseau/scout/internal/engine"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Flag groups are shared FlagSets so each lands on exactly the commands
// that act on it, while every flag binds to one package-level variable.

var (
	// credentials
	authMode          string
	token             string
	tokenEnv          string
	headers           []string
	basic             string
	clientID          string
	clientSecret      string
	clientSecretEnv   string
	clientMetadataURL string
	scope             string
	params            []string
	tokenURL          string
	authURL           string
	resource          string
	redirectPort      int
	tokenAuthMethod   string

	// policy
	allowMutations   bool
	allowDestructive bool
	onlyTools        []string
	denyTools        []string
	toolArgs         []string

	// pacing
	samples     int
	concurrency int
	rps         float64
	callTimeout time.Duration
	seed        uint64
	fillOpt     bool
	allowLoad   bool

	allowPrivateHosts     bool
	allowPlaintextAuth    bool
	allowResourceMismatch bool
	skipEraCheck          bool
	maxRes                int
	maxPrompts            int

	// output
	interactive   bool
	output        string
	reportDir     string
	captureBodies bool
	withEvents    bool
	verbose       bool
	noColor       bool
	phasesOnly    []string
	phasesSkip    []string
)

var (
	credOnce, policyOnce, paceOnce, outOnce sync.Once
	credSet, policySet, paceSet, outSet     *pflag.FlagSet
)

func credFlags() *pflag.FlagSet {
	credOnce.Do(func() {
		fs := pflag.NewFlagSet("credentials", pflag.ContinueOnError)
		fs.StringVar(&authMode, "auth", "auto", "credential mode: auto, none, bearer, client-credentials, authorization-code")
		fs.StringVar(&token, "token", "", "pre-issued bearer token (or "+creds.EnvToken+")")
		fs.StringVar(&tokenEnv, "token-env", "", "read the bearer token from this environment variable")
		fs.StringArrayVar(&headers, "header", nil, "extra header sent on every request, \"Name: value\" (repeatable; API keys, tenant ids)")
		fs.StringVar(&basic, "basic", "", "HTTP basic credentials as user:password (or "+creds.EnvBasic+")")
		fs.StringVar(&clientID, "client-id", "", "OAuth client id (or "+creds.EnvClientID+")")
		fs.StringVar(&clientSecret, "client-secret", "", "OAuth client secret (or "+creds.EnvClientSecret+"; prefer --client-secret-env)")
		fs.StringVar(&clientSecretEnv, "client-secret-env", "", "read the client secret from this environment variable")
		fs.StringVar(&clientMetadataURL, "client-metadata-url", "", "https URL of a Client ID Metadata Document to use as client id")
		fs.StringVar(&scope, "scope", "", "scope to request (default: what the server challenge asks for)")
		fs.StringArrayVar(&params, "param", nil, "extra token/authorization request parameter key=value (repeatable; e.g. profile_id=tenant-1)")
		fs.StringVar(&tokenURL, "token-url", "", "token endpoint, bypassing discovery")
		fs.StringVar(&authURL, "auth-url", "", "authorization endpoint, bypassing discovery (with --token-url)")
		fs.StringVar(&resource, "resource", "", "RFC 8707 resource indicator override")
		fs.IntVar(&redirectPort, "redirect-port", 8976, "loopback port for the authorization-code redirect")
		fs.StringVar(&tokenAuthMethod, "token-auth-method", "", "token endpoint auth: client_secret_basic, client_secret_post or none")
		credSet = fs
	})
	return credSet
}

func policyFlags() *pflag.FlagSet {
	policyOnce.Do(func() {
		fs := pflag.NewFlagSet("policy", pflag.ContinueOnError)
		fs.BoolVar(&allowMutations, "allow-mutations", false, "also invoke tools that mutate but are not destructive")
		fs.BoolVar(&allowDestructive, "allow-destructive", false, "also invoke destructive and unannotated tools (dangerous)")
		fs.StringArrayVar(&onlyTools, "only", nil, "restrict execution to this tool (repeatable)")
		fs.StringArrayVar(&denyTools, "deny", nil, "never invoke this tool (repeatable)")
		fs.StringArrayVar(&toolArgs, "arg", nil, "argument override tool.field=value (repeatable); value parsed as JSON when possible")
		fs.BoolVar(&allowPrivateHosts, "insecure-allow-private-hosts", false, "allow discovered OAuth endpoints that resolve inside your network (off by default: a server naming one could aim your credentials at it)")
		fs.BoolVar(&allowPlaintextAuth, "insecure-allow-http-auth", false, "allow discovered OAuth endpoints served over plain http (off by default: tokens would cross the network in the clear)")
		fs.BoolVar(&allowResourceMismatch, "allow-resource-mismatch", false, "continue when the protected-resource metadata names a different endpoint (RFC 9728 requires this binding)")
		fs.BoolVar(&skipEraCheck, "skip-era-check", false, "do not send the one server/discover that identifies which protocol generation the server speaks")
		policySet = fs
	})
	return policySet
}

func paceFlags() *pflag.FlagSet {
	paceOnce.Do(func() {
		fs := pflag.NewFlagSet("pacing", pflag.ContinueOnError)
		fs.IntVar(&samples, "samples", 5, "repeat calls per tool in the performance phase")
		fs.IntVar(&concurrency, "concurrency", 4, "workers in the parallel burst (0 disables)")
		fs.Float64Var(&rps, "rps", 2, "max requests per second; 0 or negative disables throttling")
		fs.DurationVar(&callTimeout, "timeout", 30*time.Second, "per-call timeout")
		fs.Uint64Var(&seed, "seed", 1, "seed for generated arguments")
		fs.BoolVar(&fillOpt, "fill-optional", false, "also populate optional schema properties")
		fs.BoolVar(&allowLoad, "allow-load", false, "run the burst unthrottled to test the server's rate limiting")
		fs.IntVar(&maxRes, "max-resources", 25, "max resources to read")
		fs.IntVar(&maxPrompts, "max-prompts", 25, "max prompts to render")
		paceSet = fs
	})
	return paceSet
}

func outputFlags() *pflag.FlagSet {
	outOnce.Do(func() {
		fs := pflag.NewFlagSet("output", pflag.ContinueOnError)
		fs.BoolVarP(&interactive, "interactive", "i", false, "pick the tools to exercise in an interactive selector before the run")
		fs.StringVar(&output, "output", "text", "output format: text, json, md, ndjson, html")
		fs.StringVar(&reportDir, "report-dir", "", "write report.{txt,md,json}, telemetry.ndjson and telemetry.har here")
		fs.BoolVar(&captureBodies, "capture-bodies", false, "record request/response bodies in telemetry (redacted, capped)")
		fs.BoolVar(&withEvents, "events", false, "embed every telemetry event in JSON output")
		fs.BoolVarP(&verbose, "verbose", "v", false, "show evidence references and full info findings")
		fs.BoolVar(&noColor, "no-color", false, "disable ANSI colour")
		fs.StringSliceVar(&phasesOnly, "phases", nil, "run only these phases (comma-separated)")
		fs.StringSliceVar(&phasesSkip, "skip-phases", nil, "skip these phases (comma-separated)")
		outSet = fs
	})
	return outSet
}

// knownFlagNames lists every flag on any command, so a config key that
// belongs to a different command is not an error.
func knownFlagNames() map[string]bool {
	known := map[string]bool{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) { known[f.Name] = true })
		c.PersistentFlags().VisitAll(func(f *pflag.Flag) { known[f.Name] = true })
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	return known
}

// buildSpec turns the parsed flags into the one value that configures a
// run. It is the only place package-level flag state is read: everything
// downstream takes the spec, which is what lets the TUI and the web UI
// drive exactly the run the CLI would have.
func buildSpec(args []string, onlyPhases []string) (engine.RunSpec, error) {
	endpoint, err := resolveEndpoint(args)
	if err != nil {
		return engine.RunSpec{}, err
	}
	overrides, err := parseToolArgs(toolArgs)
	if err != nil {
		return engine.RunSpec{}, err
	}
	hdrs := map[string]string{}
	for _, h := range headers {
		k, v, err := creds.ParseHeader(h)
		if err != nil {
			return engine.RunSpec{}, err
		}
		hdrs[k] = v
	}
	prm := url.Values{}
	for _, p := range params {
		k, v, err := creds.ParseParam(p)
		if err != nil {
			return engine.RunSpec{}, err
		}
		prm.Add(k, v)
	}
	phases := phasesOnly
	if len(onlyPhases) > 0 {
		phases = onlyPhases
	}

	spec := engine.RunSpec{
		Version: Version,
		Target:  engine.TargetSpec{Endpoint: endpoint},
		Creds: engine.CredSpec{
			Mode: authMode, Token: token, TokenEnv: tokenEnv,
			Headers: hdrs, Basic: basic,
			ClientID: clientID, ClientSecret: clientSecret, ClientSecretEnv: clientSecretEnv,
			ClientMetadataURL: clientMetadataURL, Scope: scope, Params: prm,
			TokenURL: tokenURL, AuthURL: authURL, Resource: resource,
			RedirectPort: redirectPort, TokenAuthMethod: tokenAuthMethod,
		},
		Policy: engine.PolicySpec{
			AllowMutations: allowMutations, AllowDestructive: allowDestructive,
			Only: onlyTools, Deny: denyTools, ToolArgs: overrides,
			AllowPlaintextAuth: allowPlaintextAuth, AllowPrivateHosts: allowPrivateHosts,
			AllowResourceMismatch: allowResourceMismatch, SkipEraCheck: skipEraCheck,
		},
		Pacing: engine.PacingSpec{
			Samples: samples, Concurrency: concurrency, RPS: rps,
			CallTimeout: callTimeout, Seed: seed, FillOptional: fillOpt,
			AllowLoad: allowLoad, MaxResources: maxRes, MaxPrompts: maxPrompts,
		},
		Phases: engine.PhaseSpec{Only: phases, Skip: phasesSkip},
		Output: engine.OutputSpec{
			Format: engine.Format(output), ReportDir: reportDir,
			CaptureBodies: captureBodies, WithEvents: withEvents,
			Verbose: verbose, NoColor: noColor, Interactive: interactive,
		},
	}
	return spec.WithDefaults(), nil
}

// buildCreds turns the credential flags and environment into a model.
func buildCreds() (*creds.Credentials, error) {
	c := &creds.Credentials{Mode: creds.Mode(authMode), Headers: map[string]string{}, Params: url.Values{}, Sources: map[string]string{}}
	c.SetFromFlag("token", token)
	c.SetFromFlag("client-id", clientID)
	c.SetFromFlag("client-secret", clientSecret)
	c.SetFromFlag("basic", basic)
	if err := c.SetFromEnvName("token", tokenEnv); err != nil {
		return nil, err
	}
	if err := c.SetFromEnvName("client-secret", clientSecretEnv); err != nil {
		return nil, err
	}
	c.FromEnv()
	for _, h := range headers {
		k, v, err := creds.ParseHeader(h)
		if err != nil {
			return nil, err
		}
		c.Headers[k] = v
		c.Sources["header "+k] = "flag --header"
	}
	for _, p := range params {
		k, v, err := creds.ParseParam(p)
		if err != nil {
			return nil, err
		}
		c.Params.Add(k, v)
	}
	c.ClientMetadataURL, c.Scope, c.TokenURL, c.AuthURL, c.Resource = clientMetadataURL, scope, tokenURL, authURL, resource
	c.RedirectPort, c.TokenAuthMethod = redirectPort, tokenAuthMethod
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func buildPolicy() diagnostics.Policy {
	return diagnostics.Policy{AllowMutations: allowMutations, AllowDestructive: allowDestructive, Only: onlyTools, Deny: denyTools}
}

// parseToolArgs turns --arg tool.field=value into per-tool overrides. A
// value that parses as JSON is used as such, otherwise as a string.
func parseToolArgs(items []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for _, it := range items {
		key, val, ok := strings.Cut(it, "=")
		if !ok {
			return nil, fmt.Errorf("--arg %q must be tool.field=value", it)
		}
		tool, field, ok := strings.Cut(key, ".")
		if !ok || tool == "" || field == "" {
			return nil, fmt.Errorf("--arg %q must be tool.field=value", it)
		}
		if out[tool] == nil {
			out[tool] = map[string]any{}
		}
		out[tool][field] = jsonOrString(val)
	}
	return out, nil
}

func validOutput(o string) bool { return engine.Format(o).Valid() }

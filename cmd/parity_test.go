// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// specFields maps every flag that configures a run to the RunSpec field
// that carries it.
//
// This table is the parity contract. CLI, TUI and web UI are peers, which
// is only true while every capability lives under the surfaces rather than
// in one of them — and the only way a capability reaches all three is by
// being a field on the spec. A flag with no field here is a capability the
// other two surfaces cannot have, so adding one fails the build until the
// field exists.
var specFields = map[string]string{
	// target. --stdio itself carries no field of its own: Target.Command is
	// set or it is not, and a boolean beside it would be a second source of
	// truth for the same fact. The flag is a parsing instruction — read the
	// words after -- as a program — which is why it maps to the field those
	// words land in.
	"stdio":     "Target.Command",
	"stdio-dir": "Target.Dir",
	"stdio-env": "Target.PassEnv",
	"stdio-set": "Target.Env",

	// credentials
	"auth":                "Creds.Mode",
	"token":               "Creds.Token",
	"token-env":           "Creds.TokenEnv",
	"header":              "Creds.Headers",
	"basic":               "Creds.Basic",
	"client-id":           "Creds.ClientID",
	"client-secret":       "Creds.ClientSecret",
	"client-secret-env":   "Creds.ClientSecretEnv",
	"client-metadata-url": "Creds.ClientMetadataURL",
	"scope":               "Creds.Scope",
	"param":               "Creds.Params",
	"token-url":           "Creds.TokenURL",
	"auth-url":            "Creds.AuthURL",
	"resource":            "Creds.Resource",
	"redirect-port":       "Creds.RedirectPort",
	"token-auth-method":   "Creds.TokenAuthMethod",

	// policy
	"allow-mutations":              "Policy.AllowMutations",
	"allow-destructive":            "Policy.AllowDestructive",
	"only":                         "Policy.Only",
	"deny":                         "Policy.Deny",
	"arg":                          "Policy.ToolArgs",
	"insecure-allow-private-hosts": "Policy.AllowPrivateHosts",
	"insecure-allow-http-auth":     "Policy.AllowPlaintextAuth",
	"allow-resource-mismatch":      "Policy.AllowResourceMismatch",
	"skip-era-check":               "Policy.SkipEraCheck",
	// The flag names a file and the spec carries its contents, because a
	// path in a spec asks the receiving process to read something somebody
	// else named. Spec.Gate rather than Spec.Policy: PolicySpec already
	// means what scout may do to the server, and this is what the operator
	// will accept back.
	"policy": "Gate",

	// pacing
	"samples":       "Pacing.Samples",
	"concurrency":   "Pacing.Concurrency",
	"rps":           "Pacing.RPS",
	"timeout":       "Pacing.CallTimeout",
	"seed":          "Pacing.Seed",
	"fill-optional": "Pacing.FillOptional",
	"allow-load":    "Pacing.AllowLoad",
	"max-resources": "Pacing.MaxResources",
	"max-prompts":   "Pacing.MaxPrompts",

	// phases
	"phases":      "Phases.Only",
	"skip-phases": "Phases.Skip",

	// output
	"interactive":    "Output.Interactive",
	"output":         "Output.Format",
	"report-dir":     "Output.ReportDir",
	"capture-bodies": "Output.CaptureBodies",
	"events":         "Output.WithEvents",
	"guidance":       "Output.WithGuidance",
	"verbose":        "Output.Verbose",
	"no-color":       "Output.NoColor",
	"otlp-endpoint":  "Output.OTLPEndpoint",
	"otlp-header":    "Output.OTLPHeaders",
}

// notRunFlags are flags that deliberately carry no spec field, with the
// reason. They configure how scout talks or which run to start, not what
// the run does, so each surface answers them its own way — a browser has
// no --no-color and a terminal has no HTTP status code.
var notRunFlags = map[string]string{
	"config":  "selects the file a spec is built from, before there is a spec",
	"profile": "selects which stored defaults to build the spec from",
	"debug":   "process-level logging",
	"quiet":   "process-level logging",
	"help":    "process-level",
	"version": "process-level",
}

// runFlagSets are the flag groups that configure a run, as opposed to a
// subcommand's own behaviour.
func runFlagSets() []*pflag.FlagSet {
	return []*pflag.FlagSet{targetFlags(), credFlags(), policyFlags(), paceFlags(), outputFlags()}
}

// TestEveryRunFlagHasASpecField is the parity gate. A new flag with no
// RunSpec field fails here, which is the point: the failure arrives when
// the capability is added, not when somebody notices the web UI cannot
// reach it six months later.
func TestEveryRunFlagHasASpecField(t *testing.T) {
	var missing []string
	for _, fs := range runFlagSets() {
		fs.VisitAll(func(f *pflag.Flag) {
			_, mapped := specFields[f.Name]
			_, excluded := notRunFlags[f.Name]
			if !mapped && !excluded {
				missing = append(missing, f.Name)
			}
		})
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf(`these flags configure a run but carry no RunSpec field:

    %s

A capability reachable only through a flag is one the TUI and the web UI
cannot have. Add the field to engine.RunSpec, set it in buildSpec, and
record the mapping in specFields.`, strings.Join(missing, "\n    "))
	}
}

// And the other direction: a mapping left behind after a flag is removed
// is a lie about what the spec carries.
func TestNoStaleSpecFieldMappings(t *testing.T) {
	live := map[string]bool{}
	for _, fs := range runFlagSets() {
		fs.VisitAll(func(f *pflag.Flag) { live[f.Name] = true })
	}
	var stale []string
	for name := range specFields {
		if !live[name] {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("specFields maps flags that no longer exist: %v", stale)
	}
}

// Every run flag must also be one the config file can set, or a spec built
// from a profile silently differs from the same spec built from flags.
func TestRunFlagsAreConfigurable(t *testing.T) {
	known := knownFlagNames()
	var unknown []string
	for name := range specFields {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Errorf("these run flags are not reachable from the config file: %v", unknown)
	}
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderHelp(t *testing.T) {
	var b bytes.Buffer
	renderHelp(&b, rootCmd, 80)
	out := b.String()
	for _, want := range []string{"scout", "USAGE", "EXAMPLES", "COMMANDS", "check", "FLAGS", "--log-level", "ENVIRONMENT", "--help"} {
		if !strings.Contains(out, want) {
			t.Errorf("root help lacks %q", want)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Errorf("help line wider than 80: %q", line)
		}
	}
	b.Reset()
	renderHelp(&b, checkCmd, 90)
	out = b.String()
	for _, want := range []string{"scout check", "FLAGS", "--allow-destructive", "(default 2)", "GLOBAL FLAGS"} {
		if !strings.Contains(out, want) {
			t.Errorf("check help lacks %q", want)
		}
	}
	t.Setenv("SCOUT_SHOW_LOGO", "0")
	b.Reset()
	renderHelp(&b, rootCmd, 80)
	plainOut := ansiRE.ReplaceAllString(b.String(), "")
	if strings.Contains(plainOut, "⣿") || !strings.Contains(plainOut, "  scout\n") {
		t.Errorf("logo off must render the plain title line:\n%s", plainOut)
	}
	// piped: cobra's plain help is used
	if out, code := run(t, "--help"); code != 0 || !strings.Contains(out, "Available Commands:") {
		t.Errorf("piped help must stay plain: %d %q", code, out)
	}
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

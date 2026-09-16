// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package diag

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseLevelAndString(t *testing.T) {
	cases := map[string]Level{"error": LevelError, "WARN": LevelWarn, "warning": LevelWarn, "info": LevelInfo, "": LevelInfo, " Debug ": LevelDebug}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("loud"); err == nil || !strings.Contains(err.Error(), "unknown log level") {
		t.Errorf("bad level: %v", err)
	}
	for l, s := range map[Level]string{LevelError: "error", LevelWarn: "warn", LevelInfo: "info", LevelDebug: "debug", Level(9): "Level(9)"} {
		if l.String() != s {
			t.Errorf("%d.String() = %q, want %q", int(l), l.String(), s)
		}
	}
}

func TestEmitRespectsLevelAndOutput(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(nil)
	defer SetLevel(LevelInfo)

	SetLevel(LevelWarn)
	if CurrentLevel() != LevelWarn || !Enabled(LevelError) || !Enabled(LevelWarn) || Enabled(LevelInfo) || Enabled(LevelDebug) {
		t.Error("Enabled thresholds")
	}
	Errorf("boom %d", 1)
	Warnf("careful\n")
	Infof("hidden")
	Debugf("hidden too")
	got := buf.String()
	if got != "ERROR: boom 1\nWARN: careful\n" {
		t.Errorf("output = %q", got)
	}
	buf.Reset()
	SetLevel(LevelDebug)
	Infof("i")
	Debugf("d")
	if buf.String() != "INFO: i\nDEBUG: d\n" {
		t.Errorf("debug output = %q", buf.String())
	}
	SetOutput(nil)
	// nil restores stderr: emitting must not write to buf any more.
	buf.Reset()
	SetLevel(LevelError)
	Errorf("to stderr")
	if buf.Len() != 0 {
		t.Error("SetOutput(nil) did not restore stderr")
	}
	_ = os.Stderr
}

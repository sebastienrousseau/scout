// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package diag carries scout's diagnostic output: what it is doing and why
// something was skipped, as distinct from the results it produces.
//
// Results go to stdout in the selected output format and must stay
// parseable; diagnostics therefore go to stderr, levelled, one line each,
// prefixed with the level so the stream stays greppable.
package diag

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Level is the verbosity threshold.
type Level int

const (
	// LevelError reports only failures that stopped something from working.
	LevelError Level = iota
	// LevelWarn adds conditions scout worked around or declined to act on.
	LevelWarn
	// LevelInfo adds ordinary progress narration. This is the default.
	LevelInfo
	// LevelDebug adds detail only useful when diagnosing scout itself.
	LevelDebug
)

// EnvVar sets the level for a whole shell session.
const EnvVar = "SCOUT_LOG_LEVEL"

// String returns the lowercase name accepted by ParseLevel.
func (l Level) String() string {
	switch l {
	case LevelError:
		return "error"
	case LevelWarn:
		return "warn"
	case LevelInfo:
		return "info"
	case LevelDebug:
		return "debug"
	}
	return fmt.Sprintf("Level(%d)", int(l))
}

// ParseLevel maps a name to a Level, case-insensitively.
func ParseLevel(name string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "error":
		return LevelError, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "info", "":
		return LevelInfo, nil
	case "debug":
		return LevelDebug, nil
	}
	return LevelInfo, fmt.Errorf("unknown log level %q (want error, warn, info or debug)", name)
}

var (
	mu     sync.RWMutex
	level            = LevelInfo
	output io.Writer = os.Stderr
)

// SetLevel sets the verbosity threshold.
func SetLevel(l Level) { mu.Lock(); level = l; mu.Unlock() }

// CurrentLevel reports the threshold in force.
func CurrentLevel() Level { mu.RLock(); defer mu.RUnlock(); return level }

// SetOutput redirects diagnostics; nil restores stderr.
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	if w == nil {
		w = os.Stderr
	}
	output = w
}

// Enabled reports whether a message at l would be emitted.
func Enabled(l Level) bool { return l <= CurrentLevel() }

func emit(l Level, format string, args ...any) {
	mu.RLock()
	threshold, w := level, output
	mu.RUnlock()
	if l > threshold {
		return
	}
	msg := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(w, "%s: %s\n", strings.ToUpper(l.String()), strings.TrimRight(msg, "\n"))
}

// Errorf reports a failure that stopped something from working.
func Errorf(format string, args ...any) { emit(LevelError, format, args...) }

// Warnf reports a condition scout worked around or declined to act on.
func Warnf(format string, args ...any) { emit(LevelWarn, format, args...) }

// Infof reports ordinary progress.
func Infof(format string, args ...any) { emit(LevelInfo, format, args...) }

// Debugf reports detail useful only when diagnosing scout itself.
func Debugf(format string, args ...any) { emit(LevelDebug, format, args...) }

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"fmt"
	"strings"
	"time"
)

// summarize writes the one-line outcome of a phase in plain language, for
// the phase list and the executive overview. It states what was found,
// not what was checked.
func summarize(name string, s *Session, pr PhaseResult) string {
	by := map[string]Finding{}
	fails, warns := 0, 0
	for _, f := range pr.Findings {
		by[f.ID] = f
		switch f.Status {
		case Fail:
			fails++
		case Warn:
			warns++
		}
	}
	improve := func() string {
		switch {
		case fails > 0 && warns > 0:
			return fmt.Sprintf("%s, %s", plural(fails, "issue"), plural(warns, "thing to improve", "things to improve"))
		case fails > 0:
			return plural(fails, "issue")
		case warns > 0:
			return plural(warns, "thing to improve", "things to improve")
		}
		return ""
	}
	switch name {
	case "net":
		if f, ok := by["net.dns"]; ok && f.Status == Fail {
			return "Hostname does not resolve"
		}
		if f, ok := by["net.tcp"]; ok && f.Status == Fail {
			return "Not reachable"
		}
		if f, ok := by["net.tls"]; ok && f.Status == Fail {
			return "TLS handshake fails"
		}
		if f, ok := by["net.tls"]; ok {
			out := "Reachable over " + firstWords(f.Detail, 2)
			if c, ok := by["net.tls.cert"]; ok && c.Status != Pass {
				out += ", certificate needs attention"
			}
			return out
		}
		if f, ok := by["net.scheme"]; ok && f.Status == Fail {
			return "Reachable, but over plain HTTP"
		}
		return "Reachable, plain HTTP on this machine"
	case "discovery":
		if !s.Reached {
			return "Server did not answer"
		}
		if !s.RequiresAuth {
			return "Open server, no credentials required"
		}
		if s.Discovery != nil && s.Discovery.Server != nil {
			out := "Protected, authorization via " + hostOf(s.Discovery.Server.Issuer)
			if x := improve(); x != "" {
				out += " · " + x
			}
			return out
		}
		return "Protected, but the authorization server could not be found"
	case "auth":
		if pr.Status == Skip {
			return "Not needed"
		}
		if fails > 0 {
			if f, ok := by["auth.rejects_garbage"]; ok && f.Status == Fail {
				return "Accepts invalid tokens"
			}
			if f, ok := by["auth.token"]; ok {
				return "No usable credentials: " + firstSentence(f.Detail)
			}
		}
		if s.Token != nil {
			out := "Authorized"
			if !s.Token.Expiry.IsZero() {
				out += fmt.Sprintf(", token valid for %s", time.Until(s.Token.Expiry).Round(time.Minute))
			}
			if x := improve(); x != "" {
				out += " · " + x
			}
			return out
		}
		return "Credentials accepted"
	case "handshake":
		if s.Init == nil {
			if f, ok := by["handshake.initialize"]; ok {
				return "Rejected: " + firstSentence(f.Detail)
			}
			return "Failed"
		}
		out := fmt.Sprintf("%s %s, protocol %s", s.Init.ServerInfo.Name, s.Init.ServerInfo.Version, s.Init.ProtocolVersion)
		if x := improve(); x != "" {
			out += " · " + x
		}
		return strings.TrimSpace(out)
	case "protocol":
		if x := improve(); x != "" {
			return "Mostly conformant · " + x
		}
		return "Conformant on every edge case tested"
	case "catalog":
		parts := []string{}
		if n := len(s.Tools); n > 0 {
			parts = append(parts, plural(n, "tool"))
		}
		if n := len(s.Resources); n > 0 {
			parts = append(parts, plural(n, "resource"))
		}
		if n := len(s.Prompts); n > 0 {
			parts = append(parts, plural(n, "prompt"))
		}
		if len(parts) == 0 {
			return "Nothing for an agent to use"
		}
		out := strings.Join(parts, ", ")
		if x := improve(); x != "" {
			out += " · " + x
		}
		return out
	case "execution":
		ran, ok := 0, 0
		for _, r := range s.ToolResults {
			if r.Executed {
				ran++
				if r.OK {
					ok++
				}
			}
		}
		switch {
		case len(s.Tools) == 0 && len(s.Resources) == 0:
			return "Nothing to run"
		case ran == 0:
			return "No tool was safe to run without opting in"
		}
		out := fmt.Sprintf("%d of %d tools ran cleanly", ok, ran)
		if x := improve(); x != "" {
			out += " · " + x
		}
		return out
	case "performance":
		if s.Perf == nil || len(s.Perf.Tools) == 0 {
			if s.Perf != nil && s.Perf.Ping != nil {
				return fmt.Sprintf("Round trip %s", ms(s.Perf.Ping.P50))
			}
			return "Not measured"
		}
		var worst Millis
		worstName := ""
		for _, t := range s.Perf.Tools {
			if t.P95 > worst {
				worst, worstName = t.P95, t.Name
			}
		}
		if worst.Duration() > 2*time.Second {
			return fmt.Sprintf("Slow: %s takes up to %.1fs", worstName, worst.Duration().Seconds())
		}
		return fmt.Sprintf("Fast, slowest tool answers in %s", ms(worst))
	case "resilience":
		if pr.Status == Skip {
			return "Stateless server, nothing to recover"
		}
		if fails > 0 {
			return "Does not recover cleanly"
		}
		if warns > 0 {
			return "Recovers, with a caveat"
		}
		return "Recovers from a lost session"
	}
	if x := improve(); x != "" {
		return x
	}
	return "Passed"
}

func plural(n int, singular string, pluralForm ...string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	if len(pluralForm) > 0 {
		return fmt.Sprintf("%d %s", n, pluralForm[0])
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".;:("); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func firstWords(s string, n int) string {
	w := strings.Fields(s)
	if len(w) > n {
		w = w[:n]
	}
	return strings.Join(w, " ")
}

func hostOf(issuer string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(issuer, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i > 0 {
		s = s[:i]
	}
	return s
}

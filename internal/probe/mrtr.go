// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sebastienrousseau/scout/transport"
)

// Multi Round-Trip Requests are how the 2026-07-28 revision replaced
// server-initiated sampling, elicitation and roots. A server that needs
// something from the client mid-call answers `resultType: input_required`
// with a list of client-side methods to invoke, and the client retries the
// original call with the answers attached.
//
// scout does not answer one. It has no user to elicit from and no model to
// sample, and inventing either would mean reporting on a conversation it
// fabricated. What it can do is judge the request, which is worth more than
// it sounds: an input_required a client cannot answer is a deadlock, and the
// agent it deadlocks will report nothing at all — the call simply never
// returns. That failure is invisible from inside the agent and obvious from
// here.
//
// The measurement is observational. scout cannot make a server ask for
// input; a tool that needs it is the only thing that produces one, so the
// check reports what the run happened to see and skips when it saw nothing.

// MRTRObservation is one input_required result the run received.
type MRTRObservation struct {
	// Method is the call that was interrupted, named the way the report
	// names it — "tools/call search".
	Method string `json:"method"`
	// Requests is what the server asked the client to do, carried verbatim.
	Requests []transport.InputRequest `json:"requests"`
}

// requestedMethods is the client-side methods one input_required asked for.
func requestedMethods(ir *transport.ErrInputRequired) []string {
	out := make([]string, 0, len(ir.Result.InputRequests))
	for _, r := range ir.Result.InputRequests {
		if m := strings.TrimSpace(r.Method); m != "" {
			out = append(out, m)
		}
	}
	return uniqueSorted(out)
}

// asInputRequired reports whether a call failed because the server wants
// client input, which is not a failure.
func asInputRequired(err error) (*transport.ErrInputRequired, bool) {
	var ir *transport.ErrInputRequired
	if err != nil && errors.As(err, &ir) {
		return ir, true
	}
	return nil, false
}

// checkMRTR judges the input_required results the run saw.
func checkMRTR(s *Session) Finding {
	c := s.check("protocol.mrtr", "Requests for client input are answerable")

	if len(s.MRTR) == 0 {
		if s.Stateless() {
			return c.skip("no call asked for client input, so there was nothing to judge; scout cannot make a server ask")
		}
		return c.skip("Multi Round-Trip Requests are a " + transport.V20260728 + " mechanism and no call asked for client input")
	}

	var unanswerable, methods []string
	empty := 0
	for _, o := range s.MRTR {
		if len(o.Requests) == 0 {
			// The worst shape, and the reason this check exists. The server
			// said it needs something and named nothing, so there is no
			// retry a client can construct: the call is unfinishable and
			// the agent waits forever.
			empty++
			continue
		}
		for _, r := range o.Requests {
			switch {
			case strings.TrimSpace(r.Method) == "":
				unanswerable = append(unanswerable, o.Method+": a request with no method")
			case strings.TrimSpace(r.ID) == "":
				// Without an id the client cannot say which request each
				// answer belongs to, so a call needing two is ambiguous and
				// a call needing one is a guess.
				unanswerable = append(unanswerable, fmt.Sprintf("%s: %s with no id", o.Method, r.Method))
			default:
				methods = append(methods, r.Method)
			}
		}
	}

	c = c.ev(observedCalls(s.MRTR)...)

	if empty > 0 {
		return c.fail(Critical,
			fmt.Sprintf("%s answered input_required and named no request", plural(empty, "call")),
			"list what you need in inputRequests. A client that is told input is required and not told what to supply cannot retry, so the call never completes and the agent waits on it rather than failing — which is worse than an error, because nothing is reported")
	}
	if len(unanswerable) > 0 {
		sort.Strings(unanswerable)
		return c.fail(Major,
			"a request for client input cannot be answered: "+list(unanswerable),
			"give every entry in inputRequests an id and a method. The id is how the client says which answer belongs to which request when it retries the call, and without it a call needing more than one answer has no correct retry")
	}

	return c.pass(fmt.Sprintf("%s asked for client input, and every request named a method and an id (%s)",
		plural(len(s.MRTR), "call"), list(uniqueSorted(methods))))
}

// observedCalls names the interrupted calls, for the evidence line.
func observedCalls(obs []MRTRObservation) []string {
	out := make([]string, 0, len(obs))
	for _, o := range obs {
		out = append(out, o.Method)
	}
	return uniqueSorted(out)
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

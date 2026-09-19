// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"strings"
	"testing"

	"github.com/sebastienrousseau/scout/transport"
)

// The defect this file exists for: a tool that answers input_required is a
// server implementing the current revision correctly, and scout counted it as
// a protocol error — failing the execution phase and taking the category score
// down with it. That is the ping mistake repeating, and the ping mistake cost
// two registry servers seven points each.
func TestAToolThatNeedsInputIsNotAProtocolError(t *testing.T) {
	f := newFakeServer(t)
	f.q.inputRequired = true
	_, fs := run(t, f, ccCreds(), nil)

	tools, ok := fs["execution.tools"]
	if !ok {
		t.Fatal("execution.tools is missing")
	}
	if tools.Status == Fail {
		t.Errorf("a correct MRTR server failed the execution phase: %s", tools.Detail)
	}
	if strings.Contains(tools.Detail, "1 protocol errors") {
		t.Errorf("an input_required was counted as a protocol error: %s", tools.Detail)
	}
	if !strings.Contains(tools.Detail, "needing client input") {
		t.Errorf("the detail does not say what happened instead: %s", tools.Detail)
	}

	// And it is recorded per tool, so a reader can see which one asked.
	var asked int
	for _, tr := range sessionOf(t, f).ToolResults {
		if len(tr.NeedsInput) > 0 {
			asked++
			if tr.ProtoError != "" {
				t.Errorf("%s carries both NeedsInput and a protocol error: %q", tr.Name, tr.ProtoError)
			}
			if !strings.Contains(strings.Join(tr.NeedsInput, ","), "elicitation/create") {
				t.Errorf("%s: NeedsInput = %v", tr.Name, tr.NeedsInput)
			}
		}
	}
	if asked == 0 {
		t.Error("no tool recorded that it needed input")
	}
}

// sessionOf re-runs against the same fake and returns the session, for the
// assertions that are about recorded state rather than findings.
func sessionOf(t *testing.T, f *fakeServer) *Session {
	t.Helper()
	s, _ := run(t, f, ccCreds(), nil)
	return s
}

func TestMRTRJudgesTheRequests(t *testing.T) {
	t.Run("answerable", func(t *testing.T) {
		f := newFakeServer(t)
		f.q.inputRequired = true
		_, fs := run(t, f, ccCreds(), nil)
		m, ok := fs["protocol.mrtr"]
		if !ok {
			t.Fatal("protocol.mrtr is missing")
		}
		if m.Status != Pass {
			t.Fatalf("status = %s: %s", m.Status, m.Detail)
		}
		if !strings.Contains(m.Detail, "elicitation/create") {
			t.Errorf("the detail does not say what was asked for: %s", m.Detail)
		}
		if len(m.Evidence) == 0 {
			t.Error("no evidence naming the interrupted call")
		}
	})

	// The worst shape, and the reason the check exists: the server says it
	// needs something and names nothing, so no retry can be constructed. The
	// agent waits forever and reports nothing, which is worse than an error.
	t.Run("input required and nothing named", func(t *testing.T) {
		f := newFakeServer(t)
		f.q.mrtrEmpty = true
		_, fs := run(t, f, ccCreds(), nil)
		m, ok := fs["protocol.mrtr"]
		if !ok {
			t.Fatal("protocol.mrtr is missing")
		}
		if m.Status != Fail || m.Severity != Critical {
			t.Fatalf("status = %s %s: %s", m.Status, m.Severity, m.Detail)
		}
		if !strings.Contains(m.Detail, "named no request") {
			t.Errorf("detail = %q", m.Detail)
		}
		if !strings.Contains(m.Advice, "waits on it rather than failing") {
			t.Errorf("the advice does not say why this is worse than an error: %q", m.Advice)
		}
	})

	// Without an id a client cannot correlate its answers, so a call needing
	// two requests has no correct retry and one needing a single request is a
	// guess that happens to work.
	t.Run("a request with no id", func(t *testing.T) {
		f := newFakeServer(t)
		f.q.mrtrNoID = true
		_, fs := run(t, f, ccCreds(), nil)
		m, ok := fs["protocol.mrtr"]
		if !ok {
			t.Fatal("protocol.mrtr is missing")
		}
		if m.Status != Fail || m.Severity != Major {
			t.Fatalf("status = %s %s: %s", m.Status, m.Severity, m.Detail)
		}
		for _, want := range []string{"elicitation/create with no id", "cannot be answered"} {
			if !strings.Contains(m.Detail, want) {
				t.Errorf("detail %q does not contain %q", m.Detail, want)
			}
		}
	})

	// A server that never asks for input is the ordinary case, and the check
	// has to be named and skipped rather than absent — a report that silently
	// contained one fewer check would read as a better result.
	t.Run("a server that never asks", func(t *testing.T) {
		f := newFakeServer(t)
		_, fs := run(t, f, ccCreds(), nil)
		m, ok := fs["protocol.mrtr"]
		if !ok {
			t.Fatal("protocol.mrtr is missing; it should be skipped, not absent")
		}
		if m.Status != Skip {
			t.Errorf("status = %s: %s", m.Status, m.Detail)
		}
		if !strings.Contains(m.Detail, "cannot make a server ask") && !strings.Contains(m.Detail, "no call asked") {
			t.Errorf("the skip does not say why nothing was judged: %s", m.Detail)
		}
	})
}

// A liveness call is the one request that cannot have a conversation attached
// to it, so this stays a failure — but the message has to say that rather than
// leaking a transport error string.
func TestLivenessThatNeedsInputSaysWhy(t *testing.T) {
	f := newFakeServer(t)
	f.q.pingNeedsInput = true
	_, fs := run(t, f, ccCreds(), nil)

	p, ok := fs["protocol.ping"]
	if !ok {
		t.Fatal("protocol.ping is missing")
	}
	if p.Status != Fail {
		t.Fatalf("a liveness call that needs input passed: %s", p.Detail)
	}
	// The detail has to name the mechanism rather than leak a transport
	// error string, and the advice has to say why a liveness call is the one
	// request that cannot have a conversation attached.
	for _, want := range []string{"answered input_required", "elicitation/create"} {
		if !strings.Contains(p.Detail, want) {
			t.Errorf("detail %q does not contain %q", p.Detail, want)
		}
	}
	if strings.Contains(p.Detail, "transport:") {
		t.Errorf("the detail leaks a transport error string: %q", p.Detail)
	}
	if !strings.Contains(p.Advice, "nobody is there to have") {
		t.Errorf("the advice does not explain why: %q", p.Advice)
	}
	// And the observation still reaches protocol.mrtr, so the requests are
	// judged even when the interrupted call was not a tool.
	m, ok := fs["protocol.mrtr"]
	if !ok || m.Status == Skip {
		t.Errorf("protocol.mrtr = %+v, want it to have judged the liveness request", m)
	}
}

// The predicate that decides whether a call was interrupted rather than
// broken. Everything above hangs off it, and it is cheaper to state its
// contract here than to reach every branch through a server.
func TestAsInputRequired(t *testing.T) {
	ir := &transport.ErrInputRequired{
		Method: "tools/call search",
		Result: transport.InputRequiredResult{
			ResultType: transport.ResultInputRequired,
			InputRequests: []transport.InputRequest{
				{ID: "a", Method: "elicitation/create"},
				{ID: "b", Method: "sampling/createMessage"},
				{ID: "c", Method: " "},
			},
		},
	}
	got, ok := asInputRequired(ir)
	if !ok || got != ir {
		t.Fatalf("asInputRequired did not recognise its own type: %v, %v", got, ok)
	}
	if _, ok := asInputRequired(nil); ok {
		t.Error("a nil error was read as a request for input")
	}
	if _, ok := asInputRequired(errNotInput{}); ok {
		t.Error("an unrelated error was read as a request for input")
	}
	// Sorted, deduplicated, and a blank method dropped rather than reported
	// as an empty name.
	want := "elicitation/create sampling/createMessage"
	if got := strings.Join(requestedMethods(ir), " "); got != want {
		t.Errorf("requestedMethods = %q, want %q", got, want)
	}
}

type errNotInput struct{}

func (errNotInput) Error() string { return "something else" }

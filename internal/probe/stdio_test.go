// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/internal/telemetry"
)

// The fake stdio server is this test binary, re-executed.
//
// A shell script can fake a pipe well enough for the transport tests, which
// only need a line in and a line out. A phase run needs a real handshake, a
// catalog and a tool that answers — and a shell script that does all that in
// sed is a fixture nobody can read or change. Re-executing the test binary
// gives a server written in Go, in this file, with no build step and nothing
// to keep in sync.
const fakeEnv = "SCOUT_PROBE_FAKE_STDIO"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeEnv); mode != "" {
		fakeStdioServer(mode)
		return
	}
	os.Exit(m.Run())
}

// fakeStdioServer speaks just enough MCP to get a run through every phase.
//
// mode switches on the misbehaviours the stdio checks look for, one per
// branch, the same way fakeServer's quirks do over HTTP.
func fakeStdioServer(mode string) {
	switch mode {
	case "exit-immediately":
		fmt.Fprintln(os.Stderr, "fatal: MCP_FIXTURE_TOKEN is not set")
		os.Exit(2)
	case "noise":
		fmt.Println("Listening on stdio...")
	case "orphan":
		// Starts a worker and lets it outlive the session. The server
		// itself behaves impeccably, which is the point: every other
		// check passes and the worker is still there afterwards.
		w := exec.Command(os.Args[0]) //nolint:gosec // this binary, re-executed
		w.Env = []string{fakeEnv + "=worker"}
		_ = w.Start()
	case "worker":
		time.Sleep(30 * time.Second)
		return
	case "ignores-stdin":
		// Serves normally, then refuses to notice that its input has gone,
		// and ignores the polite signal too.
		signal.Ignore(syscall.SIGTERM)
	}
	out := os.Stdout
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	fmt.Fprintln(os.Stderr, "fixture: started")
	for {
		line, err := in.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if mode == "ignores-stdin" {
				// The whole misbehaviour: stdin is gone and this keeps
				// running anyway, the way a host finds out at the third
				// session that it has three servers.
				//
				// A sleep rather than a bare block: `select {}` parks the
				// only goroutine, the runtime calls that a deadlock and
				// panics, and the fixture exits — which is the opposite of
				// what it is here to do.
				time.Sleep(10 * time.Minute)
				return
			}
			return
		}
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			// A server that ignores what it cannot parse, which is what most
			// of them do and what protocol.malformed_json reports.
			continue
		}
		if req.ID == nil {
			continue // a notification
		}
		if mode == "die-on-tool-call" && req.Method == "tools/call" {
			fmt.Fprintln(os.Stderr, "panic: nil map write in tools/call")
			os.Exit(1)
		}
		var result string
		switch req.Method {
		case "initialize":
			result = `{"protocolVersion":"2025-11-25","serverInfo":{"name":"fixture","version":"1.0.0"},"capabilities":{"tools":{}},"instructions":"A fixture."}`
		case "ping":
			result = `{}`
		case "tools/list":
			result = `{"tools":[{"name":"look","description":"Look something up by its identifier and return what is stored.","annotations":{"readOnlyHint":true},"inputSchema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}]}`
		case "tools/call":
			// A well-behaved server refuses a tool it does not have, which
			// is what protocol.unknown_tool asks about. A fixture that
			// answered anything would make that check pass over HTTP and
			// fail here for a reason that is the fixture's.
			var call struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &call)
			if call.Name != "look" {
				fmt.Fprintf(out, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32602,"message":"no such tool: %s"}}`+"\n", *req.ID, call.Name)
				continue
			}
			result = `{"content":[{"type":"text","text":"ok"}]}`
		case "resources/list":
			result = `{"resources":[]}`
		case "resources/templates/list":
			result = `{"resourceTemplates":[]}`
		case "prompts/list":
			result = `{"prompts":[]}`
		default:
			fmt.Fprintf(out, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32601,"message":"no such method"}}`+"\n", *req.ID)
			continue
		}
		fmt.Fprintf(out, `{"jsonrpc":"2.0","id":%d,"result":%s}`+"\n", *req.ID, result)
	}
}

// runStdioFixture runs the phases against the fixture in the given mode.
func runStdioFixture(t *testing.T, mode string, only ...string) (*Session, map[string]Finding) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{
		Stdio: &scout.StdioConfig{
			Command: self,
			// An explicit environment, so the fixture gets the switch and
			// nothing else — and so this test also demonstrates that a
			// server is handed what the operator named rather than whatever
			// the test runner happened to export.
			Env: []string{fakeEnv + "=" + mode},
		},
		Recorder: telemetry.New(), Version: "t", RPS: -1, Samples: 2, Concurrency: 2,
		CallTimeout: 5 * time.Second,
	}
	if len(only) > 0 {
		o.Only = only
	}
	s, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return s, findingsByID(s)
}

// TestStdioRunReachesEveryPhase is the end-to-end assertion: a program, not
// a URL, and a full report at the other end.
func TestStdioRunReachesEveryPhase(t *testing.T) {
	s, fs := runStdioFixture(t, "serve")

	expect(t, fs, "stdio.process", Pass, "pid ")
	expect(t, fs, "handshake.initialize", Pass, "fixture 1.0.0")
	expect(t, fs, "catalog.tools.list", Pass, "1 tool")
	expect(t, fs, "stdio.alive", Pass, "still running")
	expect(t, fs, "stdio.stdout_clean", Pass, "no stray output")

	// stderr is reported rather than judged: logging there is correct.
	if f := fs["stdio.stderr"]; f.Status != Info || !strings.Contains(strings.Join(f.Evidence, " "), "fixture: started") {
		t.Errorf("stdio.stderr = %s %q evidence %v", f.Status, f.Detail, f.Evidence)
	}

	// The phases that are about HTTP are skipped with a reason, not absent.
	for _, name := range []string{"discovery", "auth"} {
		var found bool
		for _, pr := range s.Results {
			if pr.Name != name {
				continue
			}
			found = true
			if pr.Status != Skip {
				t.Errorf("%s ran over stdio with status %s", name, pr.Status)
			}
			if pr.Skipped == "" {
				t.Errorf("%s was skipped with no reason, which is the thing this must never do", name)
			}
		}
		if !found {
			t.Errorf("%s is missing from the report entirely; a skipped phase still has to appear", name)
		}
	}
}

// TestStdioNamesTheChecksItCannotMake is the honesty gate for this
// transport.
//
// A stdio run contains fewer checks than an HTTP one, and a report that
// simply contained fewer would read as a better result. Every conformance
// probe that has no form over a pipe is present, skipped, and says why.
func TestStdioNamesTheChecksItCannotMake(t *testing.T) {
	_, fs := runStdioFixture(t, "serve", "net", "handshake", "protocol")
	for _, id := range []string{
		"protocol.accept_header", "protocol.get_stream",
		"protocol.bogus_session", "protocol.version_header",
		"handshake.session",
	} {
		f, ok := fs[id]
		if !ok {
			t.Errorf("%s is absent from a stdio report; it must be present and skipped", id)
			continue
		}
		if f.Status != Skip {
			t.Errorf("%s = %s over stdio, want skip", id, f.Status)
		}
		if len(f.Detail) < 20 {
			t.Errorf("%s was skipped with %q, which does not explain anything", id, f.Detail)
		}
	}
}

// TestStdioRunsTheProbesThatAreNotAboutHTTP: the other half of the same
// contract. An unknown method and a mismatched id are JSON-RPC questions,
// not HTTP ones, so skipping them over a pipe would be laziness rather than
// honesty.
func TestStdioRunsTheProbesThatAreNotAboutHTTP(t *testing.T) {
	_, fs := runStdioFixture(t, "serve", "net", "handshake", "protocol")
	expect(t, fs, "protocol.unknown_method", Pass, "-32601")
	expect(t, fs, "protocol.id_echo", Pass, "id echoed")
	expect(t, fs, "protocol.unknown_tool", Pass, "")

	// The fixture ignores what it cannot parse, which is a real finding
	// rather than a probe that could not run.
	expect(t, fs, "protocol.malformed_json", Fail, "no answer to a truncated message")
}

// TestStdioFindingsCiteEvidence: a stdio finding has to be as traceable as
// an HTTP one, or the transport reads as less rigorous than it is.
func TestStdioFindingsCiteEvidence(t *testing.T) {
	s, fs := runStdioFixture(t, "serve", "net", "handshake")
	if f := fs["handshake.initialize"]; len(f.Evidence) == 0 {
		t.Error("the handshake cites no request; the pipe is not being recorded")
	}
	if n := s.Opts.Recorder.Count(); n == 0 {
		t.Error("no telemetry recorded for a stdio run")
	}
	for _, e := range s.Opts.Recorder.Events() {
		if e.Method != telemetry.PipeMethod {
			t.Errorf("a stdio run recorded a %q event", e.Method)
		}
		if e.RPC == nil || e.RPC.Method == "" {
			t.Errorf("event %d records no JSON-RPC method", e.Seq)
		}
	}
}

// TestStdioServerThatExitsAtStartup: the common failure. A missing
// environment variable, an exec that is not there, a bad argument — all of
// them present as a process that is gone, and the only explanation is on
// stderr.
func TestStdioServerThatExitsAtStartup(t *testing.T) {
	s, fs := runStdioFixture(t, "exit-immediately")
	f, ok := fs["stdio.process"]
	if !ok || f.Status != Fail {
		t.Fatalf("stdio.process = %s %q, want fail", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "MCP_FIXTURE_TOKEN") {
		t.Errorf("the finding does not carry what the server said: %q", f.Detail)
	}
	if s.Blocked() == "" {
		t.Error("the run continued as though there were a server to ask")
	}
}

// TestStdioServerThatDiesMidRun is the difference between a failed call and
// a failed session, and a host cannot tell them apart from the outside.
func TestStdioServerThatDiesMidRun(t *testing.T) {
	_, fs := runStdioFixture(t, "die-on-tool-call")
	f, ok := fs["stdio.alive"]
	if !ok || f.Status != Fail {
		t.Fatalf("stdio.alive = %s %q, want fail", f.Status, f.Detail)
	}
	if !strings.Contains(strings.Join(f.Evidence, " "), "nil map write") {
		t.Errorf("the finding does not carry the server's own explanation: %v", f.Evidence)
	}
}

// TestStdioStrayOutputIsFatalAndNamed.
//
// One print statement on stdout corrupts the framing, and the symptom a
// host reports — a hang, or a parse error about a line nobody wrote — never
// names the cause. So the report names it.
func TestStdioStrayOutputIsFatalAndNamed(t *testing.T) {
	_, fs := runStdioFixture(t, "noise")
	f, ok := fs["stdio.stdout_clean"]
	if !ok || f.Status != Fail {
		t.Fatalf("stdio.stdout_clean = %s %q, want fail", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "Listening on stdio") {
		t.Errorf("the finding does not quote the offending line: %q", f.Detail)
	}
}

// TestStdioAndEndpointAreExclusive: a run has one target, and picking one
// silently would produce a report about a server nobody named.
func TestStdioAndEndpointAreExclusive(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Endpoint: "https://x/mcp",
		Stdio:    &scout.StdioConfig{Command: "/bin/true"},
	})
	if err == nil {
		t.Fatal("a run with both a URL and a command was accepted")
	}
}

// TestFindingDetailsAreOneLine: a server's own error text ends up in a
// detail, and some of it arrives pretty-printed. Every rendering scout has
// puts a detail on one line.
func TestFindingDetailsAreOneLine(t *testing.T) {
	_, fs := runStdioFixture(t, "serve")
	for id, f := range fs {
		if strings.ContainsAny(f.Detail, "\n\r\t") {
			t.Errorf("%s: detail spans lines: %q", id, f.Detail)
		}
	}
}

// TestStdioCleanExitAndNoZombie is the ordinary case: a server that stops
// when its input closes and takes everything it started with it.
func TestStdioCleanExitAndNoZombie(t *testing.T) {
	_, fs := runStdioFixture(t, "serve")

	expect(t, fs, "stdio.clean_exit", Pass, "exited on its own")
	expect(t, fs, "stdio.no_zombie", Pass, "process group was empty")
}

// TestStdioSeesAWorkerThatOutlivedTheServer is the fixture the roadmap
// names. Everything else about this server is impeccable — it handshakes,
// it serves a catalog, it exits the moment stdin closes — and it leaves a
// worker holding whatever it was given. No other check in the report
// notices, because no other diagnostic owns the process.
func TestStdioSeesAWorkerThatOutlivedTheServer(t *testing.T) {
	_, fs := runStdioFixture(t, "orphan")

	// The server itself did everything right, which is the point.
	expect(t, fs, "stdio.alive", Pass, "still running")
	expect(t, fs, "stdio.clean_exit", Pass, "exited on its own")

	f := fs["stdio.no_zombie"]
	if f.Status != Fail {
		t.Fatalf("a worker that outlived the server was not reported: %+v", f)
	}
	if f.Severity != Major {
		t.Errorf("want Major, got %v", f.Severity)
	}
	if !strings.Contains(f.Detail, "process group") {
		t.Errorf("the finding does not say what was seen: %q", f.Detail)
	}
	if f.Advice == "" {
		t.Error("a failing check has to say what to do about it")
	}
}

// TestStdioFailsAServerThatIgnoresItsInputClosing: closing stdin is how a
// host ends a session, and a server that carries on is one that
// accumulates, a process per session, until something runs out.
func TestStdioFailsAServerThatIgnoresItsInputClosing(t *testing.T) {
	_, fs := runStdioFixture(t, "ignores-stdin")

	f := fs["stdio.clean_exit"]
	if f.Status != Fail {
		t.Fatalf("a server that ignored stdin closing was not failed: %+v", f)
	}
	if !strings.Contains(f.Detail, "SIGKILL") {
		t.Errorf("the finding does not say what it took to stop it: %q", f.Detail)
	}

	// Its group was killed to stop it, so what it left behind cannot be
	// told apart from what the signal ended. Saying nothing is correct;
	// claiming it was clean would not be.
	if z := fs["stdio.no_zombie"]; z.Status != Skip {
		t.Errorf("after a forced kill the orphan question is unanswerable, want skip, got %s %q", z.Status, z.Detail)
	}
}

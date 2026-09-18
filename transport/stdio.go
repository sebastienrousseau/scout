// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Most MCP servers in the wild are not endpoints. They are programs a host
// starts, talks to over a pipe, and is responsible for stopping — and that
// last part is the whole difference. An HTTP transport can walk away from a
// server that misbehaves. A stdio transport owns a process, and a tool that
// leaves one running has done harm no report can undo.
//
// Everything unusual in this file follows from custody: the lifecycle is
// explicit, every exit path goes through the same Close, stderr is kept
// because a server that dies says why there and nowhere else, and a line
// longer than the cap ends the connection rather than growing a buffer
// until the machine notices.

// Errors a stdio connection reports.
var (
	// ErrProcessExited means the server is gone. Whatever it was asked
	// cannot be answered, and retrying will not change that.
	ErrProcessExited = errors.New("transport: server process exited")
	// ErrLineTooLong means a single JSON-RPC message exceeded MaxLine. It
	// ends the connection: a peer that sends an unbounded line is either
	// broken or hostile, and the only safe reading of both is to stop.
	ErrLineTooLong = errors.New("transport: message exceeded the line limit")
)

// DefaultMaxLine bounds one JSON-RPC message. Large enough for a catalog of
// a thousand tools with full schemas; small enough that a server streaming
// an endless line is stopped in under a second rather than after the
// machine starts swapping.
const DefaultMaxLine = 16 << 20 // 16 MiB

// DefaultStderrCap bounds retained stderr. A server that writes a stack
// trace is why this exists; a server that writes a log line per request is
// why it is bounded.
const DefaultStderrCap = 64 << 10 // 64 KiB

// DefaultShutdownGrace is how long a server is given to exit after its
// stdin closes, before it is killed.
const DefaultShutdownGrace = 5 * time.Second

// StdioConfig describes a server to start.
type StdioConfig struct {
	// Command is the program, and Args its arguments. Command is used as
	// given: this is not a shell, so no expansion, quoting or globbing
	// happens, and a command containing a pipe is a command with a pipe in
	// its name.
	Command string
	Args    []string
	// Dir is the working directory; empty means the caller's.
	Dir string
	// Env replaces the environment entirely. A nil Env does NOT mean the
	// caller's environment: it means BaseEnv plus whatever PassEnv names.
	//
	// This is a deliberate departure from os/exec, where nil inherits
	// everything, and it is the whole point. scout starts a program the
	// operator named in order to find out what it does; handing it every
	// variable that happens to be exported — the cloud credentials, the
	// tokens for three other services, the CI secrets — is how a
	// credential reaches a program nobody audited. That is the same shape
	// as the exfiltration path this project already found once, and the
	// answer is the same: construct what is passed, do not sanitise what
	// was inherited.
	Env []string
	// PassEnv names variables to forward from the caller's environment.
	// A server that genuinely needs GITHUB_TOKEN is ordinary; forwarding
	// it by name is how the operator says so out loud.
	PassEnv []string
	// MaxLine bounds one message; zero means DefaultMaxLine.
	MaxLine int
	// StderrCap bounds retained stderr; zero means DefaultStderrCap.
	StderrCap int
	// ShutdownGrace is how long the server has to exit after stdin closes;
	// zero means DefaultShutdownGrace.
	ShutdownGrace time.Duration
}

// BaseEnv is what a server gets when the caller names nothing.
//
// A program needs to be able to find its interpreter and its libraries, and
// an empty environment breaks almost everything for no security gain — PATH
// is not a secret. Everything that might be one is left out.
var BaseEnv = []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "SystemRoot", "COMSPEC", "PATHEXT"}

// environment builds what the child will see.
func (c StdioConfig) environment() []string {
	if c.Env != nil {
		// An explicit Env is exactly what the caller asked for, including
		// an explicitly empty one.
		return append([]string{}, c.Env...)
	}
	names := append(append([]string{}, BaseEnv...), c.PassEnv...)
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		if v, ok := os.LookupEnv(n); ok {
			out = append(out, n+"="+v)
		}
	}
	// A non-nil empty slice, so os/exec does not fall back to inheriting
	// the caller's environment when nothing matched.
	return out
}

// Stdio is a JSON-RPC connection to a server running as a child process,
// speaking newline-delimited JSON over stdin and stdout.
type Stdio struct {
	cfg StdioConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	protocolVersion atomic.Value // string
	sessionID       atomic.Value // string
	nextID          atomic.Int64
	dialect         atomic.Pointer[dialectBox]

	// writeMu serialises writes. Two goroutines interleaving halves of two
	// JSON lines produces a stream neither peer can parse, and the failure
	// looks like a protocol bug rather than a concurrency one.
	writeMu sync.Mutex
	// callMu serialises whole exchanges. The framing has no way to match a
	// response to a request other than by id, and reading another call's
	// reply is worse than waiting for it.
	callMu sync.Mutex

	stderr   *ringBuffer
	waitOnce sync.Once
	waitErr  error
	done     chan struct{}
	closed   atomic.Bool
}

// StartStdio starts the server and returns a connection to it.
//
// The process is running when this returns. A caller that gets an error
// has no process to clean up; a caller that does not must Close.
func StartStdio(ctx context.Context, cfg StdioConfig) (*Stdio, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, errors.New("transport: stdio needs a command")
	}
	if cfg.MaxLine <= 0 {
		cfg.MaxLine = DefaultMaxLine
	}
	if cfg.StderrCap <= 0 {
		cfg.StderrCap = DefaultStderrCap
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = DefaultShutdownGrace
	}

	// Deliberately not exec.CommandContext, which noctx would prefer.
	//
	// CommandContext kills the child when ctx is cancelled. Two things make
	// that wrong here. The connection outlives this call by design, so the
	// context that started it is often a short-lived setup context whose
	// cancellation must not take the server with it. And on the paths where
	// a cancellation should end the process, Close already does it —
	// politely first, by closing stdin, then by force. Having os/exec kill
	// it as well means two owners of one lifetime and a race between them.
	//
	//nolint:noctx // lifetime is owned by Close; see above
	cmd := exec.Command(cfg.Command, cfg.Args...) // #nosec G204 -- the command is the operator's own argument; that is the feature
	cmd.Dir = cfg.Dir
	cmd.Env = cfg.environment()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("transport: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("transport: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("transport: stderr pipe: %w", err)
	}

	s := &Stdio{
		cfg:    cfg,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdout, 64<<10),
		stderr: newRingBuffer(cfg.StderrCap),
		done:   make(chan struct{}),
	}
	s.protocolVersion.Store("")
	s.sessionID.Store("")
	s.dialect.Store(&dialectBox{d: &Sessioned{}})

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("transport: start %s: %w", cfg.Command, err)
	}

	// stderr is drained continuously. A server whose stderr fills the pipe
	// buffer blocks on write and stops answering, which presents as a hang
	// with no explanation anywhere.
	go func() { _, _ = io.Copy(s.stderr, stderrPipe) }()

	// One goroutine owns Wait, so the exit status is available to every
	// caller and reaped exactly once.
	go func() {
		s.waitOnce.Do(func() { s.waitErr = cmd.Wait() })
		close(s.done)
	}()

	// A context already cancelled at Start means the caller has given up,
	// and leaving the process behind would be the one outcome this file
	// exists to prevent.
	if ctx != nil {
		select {
		case <-ctx.Done():
			_ = s.Close()
			return nil, ctx.Err()
		default:
		}
	}
	return s, nil
}

// Dialect returns the binding in use.
func (s *Stdio) Dialect() Dialect { return s.dialect.Load().d }

// SetDialect replaces the binding.
func (s *Stdio) SetDialect(d Dialect) {
	if d == nil {
		return
	}
	s.dialect.Store(&dialectBox{d: d})
	if !d.Stateful() {
		s.sessionID.Store("")
	}
}

// SessionID returns the session identifier, which stdio never has: the
// connection is the session. It exists so a caller can treat the two
// transports alike.
func (s *Stdio) SessionID() string { return s.sessionID.Load().(string) }

// SetSessionID records a session identifier. It affects nothing on a pipe
// and is kept only so a stateful dialect behaves identically on both
// transports.
func (s *Stdio) SetSessionID(id string) { s.sessionID.Store(id) }

// ProtocolVersion returns the negotiated version.
func (s *Stdio) ProtocolVersion() string { return s.protocolVersion.Load().(string) }

// SetProtocolVersion records the negotiated version.
func (s *Stdio) SetProtocolVersion(v string) { s.protocolVersion.Store(v) }

// Reset drops per-session state.
//
// On HTTP this abandons a session and the next call starts a new one. A
// process has no equivalent — the session is the pipe — so this clears what
// it can and does not pretend to have restarted anything. A caller that
// wants a fresh server closes this one and starts another.
func (s *Stdio) Reset() {
	s.sessionID.Store("")
	s.protocolVersion.Store("")
}

// Stderr returns what the server has written to stderr, capped.
//
// This is not decoration. A stdio server that fails to start, crashes, or
// refuses its arguments says so on stderr and nowhere else; without this
// the diagnostic is "the process exited", which tells an operator nothing
// they can act on.
func (s *Stdio) Stderr() string { return s.stderr.String() }

// Exited reports whether the process has finished, and its error if so.
func (s *Stdio) Exited() (bool, error) {
	select {
	case <-s.done:
		return true, s.waitErr
	default:
		return false, nil
	}
}

// PID returns the server's process id, or 0 before it starts. It exists so
// a caller — or a test — can verify the process is gone rather than trust
// that it is.
func (s *Stdio) PID() int {
	if s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// NextID returns the next request id, for a caller building a raw message.
func (s *Stdio) NextID() int64 { return s.nextID.Add(1) }

// Call sends a request and waits for the matching response.
func (s *Stdio) Call(ctx context.Context, method string, params any, result any) error {
	id := s.nextID.Add(1)
	req, err := buildRPC(&id, method, params)
	if err != nil {
		return err
	}
	resp, err := s.roundTrip(ctx, req)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("%w: %s (id %d)", ErrNoResponse, method, id)
	}
	if resp.Error != nil {
		return AsProtocolError(s.Dialect().Version(), resp.Error)
	}
	if ir, ok := AsInputRequired(method, resp.Result); ok {
		return ir
	}
	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("transport: decode %s result: %w", method, err)
		}
	}
	return nil
}

// Notify sends a notification, which expects no response.
func (s *Stdio) Notify(ctx context.Context, method string, params any) error {
	req, err := buildRPC(nil, method, params)
	if err != nil {
		return err
	}
	if err := s.Dialect().PrepareBody(req); err != nil {
		return err
	}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return s.writeLine(ctx, line)
}

// roundTrip writes one request and reads until the matching response.
func (s *Stdio) roundTrip(ctx context.Context, req *Request) (*Response, error) {
	if err := s.Dialect().PrepareBody(req); err != nil {
		return nil, err
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	s.callMu.Lock()
	defer s.callMu.Unlock()

	if err := s.writeLine(ctx, line); err != nil {
		return nil, err
	}

	type result struct {
		resp *Response
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := s.readUntil(req.ID)
		ch <- result{resp, err}
	}()

	select {
	case r := <-ch:
		return r.resp, r.err
	case <-ctx.Done():
		// The read goroutine is blocked on a pipe that only the process
		// closing will release. Ending the process is the only way to free
		// it, and a caller who has cancelled wants the process gone anyway.
		_ = s.Close()
		return nil, ctx.Err()
	case <-s.done:
		// Drain a response that arrived just before exit rather than
		// reporting a crash for a call that was in fact answered.
		select {
		case r := <-ch:
			if r.resp != nil || r.err != nil {
				return r.resp, r.err
			}
		case <-time.After(100 * time.Millisecond):
		}
		return nil, s.exitError()
	}
}

// readUntil reads messages until the one answering id, skipping anything
// else the server sends.
//
// A server may interleave notifications and server-initiated requests with
// responses; the framing offers no other way to tell them apart, so
// anything that is not this id is read past. It is not discarded silently
// in spirit — a caller that needs those messages needs a different API —
// but it must not be mistaken for the answer.
func (s *Stdio) readUntil(id *int64) (*Response, error) {
	for {
		line, err := s.readLine()
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			continue
		}
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("transport: server wrote a line that is not JSON: %w", err)
		}
		if resp.ID == nil {
			// A notification or a request from the server. Not the answer.
			continue
		}
		if id != nil && *resp.ID != *id {
			continue
		}
		return &resp, nil
	}
}

// readLine reads one newline-terminated message, bounded by MaxLine.
func (s *Stdio) readLine() ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := s.stdout.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, s.exitError()
			}
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > s.cfg.MaxLine {
			// Ending the connection is the point. A peer sending an
			// unbounded line is broken or hostile, and continuing to read
			// is how a diagnostic becomes the outage.
			_ = s.Close()
			return nil, fmt.Errorf("%w: %d bytes without a newline", ErrLineTooLong, len(buf))
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

// writeLine sends one message.
func (s *Stdio) writeLine(ctx context.Context, line []byte) error {
	if s.closed.Load() {
		return ErrProcessExited
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		// A write to a server that has just died fails with EPIPE, and the
		// reaping goroutine may not have finished yet — so asking Exited
		// immediately can answer "still running" about a process that is
		// already gone, and the caller gets "broken pipe" instead of the
		// exit status and the stderr that explain it. Wait briefly for the
		// reap before deciding; a genuinely live server never gets here.
		select {
		case <-s.done:
			return s.exitError()
		case <-time.After(exitReapGrace):
		}
		return fmt.Errorf("transport: write to server: %w", err)
	}
	return nil
}

// exitReapGrace is how long a failed write waits for the process to be
// reaped before concluding the failure was something other than an exit.
// Long enough for Wait to return on a process that has already died, short
// enough not to be felt.
const exitReapGrace = 2 * time.Second

// exitError explains a dead process, with what it said on the way out.
func (s *Stdio) exitError() error {
	<-s.done
	msg := strings.TrimSpace(s.stderr.String())
	switch {
	case s.waitErr != nil && msg != "":
		// Both wrapped, so a caller can test for ErrProcessExited and still
		// reach the *exec.ExitError underneath for the exit status.
		return fmt.Errorf("%w: %w; stderr: %s", ErrProcessExited, s.waitErr, truncateForError(msg))
	case s.waitErr != nil:
		return fmt.Errorf("%w: %w", ErrProcessExited, s.waitErr)
	case msg != "":
		return fmt.Errorf("%w; stderr: %s", ErrProcessExited, truncateForError(msg))
	}
	return ErrProcessExited
}

func truncateForError(s string) string {
	const limit = 400
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

// Close ends the server and waits for it.
//
// Politely first: closing stdin is how a well-behaved MCP server is told to
// stop, and most exit on their own. One that does not is killed after the
// grace period. Close always returns having reaped the process — the one
// outcome this must never produce is a caller who thinks the server is gone
// while it is still running.
func (s *Stdio) Close() error {
	if s.closed.Swap(true) {
		<-s.done
		return nil
	}
	_ = s.stdin.Close()

	select {
	case <-s.done:
		return nil
	case <-time.After(s.cfg.ShutdownGrace):
	}

	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.done
	return nil
}

// Signal sends sig to the server, for a caller testing how it handles one.
func (s *Stdio) Signal(sig os.Signal) error {
	if s.cmd.Process == nil {
		return ErrProcessExited
	}
	return s.cmd.Process.Signal(sig)
}

// buildRPC assembles a request. Shared with the HTTP transport's own
// builder in everything but the receiver.
func buildRPC(id *int64, method string, params any) (*Request, error) {
	r := &Request{JSONRPC: "2.0", ID: id, Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("transport: encode params: %w", err)
		}
		r.Params = b
	}
	return r, nil
}

// ringBuffer keeps the last n bytes written to it.
//
// Last rather than first: a server that logs steadily and then dies pushes
// the interesting part to the end, and keeping the first 64 KiB of a chatty
// server's startup chatter would discard exactly the lines that explain the
// exit.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	n    int
	full bool
}

func newRingBuffer(n int) *ringBuffer { return &ringBuffer{buf: make([]byte, n)} }

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := len(p)
	if len(p) >= len(r.buf) {
		copy(r.buf, p[len(p)-len(r.buf):])
		r.n = 0
		r.full = true
		return total, nil
	}
	for _, b := range p {
		r.buf[r.n] = b
		r.n++
		if r.n == len(r.buf) {
			r.n = 0
			r.full = true
		}
	}
	return total, nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return string(r.buf[:r.n])
	}
	return string(r.buf[r.n:]) + string(r.buf[:r.n])
}

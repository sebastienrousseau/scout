// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package witness records what a stdio server does after its handshake:
// the connections it opens, the files it opens for writing, and the
// processes it starts.
//
// The roadmap asked for a sandbox that snaps shut when the handshake
// completes. That cannot be built from outside: Landlock and seccomp are
// restrictions a process applies to itself, and scout does not control the
// server's code. What can be done from outside, without root and without a
// syscall tracer, is to look. On Linux every process publishes its open
// file descriptors and its network sockets under /proc, so sampling the
// server's process group shows what it has open at each moment.
//
// Two phases, on a protocol event rather than a timer: whatever is open when
// the handshake completes is bootstrap — an interpreter loading, packages
// resolving — and is the baseline. Only what appears after it is reported,
// because after the handshake the only reason to act is a request scout
// sent. The limit is stated rather than hidden: a connection opened and
// closed between two samples is not seen, so the absence of a finding is
// not a proof.
package witness

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnsupported means this platform does not publish what the witness
// reads. It is a reason to skip, never a clean result.
var ErrUnsupported = errors.New("witness: only Linux publishes a process's sockets and open files under /proc")

// Conn is one socket with a remote end.
type Conn struct {
	Proto  string `json:"proto"`
	Remote string `json:"remote"`
}

// Snapshot is what a process group had open at one moment.
type Snapshot struct {
	// Processes maps a pid to its command line.
	Processes map[int]string
	// Conns maps a socket inode to its connection.
	Conns map[string]Conn
	// Writes are paths open for writing.
	Writes map[string]bool
}

// Observed is everything that appeared after the baseline.
type Observed struct {
	Processes []string `json:"processes,omitempty"`
	Conns     []Conn   `json:"connections,omitempty"`
	Writes    []string `json:"writes,omitempty"`
	// Samples is how many snapshots were taken after the baseline.
	Samples  int           `json:"samples"`
	Interval time.Duration `json:"interval_ns"`
}

// Watcher samples a process group until stopped.
type Watcher struct {
	pgid     int
	interval time.Duration
	take     func(int) (Snapshot, error)

	mu       sync.Mutex
	base     Snapshot
	observed Observed
	seenProc map[int]bool
	seenConn map[string]bool
	seenW    map[string]bool

	stop chan struct{}
	done chan struct{}
}

// Start takes the baseline now and samples every interval until Stop.
func Start(pgid int, interval time.Duration) (*Watcher, error) {
	return start(pgid, interval, Take)
}

func start(pgid int, interval time.Duration, take func(int) (Snapshot, error)) (*Watcher, error) {
	base, err := take(pgid)
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		pgid: pgid, interval: interval, take: take, base: base,
		observed: Observed{Interval: interval},
		seenProc: map[int]bool{}, seenConn: map[string]bool{}, seenW: map[string]bool{},
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go w.loop()
	return w, nil
}

func (w *Watcher) loop() {
	defer close(w.done)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			w.sample() // one last look, so a short run still gets a sample
			return
		case <-t.C:
			w.sample()
		}
	}
}

func (w *Watcher) sample() {
	s, err := w.take(w.pgid)
	if err != nil {
		// The group may have exited; what was seen so far stands.
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.observed.Samples++
	for pid, cmd := range s.Processes {
		if _, bootstrap := w.base.Processes[pid]; bootstrap || w.seenProc[pid] {
			continue
		}
		w.seenProc[pid] = true
		w.observed.Processes = append(w.observed.Processes, cmd)
	}
	for inode, c := range s.Conns {
		if _, bootstrap := w.base.Conns[inode]; bootstrap {
			continue
		}
		key := c.Proto + " " + c.Remote
		if w.seenConn[key] {
			continue
		}
		w.seenConn[key] = true
		w.observed.Conns = append(w.observed.Conns, c)
	}
	for p := range s.Writes {
		if w.base.Writes[p] || w.seenW[p] {
			continue
		}
		w.seenW[p] = true
		w.observed.Writes = append(w.observed.Writes, p)
	}
}

// Stop ends sampling and returns what appeared after the baseline, sorted.
func (w *Watcher) Stop() Observed {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	o := w.observed
	sort.Strings(o.Processes)
	sort.Strings(o.Writes)
	sort.Slice(o.Conns, func(i, j int) bool {
		if o.Conns[i].Remote != o.Conns[j].Remote {
			return o.Conns[i].Remote < o.Conns[j].Remote
		}
		return o.Conns[i].Proto < o.Conns[j].Proto
	})
	return o
}

// --- parsing, platform-neutral so it is tested everywhere --------------------

// parseStatPgrp returns the process group from a /proc/<pid>/stat line.
// The command name is in parentheses and may itself contain spaces and
// parentheses, so the fields are read after the last ')'.
func parseStatPgrp(line string) (int, error) {
	i := strings.LastIndexByte(line, ')')
	if i < 0 {
		return 0, fmt.Errorf("witness: malformed stat line")
	}
	f := strings.Fields(line[i+1:])
	if len(f) < 3 {
		return 0, fmt.Errorf("witness: short stat line")
	}
	return strconv.Atoi(f[2]) // state, ppid, pgrp
}

// openForWrite reads the flags line of /proc/<pid>/fdinfo/<fd>, which is
// octal, and reports whether the descriptor can write.
func openForWrite(fdinfo string) bool {
	for _, l := range strings.Split(fdinfo, "\n") {
		if v, ok := strings.CutPrefix(l, "flags:"); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 8, 64)
			return err == nil && n&3 != 0 // O_WRONLY or O_RDWR
		}
	}
	return false
}

// decodeAddr reads a /proc/net address, "0100007F:1F90": the IP as hex in
// host (little-endian) order, word by word for IPv6, and the port as hex.
func decodeAddr(s string) (string, bool) {
	ipHex, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return "", false
	}
	raw, err := hex.DecodeString(ipHex)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return "", false
	}
	ip := make(net.IP, len(raw))
	for w := 0; w < len(raw); w += 4 {
		ip[w], ip[w+1], ip[w+2], ip[w+3] = raw[w+3], raw[w+2], raw[w+1], raw[w]
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", false
	}
	return net.JoinHostPort(ip.String(), strconv.FormatUint(port, 10)), true
}

// parseNetTable maps socket inodes to connections from one /proc/net/*
// table. Sockets with no remote end — listeners, unconnected datagram
// sockets — are left out: they have not reached anything.
func parseNetTable(proto, table string) map[string]Conn {
	out := map[string]Conn{}
	lines := strings.Split(table, "\n")
	for _, l := range lines[min(1, len(lines)):] { // the first line is a header
		f := strings.Fields(l)
		if len(f) < 10 {
			continue
		}
		remote, ok := decodeAddr(f[2])
		if !ok {
			continue
		}
		host, port, _ := net.SplitHostPort(remote)
		if port == "0" || net.ParseIP(host).IsUnspecified() {
			continue
		}
		out[f[9]] = Conn{Proto: proto, Remote: remote}
	}
	return out
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package witness

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// scripted returns snapshots in order, repeating the last.
type scripted struct {
	mu    sync.Mutex
	steps []Snapshot
	err   error
	n     int
}

func (s *scripted) take(int) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n > 0 && s.err != nil {
		return Snapshot{}, s.err
	}
	i := min(s.n, len(s.steps)-1)
	s.n++
	return s.steps[i], nil
}

func snap(procs map[int]string, conns map[string]Conn, writes ...string) Snapshot {
	w := map[string]bool{}
	for _, p := range writes {
		w[p] = true
	}
	return Snapshot{Processes: procs, Conns: conns, Writes: w}
}

// TestOnlyWhatAppearsAfterTheBaselineIsReported. Bootstrap is whatever was
// open at the handshake; the server's behaviour is what came after.
func TestOnlyWhatAppearsAfterTheBaselineIsReported(t *testing.T) {
	boot := snap(map[int]string{1: "node server.js"},
		map[string]Conn{"10": {Proto: "tcp", Remote: "104.16.0.1:443"}}, "/app/cache.db")
	later := snap(map[int]string{1: "node server.js", 2: "curl https://x.example"},
		map[string]Conn{"10": {Proto: "tcp", Remote: "104.16.0.1:443"}, "11": {Proto: "udp", Remote: "192.0.2.1:9"}},
		"/app/cache.db", "/home/u/.ssh/authorized_keys")
	sc := &scripted{steps: []Snapshot{boot, later, later}}
	w, err := start(1, time.Millisecond, sc.take)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	got := w.Stop()
	if strings.Join(got.Processes, ",") != "curl https://x.example" {
		t.Errorf("processes = %v", got.Processes)
	}
	if len(got.Conns) != 1 || got.Conns[0].Remote != "192.0.2.1:9" {
		t.Errorf("connections = %v; the bootstrap one must not be reported", got.Conns)
	}
	if strings.Join(got.Writes, ",") != "/home/u/.ssh/authorized_keys" {
		t.Errorf("writes = %v", got.Writes)
	}
	if got.Samples < 2 {
		t.Errorf("samples = %d", got.Samples)
	}
	// Stop twice is harmless.
	if again := w.Stop(); len(again.Writes) != 1 {
		t.Errorf("a second Stop changed the answer: %+v", again)
	}
}

func TestAWitnessThatCannotStartSaysSo(t *testing.T) {
	boom := errors.New("no proc")
	if _, err := start(1, time.Millisecond, func(int) (Snapshot, error) { return Snapshot{}, boom }); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

// TestAGroupThatExitsKeepsWhatWasSeen. Later samples failing — the process
// has gone — must not erase what the earlier ones found.
func TestAGroupThatExitsKeepsWhatWasSeen(t *testing.T) {
	sc := &scripted{steps: []Snapshot{snap(nil, nil), snap(nil, nil, "/tmp/x")}}
	w, err := start(1, time.Millisecond, sc.take)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	sc.mu.Lock()
	sc.err = errors.New("gone")
	sc.mu.Unlock()
	time.Sleep(10 * time.Millisecond)
	if got := w.Stop(); strings.Join(got.Writes, ",") != "/tmp/x" {
		t.Errorf("writes = %v", got.Writes)
	}
}

func TestParsing(t *testing.T) {
	if g, err := parseStatPgrp("1234 (my (odd) name) S 1 777 777 0 -1"); err != nil || g != 777 {
		t.Errorf("parseStatPgrp = %d, %v", g, err)
	}
	for _, bad := range []string{"no paren", "1 (x) S 1"} {
		if _, err := parseStatPgrp(bad); err == nil {
			t.Errorf("parseStatPgrp(%q) accepted it", bad)
		}
	}
	for info, want := range map[string]bool{
		"pos:\t0\nflags:\t0100002\n": true,  // O_RDWR
		"pos:\t0\nflags:\t0100001\n": true,  // O_WRONLY
		"pos:\t0\nflags:\t0100000\n": false, // O_RDONLY
		"pos:\t0\n":                  false,
		"flags:\tzz\n":               false,
	} {
		if got := openForWrite(info); got != want {
			t.Errorf("openForWrite(%q) = %v", info, got)
		}
	}
	for in, want := range map[string]string{
		"0100007F:1F90":                         "127.0.0.1:8080",
		"010200C0:0009":                         "192.0.2.1:9",
		"00000000000000000000000001000000:0050": "[::1]:80",
	} {
		if got, ok := decodeAddr(in); !ok || got != want {
			t.Errorf("decodeAddr(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"nocolon", "ZZ:0050", "0100007F:ZZZZ", "0100:0050"} {
		if _, ok := decodeAddr(bad); ok {
			t.Errorf("decodeAddr(%q) accepted it", bad)
		}
	}
	table := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111\n" +
		"   1: 0100007F:C350 010200C0:0009 01 00000000:00000000 00:00000000 00000000  1000        0 222\n" +
		"   2: short line\n"
	got := parseNetTable("udp", table)
	if len(got) != 1 || got["222"].Remote != "192.0.2.1:9" || got["222"].Proto != "udp" {
		t.Errorf("parseNetTable = %+v; the listener must be left out", got)
	}
}

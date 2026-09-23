// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build linux

package witness

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procRoot is /proc, a variable so a test could point it elsewhere.
var procRoot = "/proc"

// Take snapshots every process in the group: its command line, the paths
// it has open for writing, and the sockets it holds that have a remote end.
func Take(pgid int) (Snapshot, error) {
	s := Snapshot{Processes: map[int]string{}, Conns: map[string]Conn{}, Writes: map[string]bool{}}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return s, err
	}
	var inodes []string
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "stat"))
		if err != nil {
			continue // exited between the listing and the read
		}
		if g, err := parseStatPgrp(string(stat)); err != nil || g != pgid {
			continue
		}
		cmd, _ := os.ReadFile(filepath.Join(procRoot, e.Name(), "cmdline"))
		s.Processes[pid] = strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " "))

		fdDir := filepath.Join(procRoot, e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if inode, ok := strings.CutPrefix(target, "socket:["); ok {
				inodes = append(inodes, strings.TrimSuffix(inode, "]"))
				continue
			}
			if !strings.HasPrefix(target, "/") {
				continue // pipe:[…], anon_inode:[…]
			}
			info, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "fdinfo", fd.Name()))
			if err == nil && openForWrite(string(info)) {
				s.Writes[target] = true
			}
		}
		// The socket tables are per network namespace, not per process,
		// so they are read once through a member of the group and matched
		// by inode.
		if len(s.Conns) == 0 {
			for _, t := range []string{"tcp", "tcp6", "udp", "udp6"} {
				b, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "net", t)) // #nosec G304 -- /proc/<pid>/net/{tcp,udp}[6]: a fixed table name under a pid scout listed
				if err != nil {
					continue
				}
				for inode, c := range parseNetTable(strings.TrimSuffix(t, "6"), string(b)) {
					s.Conns[inode] = c
				}
			}
		}
	}
	held := map[string]bool{}
	for _, i := range inodes {
		held[i] = true
	}
	for inode := range s.Conns {
		if !held[inode] {
			delete(s.Conns, inode)
		}
	}
	return s, nil
}

// RSS sums the resident-set size, in bytes, of every process in the group.
//
// It reads /proc/<pid>/statm rather than status: statm is seven integers on
// one line, which is the cheapest thing /proc publishes about memory, and
// a soak samples it after every call. A process that exits between the
// listing and the read is skipped, as Take skips it. Shared pages are
// counted once per process that maps them, which overstates a group of
// forked workers and does not affect a trend.
func RSS(pgid int) (int64, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0, err
	}
	var total int64
	found := false
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "stat"))
		if err != nil {
			continue
		}
		if g, err := parseStatPgrp(string(stat)); err != nil || g != pgid {
			continue
		}
		statm, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "statm"))
		if err != nil {
			continue
		}
		pages, ok := parseStatmResident(string(statm))
		if !ok {
			continue
		}
		found = true
		total += pages * int64(os.Getpagesize())
	}
	if !found {
		return 0, ErrGone
	}
	return total, nil
}

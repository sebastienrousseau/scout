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

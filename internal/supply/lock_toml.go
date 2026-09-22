// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package supply

import (
	"bufio"
	"bytes"
	"errors"
	"regexp"
	"strings"
)

// Cargo.lock and uv.lock are TOML, but they are TOML a program wrote: an
// array of [[package]] tables, one key per line, string values in double
// quotes, and arrays that open on the key's line and close on their own.
// This reads that shape. It is not a TOML parser and does not pretend to
// be one — a hand-edited file in a shape the tool never writes is refused
// or read short, never guessed at.

// tomlPackage is one [[package]] table: its single-line string keys, and
// the raw text of every other key so a reader can pick hashes out of it.
type tomlPackage struct {
	str map[string]string
	raw map[string]string
}

var (
	tomlString = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*"((?:[^"\\]|\\.)*)"\s*$`)
	tomlKey    = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*(.*)$`)
	// quotedHash finds hash = "alg:hex" inside an inline table.
	quotedHash = regexp.MustCompile(`\bhash\s*=\s*"([^"]+)"`)
)

// tomlPackages splits a generated lockfile into its [[package]] tables.
// It also returns the [metadata] table, which old Cargo lockfiles used
// for checksums.
func tomlPackages(data []byte) ([]tomlPackage, map[string]string, error) {
	var (
		pkgs     []tomlPackage
		cur      *tomlPackage
		metadata = map[string]string{}
		section  string
		openKey  string // a multi-line array still being read
		openText strings.Builder
		depth    int
	)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxLockfile)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if openKey != "" {
			openText.WriteString(line + "\n")
			depth += strings.Count(line, "[") - strings.Count(line, "]")
			if depth <= 0 {
				if cur != nil {
					cur.raw[openKey] = openText.String()
				}
				openKey, depth = "", 0
				openText.Reset()
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			if line == "[[package]]" {
				pkgs = append(pkgs, tomlPackage{str: map[string]string{}, raw: map[string]string{}})
				cur = &pkgs[len(pkgs)-1]
			} else {
				// [package.metadata] and the like belong to the package
				// but carry nothing read here; any other table ends it.
				cur = nil
			}
			continue
		}
		if m := tomlString.FindStringSubmatch(line); m != nil {
			switch {
			case cur != nil:
				cur.str[m[1]] = m[2]
			case section == "[metadata]":
				metadata[m[1]] = m[2]
			}
			continue
		}
		// A quoted key, which is what [metadata] checksums use.
		if section == "[metadata]" && strings.HasPrefix(line, `"`) {
			if k, v, ok := strings.Cut(line, `" = "`); ok {
				metadata[strings.TrimPrefix(k, `"`)] = strings.TrimSuffix(v, `"`)
			}
			continue
		}
		if m := tomlKey.FindStringSubmatch(line); m != nil {
			d := strings.Count(m[2], "[") - strings.Count(m[2], "]")
			if d > 0 {
				openKey, depth = m[1], d
				openText.WriteString(m[2] + "\n")
				continue
			}
			if cur != nil {
				cur.raw[m[1]] = m[2]
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if openKey != "" {
		return nil, nil, errors.New("an array is never closed; the file is truncated or was not written by its tool")
	}
	return pkgs, metadata, nil
}

// ---- Cargo.lock ----

func parseCargoLock(data []byte) ([]Package, *Package, error) {
	pkgs, metadata, err := tomlPackages(data)
	if err != nil {
		return nil, nil, err
	}
	var out []Package
	var members []Package
	for _, t := range pkgs {
		name, version := t.str["name"], t.str["version"]
		if name == "" {
			continue
		}
		p := Package{Ecosystem: "cargo", Name: name, Version: version}
		source, hasSource := t.str["source"]
		if !hasSource {
			// No source means a member of this workspace: the project,
			// not a dependency of it.
			members = append(members, p)
			continue
		}
		sum := t.str["checksum"]
		if sum == "" {
			// Format 1 kept checksums in [metadata], keyed by package.
			sum = metadata["checksum "+name+" "+version+" ("+source+")"]
		}
		if h, ok := hexHash(sum, "sha256"); ok {
			p.Hashes = []BOMHash{h}
		} else {
			p.Unverifiable = cargoUnverifiable(source, sum)
		}
		out = append(out, p)
	}
	var root *Package
	if len(members) == 1 {
		root = &members[0]
	} else {
		// A workspace with several members has no single root, and
		// the members are what it builds, so they are listed.
		out = append(out, members...)
	}
	return out, root, nil
}

func cargoUnverifiable(source, sum string) string {
	switch {
	case sum != "":
		return "checksum in Cargo.lock is not a well-formed SHA-256"
	case strings.HasPrefix(source, "git+"):
		return "a git dependency; Cargo records the commit, not a registry checksum"
	case strings.HasPrefix(source, "path+"):
		return "installed from a local path; no registry checksum exists"
	default:
		return "no checksum in Cargo.lock"
	}
}

// ---- uv.lock ----

func parseUVLock(data []byte) ([]Package, *Package, error) {
	pkgs, _, err := tomlPackages(data)
	if err != nil {
		return nil, nil, err
	}
	var out []Package
	var root *Package
	for _, t := range pkgs {
		name, version := t.str["name"], t.str["version"]
		if name == "" {
			continue
		}
		p := Package{Ecosystem: "pypi", Name: name, Version: version}
		source := t.raw["source"]
		if strings.Contains(source, "editable") || strings.Contains(source, "virtual") {
			// The project itself, installed from its own checkout.
			if root == nil {
				r := p
				root = &r
			}
			continue
		}
		// A distribution has one sdist and a wheel per platform. The
		// sdist is the single artifact every platform shares, so its
		// hash is the component's. Wheels alone are still pinned: pip
		// refuses any artifact whose hash uv did not record.
		sdist := quotedHash.FindStringSubmatch(t.raw["sdist"])
		wheels := quotedHash.FindAllStringSubmatch(t.raw["wheels"], -1)
		switch {
		case sdist != nil:
			if h, ok := hexHash(sdist[1], ""); ok {
				p.Hashes = []BOMHash{h}
			} else {
				p.Unverifiable = "sdist hash in uv.lock is not a well-formed digest"
			}
		case len(wheels) > 0:
			// Pinned, with no single artifact to name.
		case strings.Contains(source, "git"):
			p.Unverifiable = "a git dependency; uv records the commit, not an artifact hash"
		case strings.Contains(source, "path") || strings.Contains(source, "directory"):
			p.Unverifiable = "installed from a local path; no index hash exists"
		default:
			p.Unverifiable = "no artifact hash in uv.lock"
		}
		out = append(out, p)
	}
	return out, root, nil
}

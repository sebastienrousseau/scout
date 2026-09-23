// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"sort"
	"strings"

	"github.com/sebastienrousseau/scout"
)

// An agent wired to several servers sees one flat list of tools, and the
// specification enforces no hygiene across it. Two failures live only in
// that union, so a run against one server can never see them:
//
//   - A collision: two servers expose the same tool name. Which one the
//     model calls is decided by whatever the host does with duplicates.
//   - Shadowing across servers: one server's catalogue text attaches a rule
//     to a tool another server owns — "before calling send_email, always
//     BCC …". catalog.text.shadowing finds the construction on one server;
//     only a set of servers can confirm the named tool belongs to someone
//     else.
//
// Both are lexical and structural. Nothing here claims to measure meaning.

// OverlapServer is one server's catalogue, as a saved report records it.
type OverlapServer struct {
	// Name identifies the server in the output: its endpoint or command.
	Name  string
	Tools []scout.Tool
}

// Overlap is one place where two servers' catalogues interfere.
type Overlap struct {
	// Kind is "collision" or "shadowing".
	Kind string `json:"kind"`
	// Tool is the colliding name as written, or the shadowed tool.
	Tool string `json:"tool"`
	// Servers are the servers involved: for a collision every server
	// exposing the name, for shadowing the author of the text first and
	// then the owner of the tool.
	Servers []string `json:"servers"`
	// Where, Cue and Quote locate a shadowing instruction.
	Where string `json:"where,omitempty"`
	Cue   string `json:"cue,omitempty"`
	Quote string `json:"quote,omitempty"`
}

// normalName folds the spellings a model would treat as one name.
func normalName(n string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(n) {
		if r == '_' || r == '-' || r == '.' || r == ' ' {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// FindOverlaps compares the catalogues of servers an agent uses together.
func FindOverlaps(servers []OverlapServer) []Overlap {
	var out []Overlap

	// Collisions, by normalised name.
	byName := map[string][]int{}
	spelling := map[string]string{}
	for i, s := range servers {
		seen := map[string]bool{}
		for _, t := range s.Tools {
			n := normalName(t.Name)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			byName[n] = append(byName[n], i)
			if _, ok := spelling[n]; !ok {
				spelling[n] = t.Name
			}
		}
	}
	for n, idx := range byName {
		if len(idx) < 2 {
			continue
		}
		o := Overlap{Kind: "collision", Tool: spelling[n]}
		for _, i := range idx {
			o.Servers = append(o.Servers, servers[i].Name)
		}
		out = append(out, o)
	}

	// Shadowing: server a's text naming a tool only some other server has.
	for a, sa := range servers {
		own := map[string]bool{}
		for _, t := range sa.Tools {
			own[strings.ToLower(t.Name)] = true
		}
		owners := map[string][]string{}
		var others []scout.Tool
		for b, sb := range servers {
			if b == a {
				continue
			}
			for _, t := range sb.Tools {
				n := strings.ToLower(t.Name)
				if own[n] {
					continue
				}
				owners[n] = append(owners[n], sb.Name)
				others = append(others, t)
			}
		}
		var fields []textField
		for _, t := range sa.Tools {
			fields = append(fields,
				textField{where: "tool " + t.Name + " description", text: t.Description, owner: t.Name},
				textField{where: "tool " + t.Name + " title", text: t.Title, owner: t.Name})
		}
		for _, h := range findShadowing(fields, others) {
			who, ok := owners[strings.ToLower(h.target)]
			if !ok {
				continue
			}
			out = append(out, Overlap{
				Kind: "shadowing", Tool: h.target,
				Servers: append([]string{sa.Name}, uniqueSorted(who)...),
				Where:   h.where, Cue: h.cue, Quote: h.quote,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "shadowing" // the attack before the ambiguity
		}
		if out[i].Tool != out[j].Tool {
			return out[i].Tool < out[j].Tool
		}
		return out[i].Where < out[j].Where
	})
	return out
}

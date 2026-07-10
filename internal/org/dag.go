// Package org is the org-truth compile surface (org-truth v1): a single loadable
// DAG of who-reports-to-whom that watch synthesis, OwningXO, and dash topology
// share. PR1 ships the DAG type, channel-derived snapshot, YAML load/compile
// (structural), and roster wiring. Optional fleet-org.yaml agreement refuse is PR2.
package org

import "fmt"

// Source names how a DAG was produced.
const (
	SourceDerived = "derived" // from channels[] via roster synthesis rules
	SourceFile    = "file"    // from fleet-org.yaml (PR2 agreement path)
)

// NodeKind classifies an org node.
type NodeKind string

const (
	KindCoordinator NodeKind = "coordinator"
	KindDesk        NodeKind = "desk"
	KindAdjutant    NodeKind = "adjutant"
	KindContainer   NodeKind = "container"
	KindUnknown     NodeKind = ""
)

// Node is one agent (or optional container) in the org DAG.
type Node struct {
	ID            string
	Kind          NodeKind
	ReportsTo     string // primary parent; empty = root
	HomeChannelID string // optional Discord home binding
}

// DAG is the compiled org-truth graph. Parents may be multi-valued on the
// channel-derived path (today's AgentsAbove can list multiple owed parents);
// PrimaryParent is Parents[0] when present — design §9: single primary when an
// org *file* is present (enforced at Compile in PR2).
type DAG struct {
	Root   string
	Source string

	Nodes    map[string]Node
	Parents  map[string][]string // agent → AgentsAbove equivalent
	Children map[string][]string // agent → AgentsBelow equivalent
}

// PrimaryParent returns the first parent of agent, or "" if none.
func (d *DAG) PrimaryParent(agent string) string {
	if d == nil {
		return ""
	}
	ps := d.Parents[agent]
	if len(ps) == 0 {
		return ""
	}
	return ps[0]
}

// Snapshot builds a derived DAG by calling parent/child resolvers for each agent.
// Used by roster.Load so the stored DAG is byte-parity with AgentsAbove/AgentsBelow.
func Snapshot(root, source string, agentNames []string, parents, children func(string) []string) *DAG {
	if source == "" {
		source = SourceDerived
	}
	d := &DAG{
		Root:     root,
		Source:   source,
		Nodes:    make(map[string]Node, len(agentNames)),
		Parents:  make(map[string][]string, len(agentNames)),
		Children: make(map[string][]string, len(agentNames)),
	}
	for _, name := range agentNames {
		if name == "" {
			continue
		}
		p := cloneStrings(parents(name))
		c := cloneStrings(children(name))
		d.Parents[name] = p
		d.Children[name] = c
		reportsTo := ""
		if len(p) > 0 {
			reportsTo = p[0]
		}
		d.Nodes[name] = Node{
			ID:        name,
			Kind:      KindUnknown,
			ReportsTo: reportsTo,
		}
	}
	return d
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// ValidateStructural checks for empty ids and cycles on primary-parent edges.
func (d *DAG) ValidateStructural() error {
	if d == nil {
		return fmt.Errorf("org: nil DAG")
	}
	const white, gray, black = 0, 1, 2
	color := map[string]int{}
	var visit func(string) error
	visit = func(u string) error {
		color[u] = gray
		p := d.PrimaryParent(u)
		if p != "" {
			if _, ok := d.Nodes[p]; ok {
				switch color[p] {
				case gray:
					return fmt.Errorf("org: cycle involving %q and %q", u, p)
				case white:
					if err := visit(p); err != nil {
						return err
					}
				}
			}
		}
		color[u] = black
		return nil
	}
	for id := range d.Nodes {
		if id == "" {
			return fmt.Errorf("org: empty node id")
		}
		if color[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

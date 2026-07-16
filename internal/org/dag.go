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
	Parents  map[string][]string // child → canonical reports-to parents
	Children map[string][]string // parent → canonical direct reports
}

// AddParent adds the canonical child→parent edge and its inverse idempotently.
func (d *DAG) AddParent(child, parent string) {
	if d == nil || child == "" || parent == "" || child == parent {
		return
	}
	d.Parents[child] = appendUniqueDAG(d.Parents[child], parent)
	d.Children[parent] = appendUniqueDAG(d.Children[parent], child)
	n := d.Nodes[child]
	if n.ReportsTo == "" {
		n.ReportsTo = parent
		d.Nodes[child] = n
	}
}
func appendUniqueDAG(in []string, v string) []string {
	for _, x := range in {
		if x == v {
			return in
		}
	}
	return append(in, v)
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

// Snapshot builds a DAG from resolvers that already use canonical reporting
// semantics: Parents are who the agent reports to; Children are direct reports.
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

// ValidateStructural checks root and node invariants and rejects cycles across
// every parent edge. Derived DAGs may have multiple parents, so following only
// PrimaryParent would allow a cycle hidden in Parents[1:].
func (d *DAG) ValidateStructural() error {
	if d == nil {
		return fmt.Errorf("org: nil DAG")
	}
	if d.Root != "" && len(d.Parents[d.Root]) != 0 {
		return fmt.Errorf("org: root %q must not report to a parent", d.Root)
	}
	const white, gray, black = 0, 1, 2
	color := map[string]int{}
	var visit func(string) error
	visit = func(u string) error {
		color[u] = gray
		for _, p := range d.Parents[u] {
			if p == "" {
				return fmt.Errorf("org: node %q has an empty parent", u)
			}
			if _, ok := d.Nodes[p]; !ok {
				return fmt.Errorf("org: node %q reports to unknown parent %q", u, p)
			}
			switch color[p] {
			case gray:
				return fmt.Errorf("org: cycle involving %q and %q", u, p)
			case white:
				if err := visit(p); err != nil {
					return err
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

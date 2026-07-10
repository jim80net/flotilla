package org

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is the on-disk fleet-org document (YAML primary; JSON accepted with version).
type File struct {
	Version int        `json:"version" yaml:"version"`
	Root    string     `json:"root" yaml:"root"`
	Nodes   []FileNode `json:"nodes" yaml:"nodes"`
}

// FileNode is one entry in fleet-org.yaml.
type FileNode struct {
	ID            string   `json:"id" yaml:"id"`
	Kind          NodeKind `json:"kind" yaml:"kind"`
	ReportsTo     string   `json:"reports_to" yaml:"reports_to"`
	HomeChannelID string   `json:"home_channel_id" yaml:"home_channel_id"`
}

// LoadFile reads a fleet-org document from path. Format is chosen by extension
// (.json → JSON; otherwise YAML). Empty path is an error.
func LoadFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("org: read %q: %w", path, err)
	}
	return Parse(raw, path)
}

// Parse decodes a fleet-org document. path is used only for error context and
// extension sniffing when non-empty.
func Parse(raw []byte, path string) (*File, error) {
	var f File
	useJSON := strings.HasSuffix(strings.ToLower(path), ".json")
	if useJSON {
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("org: parse JSON %q: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(raw, &f); err != nil {
			// Fallback: try JSON when path has no extension or YAML failed on a versioned JSON blob.
			if json.Unmarshal(raw, &f) == nil && f.Version != 0 {
				return validateFile(&f, path)
			}
			return nil, fmt.Errorf("org: parse YAML %q: %w", path, err)
		}
	}
	return validateFile(&f, path)
}

func validateFile(f *File, path string) (*File, error) {
	if f.Version != 1 {
		return nil, fmt.Errorf("org: %q: unsupported version %d (want 1)", path, f.Version)
	}
	if len(f.Nodes) == 0 {
		return nil, fmt.Errorf("org: %q: no nodes", path)
	}
	seen := map[string]bool{}
	for i, n := range f.Nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("org: %q: node[%d] has empty id", path, i)
		}
		if seen[n.ID] {
			return nil, fmt.Errorf("org: %q: duplicate node id %q", path, n.ID)
		}
		seen[n.ID] = true
		switch n.Kind {
		case KindCoordinator, KindDesk, KindAdjutant, KindContainer, KindUnknown:
		default:
			return nil, fmt.Errorf("org: %q: node %q has invalid kind %q", path, n.ID, n.Kind)
		}
	}
	if f.Root != "" && !seen[f.Root] {
		return nil, fmt.Errorf("org: %q: root %q is not a node", path, f.Root)
	}
	return f, nil
}

// Compile turns a validated File into a DAG (source=file). It enforces single
// primary reports_to (design §9), unknown-parent refusal, and acyclicity.
// Call Agree(fileDAG, derived) separately for channel agreement (PR2).
func Compile(f *File) (*DAG, error) {
	if f == nil {
		return nil, fmt.Errorf("org: compile nil file")
	}
	d := &DAG{
		Root:     f.Root,
		Source:   SourceFile,
		Nodes:    make(map[string]Node, len(f.Nodes)),
		Parents:  make(map[string][]string, len(f.Nodes)),
		Children: make(map[string][]string, len(f.Nodes)),
	}
	for _, n := range f.Nodes {
		d.Nodes[n.ID] = Node{
			ID:            n.ID,
			Kind:          n.Kind,
			ReportsTo:     n.ReportsTo,
			HomeChannelID: n.HomeChannelID,
		}
		if n.ReportsTo != "" {
			if _, ok := d.Nodes[n.ReportsTo]; !ok {
				// parent may appear later in file — second pass
			}
			d.Parents[n.ID] = []string{n.ReportsTo}
		}
	}
	// Second pass: unknown parents + children index
	for id, n := range d.Nodes {
		if n.ReportsTo == "" {
			continue
		}
		if _, ok := d.Nodes[n.ReportsTo]; !ok {
			return nil, fmt.Errorf("org: node %q reports_to unknown parent %q", id, n.ReportsTo)
		}
		d.Children[n.ReportsTo] = append(d.Children[n.ReportsTo], id)
	}
	if err := d.ValidateStructural(); err != nil {
		return nil, err
	}
	return d, nil
}

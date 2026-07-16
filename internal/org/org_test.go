package org

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSnapshot_ParentsChildren(t *testing.T) {
	d := Snapshot("xo", SourceDerived, []string{"xo", "alpha-xo", "backend"},
		func(a string) []string {
			switch a {
			case "alpha-xo":
				return []string{"xo"}
			case "backend":
				return []string{"alpha-xo"}
			default:
				return nil
			}
		},
		func(a string) []string {
			switch a {
			case "xo":
				return []string{"alpha-xo"}
			case "alpha-xo":
				return []string{"backend"}
			default:
				return nil
			}
		},
	)
	if d.Source != SourceDerived || d.Root != "xo" {
		t.Fatalf("meta: source=%q root=%q", d.Source, d.Root)
	}
	if d.PrimaryParent("backend") != "alpha-xo" {
		t.Errorf("PrimaryParent(backend)=%q", d.PrimaryParent("backend"))
	}
	if !slices.Equal(d.Children["xo"], []string{"alpha-xo"}) {
		t.Errorf("Children[xo]=%v", d.Children["xo"])
	}
	if err := d.ValidateStructural(); err != nil {
		t.Fatal(err)
	}
}

func TestCompile_FileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fleet-org.yaml")
	body := []byte(`version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
  - id: alpha-xo
    kind: coordinator
    reports_to: xo
    home_channel_id: "YOUR_ALPHA_XO_CHANNEL_ID"
  - id: backend
    kind: desk
    reports_to: alpha-xo
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Compile(f)
	if err != nil {
		t.Fatal(err)
	}
	if d.Source != SourceFile {
		t.Errorf("source=%q", d.Source)
	}
	if d.PrimaryParent("backend") != "alpha-xo" {
		t.Errorf("parent=%q", d.PrimaryParent("backend"))
	}
	if d.Nodes["alpha-xo"].HomeChannelID != "YOUR_ALPHA_XO_CHANNEL_ID" {
		t.Errorf("home=%q", d.Nodes["alpha-xo"].HomeChannelID)
	}
}

func TestCompile_RejectsCycle(t *testing.T) {
	f := &File{
		Version: 1,
		Root:    "a",
		Nodes: []FileNode{
			{ID: "a", Kind: KindCoordinator, ReportsTo: "b"},
			{ID: "b", Kind: KindCoordinator, ReportsTo: "a"},
		},
	}
	if _, err := Compile(f); err == nil {
		t.Fatal("expected cycle refuse")
	}
}

func TestCompile_RejectsUnknownParent(t *testing.T) {
	f := &File{
		Version: 1,
		Root:    "xo",
		Nodes: []FileNode{
			{ID: "xo", Kind: KindCoordinator},
			{ID: "backend", Kind: KindDesk, ReportsTo: "ghost"},
		},
	}
	if _, err := Compile(f); err == nil {
		t.Fatal("expected unknown parent refuse")
	}
}

func TestDeriveFromChannels_HomeChannelStar(t *testing.T) {
	// alpha-xo home lists parent xo; backend home lists alpha-xo.
	d := DeriveFromChannels("xo",
		[]string{"xo", "alpha-xo", "backend"},
		[]Channel{
			{ChannelID: "C_CMD", XOAgent: "xo", Role: "fleet-command", Members: []string{"xo", "alpha-xo", "backend"}},
			{ChannelID: "C_ALPHA", XOAgent: "alpha-xo", Members: []string{"xo"}},
			{ChannelID: "C_BE", XOAgent: "backend", Members: []string{"alpha-xo"}},
		},
	)
	if d.PrimaryParent("alpha-xo") != "xo" {
		t.Errorf("alpha-xo parent=%q want xo", d.PrimaryParent("alpha-xo"))
	}
	if d.PrimaryParent("backend") != "alpha-xo" {
		t.Errorf("backend parent=%q want alpha-xo", d.PrimaryParent("backend"))
	}
	if d.PrimaryParent("xo") != "" {
		t.Errorf("root should have no parent, got %q", d.PrimaryParent("xo"))
	}
}

func TestDeriveFromChannels_RepeatedManyToManyEdgesAreCanonical(t *testing.T) {
	d := DeriveFromChannels("root", []string{"root", "p1", "p2", "shared", "leaf"}, []Channel{
		{ChannelID: "p1", XOAgent: "p1", Members: []string{"root"}}, {ChannelID: "p2", XOAgent: "p2", Members: []string{"root"}},
		{ChannelID: "shared-a", XOAgent: "shared", Members: []string{"p1", "p2"}}, {ChannelID: "shared-b", XOAgent: "shared", Members: []string{"p1", "p2"}},
		{ChannelID: "legacy", XOAgent: "p1", Members: []string{"leaf"}},
	})
	if !slices.Equal(d.Parents["shared"], []string{"p1", "p2"}) {
		t.Fatalf("shared parents=%v", d.Parents["shared"])
	}
	if !slices.Contains(d.Children["p1"], "shared") || !slices.Contains(d.Children["p2"], "shared") {
		t.Fatalf("inverse children p1=%v p2=%v", d.Children["p1"], d.Children["p2"])
	}
	if !slices.Equal(d.Parents["leaf"], []string{"p1"}) || !slices.Contains(d.Children["p1"], "leaf") {
		t.Fatalf("legacy edge parents=%v children=%v", d.Parents["leaf"], d.Children["p1"])
	}
}

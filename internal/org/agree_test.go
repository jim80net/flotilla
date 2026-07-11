package org

import (
	"strings"
	"testing"
)

func TestAgree_Match(t *testing.T) {
	file := Snapshot("xo", SourceFile, []string{"xo", "alpha-xo", "backend"},
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
		func(string) []string { return nil },
	)
	derived := Snapshot("xo", SourceDerived, []string{"xo", "alpha-xo", "backend"},
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
		func(string) []string { return nil },
	)
	if err := Agree(file, derived); err != nil {
		t.Fatal(err)
	}
}

func TestAgree_Disagreement(t *testing.T) {
	file := Snapshot("xo", SourceFile, []string{"backend"},
		func(a string) []string {
			if a == "backend" {
				return []string{"alpha-xo"}
			}
			return nil
		},
		func(string) []string { return nil },
	)
	derived := Snapshot("xo", SourceDerived, []string{"backend"},
		func(a string) []string {
			if a == "backend" {
				return []string{"xo"} // channels say parent is xo
			}
			return nil
		},
		func(string) []string { return nil },
	)
	err := Agree(file, derived)
	if err == nil {
		t.Fatal("expected disagreement")
	}
	if !strings.Contains(err.Error(), "backend") || !strings.Contains(err.Error(), "alpha-xo") || !strings.Contains(err.Error(), "xo") {
		t.Errorf("error should name agent and both parents: %v", err)
	}
}

func TestAgree_DerivedEmptyAllowsFileParent(t *testing.T) {
	// Org declares parent; channels assert nothing — allowed.
	file := Snapshot("xo", SourceFile, []string{"adj"},
		func(a string) []string {
			if a == "adj" {
				return []string{"xo"}
			}
			return nil
		},
		func(string) []string { return nil },
	)
	derived := Snapshot("xo", SourceDerived, []string{"adj"},
		func(string) []string { return nil },
		func(string) []string { return nil },
	)
	if err := Agree(file, derived); err != nil {
		t.Fatal(err)
	}
}

func TestCheckHomes_DuplicateUndeclared(t *testing.T) {
	f := &File{
		Version: 1,
		Root:    "xo",
		Nodes: []FileNode{
			{ID: "xo", Kind: KindCoordinator},
			{ID: "alpha-xo", Kind: KindCoordinator, ReportsTo: "xo"}, // no home_channel_id
		},
	}
	err := CheckHomes(f, map[string][]string{
		"alpha-xo": {"C_A1", "C_A2"},
	})
	if err == nil {
		t.Fatal("expected multi-home refuse")
	}
	if !strings.Contains(err.Error(), "multiple home") {
		t.Errorf("got: %v", err)
	}
}

func TestCheckHomes_DeclaredPicksOne(t *testing.T) {
	f := &File{
		Version: 1,
		Root:    "xo",
		Nodes: []FileNode{
			{ID: "xo", Kind: KindCoordinator},
			{ID: "alpha-xo", Kind: KindCoordinator, ReportsTo: "xo", HomeChannelID: "C_A1"},
		},
	}
	if err := CheckHomes(f, map[string][]string{
		"alpha-xo": {"C_A1", "C_A2"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckHomes_DuplicateChannelClaim(t *testing.T) {
	f := &File{
		Version: 1,
		Nodes: []FileNode{
			{ID: "a", HomeChannelID: "C1"},
			{ID: "b", HomeChannelID: "C1"},
		},
	}
	if err := CheckHomes(f, nil); err == nil {
		t.Fatal("expected duplicate home_channel_id refuse")
	}
}

func TestOpenOptional_Missing(t *testing.T) {
	f, err := OpenOptional("/no/such/fleet-org.yaml", false)
	if err != nil || f != nil {
		t.Fatalf("optional missing: f=%v err=%v", f, err)
	}
	_, err = OpenOptional("/no/such/fleet-org.yaml", true)
	if err == nil {
		t.Fatal("required missing should error")
	}
}

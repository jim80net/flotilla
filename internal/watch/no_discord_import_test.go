package watch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDiscordImport is the load-bearing extraction invariant: after the Transport
// SPI extraction, internal/watch MUST NOT import internal/discord — the coordination
// bus seam is fully extracted behind internal/transport. A re-introduced
// internal/discord import here means a seam leaked back across the boundary (the
// catch-up projection, a snowflake parse, a chunk call), which the byte-pinned
// extraction forbids. It parses every .go file in this directory (production AND
// test) and fails on any "github.com/jim80net/flotilla/internal/discord" import.
func TestNoDiscordImport(t *testing.T) {
	const banned = "github.com/jim80net/flotilla/internal/discord"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		for _, imp := range f.Imports {
			// imp.Path.Value is the quoted import path, e.g. "\"...internal/discord\"".
			if strings.Trim(imp.Path.Value, `"`) == banned {
				t.Errorf("%s imports %s — internal/watch must reach Discord only through internal/transport (the extracted bus seam)", name, banned)
			}
		}
	}
	if checked == 0 {
		t.Fatal("scanned 0 .go files — the import guard is not actually inspecting the package")
	}
}

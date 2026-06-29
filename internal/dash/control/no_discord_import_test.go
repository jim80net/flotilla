package control

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDiscordImport is the load-bearing graft invariant: after the dash control
// surface is re-pointed behind the Transport SPI (#188 PR2 / #106), internal/dash/
// control MUST NOT import internal/discord — the notify's outbound post + content
// cap now enter as an injected transport.Transport interface value at the wiring
// boundary (cmd/flotilla/dash.go), not as a compile-time import in this package.
// A re-introduced internal/discord import here means the seam leaked back across
// the boundary (a direct discord.Post / discord.MaxContentRunes), which the graft
// forbids. This mirrors internal/watch/no_discord_import_test.go exactly — the same
// decoupling PR1 established for the relay packages. It parses every .go file in
// this directory (production AND test) and fails on any internal/discord import.
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
				t.Errorf("%s imports %s — internal/dash/control must reach Discord only through internal/transport (the injected bus seam)", name, banned)
			}
		}
	}
	if checked == 0 {
		t.Fatal("scanned 0 .go files — the import guard is not actually inspecting the package")
	}
}

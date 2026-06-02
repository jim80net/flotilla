package deliver

import "testing"

func TestParsePane(t *testing.T) {
	out := "0:0.0\thydra-ops\n0:0.1\tx-signal-dev\n0:0.3\tv12-dev\n"

	got, err := parsePane(out, "v12-dev")
	if err != nil {
		t.Fatalf("parsePane: %v", err)
	}
	if got != "0:0.3" {
		t.Errorf("target = %q, want 0:0.3", got)
	}

	if _, err := parsePane(out, "missing"); err == nil {
		t.Error("parsePane(missing) = nil error, want error")
	}
}

func TestParsePaneIgnoresBlankLines(t *testing.T) {
	out := "\n0:0.0\thydra-ops\n\n"
	got, err := parsePane(out, "hydra-ops")
	if err != nil {
		t.Fatalf("parsePane: %v", err)
	}
	if got != "0:0.0" {
		t.Errorf("target = %q, want 0:0.0", got)
	}
}

func TestParsePaneAmbiguousIsError(t *testing.T) {
	// Two panes share a title — must error, never silently pick one.
	out := "0:0.0\tv12-dev\n1:0.0\tv12-dev\n"
	if _, err := parsePane(out, "v12-dev"); err == nil {
		t.Error("parsePane(duplicate title) = nil error, want ambiguity error")
	}
}

func TestParsePaneExactMatchNotSubstring(t *testing.T) {
	// "v12" must not match "v12-dev".
	out := "0:0.0\tv12-dev\n"
	if _, err := parsePane(out, "v12"); err == nil {
		t.Error("parsePane(substring) matched, want no match")
	}
}

func TestParsePaneMatchesGlyphPrefixedTitle(t *testing.T) {
	// Claude renames the pane to "<glyph> <name>"; the matcher must still find it.
	out := "0:0.0\t⠂ hydra-ops\n0:0.3\t✳ v12-dev\n"
	got, err := parsePane(out, "v12-dev")
	if err != nil {
		t.Fatalf("parsePane(glyph title): %v", err)
	}
	if got != "0:0.3" {
		t.Errorf("target = %q, want 0:0.3", got)
	}
	// The glyph prefix must not let "v12" match "✳ v12-dev".
	if _, err := parsePane(out, "v12"); err == nil {
		t.Error("parsePane(v12) matched a glyph-prefixed v12-dev, want no match")
	}
}

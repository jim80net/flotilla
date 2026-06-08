package deliver

import (
	"strings"
	"testing"
)

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

func TestTitleMatches(t *testing.T) {
	cases := []struct {
		title, want string
		match       bool
	}{
		{"v12-dev", "v12-dev", true},       // bare name
		{"✳ v12-dev", "v12-dev", true},     // single-glyph prefix (Claude live title)
		{"⠂ hydra-ops", "hydra-ops", true}, // different spinner glyph
		{"valbot", "valbot", true},
		{"✳ v12-dev", "v12", false},      // substring must not match
		{"my v12-dev", "v12-dev", false}, // multi-word prefix is not a glyph
		{"build v12-dev", "v12-dev", false},
		{"foo bar v12-dev", "v12-dev", false},
	}
	for _, c := range cases {
		if got := titleMatches(c.title, c.want); got != c.match {
			t.Errorf("titleMatches(%q, %q) = %v, want %v", c.title, c.want, got, c.match)
		}
	}
}

func TestClearKeysArgsIsLiteralSlash(t *testing.T) {
	// A slash command must be injected as LITERAL keystrokes (-l), the verified
	// method — NOT a bracketed paste (unverified for slash-command recognition).
	got := clearKeysArgs("0:0.0", "/clear")
	want := []string{"send-keys", "-t", "0:0.0", "-l", "--", "/clear"}
	if len(got) != len(want) {
		t.Fatalf("clearKeysArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("clearKeysArgs = %v, want %v (differ at %d)", got, want, i)
		}
	}
	var hasLiteral bool
	for _, a := range got {
		if a == "-l" {
			hasLiteral = true
		}
	}
	if !hasLiteral {
		t.Error("clearKeysArgs missing -l: the slash would be parsed as key names, not typed literally")
	}
}

func TestBufferNameIsPerProcess(t *testing.T) {
	b := bufferName()
	if !strings.HasPrefix(b, "flotilla-send-") {
		t.Errorf("bufferName = %q, want flotilla-send-<pid>", b)
	}
	if b == "flotilla-send" {
		t.Error("bufferName is the old shared constant — concurrent sends would collide")
	}
}

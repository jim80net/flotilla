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

func TestParsePaneMarkerResolvesDriftedTitle(t *testing.T) {
	// The bug: a Claude desk launched as "macro-desk-dev" retitled itself to a
	// task summary. With the stable @flotilla_agent marker it still resolves —
	// the title is now irrelevant.
	out := "0:0.0\t⠂ hydra-ops\t\n" +
		"0:0.2\t✳ Design P4 believability scorecard\tmacro-desk-dev\n"
	got, err := parsePane(out, "macro-desk-dev")
	if err != nil {
		t.Fatalf("parsePane(marker, drifted title): %v", err)
	}
	if got != "0:0.2" {
		t.Errorf("target = %q, want 0:0.2 (resolved by marker despite drifted title)", got)
	}
}

func TestParsePaneMarkerWinsOverTitleOfAnotherPane(t *testing.T) {
	// Pane A carries the marker (its title drifted); pane B coincidentally has a
	// title that would match. The authoritative marker must win.
	out := "0:0.1\t✳ some drifted summary\tv12-dev\n" + // marker pane (drifted title)
		"0:0.9\tv12-dev\t\n" // title-coincidence pane, untagged
	got, err := parsePane(out, "v12-dev")
	if err != nil {
		t.Fatalf("parsePane: %v", err)
	}
	if got != "0:0.1" {
		t.Errorf("target = %q, want 0:0.1 (marker is authoritative over a title match)", got)
	}
}

func TestParsePaneMarkerAmbiguousIsError(t *testing.T) {
	// Two panes tagged with the same marker = a mis-tagged fleet → never pick one.
	out := "0:0.1\tfoo\tv12-dev\n1:0.0\tbar\tv12-dev\n"
	if _, err := parsePane(out, "v12-dev"); err == nil {
		t.Error("parsePane(duplicate marker) = nil error, want ambiguity error")
	}
}

func TestParsePaneFallsBackToTitleWhenNoMarker(t *testing.T) {
	// A fully untagged fleet (empty marker fields) resolves by title exactly as
	// before — backward compatibility.
	out := "0:0.0\thydra-ops\t\n0:0.3\t✳ v12-dev\t\n"
	got, err := parsePane(out, "v12-dev")
	if err != nil {
		t.Fatalf("parsePane(title fallback): %v", err)
	}
	if got != "0:0.3" {
		t.Errorf("target = %q, want 0:0.3 (glyph title fallback)", got)
	}
}

func TestParsePaneMarkerDoesNotMatchEmptyWantOrEmptyMarker(t *testing.T) {
	// An empty marker must never match (an untagged pane is title-only). Searching
	// a real name against a fleet of empty markers falls through to title (and
	// here finds nothing) — it must not match an empty-marker pane.
	out := "0:0.0\tsomething\t\n0:0.1\tother\t\n"
	if _, err := parsePane(out, "macro-desk-dev"); err == nil {
		t.Error("empty markers must not match a non-empty want; expected not-found error")
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
		{"", "", false},         // empty want must never match (defensive self-guard)
		{"   ", "", false},      // whitespace-only title vs empty want must not match
		{"anything", "", false}, // empty want against any title
	}
	for _, c := range cases {
		if got := titleMatchesName(c.title, c.want); got != c.match {
			t.Errorf("titleMatchesName(%q, %q) = %v, want %v", c.title, c.want, got, c.match)
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

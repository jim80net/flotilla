package deliver

import (
	"strings"
	"testing"
)

func TestParsePane(t *testing.T) {
	out := "0:0.0\talpha-xo\n0:0.1\tdesk-g\n0:0.3\tdesk-a\n"

	got, err := parsePane(out, "desk-a")
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
	out := "\n0:0.0\talpha-xo\n\n"
	got, err := parsePane(out, "alpha-xo")
	if err != nil {
		t.Fatalf("parsePane: %v", err)
	}
	if got != "0:0.0" {
		t.Errorf("target = %q, want 0:0.0", got)
	}
}

func TestParsePaneAmbiguousIsError(t *testing.T) {
	// Two panes share a title — must error, never silently pick one.
	out := "0:0.0\tdesk-a\n1:0.0\tdesk-a\n"
	if _, err := parsePane(out, "desk-a"); err == nil {
		t.Error("parsePane(duplicate title) = nil error, want ambiguity error")
	}
}

func TestParsePaneExactMatchNotSubstring(t *testing.T) {
	// "desk" must not match "desk-a".
	out := "0:0.0\tdesk-a\n"
	if _, err := parsePane(out, "desk"); err == nil {
		t.Error("parsePane(substring) matched, want no match")
	}
}

func TestParsePaneMatchesGlyphPrefixedTitle(t *testing.T) {
	// Claude renames the pane to "<glyph> <name>"; the matcher must still find it.
	out := "0:0.0\t⠂ alpha-xo\n0:0.3\t✳ desk-a\n"
	got, err := parsePane(out, "desk-a")
	if err != nil {
		t.Fatalf("parsePane(glyph title): %v", err)
	}
	if got != "0:0.3" {
		t.Errorf("target = %q, want 0:0.3", got)
	}
	// The glyph prefix must not let "desk" match "✳ desk-a".
	if _, err := parsePane(out, "desk"); err == nil {
		t.Error("parsePane(desk) matched a glyph-prefixed desk-a, want no match")
	}
}

func TestParsePaneMarkerResolvesDriftedTitle(t *testing.T) {
	// The bug: a Claude desk launched as "desk-b" retitled itself to a
	// task summary. With the stable @flotilla_agent marker it still resolves —
	// the title is now irrelevant.
	out := "0:0.0\t⠂ alpha-xo\t\n" +
		"0:0.2\t✳ Design P4 believability scorecard\tdesk-b\n"
	got, err := parsePane(out, "desk-b")
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
	out := "0:0.1\t✳ some drifted summary\tdesk-a\n" + // marker pane (drifted title)
		"0:0.9\tdesk-a\t\n" // title-coincidence pane, untagged
	got, err := parsePane(out, "desk-a")
	if err != nil {
		t.Fatalf("parsePane: %v", err)
	}
	if got != "0:0.1" {
		t.Errorf("target = %q, want 0:0.1 (marker is authoritative over a title match)", got)
	}
}

func TestParsePaneMarkerAmbiguousIsError(t *testing.T) {
	// Two panes tagged with the same marker = a mis-tagged fleet → never pick one.
	out := "0:0.1\tfoo\tdesk-a\n1:0.0\tbar\tdesk-a\n"
	if _, err := parsePane(out, "desk-a"); err == nil {
		t.Error("parsePane(duplicate marker) = nil error, want ambiguity error")
	}
}

func TestParsePaneFallsBackToTitleWhenNoMarker(t *testing.T) {
	// A fully untagged fleet (empty marker fields) resolves by title exactly as
	// before — backward compatibility.
	out := "0:0.0\talpha-xo\t\n0:0.3\t✳ desk-a\t\n"
	got, err := parsePane(out, "desk-a")
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
	if _, err := parsePane(out, "desk-b"); err == nil {
		t.Error("empty markers must not match a non-empty want; expected not-found error")
	}
}

func TestParseFieldsRobustToTabInTitle(t *testing.T) {
	// A TUI-set pane title containing a literal tab must not corrupt the marker
	// field. target = before first tab; marker = after last tab; title = between.
	cases := []struct {
		line, target, title, marker string
	}{
		{"0:0.0\tplain title\tdesk-a", "0:0.0", "plain title", "desk-a"},
		{"0:0.0\thas\ta\ttab\tdesk-a", "0:0.0", "has\ta\ttab", "desk-a"}, // tabs in title
		{"0:0.0\tuntagged title\t", "0:0.0", "untagged title", ""},       // empty marker
		{"0:0.0\ttwo\ttab\t", "0:0.0", "two\ttab", ""},                   // tabby title, untagged
		{"0:0.0\tonly title", "0:0.0", "only title", ""},                 // 2-field variant
		{"0:0.0", "0:0.0", "", ""},                                       // target-only
	}
	for _, c := range cases {
		tg, ti, mk := parseFields(c.line)
		if tg != c.target || ti != c.title || mk != c.marker {
			t.Errorf("parseFields(%q) = (%q,%q,%q), want (%q,%q,%q)", c.line, tg, ti, mk, c.target, c.title, c.marker)
		}
	}
}

func TestParsePaneMarkerResolvesTitleWithLiteralTab(t *testing.T) {
	// A registered desk whose external TUI title contains a literal tab must
	// still resolve by its marker — the bug a greedy SplitN would introduce.
	out := "0:0.0\t⠂ alpha-xo\t\n" +
		"0:0.2\t✳ build\tstep\t2\tdesk-b\n" // title has tabs; marker is last field
	got, err := parsePane(out, "desk-b")
	if err != nil {
		t.Fatalf("parsePane(tabbed title, marker): %v", err)
	}
	if got != "0:0.2" {
		t.Errorf("target = %q, want 0:0.2 (resolved by marker despite tabs in title)", got)
	}
}

func TestTitleMatches(t *testing.T) {
	cases := []struct {
		title, want string
		match       bool
	}{
		{"desk-a", "desk-a", true},       // bare name
		{"✳ desk-a", "desk-a", true},     // single-glyph prefix (Claude live title)
		{"⠂ alpha-xo", "alpha-xo", true}, // different spinner glyph
		{"valbot", "valbot", true},
		{"✳ desk-a", "desk", false},    // substring must not match
		{"my desk-a", "desk-a", false}, // multi-word prefix is not a glyph
		{"build desk-a", "desk-a", false},
		{"foo bar desk-a", "desk-a", false},
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

func TestSlashKeysArgsIsLiteralSlash(t *testing.T) {
	// A slash command must be injected as LITERAL keystrokes (-l), the verified
	// method — NOT a bracketed paste (unverified for slash-command recognition).
	// Surface-agnostic: the same argv shape holds for any driver's reset command.
	cases := []struct {
		target, cmd string
		want        []string
	}{
		{"0:0.0", "/clear", []string{"send-keys", "-t", "0:0.0", "-l", "--", "/clear"}},       // claude-code / aider reset
		{"1:2.3", "/new", []string{"send-keys", "-t", "1:2.3", "-l", "--", "/new"}},           // grok reset
		{"a:0.0", "/new-chat", []string{"send-keys", "-t", "a:0.0", "-l", "--", "/new-chat"}}, // cursor reset
	}
	for _, c := range cases {
		got := slashKeysArgs(c.target, c.cmd)
		if len(got) != len(c.want) {
			t.Fatalf("slashKeysArgs(%q,%q) = %v, want %v", c.target, c.cmd, got, c.want)
		}
		var hasLiteral bool
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Fatalf("slashKeysArgs(%q,%q) = %v, want %v (differ at %d)", c.target, c.cmd, got, c.want, i)
			}
			if got[i] == "-l" {
				hasLiteral = true
			}
		}
		if !hasLiteral {
			t.Errorf("slashKeysArgs(%q,%q) missing -l: the slash would be parsed as key names, not typed literally", c.target, c.cmd)
		}
	}
}

func TestSendCtrlJArgsNewlineSequence(t *testing.T) {
	// SendCtrlJ types each line literally (-l) with a C-j keystroke between lines
	// (the in-composer newline, NOT a submit) and a single final Enter. This is the
	// per-driver alternate to bracketed-paste Send for harnesses without bracketed
	// paste / whose tmux newline is Ctrl+J.
	t.Run("single line → literal text then Enter", func(t *testing.T) {
		got := sendCtrlJArgs("0:0.0", "hello world")
		want := [][]string{
			{"send-keys", "-t", "0:0.0", "-l", "--", "hello world"},
			{"send-keys", "-t", "0:0.0", "--", "Enter"},
		}
		assertArgSeq(t, got, want)
	})
	t.Run("multi-line → C-j between lines, one final Enter", func(t *testing.T) {
		got := sendCtrlJArgs("0:0.0", "line one\nline two\nline three")
		want := [][]string{
			{"send-keys", "-t", "0:0.0", "-l", "--", "line one"},
			{"send-keys", "-t", "0:0.0", "--", "C-j"},
			{"send-keys", "-t", "0:0.0", "-l", "--", "line two"},
			{"send-keys", "-t", "0:0.0", "--", "C-j"},
			{"send-keys", "-t", "0:0.0", "-l", "--", "line three"},
			{"send-keys", "-t", "0:0.0", "--", "Enter"},
		}
		assertArgSeq(t, got, want)
		// Exactly one submitting Enter, and it is LAST — newlines never submit.
		enters := 0
		for i, a := range got {
			if a[len(a)-1] == "Enter" {
				enters++
				if i != len(got)-1 {
					t.Error("Enter must be the final keystroke (a mid-sequence Enter would submit early)")
				}
			}
		}
		if enters != 1 {
			t.Errorf("want exactly one Enter, got %d", enters)
		}
	})
	t.Run("blank interior line keeps its C-j (the blank line)", func(t *testing.T) {
		got := sendCtrlJArgs("0:0.0", "a\n\nb")
		want := [][]string{
			{"send-keys", "-t", "0:0.0", "-l", "--", "a"},
			{"send-keys", "-t", "0:0.0", "--", "C-j"},
			{"send-keys", "-t", "0:0.0", "-l", "--", ""},
			{"send-keys", "-t", "0:0.0", "--", "C-j"},
			{"send-keys", "-t", "0:0.0", "-l", "--", "b"},
			{"send-keys", "-t", "0:0.0", "--", "Enter"},
		}
		assertArgSeq(t, got, want)
	})
}

func assertArgSeq(t *testing.T, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("arg-seq length = %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("arg %d = %v, want %v", i, got[i], want[i])
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("arg %d = %v, want %v (differ at %d)", i, got[i], want[i], j)
			}
		}
	}
}

func TestSendEnterArgsIsSingleSubmittingEnter(t *testing.T) {
	// The confirmed-delivery retry (internal/surface.ConfirmSubmit) re-sends Enter ALONE to
	// submit a body already pasted into the composer, never re-pasting. The argv must be a
	// single submitting Enter (a key NAME, no -l) with the -- flag guard.
	cases := []struct {
		target string
		want   []string
	}{
		{"0:0.0", []string{"send-keys", "-t", "0:0.0", "--", "Enter"}},
		{"-dash:1.2", []string{"send-keys", "-t", "-dash:1.2", "--", "Enter"}}, // -- guards a dash-leading target
	}
	for _, c := range cases {
		got := sendEnterArgs(c.target)
		if len(got) != len(c.want) {
			t.Fatalf("sendEnterArgs(%q) = %v, want %v", c.target, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Fatalf("sendEnterArgs(%q) = %v, want %v (differ at %d)", c.target, got, c.want, i)
			}
		}
		// It must be a submitting Enter: the key name "Enter" (NOT typed as literal text via -l),
		// and the -- guard present so a dash-leading target is not parsed as a flag.
		for _, a := range got {
			if a == "-l" {
				t.Errorf("sendEnterArgs(%q) contains -l: Enter would be typed as literal text, not submit", c.target)
			}
		}
		if got[len(got)-1] != "Enter" {
			t.Errorf("sendEnterArgs(%q) last arg = %q, want the submitting key \"Enter\"", c.target, got[len(got)-1])
		}
		guarded := false
		for i, a := range got {
			if a == "--" && i == len(got)-2 {
				guarded = true
			}
		}
		if !guarded {
			t.Errorf("sendEnterArgs(%q) = %v, want -- immediately before Enter (dash-leading-target guard)", c.target, got)
		}
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

func TestPaneCWDArgs(t *testing.T) {
	got := paneCWDArgs("flotilla:5.0")
	want := []string{"display-message", "-p", "-t", "flotilla:5.0", "#{pane_current_path}"}
	if len(got) != len(want) {
		t.Fatalf("paneCWDArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paneCWDArgs = %v, want %v (differ at %d)", got, want, i)
		}
	}
}

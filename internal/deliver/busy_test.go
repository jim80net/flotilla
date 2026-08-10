package deliver

import "testing"

func TestParseCursorSnapshotOutput(t *testing.T) {
	cases := []struct {
		name, out                  string
		x, y                       int
		visible, inMode, wantError bool
	}{
		{"visible", "24 32 1 0\n", 24, 32, true, false, false},
		{"hidden unattached", "316 76 0 0\n", 316, 76, false, false, false},
		{"copy mode", "4 8 1 1\n", 4, 8, true, true, false},
		{"bad x", "x 8 1 0\n", 0, 0, false, false, true},
		{"bad y", "4 y 1 0\n", 0, 0, false, false, true},
		{"missing field", "4 8 0\n", 0, 0, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y, visible, inMode, err := parseCursorSnapshotOutput(tc.out)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError %v", err, tc.wantError)
			}
			if tc.wantError {
				return
			}
			if x != tc.x || y != tc.y || visible != tc.visible || inMode != tc.inMode {
				t.Fatalf("got (%d,%d,%v,%v), want (%d,%d,%v,%v)", x, y, visible, inMode, tc.x, tc.y, tc.visible, tc.inMode)
			}
		})
	}
}

func TestParseBusy(t *testing.T) {
	cases := []struct {
		name     string
		captured string
		busy     bool
	}{
		// --- EARLY working phase: glyph + gerund + "…" with NO counter yet. Measured live on
		// claude-code v2.1.178 (2026-06-16). The OLD `\(\d+s ·`-only regex returned FALSE for
		// all of these, so a short turn (or the first seconds of any turn) read as idle and a
		// confirmed delivery false-negatived. These are the regression cases.
		{"early spinner, no counter", "✻ Cooking…\n❯ ", true},
		{"early spinner, middot glyph frame", "· Cooking…\n❯ ", true},
		{"early spinner, sparkle glyph + long gerund", "✢ Quantumizing…\n❯ ", true},
		{"early spinner, hyphenated gerund", "✶ Sock-hopping…\n❯ ", true},
		// OCR-F2: the verb is token-based (not [A-Z][a-z]+), so apostrophe gerunds — real
		// claude-code spinner verbs — match in the EARLY (counterless) phase too, rather than
		// false-negativing like the old narrow class would.
		{"early spinner, apostrophe gerund", "✻ Mullin'…\n❯ ", true},
		// --- COUNTER phase: same gerund+"…" plus the elapsed counter.
		{"counter spinner (seconds)", "● 333\n✻ Frosting… (3s · ↓ 25 tokens · thinking)\n❯ ", true},
		{"counter spinner (live capture)", "✽ Scurrying… (53s · ↓ 3.4k tokens)\n❯ ", true},
		// --- MINUTE-format long turn: "(3m 14s ·" never matched the old `\(\d+s ·`; the
		// gerund+"…" marker catches it (and a >59s turn no longer reads as idle).
		{"minute-format long turn", "✻ Deliberating… (3m 14s · almost done thinking with high effort)\n❯ ", true},
		// --- Unsupported legacy prose is not a render marker, even beside composer.
		{"legacy hint beside composer", "esc to interrupt\n❯ ", false},
		{"quoted legacy hint in visible tail", "● Duty note: monitor panes that show esc to interrupt\n● acknowledged\n❯ ", false},

		// --- Idle / completed states must read as NOT busy.
		{"completed-turn summary (Worked for)", "● answer here\n✻ Worked for 8m 33s", false},
		{"completed-turn summary (Baked for)", "● answer\n✻ Baked for 7m 23s", false},
		{"idle composer quoted placeholder", "❯ Try \"how does cli.py work?\"\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},
		// The "❯"-led idle placeholder can END in the same "…" ellipsis — it must NOT read as
		// working (the regex excludes the ❯ composer prompt as a spinner glyph).
		{"idle composer ellipsis placeholder", "❯ Try a task…\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},
		{"empty idle", "❯ \n", false},
		{"idle footer only", "  user@host:~/x [Opus 4.8] ctx:57%\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},
		// "●" is the response/tool bullet, not a spinner glyph: a response line that happens to
		// be a lone gerund+"…" must NOT read as working (the "●" exclusion in the glyph class).
		{"response bullet gerund is not a spinner", "● Building…\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},
		// OCR-F1: there is no separate (unanchored) counter arm, so a "❯" composer line that
		// merely CONTAINS a counter substring cannot false-positive — the match is gated on the
		// leading glyph, and "❯" is excluded.
		{"composer line containing a counter substring", "❯ it took (3s · earlier)\n  ⏵⏵ auto mode on", false},

		// An old spinner line scrolled into history must NOT false-positive: only the row
		// immediately above the structurally located composer carries live provenance.
		{"stale spinner in scrollback", "✻ Frosting… (3s · ↓ 25 tokens)\n● done\n\n\n\n\n\n\n❯ \n  ⏵⏵ auto mode on", false},
		{"spinner in history but not composer-adjacent", "✻ Frosting… (3s · ↓ 25 tokens)\n● quoted follow-up\n❯ ", false},
	}
	for _, c := range cases {
		if got := ParseBusy(c.captured); got != c.busy {
			t.Errorf("%s: ParseBusy = %v, want %v", c.name, got, c.busy)
		}
	}
}

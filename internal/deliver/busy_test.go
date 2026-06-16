package deliver

import "testing"

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
		{"early spinner, no counter", "✻ Cooking…", true},
		{"early spinner, middot glyph frame", "· Cooking…", true},
		{"early spinner, sparkle glyph + long gerund", "✢ Quantumizing…", true},
		{"early spinner, hyphenated gerund", "✶ Sock-hopping…", true},
		// --- COUNTER phase: same gerund+"…" plus the elapsed counter.
		{"counter spinner (seconds)", "● 333\n✻ Frosting… (3s · ↓ 25 tokens · thinking)\n", true},
		{"counter spinner (live capture)", "✽ Scurrying… (53s · ↓ 3.4k tokens)", true},
		// --- MINUTE-format long turn: "(3m 14s ·" never matched the old `\(\d+s ·`; the
		// gerund+"…" marker catches it (and a >59s turn no longer reads as idle).
		{"minute-format long turn", "✻ Deliberating… (3m 14s · almost done thinking with high effort)", true},
		// --- legacy hint (current claude-code does not render it, kept as a cheap secondary).
		{"esc to interrupt", "doing work...\nesc to interrupt", true},

		// --- Idle / completed states must read as NOT busy.
		{"completed-turn summary (Worked for)", "● answer here\n✻ Worked for 8m 33s", false},
		{"completed-turn summary (Baked for)", "● answer\n✻ Baked for 7m 23s", false},
		{"idle composer quoted placeholder", "❯ Try \"how does cli.py work?\"\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},
		// The "❯"-led idle placeholder can END in the same "…" ellipsis — it must NOT read as
		// working (the regex excludes the ❯ composer prompt as a spinner glyph).
		{"idle composer ellipsis placeholder", "❯ Try a task…\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},
		{"empty idle", "❯ \n", false},
		{"idle footer only", "  jim@host:~/x [Opus 4.8] ctx:57%\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},

		// An old spinner line scrolled up in history must NOT false-positive: only the live
		// tail (last few lines) is scanned, line by line.
		{"stale spinner in scrollback", "✻ Frosting… (3s · ↓ 25 tokens)\n● done\n\n\n\n\n\n\n❯ \n  ⏵⏵ auto mode on", false},
	}
	for _, c := range cases {
		if got := ParseBusy(c.captured); got != c.busy {
			t.Errorf("%s: ParseBusy = %v, want %v", c.name, got, c.busy)
		}
	}
}

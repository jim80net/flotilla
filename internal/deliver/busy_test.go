package deliver

import "testing"

func TestParseBusy(t *testing.T) {
	cases := []struct {
		name     string
		captured string
		busy     bool
	}{
		// Real working-line captures observed from live Claude Code panes.
		{"active streaming spinner", "● 333\n✻ Frosting… (3s · ↓ 25 tokens · thinking)\n", true},
		{"active spinner gerund", "✶ Sock-hopping… (2s · ↓ 92 tokens · thinking)", true},
		{"esc to interrupt", "doing work...\nesc to interrupt", true},
		// Idle / completed states must read as NOT busy.
		{"completed-turn summary", "● answer here\n✻ Worked for 8m 33s", false},
		{"idle composer placeholder", "❯ Try \"how does cli.py work?\"\n  ⏵⏵ auto mode on (shift+tab to cycle)", false},
		{"empty idle", "❯ \n", false},
		// An old spinner line in scrollback must NOT false-positive: only the
		// live tail (last few lines) is scanned.
		{"stale spinner in scrollback", "✻ Frosting… (3s · ↓ 25 tokens)\n● done\n\n\n\n\n\n\n❯ \n  ⏵⏵ auto mode on", false},
	}
	for _, c := range cases {
		if got := ParseBusy(c.captured); got != c.busy {
			t.Errorf("%s: ParseBusy = %v, want %v", c.name, got, c.busy)
		}
	}
}

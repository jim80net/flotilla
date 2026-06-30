package deliver

import (
	"strings"
	"testing"
)

func TestClaudeRateLimitHitCurrentRegion(t *testing.T) {
	// Banner in the tail (current turn region) ⇒ hit.
	captured := strings.Repeat("\n", 40) + "Server is temporarily limiting requests\n❯ "
	if hit, detail := ClaudeRateLimitHit(captured); !hit || detail != ClaudeServerSidePhrase {
		t.Fatalf("tail hit = (%v, %q), want (true, %q)", hit, detail, ClaudeServerSidePhrase)
	}
}

func TestClaudeRateLimitHitScrollbackNotMaterial(t *testing.T) {
	// Banner only in scrollback (outside the 8-line tail) ⇒ not hit.
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "old output line")
	}
	lines = append(lines, "Server is temporarily limiting requests")
	for i := 0; i < 10; i++ {
		lines = append(lines, "recovered output")
	}
	captured := strings.Join(lines, "\n") + "\n❯ "
	if hit, _ := ClaudeRateLimitHit(captured); hit {
		t.Fatal("scrollback-only banner must not hit — stale throttle")
	}
}

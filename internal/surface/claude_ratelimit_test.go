package surface

import (
	"sync"
	"testing"
)

func TestClaudeRateLimitInstantSingleRead(t *testing.T) {
	c := claudeCode{
		capturePane: func(string) (string, error) {
			return "Server is temporarily limiting requests\n❯ ", nil
		},
	}
	pane := "claude-rate-limit-instant-pane"
	if lim, _, _ := c.RateLimited(pane); lim {
		t.Fatal("streak probe: first read must not be material")
	}
	lim, scope, detail := c.RateLimitInstant(pane)
	if !lim || scope != RateLimitServerSide || detail == "" {
		t.Fatalf("instant probe on first read = (%v,%v,%q), want material ServerSide", lim, scope, detail)
	}
}

func TestClaudeRateLimitInstantConcurrentUnderRace(t *testing.T) {
	c := claudeCode{
		capturePane: func(string) (string, error) {
			return "Server is temporarily limiting requests\n❯ ", nil
		},
	}
	pane := "claude-rate-limit-instant-race-pane"
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.RateLimitInstant(pane)
		}()
	}
	wg.Wait()
}

func TestClaudeRateLimitConsecutiveReads(t *testing.T) {
	c := claudeCode{
		capturePane: func(string) (string, error) {
			return "Server is temporarily limiting requests\n❯ ", nil
		},
	}
	pane := "claude-rate-limit-test-pane"
	if lim, _, _ := c.RateLimited(pane); lim {
		t.Fatal("first positive read must not be material (need 2 consecutive)")
	}
	lim, scope, detail := c.RateLimited(pane)
	if !lim || scope != RateLimitServerSide || detail == "" {
		t.Fatalf("second read = (%v,%v,%q), want material ServerSide", lim, scope, detail)
	}
}

func TestClaudeRateLimitGlitchResetsStreak(t *testing.T) {
	n := 0
	c := claudeCode{
		capturePane: func(string) (string, error) {
			n++
			if n == 2 {
				return "❯ ", nil // glitch — banner gone on second read
			}
			return "Server is temporarily limiting requests\n❯ ", nil
		},
	}
	pane := "claude-rate-limit-glitch-pane"
	if lim, _, _ := c.RateLimited(pane); lim {
		t.Fatal("first read alone must not be material")
	}
	if lim, _, _ := c.RateLimited(pane); lim {
		t.Fatal("glitch on second read must reset streak — not material")
	}
}

func TestClaudeRateLimitProseNotMaterial(t *testing.T) {
	c := claudeCode{
		capturePane: func(string) (string, error) {
			return "We discussed how the API rate limit exceeded our quota.\n❯ ", nil
		},
	}
	pane := "claude-rate-limit-prose-pane"
	for i := 0; i < 3; i++ {
		if lim, _, _ := c.RateLimited(pane); lim {
			t.Fatal("normal prose mentioning rate limits must not become material")
		}
	}
}

func TestClaudeRateLimitScrollbackNotMaterial(t *testing.T) {
	c := claudeCode{
		capturePane: func(string) (string, error) {
			var lines []string
			for i := 0; i < 30; i++ {
				lines = append(lines, "old")
			}
			lines = append(lines, "Server is temporarily limiting requests")
			for i := 0; i < 10; i++ {
				lines = append(lines, "recovered")
			}
			return stringsJoinLines(lines) + "\n❯ ", nil
		},
	}
	pane := "claude-rate-limit-scrollback-pane"
	for i := 0; i < 3; i++ {
		if lim, _, _ := c.RateLimited(pane); lim {
			t.Fatal("scrollback-only banner must never become material")
		}
	}
}

func stringsJoinLines(lines []string) string {
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}

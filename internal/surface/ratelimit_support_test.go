package surface

import "testing"

func TestRateLimitSupport(t *testing.T) {
	if _, ok := RateLimitSupport(newClaudeCode()); !ok {
		t.Error("claude-code should implement RateLimitProbe")
	}
	if _, ok := RateLimitSupport(newGrok()); !ok {
		t.Error("grok should implement RateLimitProbe")
	}
}

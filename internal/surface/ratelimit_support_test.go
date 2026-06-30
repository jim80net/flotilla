package surface

import "testing"

func TestRateLimitSupport(t *testing.T) {
	if _, ok := RateLimitSupport(newClaudeCode()); !ok {
		t.Error("claude-code should implement RateLimitProbe")
	}
	if _, ok := RateLimitSupport(newGrok()); !ok {
		t.Error("grok should implement RateLimitProbe")
	}
	if _, ok := RateLimitInstantSupport(newClaudeCode()); !ok {
		t.Error("claude-code should implement RateLimitInstantProbe")
	}
	if _, ok := RateLimitInstantSupport(newGrok()); !ok {
		t.Error("grok should implement RateLimitInstantProbe")
	}
}

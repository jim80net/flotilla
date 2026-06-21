package surface

import (
	"errors"
	"testing"
)

// The claude-code driver must satisfy the optional ResultReader capability (compile-time), so
// `flotilla result <claude-desk>` and the per-desk auto-mirror both read through one seam.
var _ ResultReader = claudeCode{}

func TestClaudeRegisteredResultReader(t *testing.T) {
	d, ok := Get("claude-code")
	if !ok {
		t.Fatal(`Get("claude-code") not registered`)
	}
	if _, ok := d.(ResultReader); !ok {
		t.Error("the registered claude-code driver does not implement ResultReader")
	}
}

func TestClaudeLatestResult(t *testing.T) {
	t.Run("substantive turn is returned", func(t *testing.T) {
		c := claudeCode{
			latestTurnText: func(pane string) (string, bool, error) {
				if pane != "flotilla:3.0" {
					t.Errorf("latestTurnText got pane %q, want the resolved pane", pane)
				}
				return "the turn-final report", true, nil
			},
		}
		got, err := c.LatestResult("flotilla:3.0")
		if err != nil || got != "the turn-final report" {
			t.Errorf("LatestResult = (%q, %v), want the turn-final", got, err)
		}
	})
	t.Run("no substantive turn → clear error (not empty output)", func(t *testing.T) {
		c := claudeCode{latestTurnText: func(string) (string, bool, error) { return "", false, nil }}
		if _, err := c.LatestResult("p"); err == nil {
			t.Error("LatestResult with no substantive turn must error, not return empty")
		}
	})
	t.Run("cwd-resolution error propagates", func(t *testing.T) {
		boom := errors.New("tmux boom")
		c := claudeCode{latestTurnText: func(string) (string, bool, error) { return "", false, boom }}
		if _, err := c.LatestResult("p"); !errors.Is(err, boom) {
			t.Errorf("err = %v, want the propagated cwd-resolution error", err)
		}
	})
}

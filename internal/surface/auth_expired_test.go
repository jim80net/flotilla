package surface

import (
	"errors"
	"testing"

	"github.com/jim80net/flotilla/internal/deliver"
)

func TestClaudeAssessAuthExpiredRequiresCursorProvenance(t *testing.T) {
	const frame = "prior conversation\nLogin expired - Please run /login\nfooter"
	base := claudeCode{
		paneCommand: func(string) (string, error) { return "node", nil },
		isShell:     func(string) bool { return false },
		capturePane: func(string) (string, error) { return frame, nil },
		parseBusyAt: deliver.ParseBusyAt,
	}

	t.Run("cursor-vouched dialog", func(t *testing.T) {
		c := base
		c.cursorState = func(string) (int, bool, error) { return 1, false, nil }
		if got := c.Assess("pane"); got != StateAuthExpired {
			t.Fatalf("Assess = %v, want auth-expired", got)
		}
	})
	t.Run("quoted history", func(t *testing.T) {
		c := base
		c.cursorState = func(string) (int, bool, error) { return 2, false, nil }
		if got := c.Assess("pane"); got == StateAuthExpired {
			t.Fatalf("history-only marker classified %v", got)
		}
	})
	t.Run("cursor unavailable", func(t *testing.T) {
		c := base
		c.cursorState = func(string) (int, bool, error) { return 0, false, errors.New("cursor unavailable") }
		if got := c.Assess("pane"); got == StateAuthExpired {
			t.Fatalf("unvouched marker classified %v", got)
		}
	})
	t.Run("copy mode", func(t *testing.T) {
		c := base
		c.cursorState = func(string) (int, bool, error) { return 1, true, nil }
		if got := c.Assess("pane"); got == StateAuthExpired {
			t.Fatalf("copy-mode marker classified %v", got)
		}
	})
}

func TestAuthExpiredStateLabel(t *testing.T) {
	if got := StateAuthExpired.String(); got != "auth-expired" {
		t.Fatalf("StateAuthExpired.String = %q", got)
	}
}

func TestClaudeAuthRecoveryRequiresCursorVouchedClearedComposer(t *testing.T) {
	const expired = "prior conversation\nLogin expired - Please run /login\nfooter"
	c := claudeCode{
		paneCommand: func(string) (string, error) { return "node", nil },
		isShell:     func(string) bool { return false },
		capturePane: func(string) (string, error) { return expired, nil },
		parseBusyAt: deliver.ParseBusyAt,
		cursorState: func(string) (int, bool, error) { return 0, false, errors.New("cursor unavailable") },
	}
	var observation AuthObservation
	state := AssessForFleetAuth(c, "pane", func(got AuthObservation) bool { observation = got; return true })
	if state != StateAuthExpired || observation != AuthUndetermined {
		t.Fatalf("degraded unchanged expiry = (%v, %v), want retained auth-expired+undetermined evidence", state, observation)
	}

	const healthy = "prior conversation\n❯ \nfooter"
	c.capturePane = func(string) (string, error) { return healthy, nil }
	c.cursorState = func(string) (int, bool, error) { return 1, false, nil }
	state = AssessForFleetAuth(c, "pane", func(got AuthObservation) bool { observation = got; return false })
	if state != StateIdle || observation != AuthRecovered {
		t.Fatalf("cursor-vouched cleared composer = (%v, %v), want idle+recovered", state, observation)
	}
}

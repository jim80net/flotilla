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

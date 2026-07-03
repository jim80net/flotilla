package watch

import (
	"testing"

	"github.com/jim80net/flotilla/internal/delegatenudge"
	"github.com/jim80net/flotilla/internal/idlehold"
)

// Smoke-test coordinator detectors against codex turn-finals sourced from live ~/.codex
// rollouts (2026-07-03, gpt-5.5 default, ChatGPT auth). Fixtures are the agent_message bodies
// read by readDeskTurnFinal via codexstore.LatestResult — not pane chrome.
func TestCodexCoordinatorDetectorSmoke(t *testing.T) {
	const surface = "codex"

	t.Run("short PONG reply is not IC-ing", func(t *testing.T) {
		// rollout-2026-07-03T01-46-53 agent_message "PONG"
		text := "PONG"
		if r := delegatenudge.Check(text, surface); r.InlineBuild {
			t.Fatalf("delegatenudge.Check(PONG) InlineBuild=true signal=%q", r.Signal)
		}
		if r := idlehold.Check(text); r.IdleHold {
			t.Fatalf("idlehold.Check(PONG) IdleHold=true signal=%q", r.Signal)
		}
	})

	t.Run("executive brief with dispatch is not IC-ing", func(t *testing.T) {
		text := "Executive summary: the options fix shipped.\n\n" +
			"I routed the migration to @backend via flotilla send — no action on your side."
		if r := delegatenudge.Check(text, surface); r.InlineBuild {
			t.Fatalf("delegation carve-out failed: signal=%q", r.Signal)
		}
	})

	t.Run("hands-on IC turn-finals nudge on codex coordinator", func(t *testing.T) {
		text := "I implemented the probe guard in workspace.go and opened PR #265 — CI is green."
		r := delegatenudge.Check(text, surface)
		if !r.InlineBuild {
			t.Fatal("want InlineBuild on coordinator IC prose")
		}
		if !delegatenudge.IsManagementHarness(surface) {
			t.Fatal("codex must be a management harness")
		}
	})

	t.Run("idle-hold antipattern on codex coordinator turn-final", func(t *testing.T) {
		text := "The fixtures are ready. Holding for your call on whether to push to the branch."
		r := idlehold.Check(text)
		if !r.IdleHold {
			t.Fatal("want IdleHold on authorized-work permission-seek")
		}
	})
}

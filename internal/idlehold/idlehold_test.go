package idlehold

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestCheck_IdleHoldSignals(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		signal string
	}{
		{"holding for your call", "Done the analysis. Holding for your call on next steps.", "holding-for-call"},
		{"waiting on you", "The PR is ready. Waiting on you to merge.", "waiting-for-operator"},
		{"say the word", "My recommendation is merge. Say the word and I'll push.", "say-the-word"},
		{"want me or leave", "Want me to resume the fleet, or leave it quiet?", "want-me-or-leave"},
		{"only thing waiting", "The only thing waiting on you is whether to run tests.", "only-thing-waiting"},
		{"your call end", "All gates green.\n\nYour call.", "your-call-nondecision"},
		{"wait-only wake", "I'll check back in 10 minutes once you're ready — holding for your response.", "wait-only-wake"},
		{"standing by", "Tests are green. Standing by for your go-ahead.", "standing-by"},
		{"awaiting go-ahead", "Awaiting your go-ahead to merge.", "awaiting-go-ahead"},
		{"let me know proceed", "Let me know how you'd like to proceed.", "let-me-know-proceed"},
		{"ready when you are", "PR is ready. Ready when you are.", "ready-when-you-are"},
		{"pending your input", "Pending your input on the next step.", "pending-your-input"},
		{"holding pattern", "In a holding pattern until you respond.", "holding-pattern"},
		{"should i proceed", "Should I proceed with the merge?", "should-i-proceed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Check(tc.text)
			if !r.IdleHold {
				t.Fatalf("Check(%q).IdleHold = false, want true", tc.text)
			}
			if r.Signal != tc.signal {
				t.Errorf("Signal = %q, want %q", r.Signal, tc.signal)
			}
		})
	}
}

func TestCheck_GenuineDecisionCarveOuts(t *testing.T) {
	cases := []string{
		"Holding for your call on the new metered API spend — decision-type: spend.",
		"Waiting on you: this is irreversible — delete production data.",
		"Two valid approaches with real tradeoffs — your call on the fork.",
		"Marking `[awaiting-auth]` flip the metered feed on @operator.",
		"Need your go-ahead on a not-yet-affirmed money spend for the paid probe.",
	}
	for _, text := range cases {
		if r := Check(text); r.IdleHold {
			t.Errorf("genuine decision must NOT be idle-hold: %q (signal %q)", text, r.Signal)
		}
	}
}

func TestCheck_BlockedLedgerCarveOut(t *testing.T) {
	text := "Blocked on the API key rotation. Marked `[blocked]` dependency on infra."
	if r := Check(text); r.IdleHold {
		t.Errorf("[blocked] ledger must NOT be idle-hold: signal %q", r.Signal)
	}
	text2 := "Recorded `[needs-attention]` waiting on the upstream schema change."
	if r := Check(text2); r.IdleHold {
		t.Errorf("[needs-attention] ledger must NOT be idle-hold: signal %q", r.Signal)
	}
}

func TestCheck_PastTenseNarrationNotIdleHold(t *testing.T) {
	cases := []string{
		"I was holding for the build; it finished, so I shipped the fix.",
		"We had been waiting on you but the gate cleared, so I merged.",
		"Previously I was holding for your call — no longer; PR #12 is merged.",
	}
	for _, text := range cases {
		if r := Check(text); r.IdleHold {
			t.Errorf("past-tense narration must NOT be idle-hold: %q (signal %q)", text, r.Signal)
		}
	}
}

func TestCheck_QuotedRuleMentionNotIdleHold(t *testing.T) {
	text := `Doctrine reminder: never end turns with "holding for your call" — I merged instead.`
	if r := Check(text); r.IdleHold {
		t.Errorf("quoted rule mention must NOT be idle-hold: signal %q", r.Signal)
	}
}

func TestCheck_AuthorizedWorkNotIdleHold(t *testing.T) {
	text := "Merged the PR, ran tests, filed follow-up #42. Next: implement the detector."
	if r := Check(text); r.IdleHold {
		t.Errorf("acting turn must not be idle-hold: signal %q", r.Signal)
	}
}

func TestCheck_ExtractsRecommendation(t *testing.T) {
	text := "My recommendation is merge PR #12 now. Say the word and I'll do it."
	r := Check(text)
	if !r.IdleHold {
		t.Fatal("want idle-hold")
	}
	if !strings.Contains(r.Recommendation, "merge PR #12") {
		t.Errorf("Recommendation = %q, want merge PR #12", r.Recommendation)
	}
}

func TestTracker_ConsecutiveStrikes(t *testing.T) {
	tr := NewTracker()
	hold := Check("Holding for your call.")
	if tr.Record("backend", hold) {
		t.Fatal("first strike must not meet threshold")
	}
	if tr.Strikes("backend") != 1 {
		t.Fatalf("strikes = %d, want 1", tr.Strikes("backend"))
	}
	if !tr.Record("backend", hold) {
		t.Fatal("second strike must meet threshold")
	}
	if tr.Strikes("backend") != 0 {
		t.Fatalf("strikes must reset after threshold fires, got %d", tr.Strikes("backend"))
	}
}

func TestTracker_NonMatchPreservesStrikes(t *testing.T) {
	tr := NewTracker()
	hold := Check("Waiting on you.")
	tr.Record("backend", hold)
	// A non-match that slipped past detection must NOT zero strikes.
	tr.Record("backend", Check("Merged the PR and ran tests."))
	if tr.Strikes("backend") != 1 {
		t.Fatalf("non-match must preserve strikes, got %d", tr.Strikes("backend"))
	}
	// Second hold still reaches threshold.
	if !tr.Record("backend", hold) {
		t.Fatal("second hold after missed detect must meet threshold")
	}
}

func TestTracker_ConcurrentRecordRace(t *testing.T) {
	tr := NewTracker()
	hold := Check("Holding for your call.")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			agent := fmt.Sprintf("desk-%d", n%5)
			tr.Record(agent, hold)
			_ = tr.Strikes(agent)
		}(i)
	}
	wg.Wait()
}

func TestBreakPrompt_IncludesRecommendation(t *testing.T) {
	p := BreakPrompt("merge PR #12")
	if !strings.Contains(p, "merge PR #12") {
		t.Errorf("break prompt missing recommendation: %q", p)
	}
	if !strings.Contains(p, "spend / irreversible / fork") {
		t.Error("break prompt missing decision-type escalation instruction")
	}
}

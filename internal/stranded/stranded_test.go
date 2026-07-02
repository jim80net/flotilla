package stranded

import (
	"strings"
	"testing"
)

func TestCheck_DroppedGateReport(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{
			name: "PR fix done idle no COS surface (#245 class)",
			text: "PR #245 cubic P2 fixes pushed. CI green. My work here is done.\n\nidle",
		},
		{
			name: "merge-ready no self-merge without report",
			text: "Trio complete, cubic clean on head. Ready for COS merge gate. No self-merge.\n\nNothing further from me.",
		},
		{
			name: "open cubic finding settled (#247 P3 class)",
			text: "Fix round pushed 350a1e5. Cubic re-run posted NEW P3 unresolved on TestDetector.\n\nidle",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Check(tc.text)
			if !r.Stranded {
				t.Fatalf("Check must detect stranded handoff: %q", tc.name)
			}
		})
	}
}

func TestCheck_PlannedSendStillStranded(t *testing.T) {
	text := "CI green on head. Ready for COS merge gate. No self-merge.\n" +
		"Next: run flotilla send --no-mirror cos with head SHA and CI status.\n\nidle"
	r := Check(text)
	if !r.Stranded {
		t.Fatal("turn-final that only PLANS flotilla send must still be stranded")
	}
	if r.Signal != "gate-obligation-unreported" {
		t.Fatalf("signal = %q, want gate-obligation-unreported", r.Signal)
	}
}

func TestCheck_OpenFindingsResolvedVsUnresolved(t *testing.T) {
	resolved := "Fix round pushed. Cubic re-run: NEW P3 — FIXED in abc123. No self-merge. Ready for COS merge.\n\nidle"
	r := Check(resolved)
	if r.Signal == "open-findings-settled" {
		t.Fatal("resolved finding must not false-trigger open-findings signal")
	}
	if !r.Stranded || r.Signal != "gate-obligation-unreported" {
		t.Fatalf("resolved finding + gate obligation want gate-obligation-unreported, got %+v", r)
	}

	unresolved := "NEW P2 unresolved on grok.go.\n\nidle"
	r = Check(unresolved)
	if !r.Stranded || r.Signal != "open-findings-settled" {
		t.Fatalf("unresolved finding + idle want open-findings-settled, got %+v", r)
	}
}

func TestCheck_GateReportedNotStranded(t *testing.T) {
	cases := []string{
		"Pushed c3432da. Surfaced to COS via flotilla send — turn confirmed. Ready for merge gate.",
		"Gate report posted on PR #247. Delivered to cos. CI green, cubic 0 unresolved.",
		"gh pr comment 247 with COS re-gate. No self-merge — COS owns merge.",
		"Reported to COS for the merge gate (it's theirs). Work done on my side.",
	}
	for _, text := range cases {
		if r := Check(text); r.Stranded {
			t.Errorf("reported gate work must NOT be stranded: %q (signal %q)", text[:min(60, len(text))], r.Signal)
		}
	}
}

func TestCheck_RoutineWorkNotStranded(t *testing.T) {
	cases := []string{
		"Implemented the helper, tests pass, opening PR for review.",
		"Merged locally, running go test ./...",
		"Surfaced synthesis to my channel. Nothing needs the operator.",
	}
	for _, text := range cases {
		if r := Check(text); r.Stranded {
			t.Errorf("routine work must NOT be stranded: %q", text)
		}
	}
}

func TestTracker_FiresOnFirstStrike(t *testing.T) {
	tr := NewTracker()
	r := Check("CI green. No self-merge. Ready for COS gate. idle")
	if !r.Stranded {
		t.Fatal("want stranded verdict for test")
	}
	if !tr.Record("flotilla-dev", r) {
		t.Fatal("stranded handoff must fire on first strike")
	}
}

func TestNudgePrompt_MentionsGateHolder(t *testing.T) {
	p := NudgePrompt("flotilla-dev")
	if !strings.Contains(p, "COS") && !strings.Contains(p, "gate-holder") {
		t.Errorf("nudge must name gate-holder report obligation: %q", p)
	}
	if !strings.Contains(p, "flotilla send") {
		t.Error("nudge must mention flotilla send")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

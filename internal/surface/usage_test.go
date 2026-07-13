package surface

import (
	"errors"
	"testing"
)

// grokUsageFixture is normalized from a LIVE CAPTURE of `/usage show` on the
// official Grok CLI 0.2.93 (f00f96316d), 2026-07-13. The numeric value is
// deliberately synthetic; the marker spelling and row shape are verbatim.
// The marker and percent-used field were SOURCE-REVERIFIED in the official
// 0.2.99 binary (b1b49ccb71); this is not a claim of a live 0.2.99 capture.
const grokUsageFixture = "     Weekly limit: 92%"

func TestUsageSupportIsOptional(t *testing.T) {
	if _, ok := UsageSupport(newGrok()); !ok {
		t.Fatal("grok must expose UsageProbe")
	}
	if _, ok := UsageSupport(newClaudeCode()); ok {
		t.Fatal("a driver without an authoritative usage source must not expose UsageProbe")
	}
}

func TestParseGrokWeeklyUsage(t *testing.T) {
	// Fixture provenance is documented on grokUsageFixture. xAI documents the
	// rendered percentage as used, so 92% used means 8% remaining to callers.
	used, ok := parseGrokWeeklyUsage(grokUsageFixture)
	if !ok || used != 92 {
		t.Fatalf("parseGrokWeeklyUsage = (%d, %v), want (92, true)", used, ok)
	}
}

func TestGrokUsageConvertsUsedToRemaining(t *testing.T) {
	// Fixture provenance is documented on grokUsageFixture; pane name is generic.
	g := newGrok()
	g.capturePane = func(pane string) (string, error) {
		if pane != "alpha" {
			t.Fatalf("pane = %q, want alpha", pane)
		}
		return grokUsageFixture, nil
	}

	report, ok := g.Usage("alpha")
	if !ok {
		t.Fatal("Usage returned honest absence for authoritative chrome")
	}
	if report.RemainingPercent != 8 || report.Window != "weekly" || report.Scope != RateLimitAccountSide {
		t.Fatalf("Usage = %+v, want remaining=8 window=weekly scope=account-side", report)
	}
}

func TestParseGrokWeeklyUsageRejectsNonEvidence(t *testing.T) {
	cases := []struct {
		name     string
		captured string
	}{
		{
			name: "out of range",
			// SOURCE-VERIFIED marker shape from Grok 0.2.93 and 0.2.99, with a
			// synthetic impossible value to prove validation rejects malformed chrome.
			captured: "Weekly limit: 101%",
		},
		{
			name: "prose is not chrome",
			// SOURCE-VERIFIED 0.2.93--0.2.99 marker words embedded in synthetic
			// prose; the line anchor must reject this scrollback/output lookalike.
			captured: "alpha notes that Weekly limit: 92% is shown elsewhere",
		},
		{
			name: "stale row outside live tail",
			// First row is the normalized LIVE-CAPTURED 0.2.93 marker; the
			// following generic rows move it outside grokTail bottom chrome.
			captured: "Weekly limit: 92%\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15",
		},
		{
			name: "old proposed wording is not current chrome",
			// This synthetic pre-capture proposal is intentionally unsupported;
			// the live 0.2.93 and source-verified-through-0.2.99 row is
			// `Weekly limit: N%`.
			captured: "Weekly limit left: 8%",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if used, ok := parseGrokWeeklyUsage(tc.captured); ok {
				t.Fatalf("parseGrokWeeklyUsage = (%d, true), want honest absence", used)
			}
		})
	}
}

func TestGrokUsageCaptureFailureIsAbsence(t *testing.T) {
	g := newGrok()
	g.capturePane = func(string) (string, error) { return "", errors.New("capture failed") }
	if report, ok := g.Usage("beta"); ok || report != (UsageReport{}) {
		t.Fatalf("Usage(capture failure) = (%+v, %v), want zero report and false", report, ok)
	}
}

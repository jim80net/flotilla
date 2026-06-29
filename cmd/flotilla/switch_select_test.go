package main

import (
	"testing"
	"time"
)

// These tests pin the PURE failover-target SELECTION + cap/poison bookkeeping
// for the auto-switch path (P0 task group 5). They exercise NO I/O — every
// input (the chain, the poison state, the scope, "now", the prior switch
// timestamps) is passed as an argument, so the functions are deterministic and
// independently unit-testable. The live RateLimitScope + the on-disk overlay
// land in P1/P2; this file decouples from internal/surface and internal/launch
// to stay pure (see selectFailoverTarget's doc seam).

// chainFixture returns a representative claude→grok→opencode failover chain with
// a SECOND anthropic slot (a different subscription_id, SAME provider) inserted
// to prove provider-vs-subscription discrimination.
func chainFixture() []switchSlot {
	return []switchSlot{
		{Slot: "primary", Surface: "claude-code", Provider: "anthropic", SubscriptionID: "anthropic-work"},
		{Slot: "fallback-0", Surface: "claude-code", Provider: "anthropic", SubscriptionID: "anthropic-personal"},
		{Slot: "fallback-1", Surface: "grok", Provider: "xai", SubscriptionID: "xai-default"},
		{Slot: "fallback-2", Surface: "opencode", Provider: "zai", SubscriptionID: "zai-default"},
	}
}

// 5.1 — server-side anthropic poison ⇒ selection picks the first fallback whose
// provider ∉ poisoned (the grok slot), and NEVER the anthropic-personal slot
// (different subscription_id but the SAME poisoned provider).
func TestSelectFailoverTarget_ServerSidePoisonsWholeProvider(t *testing.T) {
	chain := chainFixture()
	poisoned := PoisonState{Providers: map[string]bool{"anthropic": true}}

	target, ok := selectFailoverTarget(chain, poisoned, RateLimitServerSide)
	if !ok {
		t.Fatalf("expected a viable target, got ok=false")
	}
	if target.Provider == "anthropic" {
		t.Fatalf("server-side poison must skip ALL anthropic slots (incl. a different subscription_id); got slot %q provider %q sub %q", target.Slot, target.Provider, target.SubscriptionID)
	}
	if target.Slot != "fallback-1" || target.Provider != "xai" {
		t.Fatalf("expected the grok slot fallback-1/xai, got slot %q provider %q", target.Slot, target.Provider)
	}
}

// 5.2 — account-side poison ⇒ a same-provider alternate subscription_id is
// preferred BEFORE crossing providers. Here the primary's subscription
// (anthropic-work) is poisoned account-side, but anthropic-personal (same
// provider, different sub) is still healthy, so it MUST be chosen over the
// cross-provider grok slot.
func TestSelectFailoverTarget_AccountSidePrefersSameProviderAlternate(t *testing.T) {
	chain := chainFixture()
	poisoned := PoisonState{Subscriptions: map[string]bool{"anthropic-work": true}}

	target, ok := selectFailoverTarget(chain, poisoned, RateLimitAccountSide)
	if !ok {
		t.Fatalf("expected a viable target, got ok=false")
	}
	if target.Slot != "fallback-0" || target.Provider != "anthropic" || target.SubscriptionID != "anthropic-personal" {
		t.Fatalf("account-side poison must prefer the same-provider alternate subscription; got slot %q provider %q sub %q", target.Slot, target.Provider, target.SubscriptionID)
	}
}

// 5.2b — account-side poison with NO same-provider alternate ⇒ fall through to a
// different provider (the design's step 3 "else fall through to a different
// provider as in (2)").
func TestSelectFailoverTarget_AccountSideCrossesWhenNoInProviderAlternate(t *testing.T) {
	// A chain whose only anthropic slot is the poisoned one, plus a grok fallback.
	chain := []switchSlot{
		{Slot: "primary", Surface: "claude-code", Provider: "anthropic", SubscriptionID: "anthropic-work"},
		{Slot: "fallback-0", Surface: "grok", Provider: "xai", SubscriptionID: "xai-default"},
	}
	poisoned := PoisonState{Subscriptions: map[string]bool{"anthropic-work": true}}

	target, ok := selectFailoverTarget(chain, poisoned, RateLimitAccountSide)
	if !ok {
		t.Fatalf("expected a cross-provider target, got ok=false")
	}
	if target.Slot != "fallback-0" || target.Provider != "xai" {
		t.Fatalf("expected to cross to the grok slot when no in-provider alternate remains; got slot %q provider %q", target.Slot, target.Provider)
	}
}

// 5.3 (P1-D) — ALL providers poisoned ⇒ the function refuses (ok=false), so the
// caller can REFUSE before committing any handoff. It NEVER returns a poisoned
// slot.
func TestSelectFailoverTarget_AllProvidersPoisonedRefuses(t *testing.T) {
	chain := chainFixture()
	poisoned := PoisonState{Providers: map[string]bool{
		"anthropic": true,
		"xai":       true,
		"zai":       true,
	}}

	target, ok := selectFailoverTarget(chain, poisoned, RateLimitServerSide)
	if ok {
		t.Fatalf("all providers poisoned must refuse (ok=false); got a target slot %q provider %q", target.Slot, target.Provider)
	}
	if target != (switchSlot{}) {
		t.Fatalf("a refusal must return the zero slot, not a poisoned one; got %+v", target)
	}
}

// 5.3b — account-side where every remaining subscription's provider is poisoned
// also refuses (never returns a poisoned-provider slot to satisfy an
// account-side miss).
func TestSelectFailoverTarget_AccountSideAllPoisonedRefuses(t *testing.T) {
	chain := []switchSlot{
		{Slot: "primary", Surface: "claude-code", Provider: "anthropic", SubscriptionID: "anthropic-work"},
		{Slot: "fallback-0", Surface: "grok", Provider: "xai", SubscriptionID: "xai-default"},
	}
	poisoned := PoisonState{
		Subscriptions: map[string]bool{"anthropic-work": true},
		Providers:     map[string]bool{"xai": true},
	}

	_, ok := selectFailoverTarget(chain, poisoned, RateLimitAccountSide)
	if ok {
		t.Fatalf("no healthy alternate remains; must refuse (ok=false)")
	}
}

// --- 5.4 (P1-D) cap bookkeeping ---

// 5.4 — at most 3 auto-switches/desk/hour; the 4th within the window is refused
// with a "cap-exhausted" signal.
func TestSwitchCap_FourthWithinWindowRefused(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	window := time.Hour
	// Three prior auto-switches inside the window.
	prior := []time.Time{
		now.Add(-50 * time.Minute),
		now.Add(-30 * time.Minute),
		now.Add(-5 * time.Minute),
	}

	dec := switchCapDecision(prior, now, window, defaultAutoSwitchCap, false)
	if dec.Allowed {
		t.Fatalf("the 4th auto-switch within the window must be refused")
	}
	if !dec.CapExhausted {
		t.Fatalf("a refused-within-window decision must carry the cap-exhausted signal")
	}
}

// 5.4 — the 3rd is allowed (the cap is at-most-3, so the 4th is the first
// refusal); count is in-window only.
func TestSwitchCap_ThirdWithinWindowAllowed(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	window := time.Hour
	prior := []time.Time{
		now.Add(-40 * time.Minute),
		now.Add(-10 * time.Minute),
	}

	dec := switchCapDecision(prior, now, window, defaultAutoSwitchCap, false)
	if !dec.Allowed {
		t.Fatalf("the 3rd auto-switch within the window must be allowed (cap is 3)")
	}
	if dec.CapExhausted {
		t.Fatalf("an allowed decision must not signal cap-exhausted")
	}
}

// 5.4 — timestamps OUTSIDE the rolling window do not count against the cap.
func TestSwitchCap_StaleTimestampsDoNotCount(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	window := time.Hour
	prior := []time.Time{
		now.Add(-3 * time.Hour),    // stale
		now.Add(-2 * time.Hour),    // stale
		now.Add(-90 * time.Minute), // stale
		now.Add(-20 * time.Minute), // in-window
	}

	dec := switchCapDecision(prior, now, window, defaultAutoSwitchCap, false)
	if !dec.Allowed {
		t.Fatalf("only 1 switch is in-window; the 2nd must be allowed")
	}
	if dec.InWindowCount != 1 {
		t.Fatalf("expected InWindowCount=1 (stale ones excluded), got %d", dec.InWindowCount)
	}
}

// 5.4 — the cap-crossing EDGE fires the notify exactly once: the decision that
// FIRST crosses from allowed to refused carries CapJustExhausted; a subsequent
// refused decision does not.
func TestSwitchCap_NotifyOnceOnCrossingEdge(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	window := time.Hour

	// At the edge: exactly cap switches in-window ⇒ this 4th is the first refusal.
	atEdge := []time.Time{
		now.Add(-50 * time.Minute),
		now.Add(-30 * time.Minute),
		now.Add(-5 * time.Minute),
	}
	edge := switchCapDecision(atEdge, now, window, defaultAutoSwitchCap, false)
	if edge.Allowed || !edge.CapExhausted || !edge.CapJustExhausted {
		t.Fatalf("the cap-crossing edge must be refused, cap-exhausted, AND just-exhausted (notify once); got %+v", edge)
	}

	// Beyond the edge: cap+1 in-window ⇒ still refused but NOT the crossing edge.
	beyond := []time.Time{
		now.Add(-55 * time.Minute),
		now.Add(-50 * time.Minute),
		now.Add(-30 * time.Minute),
		now.Add(-5 * time.Minute),
	}
	past := switchCapDecision(beyond, now, window, defaultAutoSwitchCap, false)
	if past.Allowed || !past.CapExhausted {
		t.Fatalf("beyond the edge stays refused + cap-exhausted; got %+v", past)
	}
	if past.CapJustExhausted {
		t.Fatalf("the notify must fire ONLY on the crossing edge, not on every subsequent refusal")
	}
}

// 5.4 — operator-FORCED switches are UNCAPPED: forced bypasses the cap even with
// the window saturated.
func TestSwitchCap_ForcedBypassesCap(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	window := time.Hour
	saturated := []time.Time{
		now.Add(-50 * time.Minute),
		now.Add(-30 * time.Minute),
		now.Add(-20 * time.Minute),
		now.Add(-5 * time.Minute),
	}

	dec := switchCapDecision(saturated, now, window, defaultAutoSwitchCap, true /*forced*/)
	if !dec.Allowed {
		t.Fatalf("operator-forced switches are uncapped; must be allowed even when saturated")
	}
	if dec.CapExhausted || dec.CapJustExhausted {
		t.Fatalf("a forced bypass must not raise the cap-exhausted/just-exhausted signals; got %+v", dec)
	}
}

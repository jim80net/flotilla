package main

import (
	"testing"
	"time"
)

func TestProviderCooldown_ServerSidePoisonsProvider(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	var s providerCooldownStore
	s.Cooldowns = map[string]cooldownEntry{}

	s.recordStormReport("anthropic", "anthropic-work", RateLimitServerSide, now)
	s.recordStormReport("anthropic", "anthropic-personal", RateLimitServerSide, now.Add(time.Minute))

	ps := s.activePoison(now.Add(2 * time.Minute))
	if !ps.Providers["anthropic"] {
		t.Fatalf("expected anthropic provider poisoned, got %+v", ps)
	}
	if len(ps.Subscriptions) != 0 {
		t.Fatalf("server-side storm must not poison subscriptions, got %+v", ps)
	}
}

func TestProviderCooldown_AccountSidePoisonsSubscriptionOnly(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	var s providerCooldownStore
	s.Cooldowns = map[string]cooldownEntry{}

	s.recordStormReport("anthropic", "anthropic-work", RateLimitAccountSide, now)
	s.recordStormReport("anthropic", "anthropic-work", RateLimitAccountSide, now.Add(time.Minute))

	ps := s.activePoison(now.Add(2 * time.Minute))
	if ps.Providers["anthropic"] {
		t.Fatalf("account-side must not poison whole provider, got %+v", ps)
	}
	if !ps.Subscriptions["anthropic-work"] {
		t.Fatalf("expected subscription poisoned, got %+v", ps)
	}
}

func TestProviderCooldown_ExpiresAfterCooldown(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	var s providerCooldownStore
	s.Cooldowns = map[string]cooldownEntry{}
	second := now.Add(time.Minute)
	s.recordStormReport("anthropic", "", RateLimitServerSide, now)
	s.recordStormReport("anthropic", "", RateLimitServerSide, second)
	ps := s.activePoison(second.Add(serverSideCooldownDur + time.Second))
	if len(ps.Providers) != 0 {
		t.Fatalf("poison should expire after cooldown, got %+v", ps)
	}
}

func TestProviderCooldown_PruneOldReports(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	var s providerCooldownStore
	s.Reports = []stormReport{
		{Key: "anthropic", Scope: cooldownScopeServerSide, At: now.Add(-stormReportWindow - time.Minute)},
	}
	s.pruneReports(now)
	if len(s.Reports) != 0 {
		t.Fatalf("old reports must be pruned, got %d", len(s.Reports))
	}
}

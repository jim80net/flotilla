package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Storm-cooldown defaults (design §4.3 / tasks §9). Tunable later via host config;
// the pure record/lookup helpers take explicit inputs for unit tests.
const (
	stormReportThreshold   = 2
	stormReportWindow      = 10 * time.Minute
	serverSideCooldownDur  = 30 * time.Minute
	accountSideCooldownDur = 15 * time.Minute
)

type cooldownScope string

const (
	cooldownScopeServerSide  cooldownScope = "server-side"
	cooldownScopeAccountSide cooldownScope = "account-side"
)

type stormReport struct {
	Key      string        `json:"key"`
	Scope    cooldownScope `json:"scope"`
	Provider string        `json:"provider,omitempty"`
	At       time.Time     `json:"at"`
}

type cooldownEntry struct {
	Scope         cooldownScope `json:"scope"`
	CooldownUntil time.Time     `json:"cooldown_until"`
	DesksSeen     int           `json:"desks_seen"`
}

type providerCooldownStore struct {
	Reports   []stormReport            `json:"reports,omitempty"`
	Cooldowns map[string]cooldownEntry `json:"cooldowns,omitempty"`
}

func providerCooldownsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".flotilla", "provider-cooldowns.json"), nil
}

func loadProviderCooldowns() (providerCooldownStore, error) {
	path, err := providerCooldownsPath()
	if err != nil {
		return providerCooldownStore{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return providerCooldownStore{Cooldowns: map[string]cooldownEntry{}}, nil
		}
		return providerCooldownStore{}, fmt.Errorf("read %q: %w", path, err)
	}
	var s providerCooldownStore
	if err := json.Unmarshal(raw, &s); err != nil {
		return providerCooldownStore{}, fmt.Errorf("parse %q: %w", path, err)
	}
	if s.Cooldowns == nil {
		s.Cooldowns = map[string]cooldownEntry{}
	}
	return s, nil
}

func saveProviderCooldowns(s providerCooldownStore) error {
	path, err := providerCooldownsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *providerCooldownStore) pruneReports(now time.Time) {
	cutoff := now.Add(-stormReportWindow)
	kept := s.Reports[:0]
	for _, r := range s.Reports {
		if r.At.After(cutoff) {
			kept = append(kept, r)
		}
	}
	s.Reports = kept
}

// recordStormReport appends a material throttle observation and poisons the provider
// (server-side) or subscription bucket (account-side) when ≥stormReportThreshold
// reports land inside stormReportWindow. Poison gates failover TARGET selection only —
// it does not gate whether the detector enqueues a switch (that is the probe's 2-consecutive
// debounce + per-episode edge in rateLimitMaterialFromPendingLocked).
func (s *providerCooldownStore) recordStormReport(provider, subscription string, scope RateLimitScope, now time.Time) {
	s.pruneReports(now)
	var key string
	var cs cooldownScope
	var until time.Duration
	switch scope {
	case RateLimitServerSide:
		key = provider
		cs = cooldownScopeServerSide
		until = serverSideCooldownDur
	case RateLimitAccountSide:
		key = subscription
		cs = cooldownScopeAccountSide
		until = accountSideCooldownDur
	default:
		return
	}
	if key == "" {
		return
	}
	s.Reports = append(s.Reports, stormReport{Key: key, Scope: cs, Provider: provider, At: now})
	count := 0
	for _, r := range s.Reports {
		if r.Key == key && r.Scope == cs {
			count++
		}
	}
	if count >= stormReportThreshold {
		if s.Cooldowns == nil {
			s.Cooldowns = map[string]cooldownEntry{}
		}
		s.Cooldowns[key] = cooldownEntry{
			Scope:         cs,
			CooldownUntil: now.Add(until),
			DesksSeen:     count,
		}
	}
}

func (s providerCooldownStore) activePoison(now time.Time) PoisonState {
	ps := PoisonState{
		Providers:     map[string]bool{},
		Subscriptions: map[string]bool{},
	}
	for key, e := range s.Cooldowns {
		if !e.CooldownUntil.After(now) {
			continue
		}
		switch e.Scope {
		case cooldownScopeServerSide:
			ps.Providers[key] = true
		case cooldownScopeAccountSide:
			ps.Subscriptions[key] = true
		}
	}
	return ps
}

func recordProviderStorm(provider, subscription string, scope RateLimitScope, now time.Time) (PoisonState, error) {
	s, err := loadProviderCooldowns()
	if err != nil {
		return PoisonState{}, err
	}
	s.recordStormReport(provider, subscription, scope, now)
	if err := saveProviderCooldowns(s); err != nil {
		return PoisonState{}, err
	}
	return s.activePoison(now), nil
}

func loadActivePoison(now time.Time) (PoisonState, error) {
	s, err := loadProviderCooldowns()
	if err != nil {
		return PoisonState{}, err
	}
	return s.activePoison(now), nil
}

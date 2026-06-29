package main

import "time"

// switch_select holds the PURE decision logic for the auto-switch path: choosing
// a failover TARGET slot given a poison state + a rate-limit scope, and the
// per-desk switch-cap bookkeeping. Everything here is a total function of its
// arguments — NO filesystem, NO network, NO clock reads ("now", the prior switch
// timestamps, and the poison/cap state are all passed in). That keeps the
// fail-closed terminals (design §4 P1-D) unit-testable without a live pane and
// avoids the time-bomb-fixture trap (relative "now" is the caller's job, never a
// hardcoded calendar date inside the predicate).
//
// SEAM (decoupled from internal/surface on purpose): the live RateLimitScope and
// the on-disk active-harness.json overlay land in P1/P2. selectFailoverTarget
// takes a local RateLimitScope and a minimal switchSlot so this group is
// independently testable; P2 maps the real internal/surface.RateLimitScope and
// the resolved launch.Recipe chain onto these inputs. Keep this file pure when
// P2 wires it.

// RateLimitScope classifies WHY a desk was throttled, which determines the blast
// radius of the poison. It mirrors the semantics of the design's
// internal/surface.RateLimitScope (§4.1) but is declared locally to keep this
// selection logic free of the surface import (and thus pure/unit-testable).
type RateLimitScope int

const (
	// RateLimitServerSide is a provider-wide infra throttle (e.g. the Anthropic
	// 2026-06-29 event): every subscription under the provider is hit at once, so
	// failover MUST poison the WHOLE provider and cross to a different one. A
	// second slot under the same provider would not have helped.
	RateLimitServerSide RateLimitScope = iota
	// RateLimitAccountSide is a per-account/key throttle: only the offending
	// subscription_id is poisoned, so a same-provider alternate subscription is
	// preferred before crossing providers.
	RateLimitAccountSide
)

// switchSlot is the minimal projection of a failover-chain slot this selection
// needs. It is intentionally a LOCAL struct (not internal/launch.Recipe's
// in-flight slot type) so this function is pure and does not hard-depend on the
// sibling-in-progress launch schema. P2 maps a resolved chain slot onto this.
//
//   - Surface        — the registered driver name (claude-code, grok, …).
//   - Provider       — the logical provider (anthropic, xai, zai). LOAD-BEARING
//     for server-side failover: poison/selection is by provider, not surface.
//   - SubscriptionID — a billing/account bucket WITHIN a provider (NOT a secret).
//     LOAD-BEARING for account-side failover (poison one bucket, keep the rest).
//   - Slot           — the slot label ("primary", "fallback-N").
type switchSlot struct {
	Surface        string
	Provider       string
	SubscriptionID string
	Slot           string
}

// PoisonState records which providers and which subscription buckets are
// currently quarantined. A provider in Providers poisons EVERY slot under it
// (server-side blast radius); a subscription in Subscriptions poisons only that
// one bucket (account-side blast radius). Both maps are read-only here.
type PoisonState struct {
	Providers     map[string]bool // provider name → poisoned (server-side)
	Subscriptions map[string]bool // subscription_id → poisoned (account-side)
}

// providerPoisoned reports whether the slot's PROVIDER is quarantined.
func (p PoisonState) providerPoisoned(s switchSlot) bool {
	return p.Providers[s.Provider]
}

// subscriptionPoisoned reports whether the slot's SUBSCRIPTION bucket is
// quarantined.
func (p PoisonState) subscriptionPoisoned(s switchSlot) bool {
	return p.Subscriptions[s.SubscriptionID]
}

// healthy reports whether a slot is a viable TO target: neither its provider nor
// its subscription bucket is poisoned. A poisoned-provider slot is NEVER healthy
// even if its specific subscription is not individually listed (server-side hits
// the whole provider), and a poisoned-subscription slot is never healthy even if
// its provider is otherwise fine (account-side hits the one bucket).
func (p PoisonState) healthy(s switchSlot) bool {
	return !p.providerPoisoned(s) && !p.subscriptionPoisoned(s)
}

// selectFailoverTarget picks the failover TARGET slot for a throttled desk, or
// refuses (ok=false) when no viable target remains — the design's §4.2 selection
// + the P1-D "all providers poisoned ⇒ refuse" terminal.
//
// The chain is the FROM slot followed by the declared fallbacks, in priority
// order (the caller passes them already ordered: primary, fallback-0, …).
// Selection:
//
//   - ServerSide  — pick the FIRST slot (in chain order) whose provider is not
//     poisoned. The whole offending provider is already in poisoned.Providers,
//     so every slot under it (incl. a different subscription_id) is skipped.
//   - AccountSide — prefer a SAME-PROVIDER alternate first: scan for the first
//     healthy slot sharing the FROM slot's provider (a different, un-poisoned
//     subscription bucket); only if none remains in-provider do we fall through
//     to the first healthy slot under any other provider (as in the ServerSide
//     case). The FROM slot is chain[0].
//
// In both cases a slot is only chosen when PoisonState.healthy is true, so a
// poisoned slot is NEVER returned. When nothing is healthy the function returns
// the zero switchSlot and ok=false so the caller can refuse BEFORE committing any
// handoff (never commit a handoff you cannot land a takeover for).
//
// ok=false ⇒ REFUSE (the caller leaves the desk on its current harness + notifies
// the operator). This function is the fail-closed gate, not a best-effort picker.
func selectFailoverTarget(chain []switchSlot, poisoned PoisonState, scope RateLimitScope) (switchSlot, bool) {
	if scope == RateLimitAccountSide && len(chain) > 0 {
		fromProvider := chain[0].Provider
		// Prefer a healthy alternate WITHIN the same provider (a different,
		// un-poisoned subscription bucket) before crossing providers.
		for _, s := range chain {
			if s.Provider == fromProvider && poisoned.healthy(s) {
				return s, true
			}
		}
		// No in-provider alternate remains; fall through to the cross-provider
		// scan below (design §4.2 step 3 "else fall through ... as in (2)").
	}

	// ServerSide, or AccountSide with no in-provider alternate: the first healthy
	// slot in chain order, regardless of provider.
	for _, s := range chain {
		if poisoned.healthy(s) {
			return s, true
		}
	}

	// Nothing viable — refuse. The zero slot makes a misuse (ignoring ok) crash
	// loudly rather than switch to a phantom target.
	return switchSlot{}, false
}

// defaultAutoSwitchCap is the per-desk auto-switch cap inside the rolling window
// (design §4 P1-D: "Max-switches-per-desk-per-hour cap exhausted (3 auto)").
// Operator-forced switches bypass it.
const defaultAutoSwitchCap = 3

// SwitchCapDecision is the result of the per-desk cap check. It is a value
// (no I/O) so the caller decides what to do: Allowed ⇒ proceed; !Allowed ⇒ the
// defined stuck-state (desk stays put, auto-switch suppressed). CapJustExhausted
// is the cap-CROSSING edge — the caller fires the LOUD operator notification
// exactly ONCE on it (not every tick), per the P1-D "notify once on the
// cap-crossing edge" terminal.
type SwitchCapDecision struct {
	Allowed          bool // proceed with the auto-switch
	CapExhausted     bool // the window is at/over the cap (suppress further auto-switches)
	CapJustExhausted bool // THIS decision is the first refusal (fire the notify exactly once)
	InWindowCount    int  // prior in-window auto-switches counted against the cap
}

// switchCapDecision decides whether an auto-switch may proceed for a desk given
// its PRIOR auto-switch timestamps, the current time, the rolling window, the
// cap, and whether the operator FORCED this switch. It is pure: the caller reads
// the timestamps from durable state and passes "now" — no clock/filesystem read
// happens here (per the project's rule against untestable clock reads + time-bomb
// fixtures).
//
//   - forced ⇒ ALWAYS Allowed, uncapped, and it raises NEITHER the cap-exhausted
//     nor the just-exhausted signal (an operator override is not a stuck-state).
//   - otherwise: count the prior switches strictly inside the window
//     (now-window, now]. The cap is at-most-N: the (N+1)th attempt is the first
//     refusal. The crossing EDGE (exactly N in-window) sets CapJustExhausted so
//     the caller notifies once; further refusals (>N in-window) stay refused +
//     cap-exhausted but do NOT re-notify.
func switchCapDecision(prior []time.Time, now time.Time, window time.Duration, limit int, forced bool) SwitchCapDecision {
	if forced {
		// Operator override bypasses the cap entirely — never a stuck-state.
		return SwitchCapDecision{Allowed: true}
	}

	cutoff := now.Add(-window)
	inWindow := 0
	for _, t := range prior {
		// Strictly after the cutoff and not in the future: the rolling window is
		// (now-window, now]. Stale timestamps outside the window do not count.
		if t.After(cutoff) && !t.After(now) {
			inWindow++
		}
	}

	allowed := inWindow < limit
	return SwitchCapDecision{
		Allowed:      allowed,
		CapExhausted: !allowed,
		// The crossing edge is the FIRST refusal: exactly `limit` switches already
		// landed in-window, so this attempt is the (limit+1)th. A later attempt
		// (>limit in-window) is still refused but is not the edge ⇒ no re-notify.
		CapJustExhausted: inWindow == limit,
		InWindowCount:    inWindow,
	}
}

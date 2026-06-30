package surface

import "sync"

// RateLimitScope classifies WHY a desk was throttled — it drives failover target
// selection (server-side poisons the whole provider; account-side poisons one
// subscription bucket).
type RateLimitScope int

const (
	// RateLimitServerSide is a provider-wide infra throttle (e.g. Anthropic's
	// "Server is temporarily limiting requests") — failover MUST cross providers.
	RateLimitServerSide RateLimitScope = iota
	// RateLimitAccountSide is a per-account/key throttle — a same-provider alternate
	// subscription MAY be tried before crossing providers.
	RateLimitAccountSide
)

// String renders a scope for logs and wake reasons.
func (s RateLimitScope) String() string {
	switch s {
	case RateLimitServerSide:
		return "server-side"
	case RateLimitAccountSide:
		return "account-side"
	default:
		return "unknown"
	}
}

// RateLimitProbe is an OPTIONAL Driver capability (#204): report whether the pane's
// current turn region shows a material provider throttle. READ-ONLY (pane capture).
// Implementations require 2 consecutive positive reads before returning limited=true
// (mirrors confirmed-delivery clearedConfirmPolls discipline).
type RateLimitProbe interface {
	RateLimited(pane string) (limited bool, scope RateLimitScope, detail string)
}

// RateLimitSupport type-asserts the OPTIONAL RateLimitProbe capability.
func RateLimitSupport(d Driver) (RateLimitProbe, bool) {
	p, ok := d.(RateLimitProbe)
	return p, ok
}

// rateLimitMaterialPolls is how many consecutive positive reads are required before
// a throttle is treated as material (mirrors confirm.clearedConfirmPolls).
const rateLimitMaterialPolls = 2

// rateLimitStreak tracks consecutive positive reads per pane across probe calls.
type rateLimitStreak struct {
	mu     sync.Mutex
	streak map[string]int
}

func (t *rateLimitStreak) observe(pane string, hit bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.streak == nil {
		t.streak = make(map[string]int)
	}
	if hit {
		t.streak[pane]++
	} else {
		t.streak[pane] = 0
	}
	return t.streak[pane] >= rateLimitMaterialPolls
}

func (t *rateLimitStreak) clear(pane string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.streak != nil {
		delete(t.streak, pane)
	}
}

// ClearRateLimitStreak drops the consecutive-read streak for a pane (e.g. when the desk
// leaves Idle/Errored and is no longer a rate-limit probe candidate).
func ClearRateLimitStreak(pane string) { globalRateLimitStreak.clear(pane) }

// package-level streak shared by probe singletons (one registry driver per surface).
var globalRateLimitStreak rateLimitStreak

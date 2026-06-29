package roster

import (
	"errors"
	"fmt"
	"strings"
)

// Roster-wide target-resolution errors. These are the canonical sentinels for the
// ONE shared resolver (ResolveTarget); the dash control package re-exports them as
// aliases so its HTTP error mapping and tests keep working unchanged. A web
// coordination instruction and a dash control instruction resolve through the SAME
// function and therefore surface the SAME sentinels.
var (
	// ErrUnknownTarget: a route target that resolves to no roster agent.
	ErrUnknownTarget = errors.New("unknown route target (no matching agent or @desk)")
	// ErrAmbiguousTarget: a route target that matches more than one agent
	// case-insensitively with no exact match (roster names are case-sensitively
	// unique, so "alpha" + "Alpha" can coexist) — rejected, never guessed.
	ErrAmbiguousTarget = errors.New("ambiguous route target (matches multiple agents by case) — use the exact name")
)

// ResolveTarget maps a route target to a canonical roster agent name, ROSTER-WIDE:
// an empty target → the given xo; "@name"/"name" → the canonical agent
// (case-insensitive); an unknown target → ErrUnknownTarget; a case collision with no
// exact match → ErrAmbiguousTarget. It is the SINGLE roster-wide resolver shared by
// the dash control surface and the web transport (transport spec "The roster-wide
// resolver is shared, not forked") — so the exact-wins-else-ambiguous rule cannot
// drift between the two call sites.
//
// The resolution is ROSTER-WIDE by intent: it is a host-local operator console with
// no Discord channel context, so the operator can address ANY desk in the roster.
// This deliberately DIFFERS from the Discord relay, which scopes "@name" to the
// typed-in channel's members (so an @name never crosses a channel boundary). For a
// single-fleet roster the two coincide (members == all agents); for a federated
// roster this is intentionally boundary-transcending (the operator owns the whole
// fleet). It is NOT a reuse of relay.Route.
//
// Roster names are unique only CASE-SENSITIVELY, so "alpha" and "Alpha" can both
// exist. An EXACT match therefore wins first (unambiguous — the operator typed that
// exact name); only when there is no exact match and MORE THAN ONE case-insensitive
// match remains is the target ambiguous (ErrAmbiguousTarget) — rejected, never
// silently delivered to whichever is first.
func (c *Config) ResolveTarget(xo, target string) (string, error) {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(target), "@"))
	if t == "" {
		return xo, nil
	}
	var ci []string
	for _, a := range c.Agents {
		if a.Name == t {
			return a.Name, nil // exact match — unambiguous
		}
		if strings.EqualFold(a.Name, t) {
			ci = append(ci, a.Name)
		}
	}
	switch len(ci) {
	case 0:
		return "", ErrUnknownTarget
	case 1:
		return ci[0], nil
	default:
		return "", fmt.Errorf("%w: %q matches %v — use the exact name", ErrAmbiguousTarget, t, ci)
	}
}

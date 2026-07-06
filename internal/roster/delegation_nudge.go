package roster

import (
	"fmt"
	"strings"
)

// DelegationNudgePolicy gates the coordinator delegation-nudge side-effect (#232, #481).
// Zero/absent resolves to on (legacy behavior).
type DelegationNudgePolicy string

const (
	DelegationNudgeOn  DelegationNudgePolicy = "on"
	DelegationNudgeOff DelegationNudgePolicy = "off"
)

// Enabled reports whether the delegation nudge may fire.
func (p DelegationNudgePolicy) Enabled() bool {
	return p == "" || p == DelegationNudgeOn
}

// ParseDelegationNudge validates a policy string. Empty is valid (on at resolve time).
func ParseDelegationNudge(s string) (DelegationNudgePolicy, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "", string(DelegationNudgeOn), string(DelegationNudgeOff):
		return DelegationNudgePolicy(s), nil
	default:
		return "", fmt.Errorf("invalid delegation_nudge %q (want on|off)", s)
	}
}

// ResolveDelegationNudge picks the effective policy: env FLOTILLA_DELEGATION_NUDGE overrides
// roster delegation_nudge; both unset ⇒ on (no silent behavior change). Invalid values return
// an error — env typos must fail closed.
func ResolveDelegationNudge(rosterVal, envVal string) (DelegationNudgePolicy, error) {
	raw := strings.TrimSpace(rosterVal)
	if e := strings.TrimSpace(envVal); e != "" {
		raw = e
	}
	p, err := ParseDelegationNudge(raw)
	if err != nil {
		return "", err
	}
	if p == "" {
		return DelegationNudgeOn, nil
	}
	return p, nil
}

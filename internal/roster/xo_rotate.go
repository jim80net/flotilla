package roster

import (
	"fmt"
	"strings"
)

// XORotatePolicy gates idle-edge context rotation in the change-detector's
// continueXO path (#467). Zero/absent resolves to always (legacy behavior).
type XORotatePolicy string

const (
	XORotateAlways  XORotatePolicy = "always"
	XORotateNever   XORotatePolicy = "never"
	XORotateHandoff XORotatePolicy = "handoff"
)

// AllowsIdleEdgeRotate reports whether continueXO may request a bare context
// rotate (/clear) before delivering continuation or backlog wakes.
func (p XORotatePolicy) AllowsIdleEdgeRotate() bool {
	return p == "" || p == XORotateAlways
}

// ParseXORotate validates a policy string. Empty is valid (always at resolve time).
func ParseXORotate(s string) (XORotatePolicy, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "", string(XORotateAlways), string(XORotateNever), string(XORotateHandoff):
		return XORotatePolicy(s), nil
	default:
		return "", fmt.Errorf("invalid xo_rotate %q (want always|never|handoff)", s)
	}
}

// ResolveXORotate picks the effective policy: env FLOTILLA_XO_ROTATE overrides roster
// xo_rotate; both unset ⇒ always (no silent behavior change). Invalid values return
// an error — env typos must fail closed (not silently revert to always).
func ResolveXORotate(rosterVal, envVal string) (XORotatePolicy, error) {
	raw := strings.TrimSpace(rosterVal)
	if e := strings.TrimSpace(envVal); e != "" {
		raw = e
	}
	p, err := ParseXORotate(raw)
	if err != nil {
		return "", err
	}
	if p == "" {
		return XORotateAlways, nil
	}
	return p, nil
}

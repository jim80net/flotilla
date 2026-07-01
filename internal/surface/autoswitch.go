package surface

import (
	"os"
	"strings"
)

// AutoSwitchEnabled reports whether detector-enqueued harness auto-switch is on.
// It DEFAULTS ON: the preferred posture is autonomy bounded by safety guardrails, not
// autonomy gated on an operator's permission. The guardrails bound it — approval-sensitive
// desks never auto-switch, the coordinator tier never auto-switches, only desks on the
// primary surface are candidates, plus a storm-cooldown and per-desk switch caps. Disable
// explicitly with FLOTILLA_AUTOSWITCH=0/false/no/off. ONE definition shared by watch + CLI.
func AutoSwitchEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FLOTILLA_AUTOSWITCH"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

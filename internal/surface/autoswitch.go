package surface

import (
	"os"
	"strings"
)

// AutoSwitchEnabled reports whether detector-enqueued harness auto-switch is on.
// It DEFAULTS ON: the preferred posture is autonomy bounded by safety guardrails, not
// autonomy gated on an operator's permission. The guardrails bound it — approval-sensitive
// desks never auto-switch; coordinators and execution desks on the primary (claude-code)
// surface are candidates (#510); plus a storm-cooldown and per-desk switch caps. Disable
// explicitly with FLOTILLA_AUTOSWITCH=0/false/no/off. ONE definition shared by watch + CLI.
func AutoSwitchEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FLOTILLA_AUTOSWITCH"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// AutoRevertEnabled reports whether detector-enqueued restore to the preferred (primary)
// harness slot is on after usage limits clear (#510 / #466 phase 2). DEFAULTS ON, matching
// auto-switch posture. Disable with FLOTILLA_AUTOREVERT=0/false/no/off. Requires
// AutoSwitchEnabled for production dispatch wiring (watch only arms revert when switch is on).
func AutoRevertEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FLOTILLA_AUTOREVERT"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

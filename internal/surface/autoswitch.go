package surface

import "os"

// AutoSwitchEnabled is the #205 kill-switch: detector-enqueued harness auto-switch is
// DEFAULT-OFF and enabled only by FLOTILLA_AUTOSWITCH=1/true/yes. Autonomous desk-flipping
// is an operator veto-window deploy, not merge-time — ONE definition shared by watch + CLI.
func AutoSwitchEnabled() bool {
	switch os.Getenv("FLOTILLA_AUTOSWITCH") {
	case "1", "true", "TRUE", "yes":
		return true
	}
	return false
}

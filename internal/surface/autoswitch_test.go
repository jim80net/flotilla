package surface

import "testing"

func TestAutoSwitchEnabled(t *testing.T) {
	// Default (unset) is ON — autonomy bounded by the safety guardrails is the
	// preferred posture, not autonomy gated on an operator's permission.
	t.Setenv("FLOTILLA_AUTOSWITCH", "")
	if !AutoSwitchEnabled() {
		t.Fatal("default (unset) must be ON")
	}
	// Explicit affirmative values keep it on; unrecognized values also stay on
	// (default-on), so a typo fails safe toward autonomy, not toward silently off.
	for _, v := range []string{"1", "true", "TRUE", "yes", "on", "anything-else"} {
		t.Run("on/"+v, func(t *testing.T) {
			t.Setenv("FLOTILLA_AUTOSWITCH", v)
			if !AutoSwitchEnabled() {
				t.Fatalf("FLOTILLA_AUTOSWITCH=%q should be ON", v)
			}
		})
	}
	// Only explicit falsey values disable it (configurable off).
	for _, v := range []string{"0", "false", "FALSE", "no", "off", " off "} {
		t.Run("off/"+v, func(t *testing.T) {
			t.Setenv("FLOTILLA_AUTOSWITCH", v)
			if AutoSwitchEnabled() {
				t.Fatalf("FLOTILLA_AUTOSWITCH=%q should be OFF", v)
			}
		})
	}
}

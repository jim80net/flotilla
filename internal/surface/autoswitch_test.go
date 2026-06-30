package surface

import "testing"

func TestAutoSwitchEnabled(t *testing.T) {
	t.Setenv("FLOTILLA_AUTOSWITCH", "")
	if AutoSwitchEnabled() {
		t.Fatal("default must be off")
	}
	for _, v := range []string{"1", "true", "yes"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("FLOTILLA_AUTOSWITCH", v)
			if !AutoSwitchEnabled() {
				t.Fatalf("FLOTILLA_AUTOSWITCH=%q should enable", v)
			}
		})
	}
	t.Setenv("FLOTILLA_AUTOSWITCH", "on")
	if AutoSwitchEnabled() {
		t.Fatal("on is not a recognized enable value")
	}
}

package deliver

import "testing"

func TestPaneSession(t *testing.T) {
	cases := []struct {
		target, want string
	}{
		{"flotilla:0.0", "flotilla"},
		{"flotilla-backend:desk.0", "flotilla-backend"},
		{"f:0.0", "f"},
	}
	for _, c := range cases {
		if got := PaneSession(c.target); got != c.want {
			t.Errorf("PaneSession(%q) = %q, want %q", c.target, got, c.want)
		}
	}
}

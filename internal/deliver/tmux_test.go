package deliver

import "testing"

func TestParsePane(t *testing.T) {
	out := "0:0.0\thydra-ops\n0:0.1\tx-signal-dev\n0:0.3\tv12-dev\n"

	got, err := parsePane(out, "v12-dev")
	if err != nil {
		t.Fatalf("parsePane: %v", err)
	}
	if got != "0:0.3" {
		t.Errorf("target = %q, want 0:0.3", got)
	}

	if _, err := parsePane(out, "missing"); err == nil {
		t.Error("parsePane(missing) = nil error, want error")
	}
}

func TestParsePaneIgnoresBlankLines(t *testing.T) {
	out := "\n0:0.0\thydra-ops\n\n"
	got, err := parsePane(out, "hydra-ops")
	if err != nil {
		t.Fatalf("parsePane: %v", err)
	}
	if got != "0:0.0" {
		t.Errorf("target = %q, want 0:0.0", got)
	}
}

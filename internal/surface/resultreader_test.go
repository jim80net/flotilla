package surface

import (
	"errors"
	"testing"
)

func TestSurfaceFromPaneCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
		ok   bool
	}{
		{"claude", "claude-code", true},
		{"grok", "grok", true},
		{"bash", "", false},
		{"", "", false},
		{" Claude ", "claude-code", true},
	}
	for _, tc := range tests {
		got, ok := SurfaceFromPaneCommand(tc.cmd)
		if ok != tc.ok || got != tc.want {
			t.Errorf("SurfaceFromPaneCommand(%q) = (%q, %v), want (%q, %v)", tc.cmd, got, ok, tc.want, tc.ok)
		}
	}
}

func TestResolveResultReaderPrefersLiveHarness(t *testing.T) {
	paneCommand := func(string) (string, error) { return "claude", nil }

	rr, drv, live, drift, err := ResolveResultReader("grok", "p", paneCommand)
	if err != nil {
		t.Fatal(err)
	}
	if !drift || live != "claude-code" {
		t.Fatalf("drift=%v live=%q, want drift=true live=claude-code", drift, live)
	}
	if drv.Name() != "claude-code" {
		t.Fatalf("driver = %q, want claude-code", drv.Name())
	}
	if rr == nil {
		t.Fatal("expected ResultReader")
	}
}

func TestResolveResultReaderRosterWhenAligned(t *testing.T) {
	paneCommand := func(string) (string, error) { return "grok", nil }

	rr, drv, live, drift, err := ResolveResultReader("grok", "p", paneCommand)
	if err != nil {
		t.Fatal(err)
	}
	if drift || live != "grok" {
		t.Fatalf("drift=%v live=%q, want drift=false live=grok", drift, live)
	}
	if drv.Name() != "grok" {
		t.Fatalf("driver = %q, want grok", drv.Name())
	}
	if rr == nil {
		t.Fatal("expected ResultReader")
	}
}

func TestResolveResultReaderPaneCommandErrorFallsBackToRoster(t *testing.T) {
	paneCommand := func(string) (string, error) { return "", errors.New("tmux down") }

	rr, drv, live, drift, err := ResolveResultReader("grok", "p", paneCommand)
	if err != nil {
		t.Fatal(err)
	}
	if drift || live != "grok" {
		t.Fatalf("drift=%v live=%q, want roster fallback", drift, live)
	}
	if drv.Name() != "grok" || rr == nil {
		t.Fatalf("driver=%v rr=%v, want grok ResultReader", drv, rr)
	}
}

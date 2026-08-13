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
		{"codex", "codex", true},
		{"opencode", "opencode", true},
		{"pi", "pi", true},
		{"aider", "aider", true},
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

func TestResolveDriverPrefersLiveHarness586(t *testing.T) {
	// #586 specimen: roster/overlay still claude-code while pane runs grok — recycle phase-0
	// must use the grok composer probe (claude reports undetermined forever on a grok pane).
	paneCommand := func(string) (string, error) { return "grok", nil }

	drv, live, drift, err := ResolveDriver("claude-code", "p", paneCommand)
	if err != nil {
		t.Fatal(err)
	}
	if !drift || live != "grok" {
		t.Fatalf("drift=%v live=%q, want drift=true live=grok", drift, live)
	}
	if drv.Name() != "grok" {
		t.Fatalf("driver = %q, want grok", drv.Name())
	}
	if _, ok := drv.(ComposerStateProbe); !ok {
		t.Fatal("live grok driver must expose ComposerStateProbe for recycle idle∧cleared")
	}
	if _, ok := RecycleSupport(drv); !ok {
		t.Fatal("live grok driver must expose RecycleBridge for handoff paths")
	}
}

func TestResolveDriverStaleClaudeToLiveGrokDetectorFrames(t *testing.T) {
	drv, _, _, err := ResolveDriver("claude-code", "p", func(string) (string, error) { return "grok", nil })
	if err != nil {
		t.Fatal(err)
	}
	g, ok := drv.(grok)
	if !ok {
		t.Fatalf("resolved driver = %T, want grok", drv)
	}
	frames := []struct {
		name string
		cap  string
		want State
	}{
		{"working", "  ⠙ Waiting… 0.4s ⇣127k [✗]", StateWorking},
		{"idle", "  │ ❯                         │\n  ╰──── Grok 4.6 (high) · always-approve ─╯", StateIdle},
	}
	for _, tc := range frames {
		if got := g.classify(tc.cap); got != tc.want {
			t.Errorf("live Grok %s frame = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveDriverStaleClaudeUsesLiveGrokRateLimitParser(t *testing.T) {
	drv, _, _, err := ResolveDriver("claude-code", "p", func(string) (string, error) { return "grok", nil })
	if err != nil {
		t.Fatal(err)
	}
	g, ok := drv.(grok)
	if !ok {
		t.Fatalf("resolved driver = %T, want grok", drv)
	}
	g.capturePane = func(string) (string, error) {
		return "  ⠙ Rate limit exceeded; sleeping.", nil
	}
	pane := "resolved-live-grok-rate-limit"
	if limited, _, _ := g.RateLimited(pane); limited {
		t.Fatal("first Grok throttle read must not yet be material")
	}
	limited, scope, detail := g.RateLimited(pane)
	if !limited || scope != RateLimitAccountSide || detail != "Rate limit exceeded" {
		t.Fatalf("live Grok parser = (%v,%v,%q), want material account-side Grok throttle", limited, scope, detail)
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

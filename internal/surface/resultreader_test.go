package surface

import (
	"errors"
	"testing"
)

func TestResolveLiveDriverFailClosedAndLiveWins(t *testing.T) {
	drv, live, drift, err := ResolveLiveDriver("claude-code", "%1", func(string) (string, error) { return "grok", nil })
	if err != nil || drv == nil || live != "grok" || !drift {
		t.Fatalf("live grok over configured claude = drv=%v live=%q drift=%t err=%v", drv, live, drift, err)
	}
	for name, read := range map[string]func(string) (string, error){
		"read error": func(string) (string, error) { return "", errors.New("tmux unavailable") },
		"unknown":    func(string) (string, error) { return "mystery", nil },
		"shell":      func(string) (string, error) { return "bash", nil },
	} {
		t.Run(name, func(t *testing.T) {
			if got, _, _, err := ResolveLiveDriver("claude-code", "%1", read); err == nil || got != nil {
				t.Fatalf("got driver=%v err=%v, want nil/error", got, err)
			}
		})
	}
}

func TestResolveLiveDriverGenericRuntimeArgvAndRosterFallback(t *testing.T) {
	for _, tc := range []struct {
		name      string
		command   string
		roster    string
		argv      []string
		argvErr   error
		want      string
		wantDrift bool
	}{
		{"codex bin", "node", "claude-code", []string{"node", "/opt/tools/bin/codex", "--flag"}, nil, "codex", true},
		{"claude package", "node", "codex", []string{"node", "/opt/node_modules/@anthropic-ai/claude-code/cli.js"}, nil, "claude-code", true},
		{"python class", "python", "codex", []string{"python", "/opt/tools/bin/aider"}, nil, "aider", true},
		{"python3 class", "python3", "codex", []string{"python3", "/opt/tools/bin/aider"}, nil, "aider", true},
		{"bun class", "bun", "claude-code", []string{"bun", "/opt/tools/bin/codex"}, nil, "codex", true},
		{"deno class", "deno", "claude-code", []string{"deno", "/opt/tools/bin/codex"}, nil, "codex", true},
		{"unreadable argv roster fallback", "node", "codex", nil, errors.New("proc denied"), "codex", false},
		{"unnamed argv roster fallback", "node", "codex", []string{"node", "/srv/app.js"}, nil, "codex", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drv, live, drift, err := ResolveLiveDriverWithArgv(tc.roster, "%1", func(string) (string, error) { return tc.command, nil }, func(string) ([]string, error) {
				return tc.argv, tc.argvErr
			})
			if err != nil || drv == nil || drv.Name() != tc.want || live != tc.want || drift != tc.wantDrift {
				t.Fatalf("drv=%v live=%q drift=%t err=%v, want %q drift=%t", drv, live, drift, err, tc.want, tc.wantDrift)
			}
		})
	}
}

func TestNodeCodexArgvAuthorizesSelectedDriverAtSubmitBoundary(t *testing.T) {
	drv, _, _, err := ResolveLiveDriverWithArgv("claude-code", "%1",
		func(string) (string, error) { return "node", nil },
		func(string) ([]string, error) {
			return []string{"node", "/opt/harness/bin/codex", "--model", "gpt"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	submitted := false
	submit := func(selected Driver) {
		submitted = true
		if selected.Name() != "codex" {
			t.Fatalf("submitted through %q, want codex", selected.Name())
		}
	}
	submit(drv)
	if !submitted {
		t.Fatal("node + codex argv did not reach submit boundary")
	}
}

func TestResolveLiveDriverGenericRuntimeAndUnknownFailClosedControls(t *testing.T) {
	for _, cmd := range []string{"mystery-agent", "bash", "", "   "} {
		t.Run(cmd, func(t *testing.T) {
			drv, _, _, err := ResolveLiveDriverWithArgv("codex", "%1", func(string) (string, error) { return cmd, nil }, func(string) ([]string, error) {
				return []string{"node", "/opt/tools/bin/codex"}, nil
			})
			if err == nil || drv != nil {
				t.Fatalf("command %q driver=%v err=%v, want nil/error", cmd, drv, err)
			}
		})
	}
}

func TestSurfaceFromProcessArgvExactComponents(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
		ok   bool
	}{
		{[]string{"node", "/usr/local/bin/codex"}, "codex", true},
		{[]string{"node", "/pkg/claude-code/cli.js"}, "claude-code", true},
		{[]string{"python3", "/usr/local/bin/aider"}, "aider", true},
		{[]string{"node", "/srv/my-codex-helper.js"}, "", false},
		{[]string{"node", "/usr/local/bin/codex", "/usr/local/bin/grok"}, "", false},
	} {
		got, ok := SurfaceFromProcessArgv(tc.argv)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("argv=%q got=(%q,%t), want=(%q,%t)", tc.argv, got, ok, tc.want, tc.ok)
		}
	}
}

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

func TestResolveDriverUsesLiveHarnessRotationCommand(t *testing.T) {
	for _, tc := range []struct {
		name, configured, command, want string
	}{
		{"stale Claude to Grok", "claude-code", "grok", "/new"},
		{"stale Grok to Claude", "grok", "claude", "/clear"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drv, _, _, err := ResolveDriver(tc.configured, "p", func(string) (string, error) { return tc.command, nil })
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			switch d := drv.(type) {
			case grok:
				d.inject = func(_ string, command string) error { got = command; return nil }
				drv = d
			case claudeCode:
				d.clear = func(string) error { got = "/clear"; return nil }
				drv = d
			default:
				t.Fatalf("resolved rotation driver = %T", drv)
			}
			if err := RotateContext(drv, "p"); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("rotation command = %q, want %q", got, tc.want)
			}
		})
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

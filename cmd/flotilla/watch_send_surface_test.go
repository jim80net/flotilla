package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestResolveWatchSendDriverPrefersLiveHarnessAndLogsDrift(t *testing.T) {
	var logs []string
	drv, err := resolveWatchSendDriver(
		"alpha-desk",
		"claude-code",
		"session:1.2",
		func(pane string) (string, error) {
			if pane != "session:1.2" {
				t.Fatalf("pane probe = %q, want resolved delivery pane", pane)
			}
			return "grok", nil
		},
		func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if drv.Name() != "grok" {
		t.Fatalf("delivery driver = %q, want live harness grok", drv.Name())
	}
	if len(logs) != 1 {
		t.Fatalf("drift logs = %v, want one", logs)
	}
	for _, want := range []string{"delivery drift", "alpha-desk", `"claude-code"`, `"grok"`, "live harness"} {
		if !strings.Contains(logs[0], want) {
			t.Errorf("drift log %q missing %q", logs[0], want)
		}
	}
}

func TestResolveWatchSendDriverStaleOverlayCannotSelectMismatchedComposerProbe(t *testing.T) {
	configured, ok := surface.Get("claude-code")
	if !ok {
		t.Fatal("claude-code driver is not registered")
	}
	drv, err := resolveWatchSendDriver(
		"alpha-desk",
		configured.Name(),
		"session:1.2",
		func(string) (string, error) { return "grok", nil },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if drv.Name() == configured.Name() {
		t.Fatalf("stale overlay driver %q survived live-harness resolution", configured.Name())
	}
	if drv.Name() != "grok" {
		t.Fatalf("delivery driver = %q, want grok", drv.Name())
	}
	if _, ok := drv.(surface.ComposerStateProbe); !ok {
		t.Fatal("live delivery driver lost the confirmed-delivery ComposerStateProbe contract")
	}
}

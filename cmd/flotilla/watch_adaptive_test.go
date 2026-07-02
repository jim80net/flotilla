package main

import (
	"testing"
	"time"
)

func TestAdaptiveIntervalEnabledFlag(t *testing.T) {
	t.Setenv("FLOTILLA_ADAPTIVE_INTERVAL", "")
	if !adaptiveIntervalEnabled("") {
		t.Fatal("empty flag must fall through to env default (on)")
	}
	if adaptiveIntervalEnabled("false") {
		t.Fatal("false must disable")
	}
	if !adaptiveIntervalEnabled("true") {
		t.Fatal("true must enable")
	}
}

func TestOptionalDuration(t *testing.T) {
	t.Setenv("FLOTILLA_INTERVAL_FLOOR", "")
	if _, ok := optionalDuration("3m", "FLOTILLA_INTERVAL_FLOOR"); !ok {
		t.Fatal("flag value must parse")
	}
	t.Setenv("FLOTILLA_INTERVAL_FLOOR", "4m")
	if d, ok := optionalDuration("", "FLOTILLA_INTERVAL_FLOOR"); !ok || d != 4*time.Minute {
		t.Fatalf("env value = (%v, %v), want 4m", d, ok)
	}
}

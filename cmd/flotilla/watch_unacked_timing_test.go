package main

import (
	"strings"
	"testing"
	"time"
)

func TestUnackedTimingFromEnvDefaultsAndOverrides(t *testing.T) {
	defaults, err := unackedTimingFromEnv(func(string) string { return "" })
	if err != nil || defaults.ScanInterval != 10*time.Minute || defaults.MinAge != 10*time.Minute || defaults.WorkingFollowUp != 15*time.Minute {
		t.Fatalf("defaults=%+v err=%v", defaults, err)
	}
	values := map[string]string{
		"FLOTILLA_UNACKED_SCAN_INTERVAL":    "2m",
		"FLOTILLA_UNACKED_MIN_AGE":          "3m",
		"FLOTILLA_UNACKED_WORKING_FOLLOWUP": "4m",
	}
	got, err := unackedTimingFromEnv(func(name string) string { return values[name] })
	if err != nil || got.ScanInterval != 2*time.Minute || got.MinAge != 3*time.Minute || got.WorkingFollowUp != 4*time.Minute {
		t.Fatalf("overrides=%+v err=%v", got, err)
	}
}

func TestUnackedTimingFromEnvEnforcesMinAgeInvariant(t *testing.T) {
	values := map[string]string{"FLOTILLA_UNACKED_SCAN_INTERVAL": "10m", "FLOTILLA_UNACKED_MIN_AGE": "9m"}
	_, err := unackedTimingFromEnv(func(name string) string { return values[name] })
	if err == nil || !strings.Contains(err.Error(), "must be >= scan interval") {
		t.Fatalf("err=%v, want MinAge invariant failure", err)
	}
}

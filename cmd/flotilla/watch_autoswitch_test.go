package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestLeaderExhaustionBodies(t *testing.T) {
	alert := leaderExhaustionAlertBody("xo", surface.RateLimitAccountSide)
	for _, want := range []string{"LEADER EXHAUSTION", "xo", "account-side", "auto-switch"} {
		if !strings.Contains(alert, want) {
			t.Errorf("alert missing %q: %s", want, alert)
		}
	}
	adj := leaderExhaustionAdjutantBody("xo", surface.RateLimitServerSide, "/state/charter.md")
	for _, want := range []string{"URGENT", "xo", "server-side", "ESCALATE LOUDLY", "charter"} {
		if !strings.Contains(adj, want) {
			t.Errorf("adjutant body missing %q: %s", want, adj)
		}
	}
	note := coordinatorResuscitationNotifyBody("xo", "grok")
	for _, want := range []string{"xo", "grok", "resuscitated"} {
		if !strings.Contains(note, want) {
			t.Errorf("notify missing %q: %s", want, note)
		}
	}
}

func TestRateLimitSwitchArgsWeeklyExhaustionTargetsFirstFallbackWithForce(t *testing.T) {
	args := rateLimitSwitchArgs("xo", "account-side", surface.RateLimitDetailWeeklyExhausted, "/repo/flotilla.json", "/repo/flotilla-launch.json")
	want := []string{"switch", "xo", "--to", "fallback-0", "--force", "--roster", "/repo/flotilla.json", "--launch", "/repo/flotilla-launch.json"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("weekly exhaustion args = %q, want %q", args, want)
	}
}

func TestWeeklyExhaustionFirstFallbackResolvesNamedFableLaunch(t *testing.T) {
	chain := launch.Recipe{
		Launch: "grok --model grok-4.6",
		Primary: &launch.HarnessSlot{
			Surface: "grok", Launch: "grok --model grok-4.6", Provider: "xai", SubscriptionID: "weekly",
		},
		Fallbacks: []launch.HarnessSlot{
			{Surface: surface.DefaultSurface, Launch: "claude --model claude-fable-5", Provider: "anthropic"},
		},
	}
	slot, err := resolveSwitchSlot(chain, "grok", "fallback-0", false, PoisonState{}, RateLimitAccountSide)
	if err != nil {
		t.Fatal(err)
	}
	if slot.Name != "fallback-0" || slot.Launch != "claude --model claude-fable-5" {
		t.Fatalf("resolved slot = %+v, want first fallback with named Fable launch", slot)
	}
}

func TestRateLimitSwitchArgsSpinnerPathStaysCooperativeAuto(t *testing.T) {
	args := rateLimitSwitchArgs("xo", "account-side", "Rate limit exceeded", "/repo/flotilla.json", "")
	want := []string{"switch", "xo", "--auto", "--rate-limit-scope", "account-side", "--roster", "/repo/flotilla.json"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("spinner throttle args = %q, want %q", args, want)
	}
}

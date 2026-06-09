package main

import (
	"testing"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestParseRelaunchArgs(t *testing.T) {
	// Pin $FLOTILLA_LAUNCH so the --launch default is deterministic across hosts.
	t.Setenv("FLOTILLA_LAUNCH", "")
	cases := []struct {
		name       string
		args       []string
		wantAgent  string
		wantLaunch string
		wantForce  bool
		wantErr    bool
	}{
		{"agent only", []string{"hydra-ops"}, "hydra-ops", "", false, false},
		{"agent then force", []string{"hydra-ops", "--force"}, "hydra-ops", "", true, false},
		{"force then agent", []string{"--force", "hydra-ops"}, "hydra-ops", "", true, false},
		{"agent then launch", []string{"hydra-ops", "--launch", "/tmp/l.json"}, "hydra-ops", "/tmp/l.json", false, false},
		{"launch then agent", []string{"--launch", "/tmp/l.json", "hydra-ops"}, "hydra-ops", "/tmp/l.json", false, false},
		{"launch=form and force", []string{"hydra-ops", "--launch=/tmp/l.json", "--force"}, "hydra-ops", "/tmp/l.json", true, false},
		{"no agent", []string{"--force"}, "", "", false, true},
		{"empty", []string{}, "", "", false, true},
		{"extra positional", []string{"a", "b"}, "", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent, _, launchPath, force, err := parseRelaunchArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseRelaunchArgs(%v) = nil error, want error", c.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRelaunchArgs(%v): %v", c.args, err)
			}
			if agent != c.wantAgent {
				t.Errorf("agent = %q, want %q", agent, c.wantAgent)
			}
			if launchPath != c.wantLaunch {
				t.Errorf("launchPath = %q, want %q", launchPath, c.wantLaunch)
			}
			if force != c.wantForce {
				t.Errorf("force = %v, want %v", force, c.wantForce)
			}
		})
	}
}

func TestParseRelaunchArgsEnvDefault(t *testing.T) {
	// $FLOTILLA_LAUNCH pre-fills the --launch default (mirrors watch's env-var
	// defaults), overriding the roster-relative fallback.
	t.Setenv("FLOTILLA_LAUNCH", "/env/launch.json")
	_, _, launchPath, _, err := parseRelaunchArgs([]string{"hydra-ops"})
	if err != nil {
		t.Fatalf("parseRelaunchArgs: %v", err)
	}
	if launchPath != "/env/launch.json" {
		t.Errorf("launchPath = %q, want /env/launch.json (from $FLOTILLA_LAUNCH)", launchPath)
	}
}

// relaunchRec records which side effects runRelaunch performed, so the safety
// matrix can assert "refused → nothing killed" / "dead → respawned" / etc.
type relaunchRec struct {
	respawned, tagged, newSession, newWindow bool
	tagTarget                                string
}

// fakeOps builds relaunchOps from a fixed resolution + assessment + marker
// read-back, recording side effects — exercising runRelaunch's decision core
// without a live tmux server or a real agent.
func fakeOps(rec *relaunchRec, target string, outcome deliver.ResolveOutcome, st surface.State, marker string, hasSess bool) relaunchOps {
	return relaunchOps{
		resolve:    func(string) (string, deliver.ResolveOutcome, error) { return target, outcome, nil },
		assess:     func(string) surface.State { return st },
		respawn:    func(string, string, string) error { rec.respawned = true; return nil },
		readMarker: func(string) (string, error) { return marker, nil },
		hasSession: func(string) (bool, error) { return hasSess, nil },
		newSession: func(_, _, _, _ string) (string, error) { rec.newSession = true; return "flotilla:0.0", nil },
		newWindow:  func(_, _, _, _ string) (string, error) { rec.newWindow = true; return "flotilla:1.0", nil },
		tag:        func(target, _ string) error { rec.tagged = true; rec.tagTarget = target; return nil },
	}
}

// TestRunRelaunchSafetyMatrix pins the two P1 invariants: a live (or
// can't-confirm-dead) pane is NEVER respawned without --force, and the marker is
// never duplicated. Without this, the safety-critical interlock was untested.
func TestRunRelaunchSafetyMatrix(t *testing.T) {
	plan := relaunchPlan{agent: "v12-dev", key: "v12-dev", cwd: "/w", launch: "sleep 1", session: "flotilla", window: "v12-dev"}
	forced := plan
	forced.force = true

	cases := []struct {
		name                                                   string
		plan                                                   relaunchPlan
		target                                                 string
		outcome                                                deliver.ResolveOutcome
		st                                                     surface.State
		marker                                                 string
		hasSess                                                bool
		wantErr, wantRespawn, wantTag, wantNewSess, wantNewWin bool
	}{
		// Fail-safe interlock: refuse every non-shell state without --force; respawn nothing.
		{"working refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateWorking, "", false, true, false, false, false, false},
		{"idle refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateIdle, "", false, true, false, false, false, false},
		{"awaiting-approval refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateAwaitingApproval, "", false, true, false, false, false, false},
		{"errored refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateErrored, "", false, true, false, false, false, false},
		{"unknown refuse (cant confirm dead)", plan, "f:0.0", deliver.ResolveUnique, surface.StateUnknown, "", false, true, false, false, false, false},
		// Dead shell → respawn; marker confirmed → no re-tag.
		{"shell respawn confirmed", plan, "f:0.0", deliver.ResolveUnique, surface.StateShell, "v12-dev", false, false, true, false, false, false},
		// --force overrides a live state → respawn.
		{"working force respawn", forced, "f:0.0", deliver.ResolveUnique, surface.StateWorking, "v12-dev", false, false, true, false, false, false},
		// Untagged (title-resolved) dead desk → respawn + ADOPT (tag), not error.
		{"shell untagged adopt", plan, "f:0.0", deliver.ResolveUnique, surface.StateShell, "", false, false, true, true, false, false},
		// Wrong marker after respawn → error (respawn happened, no tag).
		{"shell marker mismatch", plan, "f:0.0", deliver.ResolveUnique, surface.StateShell, "other", false, true, true, false, false, false},
		// Ambiguous → refuse; nothing done.
		{"ambiguous refuse", plan, "", deliver.ResolveAmbiguous, surface.StateUnknown, "", false, true, false, false, false, false},
		// Cold create: no session → new-session + tag.
		{"none no-session cold", plan, "", deliver.ResolveNone, surface.StateUnknown, "", false, false, false, true, true, false},
		// Cold create: session exists → new-window + tag.
		{"none has-session window", plan, "", deliver.ResolveNone, surface.StateUnknown, "", true, false, false, true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &relaunchRec{}
			ops := fakeOps(rec, c.target, c.outcome, c.st, c.marker, c.hasSess)
			_, err := runRelaunch(ops, c.plan)
			if c.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if rec.respawned != c.wantRespawn {
				t.Errorf("respawned = %v, want %v", rec.respawned, c.wantRespawn)
			}
			if rec.tagged != c.wantTag {
				t.Errorf("tagged = %v, want %v", rec.tagged, c.wantTag)
			}
			if rec.newSession != c.wantNewSess {
				t.Errorf("newSession = %v, want %v", rec.newSession, c.wantNewSess)
			}
			if rec.newWindow != c.wantNewWin {
				t.Errorf("newWindow = %v, want %v", rec.newWindow, c.wantNewWin)
			}
		})
	}
}

func TestRelaunchTmuxTarget(t *testing.T) {
	cases := []struct {
		name        string
		recipe      launch.Recipe
		agent       string
		wantSession string
		wantWindow  string
	}{
		{"explicit tmux", launch.Recipe{Tmux: "flotilla:hydra-ops"}, "hydra-ops", "flotilla", "hydra-ops"},
		{"explicit other session", launch.Recipe{Tmux: "work:desk"}, "hydra-ops", "work", "desk"},
		{"absent tmux defaults to flotilla:<name>", launch.Recipe{}, "crypto-trend-dev", "flotilla", "crypto-trend-dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, w := relaunchTmuxTarget(c.recipe, c.agent)
			if s != c.wantSession || w != c.wantWindow {
				t.Errorf("relaunchTmuxTarget = (%q,%q), want (%q,%q)", s, w, c.wantSession, c.wantWindow)
			}
		})
	}
}

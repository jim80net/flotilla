package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestParseResumeArgs(t *testing.T) {
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
		{"agent only", []string{"xo"}, "xo", "", false, false},
		{"agent then force", []string{"xo", "--force"}, "xo", "", true, false},
		{"force then agent", []string{"--force", "xo"}, "xo", "", true, false},
		{"agent then launch", []string{"xo", "--launch", "/tmp/l.json"}, "xo", "/tmp/l.json", false, false},
		{"launch then agent", []string{"--launch", "/tmp/l.json", "xo"}, "xo", "/tmp/l.json", false, false},
		{"launch=form and force", []string{"xo", "--launch=/tmp/l.json", "--force"}, "xo", "/tmp/l.json", true, false},
		{"no agent", []string{"--force"}, "", "", false, true},
		{"empty", []string{}, "", "", false, true},
		{"extra positional", []string{"a", "b"}, "", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent, _, launchPath, force, _, err := parseResumeArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseResumeArgs(%v) = nil error, want error", c.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseResumeArgs(%v): %v", c.args, err)
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

func TestParseResumeArgsScheduledE2EAuthorization(t *testing.T) {
	agent, _, _, force, scheduled, err := parseResumeArgs([]string{"--force", "--scheduled-e2e", "alpha-desk"})
	if err != nil {
		t.Fatal(err)
	}
	if agent != "alpha-desk" || !force || !scheduled {
		t.Fatalf("parseResumeArgs() = agent %q force=%v scheduled=%v", agent, force, scheduled)
	}
}

func TestParseResumeArgsEnvDefault(t *testing.T) {
	// $FLOTILLA_LAUNCH pre-fills the --launch default (mirrors watch's env-var
	// defaults), overriding the roster-relative fallback.
	t.Setenv("FLOTILLA_LAUNCH", "/env/launch.json")
	_, _, launchPath, _, _, err := parseResumeArgs([]string{"xo"})
	if err != nil {
		t.Fatalf("parseResumeArgs: %v", err)
	}
	if launchPath != "/env/launch.json" {
		t.Errorf("launchPath = %q, want /env/launch.json (from $FLOTILLA_LAUNCH)", launchPath)
	}
}

// resumeRec records which side effects runResume performed, so the safety
// matrix can assert "refused → nothing killed" / "dead → respawned" / etc.
type resumeRec struct {
	respawned, killed, tagged, newSession, newWindow bool
	tagTarget                                        string
	launch                                           string
}

// fakeOps builds resumeOps from a fixed resolution + assessment + marker
// read-back, recording side effects — exercising runResume's decision core
// without a live tmux server or a real agent.
func fakeOps(rec *resumeRec, target string, outcome deliver.ResolveOutcome, st surface.State, marker string, hasSess bool) resumeOps {
	return resumeOps{
		resolve: func(string) (string, deliver.ResolveOutcome, error) { return target, outcome, nil },
		assess:  func(string) surface.State { return st },
		respawn: func(_, _, launch string) error {
			rec.respawned, rec.launch = true, launch
			return nil
		},
		readMarker: func(string) (string, error) { return marker, nil },
		killPane:   func(string) error { rec.killed = true; return nil },
		hasSession: func(string) (bool, error) { return hasSess, nil },
		newSession: func(_, _, _, launch string) (string, error) {
			rec.newSession, rec.launch = true, launch
			return "flotilla:0.0", nil
		},
		newWindow: func(_, _, _, launch string) (string, error) {
			rec.newWindow, rec.launch = true, launch
			return "flotilla:1.0", nil
		},
		tag: func(target, _ string) error { rec.tagged = true; rec.tagTarget = target; return nil },
	}
}

func TestRunResumeUsesSelectedFallbackForInPlaceColdAndForce(t *testing.T) {
	base := resumePlan{
		agent: "alpha-build", key: "alpha-build", cwd: "/work/alpha", launch: "grok --resume",
		session: "flotilla", window: "alpha-build", slot: "fallback-0",
		selectedSurface: "grok", launchSource: "/etc/flotilla-launch.json", selectionSource: "active-harness overlay",
	}
	cases := []struct {
		name    string
		target  string
		outcome deliver.ResolveOutcome
		state   surface.State
		force   bool
	}{
		{name: "in-place dead pane", target: "flotilla:1.0", outcome: deliver.ResolveUnique, state: surface.StateShell},
		{name: "cold pane", outcome: deliver.ResolveNone, state: surface.StateUnknown},
		{name: "forced live pane", target: "flotilla:1.0", outcome: deliver.ResolveUnique, state: surface.StateWorking, force: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := base
			plan.force = tc.force
			rec := &resumeRec{}
			ops := fakeOps(rec, tc.target, tc.outcome, tc.state, plan.key, false)
			msg, err := runResume(ops, plan)
			if err != nil {
				t.Fatal(err)
			}
			if rec.launch != "grok --resume" {
				t.Fatalf("launched %q, want selected fallback launch", rec.launch)
			}
			for _, want := range []string{"launch-source=/etc/flotilla-launch.json", "selection-source=active-harness overlay", "slot=fallback-0", "surface=grok"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q missing %q", msg, want)
				}
			}
		})
	}
}

// TestRunResumePreLaunchSeam pins the pre-launch prep contract: preLaunch runs
// BEFORE the process launch on every launch branch (in-place respawn, cold
// new-session, cold new-window), and never on a refusal. Codex trust seeding
// rides this seam — deleting the seam call would silently reintroduce the
// first-run trust-menu wedge, so the ordering is pinned here.
func TestRunResumePreLaunchSeam(t *testing.T) {
	plan := resumePlan{agent: "backend", key: "backend", cwd: "/w", launch: "sleep 1", session: "flotilla", window: "backend"}

	run := func(t *testing.T, target string, outcome deliver.ResolveOutcome, st surface.State, hasSess bool, p resumePlan) []string {
		t.Helper()
		var order []string
		rec := &resumeRec{}
		ops := fakeOps(rec, target, outcome, st, "backend", hasSess)
		ops.preLaunch = func() { order = append(order, "preLaunch") }
		inner := ops.respawn
		ops.respawn = func(a, b, c string) error { order = append(order, "respawn"); return inner(a, b, c) }
		innerSess := ops.newSession
		ops.newSession = func(a, b, c, d string) (string, error) {
			order = append(order, "newSession")
			return innerSess(a, b, c, d)
		}
		innerWin := ops.newWindow
		ops.newWindow = func(a, b, c, d string) (string, error) {
			order = append(order, "newWindow")
			return innerWin(a, b, c, d)
		}
		_, _ = runResume(ops, p)
		return order
	}

	if got := run(t, "f:0.0", deliver.ResolveUnique, surface.StateShell, false, plan); len(got) < 2 || got[0] != "preLaunch" || got[1] != "respawn" {
		t.Errorf("in-place respawn order = %v, want preLaunch before respawn", got)
	}
	if got := run(t, "", deliver.ResolveNone, surface.StateUnknown, false, plan); len(got) < 2 || got[0] != "preLaunch" || got[1] != "newSession" {
		t.Errorf("cold new-session order = %v, want preLaunch before newSession", got)
	}
	if got := run(t, "", deliver.ResolveNone, surface.StateUnknown, true, plan); len(got) < 2 || got[0] != "preLaunch" || got[1] != "newWindow" {
		t.Errorf("cold new-window order = %v, want preLaunch before newWindow", got)
	}
	// A refusal (live pane, no --force) must not run preLaunch: no trust is
	// seeded for a launch that never happens.
	if got := run(t, "f:0.0", deliver.ResolveUnique, surface.StateWorking, false, plan); len(got) != 0 {
		t.Errorf("refusal ran %v, want no preLaunch and no launch", got)
	}
	// A nil preLaunch (drivers with no prep) must not panic on any branch.
	rec := &resumeRec{}
	ops := fakeOps(rec, "f:0.0", deliver.ResolveUnique, surface.StateShell, "backend", false)
	if _, err := runResume(ops, plan); err != nil {
		t.Errorf("nil preLaunch respawn: %v", err)
	}
}

// TestRunResumeSafetyMatrix pins the two P1 invariants: a live (or
// can't-confirm-dead) pane is NEVER respawned without --force, and the marker is
// never duplicated. Without this, the safety-critical interlock was untested.
func TestRunResumeSafetyMatrix(t *testing.T) {
	plan := resumePlan{agent: "backend", key: "backend", cwd: "/w", launch: "sleep 1", session: "flotilla", window: "backend"}
	forced := plan
	forced.force = true

	cases := []struct {
		name                                                               string
		plan                                                               resumePlan
		target                                                             string
		outcome                                                            deliver.ResolveOutcome
		st                                                                 surface.State
		marker                                                             string
		hasSess                                                            bool
		wantErr, wantRespawn, wantKilled, wantTag, wantNewSess, wantNewWin bool
	}{
		// Fail-safe interlock: refuse every non-shell state without --force; respawn nothing.
		{"working refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateWorking, "", false, true, false, false, false, false, false},
		{"idle refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateIdle, "", false, true, false, false, false, false, false},
		{"awaiting-approval refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateAwaitingApproval, "", false, true, false, false, false, false, false},
		{"errored refuse", plan, "f:0.0", deliver.ResolveUnique, surface.StateErrored, "", false, true, false, false, false, false, false},
		{"unknown refuse (cant confirm dead)", plan, "f:0.0", deliver.ResolveUnique, surface.StateUnknown, "", false, true, false, false, false, false, false},
		// Dead shell → respawn; marker confirmed → no re-tag.
		{"shell respawn confirmed", plan, "f:0.0", deliver.ResolveUnique, surface.StateShell, "backend", false, false, true, false, false, false, false},
		// --force overrides a live state → respawn.
		{"working force respawn", forced, "f:0.0", deliver.ResolveUnique, surface.StateWorking, "backend", false, false, true, false, false, false, false},
		// Untagged (title-resolved) dead desk → respawn + ADOPT (tag), not error.
		{"shell untagged adopt", plan, "f:0.0", deliver.ResolveUnique, surface.StateShell, "", false, false, true, false, true, false, false},
		// Wrong marker after respawn → error (respawn happened, no tag).
		{"shell marker mismatch", plan, "f:0.0", deliver.ResolveUnique, surface.StateShell, "other", false, true, true, false, false, false, false},
		// Ambiguous → refuse; nothing done.
		{"ambiguous refuse", plan, "", deliver.ResolveAmbiguous, surface.StateUnknown, "", false, true, false, false, false, false, false},
		// Cold create: no session → new-session + tag.
		{"none no-session cold", plan, "", deliver.ResolveNone, surface.StateUnknown, "", false, false, false, false, true, true, false},
		// Cold create: shared session exists → new-window + tag.
		{"none has-session window", plan, "", deliver.ResolveNone, surface.StateUnknown, "", true, false, false, false, true, false, true},
		// Cold create: per-agent session exists but no pane → refuse (no orphan window).
		{"none per-agent session orphan", resumePlan{agent: "cos", key: "cos", cwd: "/w", launch: "sleep 1", session: "flotilla-cos", window: "desk", perAgentSession: true}, "", deliver.ResolveNone, surface.StateUnknown, "", true, true, false, false, false, false, false},
		// Per-agent migration: stale dead shell in legacy session → kill + cold-create.
		{"per-agent stale shell migrate", resumePlan{agent: "backend", key: "backend", cwd: "/w", launch: "sleep 1", session: "flotilla-backend", window: "desk", perAgentSession: true}, "flotilla:0.0", deliver.ResolveUnique, surface.StateShell, "backend", false, false, false, true, true, true, false},
		// Per-agent migration: live desk in legacy session → refuse (no kill).
		{"per-agent stale live refuse", resumePlan{agent: "backend", key: "backend", cwd: "/w", launch: "sleep 1", session: "flotilla-backend", window: "desk", perAgentSession: true}, "flotilla:0.0", deliver.ResolveUnique, surface.StateIdle, "backend", false, true, false, false, false, false, false},
		// Per-agent migration: --force on live stale → kill + cold-create.
		{"per-agent stale live force migrate", resumePlan{agent: "backend", key: "backend", cwd: "/w", launch: "sleep 1", session: "flotilla-backend", window: "desk", perAgentSession: true, force: true}, "flotilla:0.0", deliver.ResolveUnique, surface.StateIdle, "backend", false, false, false, true, true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &resumeRec{}
			ops := fakeOps(rec, c.target, c.outcome, c.st, c.marker, c.hasSess)
			_, err := runResume(ops, c.plan)
			if c.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if rec.respawned != c.wantRespawn {
				t.Errorf("respawned = %v, want %v", rec.respawned, c.wantRespawn)
			}
			if rec.killed != c.wantKilled {
				t.Errorf("killed = %v, want %v", rec.killed, c.wantKilled)
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

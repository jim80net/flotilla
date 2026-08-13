package main

import (
	"errors"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

type officerRouteDriver struct {
	name        string
	states      []surface.State
	assessCalls int
	disposition surface.ComposerDisposition
}

func (d *officerRouteDriver) Name() string {
	if d.name != "" {
		return d.name
	}
	return "grok"
}
func (d *officerRouteDriver) Rotate(string) error              { return nil }
func (d *officerRouteDriver) RotateStrategy() surface.Strategy { return surface.SlashCommand }
func (d *officerRouteDriver) Close(string) error               { return nil }
func (d *officerRouteDriver) Assess(string) surface.State {
	i := d.assessCalls
	d.assessCalls++
	if i >= len(d.states) {
		i = len(d.states) - 1
	}
	return d.states[i]
}
func (d *officerRouteDriver) Submit(string, string) error { return nil }
func (d *officerRouteDriver) ComposerState(string) surface.ComposerDisposition {
	return d.disposition
}

func officerDeps(capture string, audits *[]officerRouteAudit, submitted *bool) officerRouteDeps {
	return officerRouteDeps{
		capture: func(string) (string, error) { return capture, nil },
		cursor:  func(string) (int, int, bool, bool, error) { return 7, 12, true, false, nil },
		sleep:   func(time.Duration) {},
		now:     func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) },
		audit: func(r officerRouteAudit) error {
			*audits = append(*audits, r)
			return nil
		},
		submit: func(surface.Driver, string, string) error { *submitted = true; return nil },
		empty:  func(surface.Driver, string) (bool, string) { return true, "officer-confirmed exact capture" },
	}
}

func TestOfficerRouteDeliversClassifierMissWithIndependentIdleProofAndAudit(t *testing.T) {
	const capture = "generic transcript\n  │ ❯                         │\n  ╰──── model · approve ─────╯"
	drv := &officerRouteDriver{states: []surface.State{surface.StateIdle, surface.StateIdle}, disposition: surface.ComposerUndetermined}
	var audits []officerRouteAudit
	submitted := false
	err := deliverOfficerRoute(drv, "fleet-xo", "roster-coordinator", "cli-send", "desk", "%12", "grok", "grok", "work", captureDigest(capture), true, "ComposerUndetermined", officerDeps(capture, &audits, &submitted))
	if err != nil {
		t.Fatalf("deliverOfficerRoute: %v", err)
	}
	if !submitted {
		t.Fatal("classifier miss with officer-confirmed stable idle proof did not submit")
	}
	if len(audits) != 2 || audits[0].Officer != "fleet-xo" || audits[0].Outcome != "authorized-before-submit" || audits[1].Outcome != "delivered-confirmed" || audits[0].CaptureSHA != captureDigest(capture) {
		t.Fatalf("audit = %+v", audits)
	}
}

func TestWatchOfficerRouteDeliversUnseenFooterWithoutAddingRegex(t *testing.T) {
	// This intentionally invented footer is not a fixture for any surface
	// parser. The selected driver remains Undetermined; the independent probe,
	// not a new footer regex, supplies empty-main-composer proof.
	const capture = "transcript\n  │ ❯                         │\n  ╰── future-model [novel controls] ──╯"
	drv := &officerRouteDriver{states: []surface.State{surface.StateIdle, surface.StateIdle}, disposition: surface.ComposerUndetermined}
	var audits []officerRouteAudit
	submitted := false
	deps := officerDeps(capture, &audits, &submitted)
	deps.empty = func(surface.Driver, string) (bool, string) { return true, "independent-idle-cleared:control-driver" }
	err := deliverOfficerRoute(drv, "watch-daemon", "automated-independent-idle-proof", "watch-submit", "desk", "%12", "grok", "grok", "work", "", false, "ErrTransient", deps)
	if err != nil || !submitted {
		t.Fatalf("unseen-footer route err=%v submitted=%t", err, submitted)
	}
	if len(audits) != 2 || audits[0].Path != "watch-submit" || audits[0].ProbeFailed != "ErrTransient" || audits[1].Outcome != "delivered-confirmed" {
		t.Fatalf("audit = %+v", audits)
	}
}

func TestOfficerRouteFailsClosedOnBusyPaneBeforeAuditOrSubmit(t *testing.T) {
	const capture = "working output"
	drv := &officerRouteDriver{states: []surface.State{surface.StateWorking}, disposition: surface.ComposerUndetermined}
	var audits []officerRouteAudit
	submitted := false
	err := deliverOfficerRoute(drv, "fleet-xo", "roster-coordinator", "cli-send", "desk", "%12", "grok", "grok", "work", captureDigest(capture), true, "ComposerUndetermined", officerDeps(capture, &audits, &submitted))
	if err == nil {
		t.Fatal("busy pane unexpectedly authorized")
	}
	if submitted || len(audits) != 0 {
		t.Fatalf("busy outcome submitted=%t audits=%d, want false/0", submitted, len(audits))
	}
}

func TestOfficerRouteAuditFailurePreventsSubmit(t *testing.T) {
	const capture = "idle"
	drv := &officerRouteDriver{states: []surface.State{surface.StateIdle, surface.StateIdle}, disposition: surface.ComposerUndetermined}
	submitted := false
	deps := officerDeps(capture, new([]officerRouteAudit), &submitted)
	deps.audit = func(officerRouteAudit) error { return errors.New("disk full") }
	if err := deliverOfficerRoute(drv, "xo", "roster-coordinator", "cli-send", "desk", "%1", "grok", "grok", "work", captureDigest(capture), true, "ComposerUndetermined", deps); err == nil {
		t.Fatal("audit failure unexpectedly authorized")
	}
	if submitted {
		t.Fatal("audit failure submitted")
	}
}

func TestCrossDriverEmptyProofRequiresIdleClearedAndWorkingVetoes(t *testing.T) {
	selected := &officerRouteDriver{name: "grok", states: []surface.State{surface.StateIdle}, disposition: surface.ComposerUndetermined}
	control := &officerRouteDriver{name: "claude-code", states: []surface.State{surface.StateIdle}, disposition: surface.ComposerCleared}
	if ok, detail := crossDriverEmptyMainComposerWith(selected, "%1", []surface.Driver{selected, control}); !ok || detail != "independent-idle-cleared:claude-code" {
		t.Fatalf("idle+cleared control = ok=%t detail=%q", ok, detail)
	}
	working := &officerRouteDriver{name: "codex", states: []surface.State{surface.StateWorking}, disposition: surface.ComposerUndetermined}
	if ok, detail := crossDriverEmptyMainComposerWith(selected, "%1", []surface.Driver{control, working}); ok || detail != "working-veto:codex" {
		t.Fatalf("working veto = ok=%t detail=%q", ok, detail)
	}
}

func TestOfficerDetectorUnknownOverridesOnlyWithAuditedIndependentIdleProof(t *testing.T) {
	const capture = "stable idle frame"
	drv := &officerRouteDriver{states: []surface.State{surface.StateIdle, surface.StateIdle}, disposition: surface.ComposerUndetermined}
	var audits []officerRouteAudit
	submitted := false
	deps := officerDeps(capture, &audits, &submitted)
	deps.empty = func(surface.Driver, string) (bool, string) { return true, "independent-idle-cleared:control-driver" }
	if !officerDetectorIdleOverride(drv, "desk", "%1", "grok", "grok", deps) {
		t.Fatal("audited independent detector idle proof did not override Unknown")
	}
	if submitted || len(audits) != 1 || audits[0].Outcome != "detector-idle-override" {
		t.Fatalf("submitted=%t audits=%+v", submitted, audits)
	}

	drv = &officerRouteDriver{states: []surface.State{surface.StateIdle, surface.StateIdle}, disposition: surface.ComposerUndetermined}
	deps.audit = func(officerRouteAudit) error { return errors.New("disk full") }
	if officerDetectorIdleOverride(drv, "desk", "%1", "grok", "grok", deps) {
		t.Fatal("audit failure authorized detector override")
	}
}

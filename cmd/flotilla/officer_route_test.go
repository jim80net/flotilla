package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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
	if len(audits) != 2 || audits[0].Officer != "fleet-xo" || audits[0].Outcome != "attempt-owned-durable" || audits[0].TerminalOutcome != "delivery-status-unknown-no-replay" || audits[1].TerminalOutcome != "delivered-confirmed" || audits[0].CaptureSHA != captureDigest(capture) {
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
	if len(audits) != 2 || audits[0].Path != "watch-submit" || audits[0].ProbeFailed != "ErrTransient" || audits[1].TerminalOutcome != "delivered-confirmed" {
		t.Fatalf("audit = %+v", audits)
	}
}

func TestOfficerRouteStableUnknownIsDecidedByIndependentIdleProof(t *testing.T) {
	const capture = "stable unknown expert frame"
	drv := &officerRouteDriver{states: []surface.State{surface.StateUnknown, surface.StateUnknown}, disposition: surface.ComposerUndetermined}
	var audits []officerRouteAudit
	submitted := false
	deps := officerDeps(capture, &audits, &submitted)
	deps.empty = func(surface.Driver, string) (bool, string) { return true, "independent-idle-cleared:control-driver" }
	if err := deliverOfficerRoute(drv, "watch-daemon", "automated-independent-idle-proof", "watch-submit", "desk", "%12", "grok", "grok", "work", "", false, "StateUnknown", deps); err != nil {
		t.Fatalf("stable Unknown + independent idle proof: %v", err)
	}
	if !submitted {
		t.Fatal("stable Unknown + independent idle proof did not submit")
	}

	drv = &officerRouteDriver{states: []surface.State{surface.StateUnknown}, disposition: surface.ComposerUndetermined}
	submitted = false
	deps = officerDeps(capture, new([]officerRouteAudit), &submitted)
	deps.empty = func(surface.Driver, string) (bool, string) { return false, "working-veto:control-driver" }
	if err := deliverOfficerRoute(drv, "watch-daemon", "automated-independent-idle-proof", "watch-submit", "desk", "%12", "grok", "grok", "work", "", false, "StateUnknown", deps); err == nil || submitted {
		t.Fatalf("stable Unknown + working veto err=%v submitted=%t, want refusal", err, submitted)
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

func TestOfficerRouteOutcomeAuditFailureDoesNotReplay(t *testing.T) {
	const capture = "idle"
	drv := &officerRouteDriver{states: []surface.State{surface.StateIdle, surface.StateIdle}, disposition: surface.ComposerUndetermined}
	submits := 0
	auditCalls := 0
	deps := officerDeps(capture, new([]officerRouteAudit), new(bool))
	deps.submit = func(surface.Driver, string, string) error { submits++; return nil }
	deps.audit = func(record officerRouteAudit) error {
		auditCalls++
		if auditCalls == 1 {
			if record.TerminalOutcome != "delivery-status-unknown-no-replay" || record.ReplayDisposition != "never-replay-after-submit-begins" {
				t.Fatalf("pre-submit record = %+v", record)
			}
			return nil
		}
		return errors.New("outcome disk failure")
	}
	if err := deliverOfficerRoute(drv, "xo", "roster-coordinator", "cli-send", "desk", "%1", "grok", "grok", "work", captureDigest(capture), true, "ComposerUndetermined", deps); err != nil {
		t.Fatalf("confirmed delivery must not become retryable on outcome audit failure: %v", err)
	}
	if submits != 1 || auditCalls != 2 {
		t.Fatalf("submits=%d auditCalls=%d, want 1/2", submits, auditCalls)
	}
}

type auditFileStub struct {
	writeN   int
	writeErr error
	syncErr  error
	closeErr error
	synced   bool
	closed   bool
}

func (f *auditFileStub) Write(p []byte) (int, error) {
	if f.writeN == 0 && f.writeErr == nil {
		return len(p), nil
	}
	return f.writeN, f.writeErr
}
func (f *auditFileStub) Sync() error  { f.synced = true; return f.syncErr }
func (f *auditFileStub) Close() error { f.closed = true; return f.closeErr }

func TestWriteDurableOfficerAuditRequiresCompleteWriteSyncAndClose(t *testing.T) {
	good := &auditFileStub{}
	if err := writeDurableOfficerAudit(good, []byte("record\n")); err != nil || !good.synced || !good.closed {
		t.Fatalf("durable write err=%v synced=%t closed=%t", err, good.synced, good.closed)
	}
	short := &auditFileStub{writeN: 2}
	if err := writeDurableOfficerAudit(short, []byte("record\n")); !errors.Is(err, io.ErrShortWrite) || short.synced || !short.closed {
		t.Fatalf("short write err=%v synced=%t closed=%t", err, short.synced, short.closed)
	}
	for name, f := range map[string]*auditFileStub{
		"sync":  {syncErr: errors.New("sync")},
		"close": {closeErr: errors.New("close")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := writeDurableOfficerAudit(f, []byte("record\n")); err == nil || !f.closed {
				t.Fatalf("err=%v closed=%t", err, f.closed)
			}
		})
	}
}

func TestAppendOfficerRouteAuditFirstUseCreatesAndSyncsContainingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-officer-delivery-audit.jsonl")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first-use precondition stat err=%v, want absent", err)
	}
	dirSyncs := 0
	err := appendOfficerRouteAuditWithDirOpen(path, officerRouteAudit{Outcome: "attempt-owned-durable"}, func(got string) (officerAuditFile, error) {
		if got != dir {
			t.Fatalf("opened directory %q, want %q", got, dir)
		}
		dirSyncs++
		return os.Open(got)
	})
	if err != nil {
		t.Fatalf("first-use append: %v", err)
	}
	if dirSyncs != 1 {
		t.Fatalf("directory opens=%d, want 1", dirSyncs)
	}
	if body, err := os.ReadFile(path); err != nil || len(body) == 0 {
		t.Fatalf("created audit body=%q err=%v", body, err)
	}
}

func TestDirectorySyncFailurePreventsOfficerSubmit(t *testing.T) {
	const capture = "idle"
	drv := &officerRouteDriver{states: []surface.State{surface.StateIdle, surface.StateIdle}, disposition: surface.ComposerUndetermined}
	submitted := false
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-officer-delivery-audit.jsonl")
	deps := officerDeps(capture, new([]officerRouteAudit), &submitted)
	deps.audit = func(record officerRouteAudit) error {
		return appendOfficerRouteAuditWithDirOpen(path, record, func(string) (officerAuditFile, error) {
			return &auditFileStub{syncErr: errors.New("directory sync failed")}, nil
		})
	}
	err := deliverOfficerRoute(drv, "xo", "roster-coordinator", "cli-send", "desk", "%1", "grok", "grok", "work", captureDigest(capture), true, "ComposerUndetermined", deps)
	if err == nil || submitted {
		t.Fatalf("directory-sync failure err=%v submitted=%t, want error/false", err, submitted)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("control must exercise first-use creation before directory sync failure: %v", statErr)
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
	for _, tc := range []struct {
		name  string
		state surface.State
		want  string
	}{
		{"approval modal", surface.StateAwaitingApproval, "awaiting-approval-veto:modal"},
		{"input modal", surface.StateAwaitingInput, "awaiting-input-veto:modal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modal := &officerRouteDriver{name: "modal", states: []surface.State{tc.state}, disposition: surface.ComposerUndetermined}
			if ok, detail := crossDriverEmptyMainComposerWith(selected, "%1", []surface.Driver{modal, control}); ok || detail != tc.want {
				t.Fatalf("modal veto = ok=%t detail=%q want %q", ok, detail, tc.want)
			}
		})
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

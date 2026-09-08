package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/codexstore"
	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
)

const officerIdleSettle = 750 * time.Millisecond

type officerIdleSample struct {
	captureSHA string
	cursorX    int
	cursorY    int
	visible    bool
	inMode     bool
	state      surface.State
	empty      bool
	emptyProof string
}

type officerIdleProof struct {
	CaptureSHA string
	CursorX    int
	CursorY    int
	Visible    bool
	EmptyProof string
	PreState   surface.State
}

type officerRouteAudit struct {
	At                    time.Time `json:"at"`
	Officer               string    `json:"officer"`
	Authority             string    `json:"authority"`
	Path                  string    `json:"path"`
	Agent                 string    `json:"agent"`
	Pane                  string    `json:"pane"`
	LiveSurface           string    `json:"live_surface"`
	LiveCommand           string    `json:"live_pane_current_command"`
	SelectedDriver        string    `json:"selected_driver"`
	CaptureSHA            string    `json:"capture_sha256"`
	CursorX               int       `json:"cursor_x"`
	CursorY               int       `json:"cursor_y"`
	Proof                 string    `json:"idle_proof"`
	ProbeFailed           string    `json:"probe_failed"`
	PreState              string    `json:"pre_state"`
	ClassifierDisposition string    `json:"classifier_disposition"`
	Outcome               string    `json:"outcome"`
	TerminalOutcome       string    `json:"terminal_outcome"`
	ReplayDisposition     string    `json:"replay_disposition"`
}

type officerRouteDeps struct {
	capture func(string) (string, error)
	cursor  func(string) (int, int, bool, bool, error)
	sleep   func(time.Duration)
	now     func() time.Time
	audit   func(officerRouteAudit) error
	submit  func(surface.Driver, string, string) error
	empty   func(surface.Driver, string) (bool, string)
	// live proves the selected session is still bound to a running process and
	// its session store. Required before treating matching-surface Codex
	// StateErrored as an idle-proof gap (#1066 / #1067 cubic P2).
	live func(surface.Driver, string) (bool, string)
}

func captureDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// crossDriverEmptyMainComposer is the automated watch-side proof. It does not
// add a marker regex: it asks the existing registered drivers to inspect the
// same pane. Any unsafe state report vetoes. A foreign driver may positively
// report Idle + ComposerCleared; the selected driver may do so for Grok or
// Codex, whose prompt/footer structural proof does not depend on cursor
// visibility. Matching-surface Codex Assess=errored is not a composer veto:
// SessionUncooperative is pane-global chrome. proveOfficerIdle still requires
// a PID/session-store liveness probe before treating that errored sample as
// an idle-proof gap (#1066 / #1067 cubic P2).
func crossDriverEmptyMainComposer(selected surface.Driver, pane string) (bool, string) {
	return crossDriverEmptyMainComposerWith(selected, pane, surface.RegisteredDrivers())
}

func crossDriverEmptyMainComposerWith(selected surface.Driver, pane string, candidates []surface.Driver) (bool, string) {
	selectedCleared := false
	independentClearedBy := ""
	for _, candidate := range candidates {
		state := candidate.Assess(pane)
		if officerUnsafeForIdleProof(selected, state) {
			return false, state.String() + "-veto:" + candidate.Name()
		}
		if !officerComposerProofStateAllowed(selected, candidate, state) {
			continue
		}
		probe, ok := candidate.(surface.ComposerStateProbe)
		if ok && probe.ComposerState(pane) == surface.ComposerCleared {
			if candidate.Name() == selected.Name() {
				if selectedSurfaceAllowsOwnComposerProof(selected.Name()) {
					selectedCleared = true
				}
				continue
			}
			independentClearedBy = candidate.Name()
		}
	}
	if selectedCleared {
		return true, "selected:" + selected.Name() + "-idle-cleared"
	}
	if independentClearedBy != "" {
		return true, "independent-idle-cleared:" + independentClearedBy
	}
	return false, "no-independent-idle-cleared-driver"
}

func selectedSurfaceAllowsOwnComposerProof(name string) bool {
	return name == "grok" || name == "codex"
}

func selectedMatchingSurfaceComposerProof(d surface.Driver, emptyProof string) bool {
	name := d.Name()
	return selectedSurfaceAllowsOwnComposerProof(name) && emptyProof == "selected:"+name+"-idle-cleared"
}

func officerUnsafeState(state surface.State) bool {
	switch state {
	case surface.StateWorking, surface.StateAwaitingApproval, surface.StateAwaitingInput,
		surface.StateWedge, surface.StateErrored, surface.StateShell:
		return true
	default:
		return false
	}
}

// officerUnsafeForIdleProof keeps Working / awaiting / shell / wedge as vetoes.
// Matching-surface Codex StateErrored is not a composer-proof veto: the banner
// is pane-global (foreign drivers share SessionUncooperative). A dead session
// is refused later by the PID/session-store probe, not by driver name alone.
func officerUnsafeForIdleProof(selected surface.Driver, state surface.State) bool {
	if state == surface.StateErrored && selected.Name() == "codex" {
		return false
	}
	return officerUnsafeState(state)
}

func officerComposerProofStateAllowed(selected, candidate surface.Driver, state surface.State) bool {
	if state == surface.StateIdle {
		return true
	}
	if candidate.Name() != selected.Name() || selected.Name() != "codex" {
		return false
	}
	return state == surface.StateErrored || state == surface.StateUnknown
}

func officerSampleStateAllowed(d surface.Driver, state surface.State) bool {
	switch state {
	case surface.StateIdle, surface.StateUnknown:
		return true
	case surface.StateErrored:
		return d.Name() == "codex"
	default:
		return false
	}
}

func sampleOfficerIdle(d surface.Driver, pane string, deps officerRouteDeps) (officerIdleSample, error) {
	captured, err := deps.capture(pane)
	if err != nil {
		return officerIdleSample{}, fmt.Errorf("capture pane: %w", err)
	}
	x, y, visible, inMode, err := deps.cursor(pane)
	if err != nil {
		return officerIdleSample{}, fmt.Errorf("read cursor: %w", err)
	}
	empty, emptyProof := false, ""
	if deps.empty != nil {
		empty, emptyProof = deps.empty(d, pane)
	}
	return officerIdleSample{captureSHA: captureDigest(captured), cursorX: x, cursorY: y, visible: visible, inMode: inMode, state: d.Assess(pane), empty: empty, emptyProof: emptyProof}, nil
}

// proveOfficerIdle is deliberately independent of ComposerStateProbe. The
// officer confirms that an exact capture shows the clean main composer; this
// primitive then proves that same frame and cursor remain idle and unchanged
// across a settle interval. Any ambiguity fails closed.
func proveOfficerIdle(d surface.Driver, pane, expectedCaptureSHA string, cleanComposerConfirmed bool, deps officerRouteDeps) (officerIdleProof, error) {
	expectedCaptureSHA = strings.ToLower(strings.TrimSpace(expectedCaptureSHA))
	if expectedCaptureSHA != "" {
		if !cleanComposerConfirmed {
			return officerIdleProof{}, fmt.Errorf("officer must confirm the exact capture shows the clean main composer")
		}
		if len(expectedCaptureSHA) != sha256.Size*2 {
			return officerIdleProof{}, fmt.Errorf("officer capture SHA-256 must be 64 hexadecimal characters")
		}
		if _, err := hex.DecodeString(expectedCaptureSHA); err != nil {
			return officerIdleProof{}, fmt.Errorf("invalid officer capture SHA-256: %w", err)
		}
	} else if deps.empty == nil {
		return officerIdleProof{}, fmt.Errorf("no independent empty-main-composer proof configured")
	}
	first, err := sampleOfficerIdle(d, pane, deps)
	if err != nil {
		return officerIdleProof{}, err
	}
	deps.sleep(officerIdleSettle)
	second, err := sampleOfficerIdle(d, pane, deps)
	if err != nil {
		return officerIdleProof{}, err
	}
	for i, sample := range []officerIdleSample{first, second} {
		// StateUnknown is the expert failure this independent proof exists to
		// route around. Matching-surface Codex StateErrored is the same class
		// only after PID/session-store liveness succeeds (#1067 cubic P2).
		if !officerSampleStateAllowed(d, sample.state) {
			return officerIdleProof{}, fmt.Errorf("idle proof sample %d reported %s", i+1, sample.state)
		}
		if sample.state == surface.StateErrored {
			// Each errored sample re-proves PID + open rollout. The
			// officerIdleSettle window exists so a session that dies
			// between samples refuses; do not hoist this to once-per-proof.
			if deps.live == nil {
				return officerIdleProof{}, fmt.Errorf("idle proof sample %d reported errored without a session-liveness probe", i+1)
			}
			ok, detail := deps.live(d, pane)
			if !ok {
				return officerIdleProof{}, fmt.Errorf("idle proof sample %d reported errored without a live session (%s)", i+1, detail)
			}
		}
		if sample.inMode || (!sample.visible && !selectedMatchingSurfaceComposerProof(d, sample.emptyProof)) {
			return officerIdleProof{}, fmt.Errorf("idle proof sample %d has unsafe cursor state (visible=%t mode=%t)", i+1, sample.visible, sample.inMode)
		}
		if expectedCaptureSHA != "" && sample.captureSHA != expectedCaptureSHA {
			return officerIdleProof{}, fmt.Errorf("idle proof sample %d does not match the officer-confirmed capture", i+1)
		}
		if !sample.empty {
			return officerIdleProof{}, fmt.Errorf("idle proof sample %d did not positively identify an empty main composer", i+1)
		}
	}
	if first.captureSHA != second.captureSHA || first.cursorX != second.cursorX || first.cursorY != second.cursorY || first.visible != second.visible || first.inMode != second.inMode || first.state != second.state || first.emptyProof != second.emptyProof {
		return officerIdleProof{}, fmt.Errorf("pane changed during officer idle settle interval")
	}
	return officerIdleProof{CaptureSHA: first.captureSHA, CursorX: first.cursorX, CursorY: first.cursorY, Visible: first.visible, EmptyProof: first.emptyProof, PreState: first.state}, nil
}

func officerIdleProofDescription(proof officerIdleProof) string {
	cursorProof := "visible-cursor"
	if !proof.Visible {
		cursorProof = "selected-grok-structural-composer-with-hidden-cursor"
		if strings.HasPrefix(proof.EmptyProof, "selected:codex-") {
			cursorProof = "selected-codex-structural-composer-with-hidden-cursor"
		}
	}
	return "two idle/stable " + cursorProof + " samples + " + proof.EmptyProof
}

func officerComposerDispositionAllowed(d surface.Driver, proof officerIdleProof, disposition surface.ComposerDisposition) bool {
	if selectedMatchingSurfaceComposerProof(d, proof.EmptyProof) {
		return disposition == surface.ComposerCleared
	}
	if disposition == surface.ComposerUndetermined {
		return true
	}
	return false
}

func deliverOfficerRoute(d surface.Driver, officer, authority, path, agent, pane, liveSurface, liveCommand, message, expectedCaptureSHA string, cleanComposerConfirmed bool, probeFailed string, deps officerRouteDeps) error {
	proof, err := proveOfficerIdle(d, pane, expectedCaptureSHA, cleanComposerConfirmed, deps)
	if err != nil {
		return fmt.Errorf("officer route refused: %w", err)
	}
	disposition := "unavailable"
	if probe, ok := d.(surface.ComposerStateProbe); ok {
		composerDisposition := probe.ComposerState(pane)
		disposition = composerDisposition.String()
		if !officerComposerDispositionAllowed(d, proof, composerDisposition) {
			return fmt.Errorf("officer route is only for classifier gaps; composer disposition is %s", disposition)
		}
	}
	record := officerRouteAudit{
		At: deps.now().UTC(), Officer: officer, Authority: authority, Path: path, Agent: agent, Pane: pane,
		LiveSurface: liveSurface, LiveCommand: liveCommand, SelectedDriver: d.Name(), CaptureSHA: proof.CaptureSHA, CursorX: proof.CursorX, CursorY: proof.CursorY,
		Proof: officerIdleProofDescription(proof), ProbeFailed: probeFailed, PreState: proof.PreState.String(), ClassifierDisposition: disposition,
		Outcome: "attempt-owned-durable", TerminalOutcome: "delivery-status-unknown-no-replay",
		ReplayDisposition: "never-replay-after-submit-begins",
	}
	// This durable row transfers ownership of the attempt before injection. Its
	// terminal status is deliberately "unknown/no-replay": if the process dies
	// or the result append fails after keys are sent, reconciliation may inspect
	// the pane but automation must never repeat the message. A successful result
	// append refines that conservative terminal status below.
	if err := deps.audit(record); err != nil {
		return fmt.Errorf("officer route audit refused delivery: %w", err)
	}
	log.Printf("flotilla: officer-bypass: mechanics uncertain, pane independently idle path=%s officer=%s agent=%s pane=%s driver=%s", path, officer, agent, pane, d.Name())
	submitErr := deps.submit(d, pane, message)
	result := record
	result.At = deps.now().UTC()
	if submitErr != nil {
		result.Outcome = "delivery-failed"
		result.TerminalOutcome = "delivery-failed: " + submitErr.Error()
	} else {
		result.Outcome = "delivery-finished"
		result.TerminalOutcome = "delivered-confirmed"
	}
	if err := deps.audit(result); err != nil {
		// The pre-submit authorization record is already durable, so a confirmed
		// delivery is never retried merely because the outcome append failed.
		log.Printf("flotilla: officer-bypass outcome audit failed after %s to %s: %v", result.Outcome, pane, err)
		if submitErr != nil {
			return submitErr
		}
		return nil
	}
	return submitErr
}

func officerDetectorIdleOverride(d surface.Driver, agent, pane, liveSurface, liveCommand string, deps officerRouteDeps) bool {
	proof, err := proveOfficerIdle(d, pane, "", false, deps)
	if err != nil {
		return false
	}
	disposition := "unavailable"
	if probe, ok := d.(surface.ComposerStateProbe); ok {
		disposition = probe.ComposerState(pane).String()
		if disposition != surface.ComposerUndetermined.String() {
			return false
		}
	}
	record := officerRouteAudit{
		At: deps.now().UTC(), Officer: "watch-daemon", Authority: "automated-independent-idle-proof", Path: "detector-assess",
		Agent: agent, Pane: pane, LiveSurface: liveSurface, LiveCommand: liveCommand, SelectedDriver: d.Name(),
		CaptureSHA: proof.CaptureSHA, CursorX: proof.CursorX, CursorY: proof.CursorY,
		Proof:       officerIdleProofDescription(proof),
		ProbeFailed: "AssessForFleet=Unknown", PreState: surface.StateUnknown.String(), ClassifierDisposition: disposition,
		Outcome: "detector-idle-override", TerminalOutcome: "detector-idle-override",
		ReplayDisposition: "not-a-delivery",
	}
	return deps.audit(record) == nil
}

func appendOfficerRouteAudit(path string, record officerRouteAudit) error {
	return appendOfficerRouteAuditWithDirOpen(path, record, func(path string) (officerAuditFile, error) {
		return os.Open(path)
	})
}

func appendOfficerRouteAuditWithDirOpen(path string, record officerRouteAudit, openDir func(string) (officerAuditFile, error)) error {
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := writeDurableOfficerAudit(f, line); err != nil {
		return err
	}
	// The file's contents are durable, but a first-use O_CREATE also introduced
	// a directory entry. Sync the containing directory before authorization can
	// return, so a crash cannot erase the only reachable no-replay record.
	dir, err := openDir(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open officer audit directory: %w", err)
	}
	if err := syncAndCloseOfficerAuditDir(dir); err != nil {
		return fmt.Errorf("sync officer audit directory: %w", err)
	}
	return nil
}

type officerAuditFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func writeDurableOfficerAudit(f officerAuditFile, line []byte) error {
	n, writeErr := f.Write(line)
	if writeErr == nil && n != len(line) {
		writeErr = io.ErrShortWrite
	}
	var syncErr error
	if writeErr == nil {
		syncErr = f.Sync()
	}
	closeErr := f.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func syncAndCloseOfficerAuditDir(dir officerAuditFile) error {
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}

func watchOfficerRouteDeps(rosterDir string) officerRouteDeps {
	return officerRouteDeps{
		capture: deliver.CapturePane,
		cursor:  deliver.CursorSnapshot,
		sleep:   time.Sleep,
		now:     time.Now,
		empty:   crossDriverEmptyMainComposer,
		live:    officerCodexSessionLive,
		audit: func(record officerRouteAudit) error {
			return appendOfficerRouteAudit(filepath.Join(rosterDir, "flotilla-officer-delivery-audit.jsonl"), record)
		},
	}
}

// officerCodexSessionLive is the #1067 cubic P2 gate: a matching-surface Codex
// StateErrored sample is an idle-proof gap only when the pane PID is still
// running and the Codex session store still holds an open rollout for that PID.
func officerCodexSessionLive(_ surface.Driver, pane string) (bool, string) {
	pid, err := deliver.PanePID(pane)
	if err != nil {
		return false, err.Error()
	}
	if !deliver.ProcessAlive(pid) {
		return false, "pane pid not running"
	}
	cwd, err := deliver.PaneCWD(pane)
	if err != nil {
		return false, err.Error()
	}
	home, err := officerCodexHome()
	if err != nil {
		return false, err.Error()
	}
	if err := codexstore.ProcessHasOpenRollout(home, cwd, pid); err != nil {
		return false, err.Error()
	}
	return true, "pid-bound-codex-session"
}

// officerCodexHome is the Codex config root this process honors: CODEX_HOME
// when nonempty (same rule as codextrust.ConfigPath / codex_trust.go), else
// ~/.codex. Watch inherits the watch unit's environment, so a set CODEX_HOME
// must be the liveness store or matching-surface idle never delivers.
func officerCodexHome() (string, error) {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

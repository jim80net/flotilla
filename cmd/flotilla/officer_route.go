package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	EmptyProof string
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
}

type officerRouteDeps struct {
	capture func(string) (string, error)
	cursor  func(string) (int, int, bool, bool, error)
	sleep   func(time.Duration)
	now     func() time.Time
	audit   func(officerRouteAudit) error
	submit  func(surface.Driver, string, string) error
	empty   func(surface.Driver, string) (bool, string)
}

func captureDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// crossDriverEmptyMainComposer is the automated watch-side proof. It does not
// add a marker regex: it asks the existing, independently implemented
// registered drivers to inspect the same pane. Any Working report vetoes; at
// least one driver other than the selected uncertain driver must positively
// report Idle + ComposerCleared.
func crossDriverEmptyMainComposer(selected surface.Driver, pane string) (bool, string) {
	return crossDriverEmptyMainComposerWith(selected, pane, surface.RegisteredDrivers())
}

func crossDriverEmptyMainComposerWith(selected surface.Driver, pane string, candidates []surface.Driver) (bool, string) {
	clearedBy := ""
	for _, candidate := range candidates {
		state := candidate.Assess(pane)
		if state == surface.StateWorking {
			return false, "working-veto:" + candidate.Name()
		}
		if candidate.Name() == selected.Name() || state != surface.StateIdle {
			continue
		}
		probe, ok := candidate.(surface.ComposerStateProbe)
		if ok && probe.ComposerState(pane) == surface.ComposerCleared {
			clearedBy = candidate.Name()
		}
	}
	if clearedBy == "" {
		return false, "no-independent-idle-cleared-driver"
	}
	return true, "independent-idle-cleared:" + clearedBy
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
		if sample.state != surface.StateIdle {
			return officerIdleProof{}, fmt.Errorf("idle proof sample %d reported %s", i+1, sample.state)
		}
		if sample.inMode || !sample.visible {
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
	return officerIdleProof{CaptureSHA: first.captureSHA, CursorX: first.cursorX, CursorY: first.cursorY, EmptyProof: first.emptyProof}, nil
}

func deliverOfficerRoute(d surface.Driver, officer, authority, path, agent, pane, liveSurface, liveCommand, message, expectedCaptureSHA string, cleanComposerConfirmed bool, probeFailed string, deps officerRouteDeps) error {
	proof, err := proveOfficerIdle(d, pane, expectedCaptureSHA, cleanComposerConfirmed, deps)
	if err != nil {
		return fmt.Errorf("officer route refused: %w", err)
	}
	disposition := "unavailable"
	if probe, ok := d.(surface.ComposerStateProbe); ok {
		disposition = probe.ComposerState(pane).String()
		if disposition != surface.ComposerUndetermined.String() {
			return fmt.Errorf("officer route is only for classifier gaps; composer disposition is %s", disposition)
		}
	}
	record := officerRouteAudit{
		At: deps.now().UTC(), Officer: officer, Authority: authority, Path: path, Agent: agent, Pane: pane,
		LiveSurface: liveSurface, LiveCommand: liveCommand, SelectedDriver: d.Name(), CaptureSHA: proof.CaptureSHA, CursorX: proof.CursorX, CursorY: proof.CursorY,
		Proof: "two idle/stable visible-cursor samples + " + proof.EmptyProof, ProbeFailed: probeFailed, PreState: surface.StateIdle.String(), ClassifierDisposition: disposition,
		Outcome: "authorized-before-submit",
	}
	if err := deps.audit(record); err != nil {
		return fmt.Errorf("officer route audit refused delivery: %w", err)
	}
	log.Printf("flotilla: officer-bypass: mechanics uncertain, pane independently idle path=%s officer=%s agent=%s pane=%s driver=%s", path, officer, agent, pane, d.Name())
	submitErr := deps.submit(d, pane, message)
	result := record
	result.At = deps.now().UTC()
	if submitErr != nil {
		result.Outcome = "delivery-failed: " + submitErr.Error()
	} else {
		result.Outcome = "delivered-confirmed"
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
		Proof:       "two idle/stable visible-cursor samples + " + proof.EmptyProof,
		ProbeFailed: "AssessForFleet=Unknown", PreState: surface.StateUnknown.String(), ClassifierDisposition: disposition,
		Outcome: "detector-idle-override",
	}
	return deps.audit(record) == nil
}

func appendOfficerRouteAudit(path string, record officerRouteAudit) error {
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

func watchOfficerRouteDeps(rosterDir string) officerRouteDeps {
	return officerRouteDeps{
		capture: deliver.CapturePane,
		cursor:  deliver.CursorSnapshot,
		sleep:   time.Sleep,
		now:     time.Now,
		empty:   crossDriverEmptyMainComposer,
		audit: func(record officerRouteAudit) error {
			return appendOfficerRouteAudit(filepath.Join(rosterDir, "flotilla-officer-delivery-audit.jsonl"), record)
		},
	}
}

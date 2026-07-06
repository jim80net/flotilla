package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/workspace"
)

// loadWarrantCfg builds a minimal roster.Config with one eligible desk ("backend"), one
// approval-sensitive opt-out desk ("trader"), and the XO — for the deskWarrantedGate tests.
func loadWarrantCfg(t *testing.T) *roster.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "roster.json")
	js := `{
	  "xo_agent":"xo","operator_user_id":"U","channel_id":"C","heartbeat_interval":"20m",
	  "agents":[{"name":"xo"},{"name":"backend"},{"name":"trader","approval_sensitive":true}]
	}`
	if err := os.WriteFile(p, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// deskWarrantedGate builds the #189 HeartbeatWarranted seam OFF the detector lock: it reads the
// recipient's OWN backlog (absent ⇒ missing-ledger fallback ⇒ warranted, NOT the shared backlog),
// parses a present file fresh each call, and returns cfg.HeartbeatWarranted(agent, st). An
// unreadable/torn file ⇒ warranted; a present-but-sectionless file ⇒ warranted via the !Found arm
// AND alerts ONCE on the edge. The read is INJECTED so the latch + fallback are unit-testable.
func TestDeskWarrantedGate(t *testing.T) {
	cfg := loadWarrantCfg(t)

	// readState lets each subtest control what the per-recipient read returns per agent.
	type rd struct {
		content string
		exists  bool
		err     error
	}
	state := map[string]rd{}
	var alerts []string
	gate := deskWarrantedGate(cfg,
		func(agent string) ([]byte, bool, error) {
			r := state[agent]
			if r.err != nil {
				return nil, r.exists, r.err
			}
			return []byte(r.content), r.exists, nil
		},
		func(agent string) string { return "/state/flotilla-" + agent + "-backlog.md" },
		func(m string) { alerts = append(alerts, m) })

	// (a) per-recipient file ABSENT ⇒ WARRANTED (missing-ledger fallback) — the shared backlog is
	//     NEVER consulted (the read closure only sees the per-agent agent name; there is no shared path).
	state["backend"] = rd{exists: false}
	if !gate("backend") {
		t.Error("absent per-recipient backlog ⇒ warranted (missing-ledger fallback)")
	}

	// (b) PRESENT with an [in-flight] item ⇒ warranted (live actionable work).
	state["backend"] = rd{content: "## Backlog\n- [in-flight] ship it\n", exists: true}
	if !gate("backend") {
		t.Error("present backlog with actionable work ⇒ warranted")
	}

	// (c) PRESENT with everything parked ([blocked]/[awaiting-auth]/[done]) ⇒ NOT warranted (suppress).
	state["backend"] = rd{content: "## Backlog\n- [blocked] q @op\n- [awaiting-auth] go @op\n- [done] x\n", exists: true}
	if gate("backend") {
		t.Error("present backlog with no actionable work ⇒ NOT warranted (suppress the beat)")
	}

	// (d) PRESENT but sectionless (Found=false) ⇒ warranted via the !Found arm AND alerts on the
	//     #479 content-hash latch: loud on first sight, silent while the broken file is untouched.
	alerts = nil
	state["backend"] = rd{content: "# Notes\nsome prose, no ## Backlog section\n", exists: true}
	if !gate("backend") {
		t.Error("present-but-sectionless backlog ⇒ warranted (cannot prove no work)")
	}
	gate("backend")
	gate("backend")
	if len(alerts) != 1 {
		t.Errorf("present-but-sectionless must alert ONCE while untouched, got %d", len(alerts))
	}
	// The alert must hand the desk's owner everything needed to fix it: the FILE, the missing
	// HEADING, and the one-line fix (#479 owner decision — loud and self-explanatory).
	for _, want := range []string{"/state/flotilla-backend-backlog.md", "'## Backlog'", "Fix:", "#479"} {
		if !strings.Contains(alerts[0], want) {
			t.Errorf("headingless alert must contain %q, got %q", want, alerts[0])
		}
	}
	// (d2) #479: the file CHANGED but is STILL headingless (a failed fix attempt — the discovering
	//      case edited its ledger repeatedly without adding the heading) → a FRESH alert each time
	//      the content changes; repeats of the same broken content stay silent.
	state["backend"] = rd{content: "# Notes\nreconciled some markers, still no heading\n", exists: true}
	gate("backend")
	if len(alerts) != 2 {
		t.Errorf("changed-but-still-headingless must RE-alert, got %d alerts", len(alerts))
	}
	gate("backend")
	if len(alerts) != 2 {
		t.Errorf("unchanged broken content must stay silent, got %d alerts", len(alerts))
	}
	// A clean read clears the latch; a later regression re-alerts.
	state["backend"] = rd{content: "## Backlog\n- [in-flight] work\n", exists: true}
	gate("backend")
	state["backend"] = rd{content: "# Notes\nprose again\n", exists: true}
	gate("backend")
	if len(alerts) != 3 {
		t.Errorf("a cleared latch must re-fire on a new sectionless edge, got %d alerts", len(alerts))
	}

	// (e) PRESENT but UNREADABLE/torn ⇒ warranted (fail-safe), no alert (a torn read self-heals).
	alerts = nil
	state["backend"] = rd{exists: true, err: errors.New("torn mid-write")}
	if !gate("backend") {
		t.Error("unreadable/torn per-recipient backlog ⇒ warranted (fail-safe)")
	}
	if len(alerts) != 0 {
		t.Errorf("an unreadable/torn read must be SILENT, got %d alerts", len(alerts))
	}

	// (f) The HARD gate still wins: an approval-sensitive desk ("trader") with a backlog FULL of
	//     actionable work is NOT warranted (cfg.HeartbeatWarranted returns false for the HARD-gated desk).
	state["trader"] = rd{content: "## Backlog\n- [in-flight] place order\n", exists: true}
	if gate("trader") {
		t.Error("an approval-sensitive desk is HARD-gated off — never warranted even with actionable work")
	}
}

// With NO workspace, the detector continuation prompt the XO receives must be exactly
// what it was before the workspace feature: ResolvePrompt substitutes {{tracker}}/{{settle}}
// into the package builtin, leaving no placeholders and interpolating the paths at the
// same positions. This regression-locks the "additive on the no-workspace path" guarantee.
func TestDetectorContinuationBuiltinNoWorkspace(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir()) // no workspace dir for "xo"

	tracker := "/abs/state/.flotilla-state.md"
	settle := "/abs/state/flotilla-xo-settled"
	got, err := workspace.ResolvePrompt("xo", detectorContinuationBuiltin, tracker, settle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unsubstituted placeholder remains: %q", got)
	}
	for _, want := range []string{
		"[flotilla change-detector] You just finished a turn. Advance the next clear,",
		"the goal+task tracker " + tracker + "; (2) the active openspec change's unchecked tasks;",
		"signal idle by running: touch " + settle + ". (Your context is rotated between steps",
		"— rely on durable state, not this conversation.)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no-workspace prompt missing expected fragment %q\nfull: %q", want, got)
		}
	}
}

// The desk heartbeat prompt must state the FULL ledger contract a desk needs to settle — the
// marker vocabulary AND the `## Backlog` heading requirement (#479: a desk that followed the
// marker instructions to the letter, but kept its items under topic headings, was structurally
// unparseable and heartbeat-looped forever; compliance with the prompt must guarantee
// parseability).
func TestDeskContinuationPromptStatesLedgerContract(t *testing.T) {
	for _, want := range []string{
		"`## Backlog` heading",
		"parses ONLY that section",
		"`[awaiting-auth]`",
		"touch {{settle}}",
	} {
		if !strings.Contains(deskContinuationBuiltin, want) {
			t.Errorf("desk heartbeat prompt missing the ledger-contract fragment %q", want)
		}
	}
}

// The WakeBacklog prompt MUST name the driven item AND append the ack instruction — the latter is
// load-bearing: a continuously-driven XO that is never told to ack would falsely trip the AckAge
// wedge alert (the liveness backstop for an always-driving, never-settling XO).
func TestBacklogWakeBodyNamesItemAndAcks(t *testing.T) {
	ack := "\n(To ack you are alive, run: touch /x/alive)"
	body := backlogWakeBody([]string{"- [in-flight] ship the tactical PR"}, "/state/fleet-backlog.md", ack)
	if !strings.Contains(body, "ship the tactical PR") {
		t.Error("WakeBacklog body must NAME the driven item")
	}
	if !strings.HasSuffix(body, ack) {
		t.Error("WakeBacklog body MUST append the ack instruction (else a driven XO never acks → false wedge alert)")
	}
	if !strings.Contains(body, "/state/fleet-backlog.md") {
		t.Error("WakeBacklog body must point the XO at the backlog file (read durable state, not memory)")
	}
	if !strings.Contains(body, "NOT settle while unblocked work remains") {
		t.Error("WakeBacklog body must convey the mechanical no-settle contract")
	}
}

// backlogStatusGate must alert ONCE on the edge into a present-but-unparseable backlog (never a
// silent no-op), re-arm after a clean read, and fail SILENT (no gate, no alert) on absent/unreadable.
func TestBacklogStatusGateAlertOnceAndReArm(t *testing.T) {
	var content string
	var readErr error
	var alerts []string
	gate := backlogStatusGate("/x/backlog.md",
		func() ([]byte, error) {
			if readErr != nil {
				return nil, readErr
			}
			return []byte(content), nil
		},
		func(m string) { alerts = append(alerts, m) })

	// Present-but-unparseable (non-empty, no ## Backlog section) → alert exactly once across repeats.
	content = "# Doc\nsome prose, no backlog section here\n"
	gate()
	gate()
	gate()
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1 (alert ONCE on the edge into unparseable, not every tick)", len(alerts))
	}

	// A clean read re-arms the latch and does not alert.
	content = "## Backlog\n- [in-flight] real work\n"
	st := gate()
	if len(st.Unblocked) != 1 {
		t.Errorf("clean read Unblocked = %d, want 1", len(st.Unblocked))
	}
	if len(alerts) != 1 {
		t.Errorf("a clean read must not alert; alerts = %d, want 1", len(alerts))
	}

	// A markerless item is a new unparseable edge → re-fires (the latch re-armed).
	content = "## Backlog\n- markerless item with no status\n"
	gate()
	if len(alerts) != 2 {
		t.Errorf("a re-armed latch must re-fire on a new unparseable edge; alerts = %d, want 2", len(alerts))
	}

	// Absent/unreadable → silent: zero Status (no gate), no alert.
	readErr = errors.New("no such file")
	st = gate()
	if len(st.Unblocked) != 0 || st.Found {
		t.Errorf("unreadable must yield a zero Status (no gate); got %+v", st)
	}
	if len(alerts) != 2 {
		t.Errorf("unreadable must be SILENT (the file may not exist yet); alerts = %d, want 2", len(alerts))
	}
}

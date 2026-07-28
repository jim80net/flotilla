package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/loopposture"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestHumanizeAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{-5 * time.Second, "0s"},       // clock skew clamps, never a negative
		{900 * time.Millisecond, "1s"}, // rounds to the second
		{9 * time.Second, "9s"},
		{59 * time.Second, "59s"},
		{3*time.Minute + 12*time.Second, "3m12s"},
		{59*time.Minute + 59*time.Second, "59m59s"},
		{time.Hour + 4*time.Minute, "1h4m"},
		{23*time.Hour + 59*time.Minute, "23h59m"},
		{49 * time.Hour, "2d1h"},
	}
	for _, c := range cases {
		if got := humanizeAge(c.d); got != c.want {
			t.Errorf("humanizeAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestDeskStateLabel(t *testing.T) {
	snap := watch.Snapshot{DeskStates: map[string]surface.State{
		"infra":    surface.StateWorking,
		"research": surface.StateIdle,
		"data":     surface.StateShell, // rendered "crashed", not "shell"
		"feature":  surface.StateAwaitingInput,
	}}
	cases := map[string]string{
		"infra":    "working",
		"research": "idle",
		"data":     "crashed",
		"feature":  "awaiting-input",
		"missing":  "unknown", // not in the snapshot
	}
	for name, want := range cases {
		if got := deskStateLabel(snap, name); got != want {
			t.Errorf("deskStateLabel(%q) = %q, want %q", name, got, want)
		}
	}
	// A nil DeskStates map (no readable snapshot) reads every desk as unknown.
	if got := deskStateLabel(watch.Snapshot{}, "infra"); got != "unknown" {
		t.Errorf("deskStateLabel on empty snapshot = %q, want %q", got, "unknown")
	}
}

func TestFileAge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	now := mtime.Add(90 * time.Second)
	age, ok := fileAge(p, now)
	if !ok {
		t.Fatal("fileAge ok=false for an existing file")
	}
	if age != 90*time.Second {
		t.Errorf("fileAge = %v, want 90s", age)
	}
	if _, ok := fileAge(filepath.Join(dir, "nope"), now); ok {
		t.Error("fileAge ok=true for a missing file")
	}
}

func TestWriteStatus_WithSnapshot(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{
		{Name: "infra"}, {Name: "research"}, {Name: "data"},
	}}
	snap := watch.Snapshot{
		DeskStates: map[string]surface.State{
			"infra":    surface.StateWorking,
			"research": surface.StateIdle,
			"data":     surface.StateShell,
		},
		XOSettled: true,
	}
	dir := t.TempDir()
	snapPath := filepath.Join(dir, "flotilla-detector-state.json")
	ackPath := filepath.Join(dir, "flotilla-xo-alive")
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	for _, p := range []string{snapPath, ackPath} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		// snapshot 20s old, ack 5s old
		mt := now.Add(-20 * time.Second)
		if p == ackPath {
			mt = now.Add(-5 * time.Second)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	writeStatus(&buf, cfg, "research", snapPath, ackPath, snap, true, now, nil)
	out := buf.String()

	for _, want := range []string{
		"states as of 20s ago",
		"Fleet — 1 of 3 seats working",
		"Next — Almost no one is working — send work or pull the next queue item.",
		"XO research · last ack 5s ago · settled (idle)",
		"infra", "working",
		"research", "idle", "(XO)",
		"data", "crashed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q\n--- output ---\n%s", want, out)
		}
	}
	for _, jargon := range []string{"utilization:", "idle:", "empty-queue:", "accepts-dispatch:", "awaiting-authority:", "utilization wall"} {
		if strings.Contains(strings.ToLower(out), jargon) {
			t.Errorf("status output contains operator jargon %q\n%s", jargon, out)
		}
	}
	// The (XO) marker belongs to research, not infra.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "infra") && strings.Contains(line, "(XO)") {
			t.Errorf("(XO) marker wrongly on infra line: %q", line)
		}
	}
}

func TestEffectiveSurface(t *testing.T) {
	if got := effectiveSurface(""); got != "claude-code" {
		t.Errorf("effectiveSurface(\"\") = %q, want claude-code (the default driver)", got)
	}
	if got := effectiveSurface("aider"); got != "aider" {
		t.Errorf("effectiveSurface(\"aider\") = %q, want aider", got)
	}
}

func TestBuildStatusJSON(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{
		{Name: "xo"}, // empty surface ⇒ claude-code; this is the XO ⇒ role hub
		{Name: "frontend", Surface: "aider"},
		{Name: "data", Surface: "opencode"},
	}}
	snap := watch.Snapshot{DeskStates: map[string]surface.State{
		"xo":       surface.StateIdle,
		"frontend": surface.StateAwaitingApproval,
		"data":     surface.StateWorking,
	}}

	doc := buildStatusJSON(cfg, "xo", "2026-06-17T17:00:00Z", snap, nil)

	if doc.GeneratedAt != "2026-06-17T17:00:00Z" {
		t.Errorf("generated_at = %q", doc.GeneratedAt)
	}
	if doc.XO != "xo" {
		t.Errorf("xo = %q, want xo", doc.XO)
	}
	if len(doc.Agents) != 3 {
		t.Fatalf("got %d agents, want 3", len(doc.Agents))
	}
	if doc.Utilization.Working != 1 || doc.Utilization.Idle != 1 || doc.Utilization.IdleQueueUnknown != 1 || doc.Utilization.Total != 3 {
		t.Fatalf("utilization = %+v", doc.Utilization)
	}
	// XO: role hub, default surface claude-code, idle.
	xo := doc.Agents[0]
	if xo.Name != "xo" || xo.Role != "hub" || xo.Surface != "claude-code" || xo.State != "idle" {
		t.Errorf("xo item = %+v, want {xo hub claude-code idle}", xo)
	}
	// Non-XO desks carry no role; surface comes from the roster.
	if doc.Agents[1].Role != "" {
		t.Errorf("non-XO agent should have no role, got %q", doc.Agents[1].Role)
	}
	if doc.Agents[1].Surface != "aider" || doc.Agents[1].State != "awaiting-approval" {
		t.Errorf("frontend item = %+v", doc.Agents[1])
	}
	if doc.Agents[2].Surface != "opencode" || doc.Agents[2].State != "working" {
		t.Errorf("data item = %+v", doc.Agents[2])
	}

	// It must marshal to the widget's contract: an `agents` array + `generated_at`,
	// plus #524 loop_posture.
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"generated_at"`, `"utilization"`, `"idle_queue_unknown":1`, `"agents"`, `"name":"xo"`, `"role":"hub"`, `"state":"awaiting-approval"`, `"loop_posture"`, `"queue_state":"unknown"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("marshaled JSON missing %s\n%s", want, raw)
		}
	}
}

func TestBuildStatusJSON_LoopPostureV10(t *testing.T) {
	// V10: available vs parked vs drifted vs awaiting-authority on generic fixtures.
	cfg := &roster.Config{Agents: []roster.Agent{
		{Name: "xo"}, {Name: "backend"}, {Name: "frontend"}, {Name: "data"},
	}}
	snap := watch.Snapshot{
		DeskStates: map[string]surface.State{
			"xo":       surface.StateIdle,
			"backend":  surface.StateIdle,
			"frontend": surface.StateIdle,
			"data":     surface.StateIdle,
		},
		XOSettled: true,
	}
	loop := map[string]loopposture.Evidence{
		"xo": {
			Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true,
			Settled: true, BacklogKnown: true, UnblockedN: 0, Park: loopposture.ParkStrict,
		},
		"backend": {
			Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true,
			Settled: false, BacklogKnown: true, UnblockedN: 1, Park: loopposture.ParkStrict,
		},
		"frontend": {
			Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true,
			Settled: true, BacklogKnown: true, UnblockedN: 2, Park: loopposture.ParkStrict,
		},
		"data": {
			Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true,
			Settled: false, BacklogKnown: true, AwaitingAuthN: 1, Park: loopposture.ParkStrict,
		},
	}
	doc := buildStatusJSON(cfg, "xo", "2026-07-09T00:00:00Z", snap, loop)
	if doc.Utilization.Idle != 4 || doc.Utilization.IdleEmptyQueue != 2 || doc.Utilization.IdleHasQueue != 2 || doc.Utilization.AcceptsDispatch != 2 || doc.Utilization.AwaitingAuthority != 1 {
		t.Fatalf("utilization queue split = %+v", doc.Utilization)
	}
	want := map[string]string{
		"xo": "parked", "backend": "available", "frontend": "drifted", "data": "available",
	}
	for _, a := range doc.Agents {
		if a.LoopPosture != want[a.Name] {
			t.Errorf("%s loop_posture = %q, want %q (state=%s)", a.Name, a.LoopPosture, want[a.Name], a.State)
		}
		if a.State != "idle" {
			t.Errorf("%s pane state = %q, want idle (two-layer model)", a.Name, a.State)
		}
		if a.Name == "data" && a.RawLoopPosture != "awaiting-authority" {
			t.Errorf("data raw_loop_posture = %q, want awaiting-authority evidence", a.RawLoopPosture)
		}
	}
	var text bytes.Buffer
	writeStatus(&text, cfg, "xo", "missing-snapshot", "missing-ack", snap, false, time.Now(), loop)
	foundData := false
	for _, line := range strings.Split(text.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "data" {
			foundData = true
			if len(fields) < 3 || fields[2] != "available" || strings.Contains(line, "awaiting-authority") {
				t.Fatalf("CLI data row must show authority wait as available, got %q", line)
			}
		}
	}
	if !foundData {
		t.Fatalf("CLI status missing data row:\n%s", text.String())
	}
}

func TestWriteStatus_NoSnapshot(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "infra"}, {Name: "research"}}}
	dir := t.TempDir()
	snapPath := filepath.Join(dir, "missing.json")
	ackPath := filepath.Join(dir, "missing-ack")
	now := time.Now()

	var buf bytes.Buffer
	writeStatus(&buf, cfg, "infra", snapPath, ackPath, watch.Snapshot{}, false, now, nil)
	out := buf.String()

	for _, want := range []string{
		"no readable detector snapshot",
		"change_detector: true",
		"never acked",
		"infra", "unknown",
		"research", "unknown",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no-snapshot output missing %q\n--- output ---\n%s", want, out)
		}
	}
	// Without a snapshot we must NOT assert settled/active for the XO.
	if strings.Contains(out, "settled") || strings.Contains(out, "active") {
		t.Errorf("no-snapshot output should not assert XO settled state:\n%s", out)
	}
}

func TestStatusUsageVisibilityAndHonestAbsence(t *testing.T) {
	now := time.Date(2026, 7, 13, 6, 0, 0, 0, time.UTC)
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "alpha"}, {Name: "beta"}}}
	snap := watch.Snapshot{
		DeskStates: map[string]surface.State{"alpha": surface.StateIdle, "beta": surface.StateIdle},
		Usage: map[string]watch.UsageObservation{
			"alpha": {RemainingPercent: 8, Window: "weekly", ObservedAt: now.Add(-time.Hour), StaleAfter: now.Add(-time.Minute)},
		},
	}
	doc := buildStatusJSON(cfg, "alpha", now.Format(time.RFC3339), snap, nil)
	if doc.Agents[0].Usage == nil || doc.Agents[0].Usage.RemainingPercent != 8 {
		t.Fatalf("alpha JSON usage = %+v", doc.Agents[0].Usage)
	}
	if doc.Agents[1].Usage != nil {
		t.Fatalf("beta JSON usage = %+v, want omitted", doc.Agents[1].Usage)
	}
	if got := usageLabel(snap, "alpha", now); got != "8% weekly stale" {
		t.Fatalf("usageLabel(alpha) = %q", got)
	}
	if got := usageLabel(snap, "beta", now); got != "—" {
		t.Fatalf("usageLabel(beta) = %q, want honest absence", got)
	}
}

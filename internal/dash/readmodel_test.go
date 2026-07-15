package dash

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/loopposture"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestReferenceIntervalCeiling(t *testing.T) {
	if got := ReferenceIntervalCeiling(20 * time.Minute); got != 20*time.Minute {
		t.Errorf("ceiling(20m) = %v, want 20m", got)
	}
	t.Setenv("FLOTILLA_WATCH_INTERVAL", "15m")
	if got := ReferenceIntervalCeiling(20 * time.Minute); got != 15*time.Minute {
		t.Errorf("ceiling with env override = %v, want 15m", got)
	}
	t.Setenv("FLOTILLA_WATCH_INTERVAL", "")
}

func TestFreshnessThreshold(t *testing.T) {
	if got := FreshnessThreshold(20 * time.Minute); got != 60*time.Minute {
		t.Errorf("threshold(20m) = %v, want 60m", got)
	}
	// A non-positive (disabled) heartbeat falls back to the documented default.
	if got := FreshnessThreshold(0); got != 3*defaultHeartbeat {
		t.Errorf("threshold(0) = %v, want %v", got, 3*defaultHeartbeat)
	}
	if got := FreshnessThreshold(-5 * time.Minute); got != 3*defaultHeartbeat {
		t.Errorf("threshold(-5m) = %v, want %v", got, 3*defaultHeartbeat)
	}
}

func TestAssessFreshness(t *testing.T) {
	threshold := 60 * time.Minute
	cases := []struct {
		name   string
		snapOK bool
		age    time.Duration
		want   Freshness
	}{
		{"absent", false, 0, FreshnessAbsent},
		{"fresh-just-written", true, 0, FreshnessFresh},
		{"fresh-under-threshold", true, 59 * time.Minute, FreshnessFresh},
		{"fresh-at-threshold", true, 60 * time.Minute, FreshnessFresh}, // boundary: > threshold is stale
		{"stale-over-threshold", true, 61 * time.Minute, FreshnessStale},
	}
	for _, c := range cases {
		if got := assessFreshness(c.snapOK, c.age, threshold); got != c.want {
			t.Errorf("%s: assessFreshness(%v, %v) = %v, want %v", c.name, c.snapOK, c.age, got, c.want)
		}
	}
}

// TestBuildBoard_LoopPosture locks #524: board agents carry loop_posture and V10
// distinctions (available / parked / drifted / awaiting-authority).
func TestBuildBoard_LoopPosture(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{
		{Name: "xo"}, {Name: "backend"}, {Name: "frontend"}, {Name: "data"},
	}}
	snap := watch.Snapshot{
		DeskStates: map[string]surface.State{
			"xo": surface.StateIdle, "backend": surface.StateIdle,
			"frontend": surface.StateIdle, "data": surface.StateIdle,
		},
		XOSettled: true,
	}
	doc := BuildBoard(BoardInputs{
		Cfg: cfg, XO: "xo", Snap: snap, SnapOK: true, SnapAge: time.Second, Threshold: time.Hour,
		LoopByAgent: map[string]loopposture.Evidence{
			"xo":       {Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true, Settled: true, BacklogKnown: true, UnblockedN: 0},
			"backend":  {Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true, Settled: false, BacklogKnown: true, UnblockedN: 1},
			"frontend": {Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true, Settled: true, BacklogKnown: true, UnblockedN: 2},
			"data":     {Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true, BacklogKnown: true, AwaitingAuthN: 1},
		},
	})
	want := map[string]string{"xo": "parked", "backend": "available", "frontend": "drifted", "data": "available"}
	for _, a := range doc.Agents {
		if a.LoopPosture != want[a.Name] {
			t.Errorf("%s loop_posture = %q, want %q", a.Name, a.LoopPosture, want[a.Name])
		}
		if a.Name == "data" && a.RawLoopPosture != "awaiting-authority" {
			t.Errorf("data raw_loop_posture = %q, want awaiting-authority evidence", a.RawLoopPosture)
		}
	}
	raw, _ := json.Marshal(doc)
	if !strings.Contains(string(raw), `"loop_posture":"parked"`) {
		t.Errorf("board JSON missing parked loop_posture\n%s", raw)
	}
}

// TestBuildBoard_Fresh covers the superset contract: every base status field is
// present (generated_at, xo, agents[name,role,surface,state]) plus the freshness
// + xo_liveness additions, for a fresh snapshot.
func TestBuildBoard_Fresh(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{
		{Name: "xo"}, // empty surface ⇒ claude-code; the XO ⇒ role hub
		{Name: "frontend", Surface: "aider"},
		{Name: "data", Surface: "opencode"},
	}}
	snap := watch.Snapshot{
		DeskStates: map[string]surface.State{
			"xo":       surface.StateIdle,
			"frontend": surface.StateAwaitingApproval,
			"data":     surface.StateShell, // ⇒ "crashed"
		},
		XOSettled: true,
	}
	doc := BuildBoard(BoardInputs{
		Cfg:         cfg,
		XO:          "xo",
		GeneratedAt: "2026-06-18T12:00:00Z",
		Snap:        snap,
		SnapOK:      true,
		SnapAge:     30 * time.Second,
		AckOK:       true,
		AckAge:      5 * time.Second,
		Threshold:   60 * time.Minute,
	})

	if doc.GeneratedAt != "2026-06-18T12:00:00Z" || doc.XO != "xo" {
		t.Errorf("base header wrong: %+v", doc)
	}
	if doc.Freshness.State != "fresh" {
		t.Errorf("freshness = %q, want fresh", doc.Freshness.State)
	}
	if doc.Freshness.AgeSeconds != 30 || doc.Freshness.ThresholdSeconds != 3600 {
		t.Errorf("freshness ages = %+v", doc.Freshness)
	}
	if !doc.XOLiveness.Acked || doc.XOLiveness.AckAgeSeconds != 5 || !doc.XOLiveness.Settled || !doc.XOLiveness.SettledKnown {
		t.Errorf("xo liveness = %+v", doc.XOLiveness)
	}
	if len(doc.Agents) != 3 {
		t.Fatalf("got %d agents, want 3", len(doc.Agents))
	}
	xo := doc.Agents[0]
	if xo.Name != "xo" || xo.Role != "hub" || xo.Surface != "claude-code" || xo.State != "idle" {
		t.Errorf("xo item = %+v, want {xo hub claude-code idle}", xo)
	}
	if doc.Agents[1].Role != "" || doc.Agents[1].Surface != "aider" || doc.Agents[1].State != "awaiting-approval" {
		t.Errorf("frontend item = %+v", doc.Agents[1])
	}
	if doc.Agents[2].State != "crashed" {
		t.Errorf("data item state = %q, want crashed", doc.Agents[2].State)
	}

	// The marshaled JSON must carry the base status contract verbatim (the landing
	// widget consumes exactly these), plus the superset additions.
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"generated_at"`, `"xo":"xo"`, `"agents"`, `"name":"xo"`, `"role":"hub"`,
		`"surface":"aider"`, `"state":"awaiting-approval"`,
		`"freshness"`, `"xo_liveness"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("board JSON missing %s\n%s", want, raw)
		}
	}
}

// TestBuildBoard_CosCoordinator locks F#383 criterion 1's identity half: the board
// exposes the CoS as a distinct coordinator ONLY when the roster names one that isn't
// already the primary XO — so the conversations rail can pin the CoS thread without
// double-listing the XO. (The rail-pin half is asserted as a dash.js marker in server_test.)
func TestBuildBoard_CosCoordinator(t *testing.T) {
	base := watch.Snapshot{DeskStates: map[string]surface.State{"cos": surface.StateIdle, "alpha-xo": surface.StateIdle}}
	// Distinct CoS ⇒ exposed.
	doc := BuildBoard(BoardInputs{
		Cfg: &roster.Config{CosAgent: "cos", Agents: []roster.Agent{{Name: "alpha-xo"}, {Name: "cos"}}},
		XO:  "alpha-xo", Snap: base, SnapOK: true, Threshold: time.Hour,
	})
	if doc.Cos != "cos" {
		t.Errorf("distinct cos_agent must be exposed as doc.Cos, got %q", doc.Cos)
	}
	if raw, _ := json.Marshal(doc); !strings.Contains(string(raw), `"cos":"cos"`) {
		t.Errorf("board JSON must carry the cos field for a distinct coordinator\n%s", raw)
	}
	// CoS identical to the XO ⇒ NOT re-exposed (single-fleet dogfood: XO already IS the coordinator).
	same := BuildBoard(BoardInputs{
		Cfg: &roster.Config{CosAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}},
		XO:  "xo", Snap: watch.Snapshot{}, Threshold: time.Hour,
	})
	if same.Cos != "" {
		t.Errorf("cos_agent identical to XO must not be re-exposed, got %q", same.Cos)
	}
	// No cos_agent ⇒ empty.
	none := BuildBoard(BoardInputs{
		Cfg: &roster.Config{Agents: []roster.Agent{{Name: "xo"}}},
		XO:  "xo", Snap: watch.Snapshot{}, Threshold: time.Hour,
	})
	if none.Cos != "" {
		t.Errorf("unset cos_agent must leave doc.Cos empty, got %q", none.Cos)
	}
}

// TestBuildBoard_Absent: no snapshot ⇒ every desk unknown, generated_at empty,
// settled NOT asserted, freshness absent.
func TestBuildBoard_Absent(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "infra"}, {Name: "research"}}}
	doc := BuildBoard(BoardInputs{
		Cfg:       cfg,
		XO:        "infra",
		Snap:      watch.Snapshot{},
		SnapOK:    false,
		AckOK:     false,
		Threshold: 60 * time.Minute,
	})
	if doc.Freshness.State != "absent" {
		t.Errorf("freshness = %q, want absent", doc.Freshness.State)
	}
	if doc.GeneratedAt != "" {
		t.Errorf("generated_at = %q, want empty when absent", doc.GeneratedAt)
	}
	if doc.Freshness.Age != "" || doc.Freshness.AgeSeconds != 0 {
		t.Errorf("absent must carry no age: %+v", doc.Freshness)
	}
	if doc.XOLiveness.SettledKnown {
		t.Error("settled must NOT be asserted without a snapshot")
	}
	if doc.XOLiveness.Acked {
		t.Error("acked must be false when no ack file")
	}
	for _, a := range doc.Agents {
		if a.State != "unknown" {
			t.Errorf("desk %q state = %q, want unknown", a.Name, a.State)
		}
	}
	if !strings.Contains(doc.Freshness.Message, "no detector snapshot") {
		t.Errorf("absent banner = %q", doc.Freshness.Message)
	}
}

// TestBuildBoard_Stale: present-but-old snapshot ⇒ states shown, marked stale,
// with a "watch may be down" banner.
func TestBuildBoard_Stale(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "infra"}}}
	snap := watch.Snapshot{DeskStates: map[string]surface.State{"infra": surface.StateWorking}}
	doc := BuildBoard(BoardInputs{
		Cfg:       cfg,
		XO:        "infra",
		Snap:      snap,
		SnapOK:    true,
		SnapAge:   2 * time.Hour, // > 60m threshold
		Threshold: 60 * time.Minute,
	})
	if doc.Freshness.State != "stale" {
		t.Errorf("freshness = %q, want stale", doc.Freshness.State)
	}
	if doc.Agents[0].State != "working" {
		t.Errorf("stale still shows the state, got %q", doc.Agents[0].State)
	}
	if !strings.Contains(doc.Freshness.Message, "may be down") {
		t.Errorf("stale banner = %q", doc.Freshness.Message)
	}
}

func TestBuildBoard_UsageObservationIsOptional(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "alpha"}, {Name: "beta"}}}
	doc := BuildBoard(BoardInputs{
		Cfg:    cfg,
		XO:     "alpha",
		SnapOK: true,
		Snap: watch.Snapshot{
			DeskStates: map[string]surface.State{"alpha": surface.StateIdle, "beta": surface.StateIdle},
			Usage: map[string]watch.UsageObservation{
				"alpha": {RemainingPercent: 8, Window: "weekly"},
			},
		},
		Threshold: time.Minute,
	})
	if doc.Agents[0].Usage == nil || doc.Agents[0].Usage.RemainingPercent != 8 {
		t.Fatalf("alpha dash usage = %+v", doc.Agents[0].Usage)
	}
	if doc.Agents[1].Usage != nil {
		t.Fatalf("beta dash usage = %+v, want omitted", doc.Agents[1].Usage)
	}
}

func TestBuildTopology_SingleFleet(t *testing.T) {
	// Legacy single channel_id + xo_agent ⇒ one synthesized binding, every agent a member.
	cfg, err := loadInlineRoster(t, `{
		"channel_id": "C123",
		"xo_agent": "xo",
		"agents": [{"name": "xo"}, {"name": "alpha"}, {"name": "beta"}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildTopology(cfg)
	if len(doc.Channels) != 1 {
		t.Fatalf("single-fleet should render exactly one binding, got %d", len(doc.Channels))
	}
	ch := doc.Channels[0]
	if ch.ChannelID != "C123" || ch.XOAgent != "xo" {
		t.Errorf("binding = %+v", ch)
	}
	if strings.Join(ch.Members, ",") != "xo,alpha,beta" {
		t.Errorf("members = %v, want all agents", ch.Members)
	}
	if doc.Note != "" {
		t.Errorf("single-fleet should have no note, got %q", doc.Note)
	}
	// org-truth PR4: derived org DAG is always attached after Load
	if doc.OrgSource != "derived" {
		t.Errorf("org_source=%q want derived", doc.OrgSource)
	}
	if len(doc.OrgNodes) == 0 {
		t.Error("expected org_nodes from derived DAG")
	}
}

func TestBuildTopology_OrgNodesFromFile(t *testing.T) {
	// Federated roster + agreeing fleet-org.yaml → org_source=file and parents match.
	dir := t.TempDir()
	rosterPath := dir + "/flotilla.json"
	orgPath := dir + "/fleet-org.yaml"
	rosterBody := `{
		"xo_agent": "xo",
		"agents": [{"name": "xo"}, {"name": "alpha-xo"}, {"name": "backend"}],
		"channels": [
			{"channel_id": "C_CMD", "xo_agent": "xo", "role": "fleet-command", "members": ["xo", "alpha-xo", "backend"]},
			{"channel_id": "C_ALPHA", "xo_agent": "alpha-xo", "members": ["xo"]},
			{"channel_id": "C_BE", "xo_agent": "backend", "members": ["alpha-xo"]}
		]
	}`
	orgBody := `version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
  - id: alpha-xo
    kind: coordinator
    reports_to: xo
    home_channel_id: "C_ALPHA"
  - id: backend
    kind: desk
    reports_to: alpha-xo
    home_channel_id: "C_BE"
`
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orgPath, []byte(orgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildTopology(cfg)
	if doc.OrgSource != "file" {
		t.Fatalf("org_source=%q want file", doc.OrgSource)
	}
	if doc.OrgRoot != "xo" {
		t.Errorf("org_root=%q", doc.OrgRoot)
	}
	byID := map[string]TopologyOrgNode{}
	for _, n := range doc.OrgNodes {
		byID[n.ID] = n
	}
	if byID["backend"].Parent != "alpha-xo" {
		t.Errorf("backend parent=%q", byID["backend"].Parent)
	}
	if byID["alpha-xo"].Parent != "xo" {
		t.Errorf("alpha-xo parent=%q", byID["alpha-xo"].Parent)
	}
}

func TestBuildTopology_Federated(t *testing.T) {
	cfg, err := loadInlineRoster(t, `{
		"xo_agent": "meta",
		"agents": [{"name": "meta"}, {"name": "xo-a"}, {"name": "desk-a"}],
		"channels": [
			{"channel_id": "Cmeta", "xo_agent": "meta", "members": ["xo-a"], "role": "fleet-command"},
			{"channel_id": "Ca", "xo_agent": "xo-a", "members": ["desk-a"], "role": "project"}
		]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildTopology(cfg)
	if len(doc.Channels) != 2 {
		t.Fatalf("federated should render two bindings, got %d", len(doc.Channels))
	}
	if doc.Channels[0].Role != "fleet-command" || doc.Channels[1].XOAgent != "xo-a" {
		t.Errorf("federated bindings = %+v", doc.Channels)
	}
}

// TestBuildTopology_Coordinators: the coordinator set is primary XO + CoS + binding XOs with
// span of control (#460); member-only desks and solo mirror-channel owners are excluded.
func TestBuildTopology_Coordinators(t *testing.T) {
	cfg, err := loadInlineRoster(t, `{
		"xo_agent": "meta",
		"cos_agent": "cos",
		"agents": [{"name": "meta"}, {"name": "cos"}, {"name": "xo-a"}, {"name": "desk-x"}],
		"channels": [
			{"channel_id": "Cmeta", "xo_agent": "meta", "members": ["cos", "xo-a", "desk-x"], "role": "fleet-command"},
			{"channel_id": "Ca", "xo_agent": "xo-a", "members": [], "role": "project"}
		]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildTopology(cfg)
	// #460: xo-a owns Ca with no subordinate members — not a coordinator; desk-x is member-only.
	want := []string{"cos", "meta"}
	if len(doc.Coordinators) != len(want) {
		t.Fatalf("coordinators = %v, want %v", doc.Coordinators, want)
	}
	for i, c := range want {
		if doc.Coordinators[i] != c {
			t.Errorf("coordinators[%d] = %q, want %q (full: %v)", i, doc.Coordinators[i], c, doc.Coordinators)
		}
	}
	for _, excluded := range []string{"desk-x", "xo-a"} {
		for _, c := range doc.Coordinators {
			if c == excluded {
				t.Errorf("%q must NOT be a coordinator", excluded)
			}
		}
	}
}

// TestBuildTopology_CoordinatorTierOnly502 pins the Fleet Command rail's source set against
// the FULL deployment shape that produced #502 (execution desks on the rail): every desk owns
// a solo mirror channel (the #460 trap), supervisors appear BOTH ways (#481 dual-shape: a
// shape-1 XO whose home lists a non-XO subordinate, and shape-2 supervisors listed as the
// sole observers on desk-home channels), and everyone is a fleet-command member. The rail
// must show the coordinator TIER only — no execution desk qualifies through any shape.
//
// Shape-2 supervisors also own empty mirrors (Cxf/Cxo) here to match live deployments where
// every agent owns a channel. #507 closed the hole where omitting those mirrors inverted
// classification — see TestBuildTopology_SupervisorWithoutOwnedChannel507.
func TestBuildTopology_CoordinatorTierOnly502(t *testing.T) {
	cfg, err := loadInlineRoster(t, `{
		"xo_agent": "cos",
		"agents": [{"name": "cos"}, {"name": "xo-fleet"}, {"name": "xo-proj"}, {"name": "xo-observer"},
			{"name": "trial-xo"}, {"name": "backend"}, {"name": "frontend"}, {"name": "data"}, {"name": "builder"}],
		"channels": [
			{"channel_id": "Ccmd", "xo_agent": "cos", "members": ["cos", "xo-fleet", "xo-proj", "xo-observer", "trial-xo", "backend", "frontend", "data", "builder"], "role": "fleet-command"},
			{"channel_id": "Cxf", "xo_agent": "xo-fleet", "members": []},
			{"channel_id": "Cxo", "xo_agent": "xo-observer", "members": []},
			{"channel_id": "Cbe", "xo_agent": "backend", "members": ["xo-fleet"]},
			{"channel_id": "Cfe", "xo_agent": "frontend", "members": ["xo-fleet"]},
			{"channel_id": "Cda", "xo_agent": "data", "members": []},
			{"channel_id": "Cpr", "xo_agent": "xo-proj", "members": ["cos", "xo-proj", "builder"]},
			{"channel_id": "Ctr", "xo_agent": "trial-xo", "members": ["cos", "xo-observer"]}
		]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildTopology(cfg)
	// cos = primary; xo-fleet supervises two desk-homes (shape-2); xo-proj has a non-XO
	// subordinate (shape-1); xo-observer supervises the trial XO's home (shape-2).
	want := []string{"cos", "xo-fleet", "xo-observer", "xo-proj"}
	if len(doc.Coordinators) != len(want) {
		t.Fatalf("coordinators = %v, want %v", doc.Coordinators, want)
	}
	for i, c := range want {
		if doc.Coordinators[i] != c {
			t.Errorf("coordinators[%d] = %q, want %q (full: %v)", i, doc.Coordinators[i], c, doc.Coordinators)
		}
	}
	// The exclusions ARE the regression: a solo mirror owner (data), supervised desk-home
	// owners (backend/frontend), a member-only desk (builder), and a supervised XO whose
	// own home holds only XO observers (trial-xo) must never reach the rail's set.
	for _, excluded := range []string{"backend", "frontend", "data", "builder", "trial-xo"} {
		for _, c := range doc.Coordinators {
			if c == excluded {
				t.Errorf("%q is an execution-tier agent and must NOT be a rail coordinator (#502)", excluded)
			}
		}
	}
}

// TestBuildTopology_SupervisorWithoutOwnedChannel507 is the #507 pin: same coordinator tier
// as #502 but shape-2 supervisors (xo-fleet, xo-observer) own NO mirror channels. Span must
// still classify from membership alone — no inversion to [backend, cos, frontend, trial-xo, …].
func TestBuildTopology_SupervisorWithoutOwnedChannel507(t *testing.T) {
	cfg, err := loadInlineRoster(t, `{
		"xo_agent": "cos",
		"agents": [{"name": "cos"}, {"name": "xo-fleet"}, {"name": "xo-proj"}, {"name": "xo-observer"},
			{"name": "trial-xo"}, {"name": "backend"}, {"name": "frontend"}, {"name": "data"}, {"name": "builder"}],
		"channels": [
			{"channel_id": "Ccmd", "xo_agent": "cos", "members": ["cos", "xo-fleet", "xo-proj", "xo-observer", "trial-xo", "backend", "frontend", "data", "builder"], "role": "fleet-command"},
			{"channel_id": "Cbe", "xo_agent": "backend", "members": ["xo-fleet"]},
			{"channel_id": "Cfe", "xo_agent": "frontend", "members": ["xo-fleet"]},
			{"channel_id": "Cda", "xo_agent": "data", "members": []},
			{"channel_id": "Cpr", "xo_agent": "xo-proj", "members": ["cos", "xo-proj", "builder"]},
			{"channel_id": "Ctr", "xo_agent": "trial-xo", "members": ["cos", "xo-observer"]}
		]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	// Premise: supervisors own nothing.
	for _, name := range []string{"xo-fleet", "xo-observer"} {
		if cfg.IsXO(name) {
			t.Fatalf("%q owns a channel — fixture must omit supervisor mirrors", name)
		}
		if !cfg.IsCoordinator(name) {
			t.Errorf("IsCoordinator(%q)=false; membership-only supervisor must classify (#507)", name)
		}
	}
	doc := BuildTopology(cfg)
	want := []string{"cos", "xo-fleet", "xo-observer", "xo-proj"}
	if len(doc.Coordinators) != len(want) {
		t.Fatalf("coordinators = %v, want %v (pre-#507 inverted to desks)", doc.Coordinators, want)
	}
	for i, c := range want {
		if doc.Coordinators[i] != c {
			t.Errorf("coordinators[%d] = %q, want %q (full: %v)", i, doc.Coordinators[i], c, doc.Coordinators)
		}
	}
	for _, excluded := range []string{"backend", "frontend", "data", "builder", "trial-xo"} {
		for _, c := range doc.Coordinators {
			if c == excluded {
				t.Errorf("%q must NOT be a rail coordinator without supervisor mirrors (#507)", excluded)
			}
		}
	}
}

func TestBuildTopology_ClockOnly(t *testing.T) {
	// No channel_id and no channels[] ⇒ no bindings, an explanatory note.
	cfg, err := loadInlineRoster(t, `{"xo_agent": "xo", "agents": [{"name": "xo"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildTopology(cfg)
	if len(doc.Channels) != 0 {
		t.Errorf("clock-only should render no bindings, got %d", len(doc.Channels))
	}
	if doc.Note == "" {
		t.Error("clock-only should carry an explanatory note")
	}
}

// TestBuildTopology_NoAlias: the topology must not alias the roster's Members
// slice (Bindings shares the header in the federation path).
func TestBuildTopology_NoAlias(t *testing.T) {
	cfg, err := loadInlineRoster(t, `{
		"agents": [{"name": "xo"}, {"name": "d1"}],
		"channels": [{"channel_id": "C1", "xo_agent": "xo", "members": ["d1"]}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	doc := BuildTopology(cfg)
	doc.Channels[0].Members[0] = "MUTATED"
	if cfg.Channels[0].Members[0] != "d1" {
		t.Error("BuildTopology aliased the roster's Members slice — mutation leaked back")
	}
}

func TestParseLedgerLine(t *testing.T) {
	// A real cos.Line render round-trips into structured fields.
	line := strings.TrimRight(cos.Line(cos.Entry{
		Time:    time.Date(2026, 6, 18, 14, 3, 5, 0, time.UTC),
		Channel: "C123",
		From:    "operator",
		To:      "xo",
		Gist:    "ship the dash",
	}), "\n")
	e := ParseLedgerLine(line)
	if !e.Parsed {
		t.Fatalf("expected a parsed line, got raw-only: %q", line)
	}
	if e.Time != "2026-06-18T14:03:05Z" || e.Channel != "C123" || e.From != "operator" || e.To != "xo" || e.Gist != "ship the dash" {
		t.Errorf("parsed entry = %+v", e)
	}

	// An empty channel renders "-" and still parses.
	line2 := strings.TrimRight(cos.Line(cos.Entry{
		Time: time.Date(2026, 6, 18, 14, 4, 0, 0, time.UTC),
		From: "xo", To: "operator", Gist: "done",
	}), "\n")
	e2 := ParseLedgerLine(line2)
	if !e2.Parsed || e2.Channel != "-" || e2.From != "xo" || e2.To != "operator" {
		t.Errorf("empty-channel entry = %+v", e2)
	}

	// A gist containing the field separator stays intact (SplitN bounds the split).
	line3 := strings.TrimRight(cos.Line(cos.Entry{
		Time:    time.Date(2026, 6, 18, 14, 5, 0, 0, time.UTC),
		Channel: "C1", From: "operator", To: "xo", Gist: "a · b · c",
	}), "\n")
	e3 := ParseLedgerLine(line3)
	if !e3.Parsed || e3.Gist != "a · b · c" {
		t.Errorf("separator-bearing gist mis-parsed: %+v", e3)
	}

	// A non-conforming line is carried verbatim, never dropped.
	junk := ParseLedgerLine("just some freeform note")
	if junk.Parsed || junk.Raw != "just some freeform note" {
		t.Errorf("non-conforming line = %+v, want raw-only", junk)
	}
}

func TestBuildHistory(t *testing.T) {
	ledger := strings.Join([]string{
		strings.TrimRight(cos.Line(cos.Entry{Time: time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC), Channel: "C1", From: "operator", To: "xo", Gist: "first"}), "\n"),
		strings.TrimRight(cos.Line(cos.Entry{Time: time.Date(2026, 6, 18, 11, 0, 0, 0, time.UTC), Channel: "C1", From: "xo", To: "operator", Gist: "second"}), "\n"),
		"", // trailing blank line is skipped
	}, "\n")
	backlogMD := "## Backlog\n- [in-flight] build dash\n- [blocked] await review\n- [awaiting-auth] flip feed @operator\n- [done] design\n"

	doc := BuildHistory(ledger, backlogMD)
	if len(doc.Ledger) != 2 {
		t.Fatalf("got %d ledger entries, want 2", len(doc.Ledger))
	}
	// Reverse-chronological: most recent ("second") first.
	if doc.Ledger[0].Gist != "second" || doc.Ledger[1].Gist != "first" {
		t.Errorf("ledger not reverse-chronological: %+v", doc.Ledger)
	}
	if !doc.Backlog.Found {
		t.Error("backlog section not found")
	}
	if len(doc.Backlog.Unblocked) != 1 || doc.Backlog.Blocked != 1 || doc.Backlog.Done != 1 {
		t.Errorf("backlog classification = %+v", doc.Backlog)
	}
	// The authorizations ledger is surfaced separately from blocked (the split's whole rationale).
	if doc.Backlog.AwaitingAuth != 1 {
		t.Errorf("backlog AwaitingAuth = %d, want 1 (the authorizations ledger must reach the read-model)", doc.Backlog.AwaitingAuth)
	}
	// And it is encoded under its own JSON key so the dash can display it.
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"awaiting_auth":1`) {
		t.Errorf("read-model JSON missing awaiting_auth:1: %s", raw)
	}
}

// TestBuildHistory_NoAwaitingAuth pins backward-compatibility: a backlog with no
// [awaiting-auth] items reports zero — the dash projection is additive.
func TestBuildHistory_NoAwaitingAuth(t *testing.T) {
	doc := BuildHistory("", "## Backlog\n- [in-flight] x\n- [blocked] y\n- [done] z\n")
	if doc.Backlog.AwaitingAuth != 0 {
		t.Errorf("AwaitingAuth = %d, want 0 (no awaiting-auth items)", doc.Backlog.AwaitingAuth)
	}
}

func TestBuildHistory_Empty(t *testing.T) {
	doc := BuildHistory("", "")
	if len(doc.Ledger) != 0 {
		t.Errorf("empty ledger should yield no entries, got %d", len(doc.Ledger))
	}
	// JSON must encode an empty array, never null (the frontend iterates it).
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ledger":[]`) || !strings.Contains(string(raw), `"unblocked":[]`) {
		t.Errorf("empty history must encode [] not null: %s", raw)
	}
}

func TestBuildHistoryPageStableIdentityAndDeskSignature(t *testing.T) {
	base := strings.Join([]string{
		`- 2026-07-15T00:01:00Z · alpha-room · operator → @alpha · "one"`,
		`- 2026-07-15T00:02:00Z · beta-room · operator → @beta · "two"`,
		`- 2026-07-15T00:03:00Z · alpha-room · @alpha → operator · "three"`,
	}, "\n")
	alpha := BuildHistoryPage(base, "", HistoryPageOptions{Desk: "alpha", Limit: 10})
	if len(alpha.Ledger) != 2 || alpha.Ledger[0].ID != "3" || alpha.Ledger[1].ID != "1" {
		t.Fatalf("alpha page = %+v", alpha.Ledger)
	}

	// An unrelated append changes neither the selected thread signature nor its
	// existing source identities, so a metadata tick can remain metadata-only.
	withBetaAppend := base + `
- 2026-07-15T00:04:00Z · beta-room · @beta → operator · "four"`
	alphaAfter := BuildHistoryPage(withBetaAppend, "", HistoryPageOptions{Desk: "alpha", Limit: 10})
	if alphaAfter.Signature != alpha.Signature || alphaAfter.Ledger[0].ID != "3" || alphaAfter.Ledger[1].ID != "1" {
		t.Fatalf("unrelated append moved alpha history: before=%+v after=%+v", alpha, alphaAfter)
	}

	withAlphaAppend := withBetaAppend + `
- 2026-07-15T00:05:00Z · alpha-room · operator → @alpha · "five"`
	changed := BuildHistoryPage(withAlphaAppend, "", HistoryPageOptions{Desk: "alpha", Limit: 10})
	if changed.Signature == alpha.Signature || changed.Ledger[0].ID != "5" {
		t.Fatalf("selected-desk append not reflected: %+v", changed)
	}
}

func TestBuildHistoryPageRawParticipantBoundary(t *testing.T) {
	raw := strings.Join([]string{
		"malformed note for alpha-xo only",
		"malformed note for @alpha only",
	}, "\n")
	page := BuildHistoryPage(raw, "", HistoryPageOptions{Desk: "alpha", Limit: 10})
	if len(page.Ledger) != 1 || page.Ledger[0].ID != "2" {
		t.Fatalf("raw participant boundary page = %+v", page.Ledger)
	}
}

// TestStatusVocabularyParity pins the reimplemented label helpers to the same
// vocabulary cmd/flotilla/status.go defines (the contract of record).
func TestStatusVocabularyParity(t *testing.T) {
	if effectiveSurface("") != "claude-code" || effectiveSurface("grok") != "grok" {
		t.Error("effectiveSurface drifted from the status contract")
	}
	snap := watch.Snapshot{DeskStates: map[string]surface.State{
		"a": surface.StateWorking, "b": surface.StateIdle, "c": surface.StateShell,
		"d": surface.StateAwaitingInput, "e": surface.StateAwaitingApproval, "f": surface.StateErrored,
	}}
	want := map[string]string{
		"a": "working", "b": "idle", "c": "crashed", "d": "awaiting-input",
		"e": "awaiting-approval", "f": "errored", "missing": "unknown",
	}
	for name, w := range want {
		if got := deskStateLabel(snap, name); got != w {
			t.Errorf("deskStateLabel(%q) = %q, want %q", name, got, w)
		}
	}
}

func TestHumanizeAge(t *testing.T) {
	cases := map[time.Duration]string{
		0:                              "0s",
		-5 * time.Second:               "0s",
		9 * time.Second:                "9s",
		3*time.Minute + 12*time.Second: "3m12s",
		time.Hour + 4*time.Minute:      "1h4m",
		49 * time.Hour:                 "2d1h",
	}
	for d, want := range cases {
		if got := humanizeAge(d); got != want {
			t.Errorf("humanizeAge(%v) = %q, want %q", d, got, want)
		}
	}
}

package dash

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

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

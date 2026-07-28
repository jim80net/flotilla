package dash

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dash/tracker"
	"github.com/jim80net/flotilla/internal/roster"
)

type observedLedgerTracker struct {
	*fakeTracker
	started      chan<- struct{}
	release      <-chan struct{}
	active, peak *atomic.Int32
	total        *atomic.Int32
}

func (t *observedLedgerTracker) List(_ context.Context, _ tracker.ListFilter) ([]tracker.Issue, error) {
	t.total.Add(1)
	active := t.active.Add(1)
	defer t.active.Add(-1)
	for {
		peak := t.peak.Load()
		if active <= peak || t.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	t.started <- struct{}{}
	<-t.release
	return nil, nil
}

const multiRepoLedgerRoster = `{
  "operator_user_id":"U", "xo_agent":"root",
  "agents":[
    {"name":"root"},
    {"name":"alpha-xo","coordinator":true,"primary_repo":"acme/alpha"},
    {"name":"alpha-desk","primary_repo":"acme/alpha"},
    {"name":"beta-xo","coordinator":true,"primary_repo":"acme/beta"},
    {"name":"beta-desk","primary_repo":"acme/beta"},
    {"name":"delta-xo","coordinator":true,"work_ledger_repositoryless":true},
    {"name":"gamma-xo","coordinator":true}
  ],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"root","role":"fleet-command","members":["root","alpha-xo","beta-xo","delta-xo","gamma-xo"]},
    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["root"]},
    {"channel_id":"C_ALPHA_DESK","xo_agent":"alpha-desk","members":["alpha-xo"]},
    {"channel_id":"C_BETA","xo_agent":"beta-xo","members":["root"]},
    {"channel_id":"C_BETA_DESK","xo_agent":"beta-desk","members":["beta-xo"]},
    {"channel_id":"C_DELTA","xo_agent":"delta-xo","members":["root"]},
    {"channel_id":"C_GAMMA","xo_agent":"gamma-xo","members":["root"]}
  ]
}`

func TestBuildWorkLedgerGroupsRealIssuesByFlotillaAndDesk(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	orgPath := filepath.Join(dir, "fleet-org.yaml")
	rosterBody := `{
  "operator_user_id":"U", "xo_agent":"meta-xo",
  "agents":[
    {"name":"meta-xo"},
    {"name":"alpha-xo","coordinator":true},
    {"name":"backend","primary_repo":"acme/product"}
  ],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command","members":["meta-xo","alpha-xo","backend"]},
    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["meta-xo"]},
    {"channel_id":"C_BE","xo_agent":"backend","members":["alpha-xo"]}
  ]
}`
	orgBody := `version: 1
root: meta-xo
nodes:
  - id: meta-xo
    kind: coordinator
  - id: alpha-xo
    kind: coordinator
    reports_to: meta-xo
    home_channel_id: C_ALPHA
  - id: backend
    kind: desk
    reports_to: alpha-xo
    home_channel_id: C_BE
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
	if got := flotillaForDesk(cfg, "alpha-xo"); got != "alpha-xo" {
		t.Fatalf("flotillaForDesk(alpha-xo) = %q, want alpha-xo", got)
	}
	if got := flotillaForDesk(cfg, "backend"); got != "alpha-xo" {
		t.Fatalf("flotillaForDesk(backend) = %q, want alpha-xo", got)
	}
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	openedAt := now.Add(-6 * time.Hour).Format(time.RFC3339)
	issues := []tracker.Issue{
		{Number: 10, Title: "active build", State: "OPEN", Desk: "backend", CreatedAt: openedAt},
		{Number: 11, Title: "recent ship", State: "CLOSED", Desk: "backend", ClosedAt: now.Add(-48 * time.Hour).Format(time.RFC3339)},
		{Number: 12, Title: "old close", State: "CLOSED", Desk: "backend", ClosedAt: now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)},
		// No desk trailer: repo attribution remains honest-unassigned within alpha.
		{Number: 13, Title: "repo-attributed ship", State: "CLOSED", ClosedAt: now.Add(-time.Hour).Format(time.RFC3339)},
	}
	goals := GoalsDoc{Goals: []RenderedGoal{{
		ID: "ship", Title: "Ship the product", Owner: "backend",
		WorkItems: []RenderedWorkItem{{Kind: "issue", Ref: "acme/product#10", Class: "in-flight", Detail: "open"}},
	}}}

	doc := BuildWorkLedger("acme/product", issues, goals, cfg, now)
	if doc.InFlightCount != 1 || doc.ShippedCount != 2 {
		t.Fatalf("counts = in-flight %d shipped %d", doc.InFlightCount, doc.ShippedCount)
	}
	if len(doc.Flotillas) != 1 || doc.Flotillas[0].Name != "alpha-xo" {
		t.Fatalf("flotillas = %+v, want alpha-xo", doc.Flotillas)
	}
	if len(doc.Flotillas[0].Desks) != 1 {
		t.Fatalf("desks = %+v, want repository-attributed backend", doc.Flotillas[0].Desks)
	}
	var backend *WorkLedgerDesk
	for i := range doc.Flotillas[0].Desks {
		d := &doc.Flotillas[0].Desks[i]
		switch d.Name {
		case "backend":
			backend = d
		}
	}
	if backend == nil || len(backend.InFlight) != 1 || len(backend.Shipped) != 2 {
		t.Fatalf("backend = %+v", backend)
	}
	if backend.InFlight[0].GoalTitle != "Ship the product" {
		t.Errorf("goal context = %+v", backend.InFlight[0])
	}
	if backend.InFlight[0].Issue.CreatedAt != openedAt {
		t.Errorf("open timestamp = %q, want %q", backend.InFlight[0].Issue.CreatedAt, openedAt)
	}
}

func TestHandleWorkLedgerReusesOneTrackerSnapshotForGoals(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC))
	fake := &fakeTracker{issues: []tracker.Issue{{
		Number: 1, Title: "moving", State: "OPEN", Body: "goal-id: ship",
	}}}
	srv.tracker = fake
	srv.cfg.Repo = "acme/product"
	srv.ledgerTrackers = map[string]tracker.Tracker{"acme/product": fake}

	rec := doGet(t, srv, "/api/work-ledger?state=all")
	if rec.Code != 200 {
		t.Fatalf("work-ledger status = %d, body=%s", rec.Code, rec.Body.String())
	}
	timing := rec.Header().Get("Server-Timing")
	for _, stage := range []string{"github-list;dur=", "derive;dur=", "total;dur="} {
		if !strings.Contains(timing, stage) {
			t.Fatalf("Server-Timing = %q, want %q", timing, stage)
		}
	}
	if fake.calls != 1 {
		t.Fatalf("tracker List calls = %d, want one shared issue snapshot", fake.calls)
	}
	if fake.lastFilter.State != "all" || fake.lastFilter.Limit != 200 || !fake.lastFilter.IncludeBody {
		t.Fatalf("work-ledger filter = %+v", fake.lastFilter)
	}
}

func TestBuildMultiRepoWorkLedgerShowsUnlinkedOpenWorkByRepositoryOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(path, []byte(multiRepoLedgerRoster), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	coverage := WorkLedgerCoverage{Complete: false, ExpectedRepos: 2, IndexedRepos: []string{"acme/alpha", "acme/beta"}, UnmappedDomains: []string{"gamma-xo"}}
	doc := BuildMultiRepoWorkLedger("acme/alpha", []WorkLedgerRepoIssues{
		{Repo: "acme/alpha", Issues: []tracker.Issue{{Number: 7, Title: "unlinked alpha", State: "OPEN"}}},
		{Repo: "acme/beta", Issues: []tracker.Issue{{Number: 7, Title: "same number, other repo", State: "OPEN"}}},
	}, GoalsDoc{}, cfg, coverage, now)
	if doc.InFlightCount != 2 || len(doc.Flotillas) != 2 {
		t.Fatalf("ledger = %+v, want two visible moving product buckets", doc)
	}
	got := map[string]string{}
	for _, flotilla := range doc.Flotillas {
		for _, desk := range flotilla.Desks {
			if len(desk.InFlight) == 1 {
				got[flotilla.Name] = desk.InFlight[0].Repo
			}
		}
	}
	if got["alpha-xo"] != "acme/alpha" || got["beta-xo"] != "acme/beta" {
		t.Fatalf("repo attribution = %+v", got)
	}
}

func TestHandleWorkLedgerReturnsPartialCoverageWithoutDroppingCleanRepos(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, multiRepoLedgerRoster, now)
	alpha := &fakeTracker{issues: []tracker.Issue{{Number: 1, Title: "moving", State: "OPEN"}}}
	beta := &fakeTracker{err: tracker.ErrRateLimited}
	srv.cfg.Repo = "acme/alpha"
	srv.tracker = alpha
	srv.ledgerTrackers = map[string]tracker.Tracker{"acme/alpha": alpha, "acme/beta": beta}
	rec := doGet(t, srv, "/api/work-ledger?state=all")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var doc WorkLedgerDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Coverage.Complete || len(doc.Coverage.FailedRepos) != 1 || doc.Coverage.FailedRepos[0].Repo != "acme/beta" {
		t.Fatalf("coverage = %+v", doc.Coverage)
	}
	if doc.InFlightCount != 1 || len(doc.Flotillas) != 1 || doc.Flotillas[0].Name != "alpha-xo" {
		t.Fatalf("clean partial result = %+v", doc)
	}
	if len(doc.Coverage.UnmappedDomains) != 1 || doc.Coverage.UnmappedDomains[0] != "gamma-xo" {
		t.Fatalf("unmapped domains = %+v", doc.Coverage.UnmappedDomains)
	}
	states := map[string]string{}
	for _, domain := range doc.Coverage.Domains {
		states[domain.Name] = domain.State
	}
	wantStates := map[string]string{
		"alpha-xo": "mapped", "beta-xo": "failed", "delta-xo": "repository-less", "gamma-xo": "missing",
	}
	for name, want := range wantStates {
		if states[name] != want {
			t.Fatalf("domain states = %+v, want %s=%s", states, name, want)
		}
	}
}

func TestWorkLedgerReposCoalescesMappingsAndKeepsSafetyBound(t *testing.T) {
	cfg := &roster.Config{Agents: []roster.Agent{
		{Name: "alpha", PrimaryRepo: "example/shared", SecondaryRepos: []string{"example/extra"}},
		{Name: "beta", PrimaryRepo: "EXAMPLE/shared"},
	}}
	for i := 0; i < maxWorkLedgerRepos+4; i++ {
		cfg.Agents = append(cfg.Agents, roster.Agent{Name: "desk-" + strconv.Itoa(i), PrimaryRepo: fmt.Sprintf("example/repo-%02d", i)})
	}

	selected, omitted, _ := workLedgerRepos("example/shared", GoalsDoc{}, cfg)
	if len(selected) != maxWorkLedgerRepos || len(omitted) != 6 {
		t.Fatalf("selected=%d omitted=%d, want %d/6", len(selected), len(omitted), maxWorkLedgerRepos)
	}
	shared := 0
	for _, repo := range append(append([]string{}, selected...), omitted...) {
		if strings.EqualFold(repo, "example/shared") {
			shared++
		}
	}
	if shared != 1 {
		t.Fatalf("coalesced shared repo count = %d, repos=%v omitted=%v", shared, selected, omitted)
	}
}

func TestHandleWorkLedgerBoundsParallelRepositoryReads(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	coordinator := true
	srv.roster = &roster.Config{}
	srv.ledgerTrackers = map[string]tracker.Tracker{}
	started := make(chan struct{}, 12)
	release := make(chan struct{})
	var active, peak, total atomic.Int32
	for i := 0; i < 12; i++ {
		repo := fmt.Sprintf("example/product-%02d", i)
		srv.roster.Agents = append(srv.roster.Agents, roster.Agent{Name: fmt.Sprintf("product-%02d", i), Coordinator: &coordinator, PrimaryRepo: repo})
		srv.ledgerTrackers[repo] = &observedLedgerTracker{
			fakeTracker: &fakeTracker{}, started: started, release: release,
			active: &active, peak: &peak, total: &total,
		}
	}
	srv.cfg.Repo = "example/product-00"
	srv.tracker = srv.ledgerTrackers[srv.cfg.Repo]

	done := make(chan int, 1)
	go func() { done <- doGet(t, srv, "/api/work-ledger?state=all").Code }()
	for i := 0; i < workLedgerFanoutParallel; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("only %d repository reads started before timeout", i)
		}
	}
	if got := peak.Load(); got != workLedgerFanoutParallel {
		t.Fatalf("peak parallel reads = %d, want %d", got, workLedgerFanoutParallel)
	}
	select {
	case <-started:
		t.Fatal("a fifth repository read started while all four workers were blocked")
	default:
	}
	close(release)
	if status := <-done; status != 200 {
		t.Fatalf("work-ledger status = %d", status)
	}
	if got := total.Load(); got != 12 {
		t.Fatalf("repository reads = %d, want 12", got)
	}
}

func TestOrderedWorkLedgerFlotillasRanksMovingWorkFirst(t *testing.T) {
	groups := map[string][]WorkLedgerDesk{
		"alpha": {
			{Name: "alpha-desk", Shipped: []WorkLedgerItem{{Issue: tracker.Issue{Number: 1}}}},
		},
		"zeta": {
			{Name: "a-shipped", Shipped: []WorkLedgerItem{{Issue: tracker.Issue{Number: 2}}}},
			{Name: "z-moving", InFlight: []WorkLedgerItem{{Issue: tracker.Issue{Number: 3}}}},
		},
	}

	got := orderedWorkLedgerFlotillas(groups, nil)
	if len(got) != 2 || got[0].Name != "zeta" {
		t.Fatalf("flotillas = %+v, want moving zeta first", got)
	}
	if len(got[0].Desks) != 2 || got[0].Desks[0].Name != "z-moving" {
		t.Fatalf("zeta desks = %+v, want moving desk first", got[0].Desks)
	}
}

func TestOrderedWorkLedgerFlotillasPreservesRosterRankWithinMovingWork(t *testing.T) {
	groups := map[string][]WorkLedgerDesk{
		"alpha": {{Name: "alpha-desk", InFlight: []WorkLedgerItem{{Issue: tracker.Issue{Number: 1}}}}},
		"beta": {
			{Name: "late-desk", InFlight: []WorkLedgerItem{{Issue: tracker.Issue{Number: 2}}}},
			{Name: "early-desk", InFlight: []WorkLedgerItem{{Issue: tracker.Issue{Number: 3}}}},
		},
	}
	cfg := &roster.Config{
		Agents: []roster.Agent{{Name: "early-desk"}, {Name: "late-desk"}, {Name: "alpha-desk"}},
		Channels: []roster.Channel{
			{ChannelID: "C_BETA", XOAgent: "beta", Role: "project"},
			{ChannelID: "C_ALPHA", XOAgent: "alpha", Role: "project"},
		},
	}

	got := orderedWorkLedgerFlotillas(groups, cfg)
	if len(got) != 2 || got[0].Name != "beta" {
		t.Fatalf("flotillas = %+v, want roster-ranked beta first", got)
	}
	if got[0].Desks[0].Name != "early-desk" {
		t.Fatalf("beta desks = %+v, want roster-ranked early-desk first", got[0].Desks)
	}
}

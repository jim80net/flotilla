package dash

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dash/tracker"
	"github.com/jim80net/flotilla/internal/roster"
)

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
	issues := []tracker.Issue{
		{Number: 10, Title: "active build", State: "OPEN", Desk: "backend"},
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
	if len(doc.Flotillas[0].Desks) != 2 {
		t.Fatalf("desks = %+v, want backend + Unassigned", doc.Flotillas[0].Desks)
	}
	var backend, unassigned *WorkLedgerDesk
	for i := range doc.Flotillas[0].Desks {
		d := &doc.Flotillas[0].Desks[i]
		switch d.Name {
		case "backend":
			backend = d
		case "Unassigned":
			unassigned = d
		}
	}
	if backend == nil || len(backend.InFlight) != 1 || len(backend.Shipped) != 1 {
		t.Fatalf("backend = %+v", backend)
	}
	if backend.InFlight[0].GoalTitle != "Ship the product" {
		t.Errorf("goal context = %+v", backend.InFlight[0])
	}
	if unassigned == nil || len(unassigned.Shipped) != 1 {
		t.Fatalf("unassigned = %+v", unassigned)
	}
}

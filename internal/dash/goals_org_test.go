package dash

import (
	"slices"
	"strings"
	"testing"
)

func TestOrgOwnerDiagnostics_Mismatch(t *testing.T) {
	// Parent goal owned by xo; child desk goal owned by backend whose org parent is alpha-xo.
	in := GoalsInputs{
		FileOK: true,
		File: GoalsFile{
			Version: 1,
			Goals: []Goal{
				{ID: "fleet", Title: "Fleet", Scope: ScopeFlotilla, Owner: "xo"},
				{ID: "desk-goal", Title: "Desk", Scope: ScopeOrgDesk, Parent: "fleet", Owner: "backend"},
			},
		},
		OrgParents: map[string]string{"backend": "alpha-xo"},
		OrgSource:  "derived",
	}
	doc := BuildGoals(in)
	if !doc.Found {
		t.Fatalf("found=false err=%s", doc.Error)
	}
	if doc.OrgSource != "derived" {
		t.Errorf("org_source=%q", doc.OrgSource)
	}
	if len(doc.OrgDiagnostics) == 0 {
		t.Fatal("expected org diagnostic for owner/parent mismatch")
	}
	joined := strings.Join(doc.OrgDiagnostics, " ")
	if !strings.Contains(joined, "backend") || !strings.Contains(joined, "alpha-xo") {
		t.Errorf("diagnostic=%q", joined)
	}
}

func TestOrgOwnerDiagnostics_Agree(t *testing.T) {
	in := GoalsInputs{
		FileOK: true,
		File: GoalsFile{
			Version: 1,
			Goals: []Goal{
				{ID: "fleet", Title: "Fleet", Scope: ScopeFlotilla, Owner: "xo"},
				{ID: "alpha", Title: "Alpha", Scope: ScopeFlotilla, Parent: "fleet", Owner: "alpha-xo"},
				{ID: "desk-goal", Title: "Desk", Scope: ScopeOrgDesk, Parent: "alpha", Owner: "backend"},
			},
		},
		OrgParents: map[string]string{
			"alpha-xo": "xo",
			"backend":  "alpha-xo",
		},
		OrgSource: "file",
	}
	doc := BuildGoals(in)
	if len(doc.OrgDiagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", doc.OrgDiagnostics)
	}
	if doc.OrgSource != "file" {
		t.Errorf("org_source=%q", doc.OrgSource)
	}
}

func TestOrgStrictGoals_Env(t *testing.T) {
	t.Setenv("FLOTILLA_ORG_STRICT_GOALS", "")
	if orgStrictGoals() {
		t.Error("empty should be false")
	}
	t.Setenv("FLOTILLA_ORG_STRICT_GOALS", "1")
	if !orgStrictGoals() {
		t.Error("1 should be true")
	}
	t.Setenv("FLOTILLA_ORG_STRICT_GOALS", "true")
	if !orgStrictGoals() {
		t.Error("true should be true")
	}
}

func TestMaterializeDesk_UsesOrgParentHub(t *testing.T) {
	// Authored hub owned by alpha-xo; channel lists backend under xo fleet-command.
	// Org parent of backend is alpha-xo → desk should attach under alpha hub, not meta.
	in := GoalsInputs{
		FileOK: true,
		File: GoalsFile{
			Version: 1,
			Goals: []Goal{
				{ID: "root", Title: "Root", Scope: ScopeFlotilla, Owner: "xo"},
				{ID: "alpha-hub", Title: "Alpha", Scope: ScopeFlotilla, Parent: "root", Owner: "alpha-xo"},
			},
		},
		MetaXO: "xo",
		Channels: []DeskChannel{
			{ChannelID: "C_CMD", XOAgent: "xo", Members: []string{"xo", "alpha-xo", "backend"}},
			{ChannelID: "C_A", XOAgent: "alpha-xo", Members: []string{"xo", "backend"}},
		},
		OrgParents: map[string]string{"backend": "alpha-xo"},
		OrgSource:  "derived",
	}
	doc := BuildGoals(in)
	var desk *RenderedGoal
	for i := range doc.Goals {
		if doc.Goals[i].Source == "roster" && doc.Goals[i].Owner == "backend" {
			desk = &doc.Goals[i]
			break
		}
	}
	if desk == nil {
		t.Fatal("expected materialized backend desk")
	}
	if desk.Parent != "alpha-hub" {
		t.Errorf("desk parent=%q want alpha-hub (org parent alpha-xo)", desk.Parent)
	}
}

func TestMaterializeDesks_NeverFallsBackToForeignRoot(t *testing.T) {
	in := GoalsInputs{FileOK: true, File: GoalsFile{Version: 1, Goals: []Goal{
		{ID: "office", Title: "Office", Scope: ScopeFlotilla, Owner: "office-xo"},
		{ID: "product", Title: "Product", Scope: ScopeFlotilla, Owner: "product-xo"},
	}}, MetaXO: "cos", Channels: []DeskChannel{
		{ChannelID: "office-rollup", XOAgent: "office-xo", Members: []string{"cos", "foreign-desk"}},
		{ChannelID: "product-home", XOAgent: "product-xo", Members: []string{"cos", "product-desk"}},
	}, OrgParents: map[string]string{
		"office-xo": "cos", "product-xo": "cos", "foreign-desk": "cos", "product-desk": "product-xo",
	}, OrgSource: "derived"}
	doc := BuildGoals(in)
	byOwner := map[string]RenderedGoal{}
	for _, g := range doc.Goals {
		byOwner[g.Owner] = g
	}
	if got := byOwner["foreign-desk"].Parent; got != "hub:cos" {
		t.Fatalf("foreign desk parent=%q want synthetic CoS hub", got)
	}
	if got := byOwner["product-desk"].Parent; got != "product" {
		t.Fatalf("product desk parent=%q want product", got)
	}
	if slices.Contains(byOwner["office-xo"].Children, byOwner["foreign-desk"].ID) {
		t.Fatalf("office swallowed foreign desk: %#v", byOwner["office-xo"].Children)
	}
	if len(doc.OrgDiagnostics) != 0 {
		t.Fatalf("org diagnostics=%v", doc.OrgDiagnostics)
	}
}

func TestMaterializeDesks_MissingProductHubBuildsOrgChain(t *testing.T) {
	in := GoalsInputs{FileOK: true, File: GoalsFile{Version: 1, Goals: []Goal{{ID: "office", Title: "Office", Scope: ScopeFlotilla, Owner: "office-xo"}}}, MetaXO: "cos",
		Channels:   []DeskChannel{{ChannelID: "rollup", XOAgent: "office-xo", Members: []string{"alpha-desk"}}},
		OrgParents: map[string]string{"alpha-desk": "alpha-xo", "alpha-xo": "cos"}, OrgSource: "derived"}
	doc := BuildGoals(in)
	byOwner := map[string]RenderedGoal{}
	for _, g := range doc.Goals {
		byOwner[g.Owner] = g
	}
	if byOwner["alpha-desk"].Parent != "hub:alpha-xo" || byOwner["alpha-xo"].Parent != "hub:cos" {
		t.Fatalf("chain desk=%#v xo=%#v cos=%#v", byOwner["alpha-desk"], byOwner["alpha-xo"], byOwner["cos"])
	}
}

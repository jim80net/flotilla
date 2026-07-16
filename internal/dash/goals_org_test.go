package dash

import (
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

func TestMaterializeDesks_ReusesLegacyAuthoredOwnerRoot(t *testing.T) {
	in := GoalsInputs{FileOK: true, File: GoalsFile{Version: 1, Goals: []Goal{
		{ID: "office-root", Title: "Office", Scope: ScopeProject, Owner: "office-xo"},
		{ID: "venture", Title: "Venture", Scope: ScopeProject, Parent: "office-root", Owner: "office-xo"},
		{ID: "trading", Title: "Trading", Scope: ScopeProject, Parent: "office-root", Owner: "office-xo"},
	}}, MetaXO: "cos", Channels: []DeskChannel{{ChannelID: "office", XOAgent: "office-xo", Members: []string{"cos", "office-desk", "foreign-desk"}}},
		OrgParents: map[string]string{"office-xo": "cos", "office-desk": "office-xo", "foreign-desk": "cos"}, OrgSource: "derived"}
	doc := BuildGoals(in)
	byOwner := map[string][]RenderedGoal{}
	byID := map[string]RenderedGoal{}
	for _, g := range doc.Goals {
		byOwner[g.Owner] = append(byOwner[g.Owner], g)
		byID[g.ID] = g
	}
	if _, ok := byID["hub:office-xo"]; ok {
		t.Fatal("duplicate synthetic office hub created")
	}
	if got := byID["office-root"].Parent; got != "hub:cos" {
		t.Fatalf("authored office parent=%q", got)
	}
	if got := byOwner["office-desk"][0].Parent; got != "office-root" {
		t.Fatalf("office desk parent=%q", got)
	}
	if got := byOwner["foreign-desk"][0].Parent; got != "hub:cos" {
		t.Fatalf("foreign desk parent=%q", got)
	}
	if byID["venture"].Parent != "office-root" || byID["trading"].Parent != "office-root" {
		t.Fatalf("same-owner subtree split venture=%q trading=%q", byID["venture"].Parent, byID["trading"].Parent)
	}
	if len(doc.OrgDiagnostics) != 0 {
		t.Fatalf("diagnostics=%v", doc.OrgDiagnostics)
	}
}

func TestMaterializeDesks_FollowsOrgHierarchyWithoutFirstRootFallback(t *testing.T) {
	in := GoalsInputs{
		FileOK: true,
		File: GoalsFile{Version: 1, Goals: []Goal{
			// Deliberately list the finance hub first: historical deskHubFor used
			// this first root as the fallback for every unrelated desk.
			{ID: "finance", Title: "Finance", Scope: ScopeFlotilla, Owner: "finance-xo"},
			{ID: "alpha", Title: "Alpha Product", Scope: ScopeFlotilla, Owner: "alpha-xo"},
		}},
		MetaXO: "coord",
		Channels: []DeskChannel{
			{ChannelID: "C_FIN", XOAgent: "finance-xo", Members: []string{"coord", "finance-desk", "alpha-desk", "beta-desk"}},
			{ChannelID: "C_ALPHA", XOAgent: "alpha-xo", Members: []string{"coord", "alpha-desk"}},
			{ChannelID: "C_BETA", XOAgent: "beta-xo", Members: []string{"coord", "beta-desk"}},
		},
		OrgParents: map[string]string{
			"finance-xo":   "coord",
			"alpha-xo":     "coord",
			"beta-xo":      "coord",
			"finance-desk": "finance-xo",
			"alpha-desk":   "alpha-xo",
			"beta-desk":    "beta-xo",
		},
		OrgSource: "derived",
	}
	doc := BuildGoals(in)
	byOwner := make(map[string]RenderedGoal)
	for _, g := range doc.Goals {
		byOwner[g.Owner] = g
	}

	if got := byOwner["coord"]; got.Parent != "" || got.Scope != "flotilla" || got.Source != "roster" {
		t.Fatalf("materialized coordinator hub = %+v, want roster root hub", got)
	}
	for owner, wantParent := range map[string]string{
		"finance-xo": "hub:coord", "alpha-xo": "hub:coord", "beta-xo": "hub:coord",
		"finance-desk": "finance", "alpha-desk": "alpha", "beta-desk": "hub:beta-xo",
	} {
		got, ok := byOwner[owner]
		if !ok {
			t.Errorf("missing owner %q", owner)
			continue
		}
		if got.Parent != wantParent {
			t.Errorf("%s parent=%q want %q", owner, got.Parent, wantParent)
		}
	}
	finance := byOwner["finance-xo"]
	for _, child := range finance.Children {
		if child == byOwner["alpha-desk"].ID || child == byOwner["beta-desk"].ID {
			t.Errorf("finance hub swallowed foreign desk %q; children=%v", child, finance.Children)
		}
	}
	if len(doc.OrgDiagnostics) != 0 {
		t.Errorf("aligned generic hierarchy produced diagnostics: %v", doc.OrgDiagnostics)
	}
	// Parent-first stream is the shared contract used by both the desktop map and
	// phone outline; every non-root must follow its parent.
	position := make(map[string]int)
	for i, g := range doc.Goals {
		position[g.ID] = i
	}
	for _, g := range doc.Goals {
		if g.Parent != "" && position[g.Parent] >= position[g.ID] {
			t.Errorf("parent %q must precede child %q; order=%v", g.Parent, g.ID, position)
		}
	}
}

func TestDeskHubFor_DoesNotBorrowFirstRoot(t *testing.T) {
	doc := GoalsDoc{Goals: []RenderedGoal{
		{ID: "finance", Owner: "finance-xo", Scope: "flotilla", Depth: 0},
		{ID: "alpha-task", Owner: "alpha-xo", Scope: "task", Depth: 1},
	}}
	if id, depth := deskHubFor(&doc, DeskChannel{ChannelID: "C_ALPHA", XOAgent: "alpha-xo"}); id != "" || depth != -1 {
		t.Fatalf("unrelated channel borrowed first root or owner task: id=%q depth=%d", id, depth)
	}
}

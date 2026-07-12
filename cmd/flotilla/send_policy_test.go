package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/org"
)

func genericSendDAG() *org.DAG {
	return &org.DAG{
		Root: "xo",
		Nodes: map[string]org.Node{
			"xo":             {ID: "xo", Kind: org.KindCoordinator},
			"alpha-xo":       {ID: "alpha-xo", Kind: org.KindCoordinator, ReportsTo: "xo"},
			"alpha-backend":  {ID: "alpha-backend", Kind: org.KindDesk, ReportsTo: "alpha-xo"},
			"alpha-frontend": {ID: "alpha-frontend", Kind: org.KindDesk, ReportsTo: "alpha-xo"},
			"beta-xo":        {ID: "beta-xo", Kind: org.KindCoordinator, ReportsTo: "xo"},
			"beta-data":      {ID: "beta-data", Kind: org.KindDesk, ReportsTo: "beta-xo"},
		},
		Parents: map[string][]string{
			"alpha-xo": {"xo"}, "alpha-backend": {"alpha-xo"}, "alpha-frontend": {"alpha-xo"},
			"beta-xo": {"xo"}, "beta-data": {"beta-xo"},
		},
		Children: map[string][]string{
			"xo": {"alpha-xo", "beta-xo"}, "alpha-xo": {"alpha-backend", "alpha-frontend"}, "beta-xo": {"beta-data"},
		},
	}
}

func TestAuthorizeSendOrgQuadrants(t *testing.T) {
	dag := genericSendDAG()
	tests := []struct {
		name, from, to string
		wantAllowed    bool
	}{
		{"XO to own desk", "alpha-xo", "alpha-backend", true},
		{"desk to own XO", "alpha-backend", "alpha-xo", true},
		{"coordinator to foreign desk", "alpha-xo", "beta-data", true},
		{"desk to own venture desk", "alpha-backend", "alpha-frontend", true},
		{"desk to foreign desk", "alpha-backend", "beta-data", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := authorizeSend(dag, tt.from, tt.to, false)
			if d.Allowed != tt.wantAllowed {
				t.Fatalf("allowed=%v, reason=%q", d.Allowed, d.Reason)
			}
		})
	}
}

func TestAuthorizeSendForeignDeskErrorNamesForbiddingOrgEdge(t *testing.T) {
	d := authorizeSend(genericSendDAG(), "alpha-backend", "beta-data", false)
	for _, want := range []string{"alpha-backend", "alpha-xo", "beta-data", "beta-xo", "--cross-venture"} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("reason %q missing %q", d.Reason, want)
		}
	}
}

func TestAuthorizeSendCrossVentureOverride(t *testing.T) {
	d := authorizeSend(genericSendDAG(), "alpha-backend", "beta-data", true)
	if !d.Allowed || !d.Audit {
		t.Fatalf("override decision = %+v, want allowed+audit", d)
	}
}

func TestAuthorizeSendDerivedDAGInfersRolesFromEdges(t *testing.T) {
	dag := genericSendDAG()
	for id, n := range dag.Nodes {
		n.Kind = org.KindUnknown
		dag.Nodes[id] = n
	}
	if d := authorizeSend(dag, "alpha-xo", "beta-data", false); !d.Allowed {
		t.Fatalf("edge-inferred coordinator blocked: %+v", d)
	}
	if d := authorizeSend(dag, "alpha-backend", "beta-data", false); d.Allowed {
		t.Fatalf("edge-inferred foreign desk allowed: %+v", d)
	}
}

func TestAuthorizeSendOperatorSentinelIsDirect(t *testing.T) {
	if d := authorizeSend(genericSendDAG(), "me", "beta-data", false); !d.Allowed {
		t.Fatalf("operator-direct send blocked: %+v", d)
	}
}

func TestAuthorizeSendUnknownSenderFailsClosed(t *testing.T) {
	d := authorizeSend(genericSendDAG(), "typo-desk", "beta-data", false)
	if d.Allowed || !strings.Contains(d.Reason, "absent from the compiled org DAG") {
		t.Fatalf("unknown sender decision = %+v", d)
	}
}

func TestAuthorizeSendNilDAGFailsClosed(t *testing.T) {
	if d := authorizeSend(nil, "alpha-backend", "beta-data", false); d.Allowed {
		t.Fatalf("nil DAG should block: %+v", d)
	}
}

func TestAuthorizeSendOwnerlessDeskNamesMissingEdge(t *testing.T) {
	dag := genericSendDAG()
	dag.Parents["beta-data"] = nil
	d := authorizeSend(dag, "alpha-backend", "beta-data", false)
	if d.Allowed || !strings.Contains(d.Reason, "cannot resolve owning coordinator edges") {
		t.Fatalf("ownerless decision = %+v", d)
	}
}

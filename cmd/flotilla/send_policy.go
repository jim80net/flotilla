package main

import (
	"fmt"

	"github.com/jim80net/flotilla/internal/org"
)

type sendDecision struct {
	Allowed bool
	Audit   bool
	Reason  string
}

// authorizeSend enforces the cross-venture tasking boundary from the compiled
// org DAG. Names are opaque identifiers: role and ownership come only from node
// kinds and leader edges. The literal "me" is the documented operator-direct
// sentinel; every other sender must exist in the DAG so a typo cannot bypass policy.
func authorizeSend(dag *org.DAG, from, to string, crossVenture bool) sendDecision {
	if dag == nil {
		return sendDecision{Reason: "send blocked: compiled org DAG is unavailable"}
	}
	fromNode, fromKnown := dag.Nodes[from]
	toNode, toKnown := dag.Nodes[to]
	if !toKnown {
		return sendDecision{Reason: fmt.Sprintf("send blocked: recipient %q is absent from the compiled org DAG", to)}
	}
	if !fromKnown {
		if from == "me" {
			return sendDecision{Allowed: true}
		}
		return sendDecision{Reason: fmt.Sprintf("send blocked: sender %q is absent from the compiled org DAG (operator-direct sends use --from me)", from)}
	}
	if dagCoordinator(dag, fromNode) || fromNode.Kind == org.KindAdjutant {
		return sendDecision{Allowed: true}
	}
	if !dagDesk(dag, fromNode) || !dagDesk(dag, toNode) {
		return sendDecision{Allowed: true}
	}
	fromXO := owningCoordinator(dag, from)
	toXO := owningCoordinator(dag, to)
	if fromXO == "" || toXO == "" {
		return sendDecision{Reason: fmt.Sprintf("send blocked: compiled org DAG cannot resolve owning coordinator edges for %q → %q (sender owner %q, recipient owner %q)", from, to, fromXO, toXO)}
	}
	if fromXO != "" && fromXO == toXO {
		return sendDecision{Allowed: true}
	}
	reason := fmt.Sprintf("send blocked: desk %q reports to %q, but desk %q reports to %q; that org edge forbids cross-venture desk tasking (use --cross-venture for an audited override)", from, fromXO, to, toXO)
	if crossVenture {
		return sendDecision{Allowed: true, Audit: true, Reason: reason}
	}
	return sendDecision{Reason: reason}
}

func dagCoordinator(dag *org.DAG, n org.Node) bool {
	return n.Kind == org.KindCoordinator || n.ID == dag.Root || len(dag.Children[n.ID]) > 0
}

func dagDesk(dag *org.DAG, n org.Node) bool {
	if n.Kind == org.KindDesk {
		return true
	}
	return n.Kind == org.KindUnknown && !dagCoordinator(dag, n)
}

func owningCoordinator(dag *org.DAG, agent string) string {
	seen := map[string]bool{}
	for p := dag.PrimaryParent(agent); p != "" && !seen[p]; p = dag.PrimaryParent(p) {
		seen[p] = true
		n, ok := dag.Nodes[p]
		if ok && dagCoordinator(dag, n) {
			return p
		}
	}
	return ""
}

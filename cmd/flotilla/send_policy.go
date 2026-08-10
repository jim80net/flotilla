package main

import (
	"fmt"

	"github.com/jim80net/flotilla/internal/org"
	"github.com/jim80net/flotilla/internal/roster"
)

type sendDecision struct {
	Allowed bool
	Audit   bool
	Reason  string
}

// authorizeSend enforces the cross-venture tasking boundary from the roster's
// compiled org truth. Names are opaque identifiers. Role classification uses the
// roster classifier, and ownership uses OwningXO — the inversion-aware accessor
// for both file DAGs and channel-derived synthesis DAGs (#652).
func authorizeSend(cfg *roster.Config, from, to string, crossVenture bool) sendDecision {
	if cfg == nil || cfg.Org() == nil {
		return sendDecision{Reason: "send blocked: compiled org DAG is unavailable"}
	}
	dag := cfg.Org()
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
	if fromNode.Kind == org.KindAdjutant {
		if adjutantLaneTarget(dag, from, to) {
			return sendDecision{Allowed: true}
		}
		reason := fmt.Sprintf("send blocked: adjutant %q may task only its owning coordinator %q or its own reporting subtree; recipient %q is outside that lane (use --cross-venture for an audited override)", from, dag.PrimaryParent(from), to)
		if crossVenture {
			return sendDecision{Allowed: true, Audit: true, Reason: reason}
		}
		return sendDecision{Reason: reason}
	}
	if rosterCoordinator(cfg, fromNode) {
		return sendDecision{Allowed: true}
	}
	if !rosterDesk(cfg, fromNode) || !rosterDesk(cfg, toNode) {
		return sendDecision{Allowed: true}
	}
	fromXO := cfg.OwningXO(from, dag.Root)
	toXO := cfg.OwningXO(to, dag.Root)
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

func adjutantLaneTarget(dag *org.DAG, adjutant, target string) bool {
	if dag == nil || adjutant == "" || target == "" {
		return false
	}
	if target == adjutant || target == dag.PrimaryParent(adjutant) {
		return true
	}
	seen := map[string]bool{adjutant: true}
	queue := append([]string(nil), dag.Children[adjutant]...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == target {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		queue = append(queue, dag.Children[current]...)
	}
	return false
}

func rosterDesk(cfg *roster.Config, n org.Node) bool {
	if n.Kind == org.KindDesk {
		return true
	}
	return n.Kind == org.KindUnknown && !rosterCoordinator(cfg, n)
}

func rosterCoordinator(cfg *roster.Config, n org.Node) bool {
	return n.Kind == org.KindCoordinator || cfg.IsCoordinator(n.ID)
}

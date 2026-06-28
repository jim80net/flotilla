package roster

import "fmt"

// Synthesis routing — the visibility-synthesis (B2) read/owed/post derivations over the
// federation membership graph, plus the load-time acyclicity assertion. These are pure
// read-only derivations over Bindings(); none mutates a binding's Members slice (the
// read-only-slice contract documented on Bindings).
//
// THE FLEET-COMMAND OVERLOAD (the implement-gate P0). `members[]` carries two opposite
// meanings. In a per-XO HOME channel, members is the agent's PARENT up-link (e.g.
// the data desk's channel lists [xo]). In the fleet-command BROADCAST channel
// it is the meta-XO's full command DOWN-list (every agent it can address). Read as a
// synthesis edge, the broadcast channel inverts the hierarchy — a leaf desk that is a
// member of it would treat the broadcaster (the meta-XO) as a subordinate, and the graph
// cycles. So a role=="fleet-command" channel contributes ZERO synthesis edges: it is
// excluded from AgentsBelow, AgentsAbove, and the DAG. It REMAINS a POST target
// (OwnedChannels includes it) — the meta-XO posts its Tier-3 synthesis INTO #c2.

// IsFleetCommand reports whether a channel is the fleet-command/broadcast channel, whose
// members are command targets rather than synthesis parents (see the package note above).
func (ch Channel) IsFleetCommand() bool { return ch.Role == "fleet-command" }

// OwnedChannels returns every channel id an agent is the XO of — the synthesis POST
// target. It generalizes ChannelForXO (which returns only the first/home channel) to the
// multi-hub case, and INCLUDES any fleet-command channel the agent owns (the meta-XO
// posts its Tier-3 synthesis into the fleet-command channel it owns). Order-stable
// (binding order); empty for an agent that owns no channel.
func (c *Config) OwnedChannels(agent string) []string {
	var out []string
	for _, ch := range c.Bindings() {
		if ch.XOAgent == agent {
			out = append(out, ch.ChannelID)
		}
	}
	return out
}

// AgentsBelow returns the tier BELOW an agent — its synthesis READ set. For every
// NON-fleet-command channel whose members list the agent, that channel's XO is a
// subordinate. Two exclusions, both load-bearing: the channel's own XO (`!= agent` — read
// strictly below, never your own channel: the self-loop guard) and fleet-command channels
// (a broadcast channel's members are command targets, not subordinates — without this a
// leaf desk would "synthesize" the meta-XO and the graph would cycle). De-duplicated and
// order-stable.
func (c *Config) AgentsBelow(agent string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() || ch.XOAgent == agent {
			continue
		}
		if !memberOf(ch.Members, agent) || seen[ch.XOAgent] {
			continue
		}
		seen[ch.XOAgent] = true
		out = append(out, ch.XOAgent)
	}
	return out
}

// AgentsAbove returns the synthesizing PARENTS of an agent — the agents OWED a synthesis
// when this agent finishes. It is the members (minus self) of the NON-fleet-command
// channels the agent OWNS, and is the EXACT relational inverse of AgentsBelow
// (C ∈ AgentsBelow(P) ⟺ P ∈ AgentsAbove(C)). A boat whose owned channel lists two parents
// marks BOTH owed; the root (whose only owned channel is fleet-command, or which owns no
// non-empty channel) has no parent. It replaces the wrong-typed BindingForChannel for the
// detector's owed-marking: the detector holds an agent NAME, not a channel id.
func (c *Config) AgentsAbove(agent string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() || ch.XOAgent != agent {
			continue
		}
		for _, m := range ch.Members {
			if m == agent || seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// assertSynthesisAcyclic returns an error if the synthesis-edge graph contains a cycle, so
// a federation that would form a synthesis feedback loop refuses to start (fail-closed,
// like every other roster invariant). Edges run ch.XOAgent → m for each member m of each
// binding, EXCLUDING (1) self-edges (m == ch.XOAgent — a home-channel self-membership is
// not a cycle) and (2) fleet-command channels (a broadcast channel contributes no
// synthesis edges). A genuine cycle is a mutual membership between two distinct
// non-fleet-command channels. Runs once at load, not on the synthesis hot path.
func (c *Config) assertSynthesisAcyclic() error {
	adj := map[string][]string{}
	for _, ch := range c.Bindings() {
		if ch.IsFleetCommand() {
			continue
		}
		for _, m := range ch.Members {
			if m != ch.XOAgent {
				adj[ch.XOAgent] = append(adj[ch.XOAgent], m)
			}
		}
	}
	// Iterative-free DFS with white/gray/black coloring; gray-on-gray is a back-edge
	// (cycle). Start nodes iterate Bindings() order for a deterministic error.
	const white, gray, black = 0, 1, 2
	color := map[string]int{}
	var onCycle string
	var visit func(string) bool
	visit = func(u string) bool {
		color[u] = gray
		for _, v := range adj[u] {
			switch color[v] {
			case gray:
				onCycle = v
				return true
			case white:
				if visit(v) {
					return true
				}
			}
		}
		color[u] = black
		return false
	}
	for _, ch := range c.Bindings() {
		if color[ch.XOAgent] == white && visit(ch.XOAgent) {
			return fmt.Errorf("synthesis routing would cycle: agent %q is reachable from itself through the channel-membership graph (a mutual membership between two distinct non-fleet-command channels)", onCycle)
		}
	}
	return nil
}

// OwningXO resolves the XO that OWNS a desk — the cap-escalation target for the recursive
// desk-heartbeat (#183 §8e: a wedged desk surfaces LOUDLY to its owning XO). It is topology-robust:
//
//  1. Federated home-channel shape — a desk that OWNS a (non-fleet-command) home channel naming its
//     parent: AgentsAbove(agent) resolves the parent (a leaf → its project-XO, a project-XO → the
//     meta-XO). The first parent is the owner.
//  2. Legacy star — a leaf owns no channel (AgentsAbove is EMPTY); the owner is the XO of the
//     (non-fleet-command) channel the desk is a MEMBER of.
//  3. Fallback — neither resolves (the root, or an unknown agent): the supplied primaryXO.
//
// The fleet-command broadcast channel is excluded from the membership scan (its members are command
// targets, not an ownership relation — the same load-bearing exclusion AgentsBelow/AgentsAbove make).
// Read-only over Bindings(); never mutates a Members slice.
func (c *Config) OwningXO(agent, primaryXO string) string {
	if parents := c.AgentsAbove(agent); len(parents) > 0 {
		return parents[0]
	}
	// The membership-scan fallback is for the LEGACY STAR ONLY (a leaf that owns no channel). In the
	// federated home-channel shape a PARENT's home channel lists its child-XOs as members, so scanning
	// memberships for an agent that DOES own a channel would invert the hierarchy (e.g. the meta is a
	// member of each project-XO's home channel → it would resolve to a project-XO). An agent with an
	// empty AgentsAbove that ALSO owns a channel is the federated ROOT → the primaryXO fallback.
	if len(c.OwnedChannels(agent)) == 0 {
		for _, ch := range c.Bindings() {
			if ch.IsFleetCommand() || ch.XOAgent == agent {
				continue
			}
			if memberOf(ch.Members, agent) {
				return ch.XOAgent
			}
		}
	}
	return primaryXO
}

// memberOf reports whether name is in members.
func memberOf(members []string, name string) bool {
	for _, m := range members {
		if m == name {
			return true
		}
	}
	return false
}
